package embedder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type NIMEmbedder struct {
	BaseURL string
	APIKey  string
	Model   string
	Client  *http.Client
}

func (e NIMEmbedder) Dimensions() int {
	return 1024
}

func (e NIMEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	return e.embed(ctx, texts, "passage")
}

func (e NIMEmbedder) EmbedQuery(ctx context.Context, texts []string) ([][]float32, error) {
	return e.embed(ctx, texts, "query")
}

func (e NIMEmbedder) embed(ctx context.Context, texts []string, inputType string) ([][]float32, error) {
	body := map[string]any{
		"model":           e.Model,
		"input":           texts,
		"input_type":      inputType,
		"modality":        "text",
		"encoding_format": "float",
		"truncate":        "END",
	}
	var response struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := e.post(ctx, strings.TrimRight(e.BaseURL, "/")+"/embeddings", body, &response); err != nil {
		return nil, err
	}
	vectors := make([][]float32, len(response.Data))
	for i := range response.Data {
		vectors[i] = response.Data[i].Embedding
	}
	if len(vectors) != len(texts) {
		return nil, fmt.Errorf("embedding count mismatch: got %d want %d", len(vectors), len(texts))
	}
	return vectors, nil
}

func (e NIMEmbedder) post(ctx context.Context, url string, body any, dest any) error {
	data, _ := json.Marshal(body)
	client := e.Client
	if client == nil {
		client = &http.Client{Timeout: 90 * time.Second}
	}
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+e.APIKey)
		res, err := client.Do(req)
		if err != nil {
			return err
		}
		if res.StatusCode >= 200 && res.StatusCode < 300 {
			defer res.Body.Close()
			return json.NewDecoder(res.Body).Decode(dest)
		}
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		res.Body.Close()
		lastErr = fmt.Errorf("NVIDIA embeddings status %s: %s", res.Status, strings.TrimSpace(string(body)))
		if res.StatusCode != http.StatusTooManyRequests {
			return lastErr
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(retryDelay(res.Header.Get("Retry-After"), attempt)):
		}
	}
	return lastErr
}

func retryDelay(retryAfter string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	return time.Duration(attempt+1) * 5 * time.Second
}

type OllamaEmbedder struct {
	Host   string
	Model  string
	Client *http.Client
}

func (o OllamaEmbedder) Dimensions() int {
	return 768
}

func (o OllamaEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	vectors := make([][]float32, 0, len(texts))
	for _, text := range texts {
		body := map[string]any{"model": o.Model, "prompt": text}
		var response struct {
			Embedding []float32 `json:"embedding"`
		}
		data, _ := json.Marshal(body)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(o.Host, "/")+"/api/embeddings", bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		client := o.Client
		if client == nil {
			client = &http.Client{Timeout: 90 * time.Second}
		}
		res, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			res.Body.Close()
			return nil, fmt.Errorf("ollama embeddings status %s", res.Status)
		}
		err = json.NewDecoder(res.Body).Decode(&response)
		res.Body.Close()
		if err != nil {
			return nil, err
		}
		vectors = append(vectors, response.Embedding)
	}
	return vectors, nil
}

type DeepInfraEmbedder struct {
	BaseURL     string
	APIKey      string
	Model       string
	DimensionsN int
	Client      *http.Client
}

func (e DeepInfraEmbedder) Dimensions() int {
	if e.DimensionsN > 0 {
		return e.DimensionsN
	}
	return 1024
}

func (e DeepInfraEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	return e.embed(ctx, texts)
}

func (e DeepInfraEmbedder) EmbedQuery(ctx context.Context, texts []string) ([][]float32, error) {
	queryTexts := make([]string, len(texts))
	for i, text := range texts {
		queryTexts[i] = "Represent this sentence for searching relevant passages: " + text
	}
	return e.embed(ctx, queryTexts)
}

func (e DeepInfraEmbedder) embed(ctx context.Context, texts []string) ([][]float32, error) {
	input := any(texts)
	if len(texts) == 1 {
		input = texts[0]
	}
	body := map[string]any{
		"model":           e.Model,
		"input":           input,
		"encoding_format": "float",
	}
	var response struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := e.post(ctx, strings.TrimRight(e.BaseURL, "/")+"/embeddings", body, &response); err != nil {
		return nil, err
	}
	vectors := make([][]float32, len(response.Data))
	for i := range response.Data {
		vectors[i] = response.Data[i].Embedding
	}
	if len(vectors) != len(texts) {
		return nil, fmt.Errorf("embedding count mismatch: got %d want %d", len(vectors), len(texts))
	}
	return vectors, nil
}

func (e DeepInfraEmbedder) post(ctx context.Context, url string, body any, dest any) error {
	data, _ := json.Marshal(body)
	client := e.Client
	if client == nil {
		client = &http.Client{Timeout: 90 * time.Second}
	}
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+e.APIKey)
		res, err := client.Do(req)
		if err != nil {
			return err
		}
		if res.StatusCode >= 200 && res.StatusCode < 300 {
			defer res.Body.Close()
			return json.NewDecoder(res.Body).Decode(dest)
		}
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		res.Body.Close()
		lastErr = fmt.Errorf("DeepInfra embeddings status %s: %s", res.Status, strings.TrimSpace(string(body)))
		if res.StatusCode != http.StatusTooManyRequests {
			return lastErr
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(retryDelay(res.Header.Get("Retry-After"), attempt)):
		}
	}
	return lastErr
}
