// Package proxy implements auto-recall injection: a reverse proxy in front
// of an LLM API that searches evermemo for memories relevant to the user's
// latest message and injects them as system context — so agents get memory
// without ever calling a tool.
package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Evermemoai/evermemo/internal/store"
)

// Searcher is the memory capability the proxy needs (satisfied by
// *store.Store and *client.Client).
type Searcher interface {
	Search(query, namespace string, limit int) ([]*store.Memory, error)
}

// Config configures the proxy.
type Config struct {
	Target    string // upstream LLM API base URL, e.g. https://api.openai.com
	Namespace string // memory namespace to search ("" = all)
	Limit     int    // max memories to inject (default 5)
}

// Handler returns a reverse proxy that injects relevant memories into
// OpenAI-style (/v1/chat/completions) and Anthropic-style (/v1/messages)
// chat requests, passing everything else through untouched.
func Handler(mem Searcher, cfg Config) (http.Handler, error) {
	target, err := url.Parse(strings.TrimRight(cfg.Target, "/"))
	if err != nil || target.Scheme == "" {
		return nil, fmt.Errorf("invalid target URL %q", cfg.Target)
	}
	if cfg.Limit <= 0 {
		cfg.Limit = 5
	}
	client := &http.Client{Timeout: 5 * time.Minute}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 32<<20))
		if err != nil {
			http.Error(w, "reading request: "+err.Error(), http.StatusBadRequest)
			return
		}

		if r.Method == http.MethodPost && isChatPath(r.URL.Path) {
			if injected, ok := inject(mem, cfg, r.URL.Path, body); ok {
				body = injected
			}
		}

		out, err := http.NewRequestWithContext(r.Context(), r.Method, target.String()+r.URL.RequestURI(), bytes.NewReader(body))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		out.Header = r.Header.Clone()
		out.Header.Del("Accept-Encoding") // avoid double compression handling
		out.Header.Set("Content-Length", fmt.Sprint(len(body)))
		out.Host = target.Host

		resp, err := client.Do(out)
		if err != nil {
			http.Error(w, "upstream: "+err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		for k, vv := range resp.Header {
			for _, v := range vv {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		// io.Copy streams SSE responses through unbuffered.
		if f, ok := w.(http.Flusher); ok {
			buf := make([]byte, 4096)
			for {
				n, rerr := resp.Body.Read(buf)
				if n > 0 {
					w.Write(buf[:n])
					f.Flush()
				}
				if rerr != nil {
					break
				}
			}
		} else {
			io.Copy(w, resp.Body)
		}
	}), nil
}

func isChatPath(p string) bool {
	return strings.HasSuffix(p, "/chat/completions") || strings.HasSuffix(p, "/v1/messages")
}

// inject searches memories for the last user message and adds them as system
// context. Returns the rewritten body and whether injection happened.
func inject(mem Searcher, cfg Config, path string, body []byte) ([]byte, bool) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, false
	}
	query := lastUserMessage(payload)
	if query == "" {
		return nil, false
	}
	results, err := mem.Search(query, cfg.Namespace, cfg.Limit)
	if err != nil || len(results) == 0 {
		return nil, false
	}

	var b strings.Builder
	b.WriteString("Relevant memories from your shared memory store (evermemo):\n")
	for i, m := range results {
		fmt.Fprintf(&b, "%d. %s", i+1, m.Content)
		if m.Agent != "" {
			fmt.Fprintf(&b, " (by: %s)", m.Agent)
		}
		b.WriteString("\n")
	}
	note := b.String()

	if strings.HasSuffix(path, "/v1/messages") {
		// Anthropic: top-level "system" (string or content-block array).
		switch sys := payload["system"].(type) {
		case string:
			payload["system"] = sys + "\n\n" + note
		case []any:
			payload["system"] = append(sys, map[string]any{"type": "text", "text": note})
		default:
			payload["system"] = note
		}
	} else {
		// OpenAI: prepend a system message.
		msgs, _ := payload["messages"].([]any)
		payload["messages"] = append([]any{map[string]any{"role": "system", "content": note}}, msgs...)
	}

	out, err := json.Marshal(payload)
	if err != nil {
		return nil, false
	}
	log.Printf("injected %d memories into %s", len(results), path)
	return out, true
}

// lastUserMessage extracts the text of the final user turn.
func lastUserMessage(payload map[string]any) string {
	msgs, _ := payload["messages"].([]any)
	for i := len(msgs) - 1; i >= 0; i-- {
		m, _ := msgs[i].(map[string]any)
		if m == nil || m["role"] != "user" {
			continue
		}
		switch c := m["content"].(type) {
		case string:
			return c
		case []any: // content blocks
			var parts []string
			for _, blk := range c {
				if b, _ := blk.(map[string]any); b != nil {
					if t, _ := b["text"].(string); t != "" {
						parts = append(parts, t)
					}
				}
			}
			return strings.Join(parts, " ")
		}
	}
	return ""
}
