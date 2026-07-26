package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"evermemo/internal/store"
)

func testServer(t *testing.T, cfg Config) (*httptest.Server, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	srv := httptest.NewServer(Handler(st, cfg))
	t.Cleanup(func() { srv.Close(); st.Close() })
	return srv, st
}

func request(t *testing.T, method, url, key, body string) (*http.Response, string) {
	t.Helper()
	var rd *bytes.Reader
	if body != "" {
		rd = bytes.NewReader([]byte(body))
	} else {
		rd = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, url, rd)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	buf.ReadFrom(resp.Body)
	return resp, buf.String()
}

func TestOpenAccessWhenNoKeys(t *testing.T) {
	srv, _ := testServer(t, Config{})
	resp, _ := request(t, "POST", srv.URL+"/v1/memories", "", `{"content":"open"}`)
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("status = %d, want 201", resp.StatusCode)
	}
}

func TestSharedKeyAuth(t *testing.T) {
	srv, _ := testServer(t, Config{Auth: Auth{SharedKey: "s3cret"}})

	resp, _ := request(t, "GET", srv.URL+"/v1/memories", "", "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no key: status = %d, want 401", resp.StatusCode)
	}
	resp, _ = request(t, "GET", srv.URL+"/v1/memories", "wrong", "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong key: status = %d, want 401", resp.StatusCode)
	}
	resp, _ = request(t, "GET", srv.URL+"/v1/memories", "s3cret", "")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("good key: status = %d, want 200", resp.StatusCode)
	}
	// /health is never protected.
	resp, _ = request(t, "GET", srv.URL+"/health", "", "")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("health: status = %d, want 200", resp.StatusCode)
	}
}

func TestPerAgentProvenance(t *testing.T) {
	srv, _ := testServer(t, Config{Auth: Auth{AgentKeys: map[string]string{"claude": "ck"}}})

	resp, body := request(t, "POST", srv.URL+"/v1/memories", "ck", `{"content":"a fact"}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	var m store.Memory
	json.Unmarshal([]byte(body), &m)
	if m.Agent != "claude" {
		t.Errorf("agent = %q, want claude", m.Agent)
	}
}

func TestUpdateEndpoint(t *testing.T) {
	srv, st := testServer(t, Config{})
	m, _ := st.Add(store.AddRequest{Content: "before"})

	resp, body := request(t, "PUT", srv.URL+"/v1/memories/"+m.ID, "", `{"content":"after"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	var upd store.Memory
	json.Unmarshal([]byte(body), &upd)
	if upd.Content != "after" {
		t.Errorf("content = %q, want after", upd.Content)
	}
	resp, _ = request(t, "PUT", srv.URL+"/v1/memories/mem_missing", "", `{"content":"x"}`)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("missing id: status = %d, want 404", resp.StatusCode)
	}
}

func TestTTLViaAPI(t *testing.T) {
	srv, st := testServer(t, Config{})
	resp, body := request(t, "POST", srv.URL+"/v1/memories", "", `{"content":"short-lived","ttl":"1h"}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	var m store.Memory
	json.Unmarshal([]byte(body), &m)
	if m.ExpiresAt == nil {
		t.Error("expires_at not set")
	}
	got, _ := st.Get(m.ID)
	if got.ExpiresAt == nil {
		t.Error("stored memory missing expiry")
	}
	resp, _ = request(t, "POST", srv.URL+"/v1/memories", "", `{"content":"x","ttl":"banana"}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("bad ttl: status = %d, want 400", resp.StatusCode)
	}
}

func TestRateLimit(t *testing.T) {
	srv, _ := testServer(t, Config{RatePerMin: 2})

	var last int
	for i := 0; i < 3; i++ {
		resp, _ := request(t, "GET", srv.URL+"/v1/memories", "", "")
		last = resp.StatusCode
	}
	if last != http.StatusTooManyRequests {
		t.Errorf("third request: status = %d, want 429", last)
	}
	// /health is not rate limited.
	resp, _ := request(t, "GET", srv.URL+"/health", "", "")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("health: status = %d, want 200", resp.StatusCode)
	}
}

func TestMCPOverHTTP(t *testing.T) {
	srv, _ := testServer(t, Config{Auth: Auth{AgentKeys: map[string]string{"bot": "bk"}}, Version: "test"})

	// initialize
	resp, body := request(t, "POST", srv.URL+"/mcp", "bk",
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "evermemo") {
		t.Fatalf("initialize: status = %d, body = %s", resp.StatusCode, body)
	}

	// add via MCP tool, check provenance flows from the bearer key
	resp, body = request(t, "POST", srv.URL+"/mcp", "bk",
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"add_memory","arguments":{"content":"mcp http works"}}}`)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "Stored memory") {
		t.Fatalf("add: status = %d, body = %s", resp.StatusCode, body)
	}
	resp, body = request(t, "POST", srv.URL+"/mcp", "bk",
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"search_memory","arguments":{"query":"mcp http"}}}`)
	if !strings.Contains(body, "mcp http works") || !strings.Contains(body, "(by: bot)") {
		t.Errorf("search: body = %s", body)
	}

	// unauthenticated
	resp, _ = request(t, "POST", srv.URL+"/mcp", "",
		`{"jsonrpc":"2.0","id":4,"method":"ping"}`)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no key: status = %d, want 401", resp.StatusCode)
	}

	// notification (no id) → 202
	resp, _ = request(t, "POST", srv.URL+"/mcp", "bk",
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("notification: status = %d, want 202", resp.StatusCode)
	}
}

func TestParseAgentKeys(t *testing.T) {
	got := ParseAgentKeys(" alice:k1, bob:k2 ,broken,also: ,:x ")
	want := fmt.Sprintf("%v", map[string]string{"alice": "k1", "bob": "k2"})
	if fmt.Sprintf("%v", got) != want {
		t.Errorf("got %v", got)
	}
}
