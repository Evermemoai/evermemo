// Package embed provides text-embedding clients for semantic search.
// Supports Ollama (local, free) and any OpenAI-compatible /v1/embeddings API.
package embed

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Client calls an embedding provider over HTTP.
type Client struct {
	url      string // base URL, e.g. http://localhost:11434
	model    string
	apiKey   string
	provider string // "ollama" or "openai"
	http     *http.Client
}

// FromEnv builds a Client from EVERMEMO_EMBED_* environment variables.
// Returns nil (semantic search disabled) when EVERMEMO_EMBED_URL is unset.
//
//	EVERMEMO_EMBED_URL       e.g. http://localhost:11434 (Ollama) or https://api.openai.com
//	EVERMEMO_EMBED_MODEL     e.g. nomic-embed-text or text-embedding-3-small
//	EVERMEMO_EMBED_API_KEY   bearer key for OpenAI-compatible providers
//	EVERMEMO_EMBED_PROVIDER  "ollama" (default) or "openai"
func FromEnv() *Client {
	url := strings.TrimSpace(os.Getenv("EVERMEMO_EMBED_URL"))
	if url == "" {
		return nil
	}
	provider := strings.ToLower(strings.TrimSpace(os.Getenv("EVERMEMO_EMBED_PROVIDER")))
	if provider == "" {
		if os.Getenv("EVERMEMO_EMBED_API_KEY") != "" {
			provider = "openai"
		} else {
			provider = "ollama"
		}
	}
	model := os.Getenv("EVERMEMO_EMBED_MODEL")
	if model == "" {
		if provider == "ollama" {
			model = "nomic-embed-text"
		} else {
			model = "text-embedding-3-small"
		}
	}
	return &Client{
		url:      strings.TrimRight(url, "/"),
		model:    model,
		apiKey:   os.Getenv("EVERMEMO_EMBED_API_KEY"),
		provider: provider,
		http:     &http.Client{Timeout: 30 * time.Second},
	}
}

// Embed returns the embedding vector for text.
func (c *Client) Embed(text string) ([]float32, error) {
	var (
		path string
		body any
	)
	if c.provider == "ollama" {
		path = "/api/embeddings"
		body = map[string]string{"model": c.model, "prompt": text}
	} else {
		path = "/v1/embeddings"
		body = map[string]any{"model": c.model, "input": text}
	}
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("POST", c.url+path, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding provider: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("embedding provider: HTTP %d: %s", resp.StatusCode, truncate(string(data), 200))
	}
	if c.provider == "ollama" {
		var out struct {
			Embedding []float32 `json:"embedding"`
		}
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, err
		}
		return out.Embedding, nil
	}
	var out struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	if len(out.Data) == 0 {
		return nil, fmt.Errorf("embedding provider returned no data")
	}
	return out.Data[0].Embedding, nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
