// Package llm provides a minimal chat-completion client for consolidation
// jobs. Supports Ollama (local, free) and any OpenAI-compatible API.
package llm

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

// Client calls a chat-completion provider over HTTP.
type Client struct {
	url      string
	model    string
	apiKey   string
	provider string // "ollama" or "openai"
	http     *http.Client
}

// FromEnv builds a Client from EVERMEMO_LLM_* environment variables.
// Returns nil when EVERMEMO_LLM_URL is unset.
//
//	EVERMEMO_LLM_URL       e.g. http://localhost:11434 (Ollama) or https://api.openai.com
//	EVERMEMO_LLM_MODEL     e.g. llama3.2 or gpt-4o-mini
//	EVERMEMO_LLM_API_KEY   bearer key for OpenAI-compatible providers
//	EVERMEMO_LLM_PROVIDER  "ollama" (default) or "openai"
func FromEnv() *Client {
	url := strings.TrimSpace(os.Getenv("EVERMEMO_LLM_URL"))
	if url == "" {
		return nil
	}
	provider := strings.ToLower(strings.TrimSpace(os.Getenv("EVERMEMO_LLM_PROVIDER")))
	if provider == "" {
		if os.Getenv("EVERMEMO_LLM_API_KEY") != "" {
			provider = "openai"
		} else {
			provider = "ollama"
		}
	}
	model := os.Getenv("EVERMEMO_LLM_MODEL")
	if model == "" {
		if provider == "ollama" {
			model = "llama3.2"
		} else {
			model = "gpt-4o-mini"
		}
	}
	return &Client{
		url:      strings.TrimRight(url, "/"),
		model:    model,
		apiKey:   os.Getenv("EVERMEMO_LLM_API_KEY"),
		provider: provider,
		http:     &http.Client{Timeout: 120 * time.Second},
	}
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Chat sends a system + user prompt and returns the assistant's reply text.
func (c *Client) Chat(system, user string) (string, error) {
	msgs := []message{{Role: "system", Content: system}, {Role: "user", Content: user}}

	var path string
	var body any
	if c.provider == "ollama" {
		path = "/api/chat"
		body = map[string]any{"model": c.model, "messages": msgs, "stream": false}
	} else {
		path = "/v1/chat/completions"
		body = map[string]any{"model": c.model, "messages": msgs}
	}
	b, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequest("POST", c.url+path, bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("llm provider: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("llm provider: HTTP %d: %s", resp.StatusCode, truncate(string(data), 300))
	}
	if c.provider == "ollama" {
		var out struct {
			Message message `json:"message"`
		}
		if err := json.Unmarshal(data, &out); err != nil {
			return "", err
		}
		return out.Message.Content, nil
	}
	var out struct {
		Choices []struct {
			Message message `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", err
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("llm provider returned no choices")
	}
	return out.Choices[0].Message.Content, nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
