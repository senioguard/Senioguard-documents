package embedder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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
	body := map[string]any{"model": e.Model, "input": texts}
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.APIKey)
	client := e.Client
	if client == nil {
		client = &http.Client{Timeout: 90 * time.Second}
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("NVIDIA embeddings status %s", res.Status)
	}
	return json.NewDecoder(res.Body).Decode(dest)
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
