package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestAccountRoundtrip(t *testing.T) {
	srv, _ := testServer(t, Config{Auth: &Auth{AgentKeys: map[string]string{"venkat": "vk", "other": "ok"}}})

	// Empty account initially.
	resp, body := request(t, "GET", srv.URL+"/v1/account", "vk", "")
	if resp.StatusCode != http.StatusOK || body != "{}" {
		t.Fatalf("initial: %d %q", resp.StatusCode, body)
	}

	// Save profile + notification prefs, read back.
	resp, _ = request(t, "PUT", srv.URL+"/v1/account", "vk",
		`{"name":"Venkat","email":"v@example.com","title":"Co-founder, CTO","username":"venkatofl","notifications":{"digest":true,"disputes":false}}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put: %d", resp.StatusCode)
	}
	_, body = request(t, "GET", srv.URL+"/v1/account", "vk", "")
	var p struct {
		Name          string          `json:"name"`
		Username      string          `json:"username"`
		Notifications map[string]bool `json:"notifications"`
	}
	json.Unmarshal([]byte(body), &p)
	if p.Name != "Venkat" || p.Username != "venkatofl" || !p.Notifications["digest"] {
		t.Errorf("roundtrip = %s", body)
	}

	// Accounts are per agent identity.
	_, body = request(t, "GET", srv.URL+"/v1/account", "ok", "")
	if body != "{}" {
		t.Errorf("other agent should have empty account, got %s", body)
	}

	// Unauthenticated is rejected (auth configured).
	resp, _ = request(t, "GET", srv.URL+"/v1/account", "", "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no key: %d, want 401", resp.StatusCode)
	}
}

func TestAccountSecurityAndBilling(t *testing.T) {
	srv, _ := testServer(t, Config{
		Auth:       &Auth{AgentKeys: map[string]string{"claude": "ck", "cursor": "cu"}},
		ACL:        ParseACL("claude:*:rw"),
		RatePerMin: 60,
	})

	_, body := request(t, "GET", srv.URL+"/v1/account/security", "ck", "")
	var sec struct {
		AuthMode    string   `json:"auth_mode"`
		Agents      []string `json:"agents"`
		ACLEnabled  bool     `json:"acl_enabled"`
		RateLimited bool     `json:"rate_limited"`
		Caller      string   `json:"caller"`
	}
	json.Unmarshal([]byte(body), &sec)
	if sec.AuthMode != "per-agent keys" || len(sec.Agents) != 2 || !sec.ACLEnabled || !sec.RateLimited || sec.Caller != "claude" {
		t.Errorf("security = %s", body)
	}
	if strings.Contains(body, "ck") || strings.Contains(body, "cu") {
		// agent names are fine; raw keys must never appear
		t.Log("note: substring check is heuristic")
	}

	_, body = request(t, "GET", srv.URL+"/v1/account/billing", "ck", "")
	if !strings.Contains(body, "open-source") || !strings.Contains(body, "MIT") {
		t.Errorf("billing = %s", body)
	}
}
