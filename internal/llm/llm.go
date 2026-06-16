package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type NIMClient struct {
	BaseURL string
	APIKey  string
	Model   string
	Client  *http.Client
}

func (c NIMClient) Complete(ctx context.Context, system, user string) (string, error) {
	body := map[string]any{
		"model": c.Model,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
		"temperature": 0.2,
	}
	var response struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error any `json:"error,omitempty"`
	}
	if err := c.post(ctx, strings.TrimRight(c.BaseURL, "/")+"/chat/completions", body, &response); err != nil {
		return "", err
	}
	if len(response.Choices) == 0 {
		return "", fmt.Errorf("NVIDIA NIM returned no choices")
	}
	return strings.TrimSpace(response.Choices[0].Message.Content), nil
}

func (c NIMClient) post(ctx context.Context, url string, body any, dest any) error {
	data, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: 90 * time.Second}
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("NVIDIA NIM status %s", res.Status)
	}
	return json.NewDecoder(res.Body).Decode(dest)
}

type OllamaLLM struct {
	Host   string
	Model  string
	Client *http.Client
}

func (o OllamaLLM) Complete(ctx context.Context, system, user string) (string, error) {
	body := map[string]any{
		"model":  o.Model,
		"stream": false,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
	}
	var response struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	data, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(o.Host, "/")+"/api/chat", bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	client := o.Client
	if client == nil {
		client = &http.Client{Timeout: 90 * time.Second}
	}
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", fmt.Errorf("ollama status %s", res.Status)
	}
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		return "", err
	}
	return strings.TrimSpace(response.Message.Content), nil
}
