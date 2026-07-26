package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Evermemoai/evermemo/internal/store"
)

type fakeSearcher struct{ hits []*store.Memory }

func (f *fakeSearcher) Search(q, ns string, limit int) ([]*store.Memory, error) {
	return f.hits, nil
}

func TestInjectOpenAI(t *testing.T) {
	// Upstream echoes the body it received.
	var received []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received, _ = io.ReadAll(r.Body)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	mem := &fakeSearcher{hits: []*store.Memory{{Content: "user prefers dark mode", Agent: "claude"}}}
	h, err := Handler(mem, Config{Target: upstream.URL})
	if err != nil {
		t.Fatal(err)
	}
	p := httptest.NewServer(h)
	defer p.Close()

	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"what theme should I use?"}]}`
	resp, err := http.Post(p.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	var payload struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(received, &payload); err != nil {
		t.Fatalf("upstream body: %v", err)
	}
	if len(payload.Messages) != 2 || payload.Messages[0].Role != "system" ||
		!strings.Contains(payload.Messages[0].Content, "dark mode") {
		t.Errorf("injected payload = %s", received)
	}
}

func TestInjectAnthropic(t *testing.T) {
	var received []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received, _ = io.ReadAll(r.Body)
		w.Write([]byte(`{}`))
	}))
	defer upstream.Close()

	mem := &fakeSearcher{hits: []*store.Memory{{Content: "invoices are net-30"}}}
	h, _ := Handler(mem, Config{Target: upstream.URL})
	p := httptest.NewServer(h)
	defer p.Close()

	body := `{"model":"claude","system":"You are helpful.","messages":[{"role":"user","content":[{"type":"text","text":"invoice terms?"}]}]}`
	resp, _ := http.Post(p.URL+"/v1/messages", "application/json", strings.NewReader(body))
	resp.Body.Close()

	if !strings.Contains(string(received), "net-30") || !strings.Contains(string(received), "You are helpful.") {
		t.Errorf("system not augmented: %s", received)
	}
}

func TestPassthroughNonChat(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"models":[]}`))
	}))
	defer upstream.Close()

	h, _ := Handler(&fakeSearcher{}, Config{Target: upstream.URL})
	p := httptest.NewServer(h)
	defer p.Close()

	resp, err := http.Get(p.URL + "/v1/models")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("passthrough failed: %v %v", err, resp)
	}
	resp.Body.Close()
}
