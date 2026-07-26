// Package client implements an HTTP client for a remote evermemo server,
// letting local interfaces (MCP, CLI) operate on a shared central memory hub.
package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"evermemo/internal/store"
)

// Client talks to a remote evermemo HTTP API.
type Client struct {
	base   string // e.g. "https://memory.internal:7777"
	apiKey string
	agent  string // optional identity sent as X-Agent when no per-agent key is used
	http   *http.Client
}

// New creates a client for the evermemo server at baseURL.
func New(baseURL, apiKey, agent string) *Client {
	return &Client{
		base:   strings.TrimRight(baseURL, "/"),
		apiKey: apiKey,
		agent:  agent,
		http:   &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *Client) do(method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+path, rd)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	if c.agent != "" {
		req.Header.Set("X-Agent", c.agent)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("reaching memory hub: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &e) == nil && e.Error != "" {
			return fmt.Errorf("hub: %s", e.Error)
		}
		return fmt.Errorf("hub: HTTP %d", resp.StatusCode)
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

// Add stores a memory on the hub. The agent identity comes from the client's
// credentials (per-agent key or X-Agent header), so the parameter is unused
// beyond satisfying the shared backend shape.
func (c *Client) Add(content string, tags []string, namespace string, metadata map[string]any, _ string) (*store.Memory, error) {
	var m store.Memory
	err := c.do("POST", "/v1/memories", map[string]any{
		"content": content, "tags": tags, "namespace": namespace, "metadata": metadata,
	}, &m)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (c *Client) list(q url.Values) ([]*store.Memory, error) {
	var out struct {
		Memories []*store.Memory `json:"memories"`
	}
	if err := c.do("GET", "/v1/memories?"+q.Encode(), nil, &out); err != nil {
		return nil, err
	}
	return out.Memories, nil
}

// Search runs a full-text search on the hub.
func (c *Client) Search(query, namespace string, limit int) ([]*store.Memory, error) {
	q := url.Values{"q": {query}}
	if namespace != "" {
		q.Set("namespace", namespace)
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	return c.list(q)
}

// List returns recent memories from the hub.
func (c *Client) List(namespace string, limit int) ([]*store.Memory, error) {
	q := url.Values{}
	if namespace != "" {
		q.Set("namespace", namespace)
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	return c.list(q)
}

// Delete removes a memory on the hub.
func (c *Client) Delete(id string) error {
	return c.do("DELETE", "/v1/memories/"+url.PathEscape(id), nil, nil)
}
