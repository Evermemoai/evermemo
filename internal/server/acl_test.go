package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Evermemoai/evermemo/internal/store"
)

func TestParseACL(t *testing.T) {
	acl := ParseACL("claude:eng:rw, cursor:eng:r, auditor:*:r, bad, x:y:z")
	if acl == nil || len(acl.rules) != 3 {
		t.Fatalf("rules = %+v", acl)
	}
	if ParseACL("") != nil {
		t.Error("empty ACL should be nil")
	}

	cases := []struct {
		agent, ns string
		write     bool
		want      bool
	}{
		{"claude", "eng", true, true},
		{"claude", "eng", false, true},
		{"cursor", "eng", false, true},
		{"cursor", "eng", true, false}, // read-only
		{"cursor", "hr", false, false}, // no grant
		{"auditor", "hr", false, true}, // wildcard ns read
		{"auditor", "*", false, true},  // cross-namespace read
		{"auditor", "hr", true, false}, // wildcard is read-only
		{"claude", "*", false, false},  // no wildcard grant
		{"stranger", "eng", false, false},
	}
	for _, c := range cases {
		if got := acl.Allow(c.agent, c.ns, c.write); got != c.want {
			t.Errorf("Allow(%q,%q,write=%v) = %v, want %v", c.agent, c.ns, c.write, got, c.want)
		}
	}
	// nil ACL allows everything.
	var none *ACL
	if !none.Allow("anyone", "anything", true) {
		t.Error("nil ACL should allow all")
	}
}

func TestACLEnforcementHTTP(t *testing.T) {
	cfg := Config{
		Auth: &Auth{AgentKeys: map[string]string{"finbot": "fk", "hrbot": "hk"}},
		ACL:  ParseACL("finbot:finance:rw,hrbot:hr:rw,hrbot:finance:r"),
	}
	srv, st := testServer(t, cfg)

	// finbot writes finance: allowed.
	resp, body := request(t, "POST", srv.URL+"/v1/memories", "fk", `{"content":"Q3 budget approved","namespace":"finance"}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("finbot write: %d %s", resp.StatusCode, body)
	}
	var m store.Memory
	json.Unmarshal([]byte(body), &m)

	// hrbot writes finance: denied (read-only there).
	resp, _ = request(t, "POST", srv.URL+"/v1/memories", "hk", `{"content":"sneaky","namespace":"finance"}`)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("hrbot write finance: %d, want 403", resp.StatusCode)
	}

	// hrbot reads finance: allowed.
	resp, _ = request(t, "GET", srv.URL+"/v1/memories?namespace=finance", "hk", "")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("hrbot read finance: %d, want 200", resp.StatusCode)
	}

	// finbot reads hr: denied.
	resp, _ = request(t, "GET", srv.URL+"/v1/memories?namespace=hr", "fk", "")
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("finbot read hr: %d, want 403", resp.StatusCode)
	}

	// Cross-namespace list without wildcard: denied.
	resp, _ = request(t, "GET", srv.URL+"/v1/memories", "fk", "")
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("finbot cross-ns list: %d, want 403", resp.StatusCode)
	}

	// hrbot deletes finbot's finance memory: denied.
	resp, _ = request(t, "DELETE", srv.URL+"/v1/memories/"+m.ID, "hk", "")
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("hrbot delete finance: %d, want 403", resp.StatusCode)
	}
	if _, err := st.Get(m.ID); err != nil {
		t.Error("memory should survive denied delete")
	}
}

func TestACLEnforcementMCP(t *testing.T) {
	cfg := Config{
		Auth:    &Auth{AgentKeys: map[string]string{"finbot": "fk", "hrbot": "hk"}},
		ACL:     ParseACL("finbot:finance:rw,hrbot:hr:rw"),
		Version: "test",
	}
	srv, _ := testServer(t, cfg)

	// hrbot tries to add to finance via MCP: denied.
	_, body := request(t, "POST", srv.URL+"/mcp", "hk",
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"add_memory","arguments":{"content":"x","namespace":"finance"}}}`)
	if !strings.Contains(body, "not allowed") {
		t.Errorf("mcp add should be denied: %s", body)
	}

	// finbot adds to finance via MCP: allowed.
	_, body = request(t, "POST", srv.URL+"/mcp", "fk",
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"add_memory","arguments":{"content":"budget","namespace":"finance"}}}`)
	if !strings.Contains(body, "Stored memory") {
		t.Errorf("mcp add should succeed: %s", body)
	}
}

func TestLinkAndVerifyEndpoints(t *testing.T) {
	srv, st := testServer(t, Config{Auth: &Auth{AgentKeys: map[string]string{"bot": "bk", "bot2": "b2"}}})
	a, _ := st.Add(store.AddRequest{Content: "old"})
	b, _ := st.Add(store.AddRequest{Content: "new"})

	// Link.
	resp, body := request(t, "POST", srv.URL+"/v1/memories/"+b.ID+"/links", "bk",
		`{"rel":"supersedes","to":"`+a.ID+`"}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("link: %d %s", resp.StatusCode, body)
	}
	resp, _ = request(t, "POST", srv.URL+"/v1/memories/"+b.ID+"/links", "bk", `{"rel":"nonsense","to":"`+a.ID+`"}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("bad rel: %d, want 400", resp.StatusCode)
	}

	// GET returns links.
	_, body = request(t, "GET", srv.URL+"/v1/memories/"+a.ID, "bk", "")
	if !strings.Contains(body, "supersedes") {
		t.Errorf("get missing links: %s", body)
	}

	// Verify: confirm then dispute by different agents.
	resp, body = request(t, "POST", srv.URL+"/v1/memories/"+a.ID+"/verify", "bk", `{"vote":"confirm"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("verify: %d %s", resp.StatusCode, body)
	}
	var m store.Memory
	json.Unmarshal([]byte(body), &m)
	if m.Confidence != 0.7 {
		t.Errorf("confidence = %v, want 0.7", m.Confidence)
	}
	_, body = request(t, "POST", srv.URL+"/v1/memories/"+a.ID+"/verify", "b2", `{"vote":"dispute","note":"outdated"}`)
	json.Unmarshal([]byte(body), &m)
	if m.Confidence != 0.55 {
		t.Errorf("confidence = %v, want 0.55", m.Confidence)
	}

	resp, _ = request(t, "POST", srv.URL+"/v1/memories/"+a.ID+"/verify", "bk", `{"vote":"maybe"}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("bad vote: %d, want 400", resp.StatusCode)
	}
}
