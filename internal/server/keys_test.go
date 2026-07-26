package server

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestKeysFileRotation(t *testing.T) {
	keysPath := filepath.Join(t.TempDir(), "keys.txt")
	write := func(content string) {
		if err := os.WriteFile(keysPath, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		// Ensure mtime advances on filesystems with coarse resolution.
		future := time.Now().Add(2 * time.Second)
		os.Chtimes(keysPath, future, future)
	}
	write("# initial keys\nalice:ka1\nbob:kb1\n")

	srv, _ := testServer(t, Config{Auth: &Auth{KeysFile: keysPath}})

	resp, _ := request(t, "GET", srv.URL+"/v1/memories", "ka1", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("alice key: %d, want 200", resp.StatusCode)
	}
	resp, _ = request(t, "GET", srv.URL+"/v1/memories", "ka2", "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unknown key: %d, want 401", resp.StatusCode)
	}

	// Rotate alice's key and revoke bob — no restart.
	write("alice:ka2\n")

	resp, _ = request(t, "GET", srv.URL+"/v1/memories", "ka2", "")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("rotated key: %d, want 200", resp.StatusCode)
	}
	resp, _ = request(t, "GET", srv.URL+"/v1/memories", "ka1", "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("old key still works: %d, want 401", resp.StatusCode)
	}
	resp, _ = request(t, "GET", srv.URL+"/v1/memories", "kb1", "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("revoked key still works: %d, want 401", resp.StatusCode)
	}
}

func TestParseKeysFile(t *testing.T) {
	got := parseKeysFile("# comment\n\n alice : ka \nbroken\nbob:kb\n")
	if len(got) != 2 || got["alice"] != "ka" || got["bob"] != "kb" {
		t.Errorf("got %v", got)
	}
}
