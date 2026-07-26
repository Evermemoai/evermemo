// Package server implements evermemo's HTTP API and the streamable-HTTP MCP
// transport, with per-agent authentication, provenance, and rate limiting.
package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Evermemoai/evermemo/internal/mcp"
	"github.com/Evermemoai/evermemo/internal/store"
)

type ctxKey int

const agentKey ctxKey = 0

// Auth describes how the API authenticates callers.
//
//   - SharedKey: one bearer key for everyone (legacy, $EVERMEMO_API_KEY).
//   - AgentKeys: per-agent identities, "name -> key" ($EVERMEMO_AGENT_KEYS,
//     format "alice:key1,bob:key2"). The authenticated agent name is recorded
//     as provenance on every memory it writes.
//   - KeysFile: path to a file of per-agent keys (one "agent:key" per line,
//     # comments allowed). The file is re-read whenever its mtime changes,
//     so keys can be added, rotated, or revoked without restarting the hub.
//     File entries override AgentKeys on name collision.
//
// If all are empty the API is open (agent name may still be supplied via
// the X-Agent header).
type Auth struct {
	SharedKey string
	AgentKeys map[string]string
	KeysFile  string

	mu       sync.Mutex
	fileMod  time.Time
	fileKeys map[string]string
}

// enabled reports whether any authentication is configured.
func (a *Auth) enabled() bool {
	return a.SharedKey != "" || len(a.AgentKeys) > 0 || a.KeysFile != ""
}

// agentKeys returns the effective per-agent key set, reloading the keys
// file when it has changed on disk.
func (a *Auth) agentKeys() map[string]string {
	if a.KeysFile == "" {
		return a.AgentKeys
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if fi, err := os.Stat(a.KeysFile); err == nil {
		if !fi.ModTime().Equal(a.fileMod) {
			if data, err := os.ReadFile(a.KeysFile); err == nil {
				a.fileKeys = parseKeysFile(string(data))
				a.fileMod = fi.ModTime()
				log.Printf("reloaded %d agent keys from %s", len(a.fileKeys), a.KeysFile)
			}
		}
	} else {
		// File removed: revoke file-sourced keys.
		a.fileKeys = nil
		a.fileMod = time.Time{}
	}
	merged := make(map[string]string, len(a.AgentKeys)+len(a.fileKeys))
	for k, v := range a.AgentKeys {
		merged[k] = v
	}
	for k, v := range a.fileKeys {
		merged[k] = v
	}
	return merged
}

// parseKeysFile parses "agent:key" lines; blank lines and # comments allowed.
func parseKeysFile(s string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, key, ok := strings.Cut(line, ":")
		if ok && name != "" && key != "" {
			out[strings.TrimSpace(name)] = strings.TrimSpace(key)
		}
	}
	return out
}

// Config configures the HTTP server.
type Config struct {
	Auth       *Auth
	ACL        *ACL   // nil = no namespace restrictions
	RatePerMin int    // per-caller requests/minute on /v1 and /mcp; 0 = unlimited
	Version    string // reported by the MCP transport
	CertFile   string // serve HTTPS when both CertFile and KeyFile are set
	KeyFile    string
}

// ACL restricts which agents may read/write which namespaces.
// Rules come from $EVERMEMO_ACL: "agent:namespace:perm" triples, comma
// separated; agent and namespace may be "*"; perm is "r" or "rw".
// Example: "claude:eng:rw,cursor:eng:r,auditor:*:r".
// When an ACL is configured, anything not explicitly allowed is denied.
type ACL struct {
	rules []aclRule
}

type aclRule struct {
	agent, ns string
	write     bool
}

// ParseACL parses $EVERMEMO_ACL; returns nil (no restrictions) for empty input.
func ParseACL(s string) *ACL {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	acl := &ACL{}
	for _, part := range strings.Split(s, ",") {
		f := strings.Split(strings.TrimSpace(part), ":")
		if len(f) != 3 || f[0] == "" || f[1] == "" {
			continue
		}
		perm := strings.ToLower(f[2])
		if perm != "r" && perm != "rw" {
			continue
		}
		acl.rules = append(acl.rules, aclRule{agent: f[0], ns: f[1], write: perm == "rw"})
	}
	return acl
}

// Allow reports whether agent may access namespace ns. Pass ns="*" to ask
// for cross-namespace access (only a literal "*" namespace rule grants it).
func (a *ACL) Allow(agent, ns string, write bool) bool {
	if a == nil {
		return true
	}
	if ns == "" {
		ns = "default"
	}
	for _, r := range a.rules {
		if r.agent != "*" && r.agent != agent {
			continue
		}
		if ns == "*" && r.ns != "*" {
			continue
		}
		if r.ns != "*" && r.ns != ns {
			continue
		}
		if write && !r.write {
			continue
		}
		return true
	}
	return false
}

// ParseAgentKeys parses "name:key,name2:key2" into a map.
func ParseAgentKeys(s string) map[string]string {
	out := map[string]string{}
	for _, pair := range strings.Split(s, ",") {
		name, key, ok := strings.Cut(strings.TrimSpace(pair), ":")
		if ok && name != "" && key != "" {
			out[name] = key
		}
	}
	return out
}

// Run starts the HTTP API on addr and blocks until SIGINT/SIGTERM, then
// shuts down gracefully.
func Run(addr string, st *store.Store, cfg Config) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           Handler(st, cfg),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		if cfg.CertFile != "" && cfg.KeyFile != "" {
			errCh <- srv.ListenAndServeTLS(cfg.CertFile, cfg.KeyFile)
		} else {
			errCh <- srv.ListenAndServe()
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Println("shutting down…")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

// Handler builds the full HTTP handler (exported for tests).
func Handler(st *store.Store, cfg Config) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		n, _ := st.Count()
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "memories": n})
	})

	mux.HandleFunc("POST /v1/memories", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Content   string         `json:"content"`
			Tags      []string       `json:"tags"`
			Namespace string         `json:"namespace"`
			Metadata  map[string]any `json:"metadata"`
			TTL       string         `json:"ttl"` // e.g. "90m", "24h", "7d"
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
			return
		}
		ttl, err := store.ParseTTL(req.TTL)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if !cfg.ACL.Allow(agentFrom(r), req.Namespace, true) {
			writeError(w, http.StatusForbidden, "not allowed to write this namespace")
			return
		}
		mem, err := st.Add(store.AddRequest{
			Content:   req.Content,
			Tags:      req.Tags,
			Namespace: req.Namespace,
			Metadata:  req.Metadata,
			Agent:     agentFrom(r),
			TTL:       ttl,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, mem)
	})

	mux.HandleFunc("GET /v1/memories", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		ns := q.Get("namespace")
		limit, _ := strconv.Atoi(q.Get("limit"))

		checkNS := ns
		if checkNS == "" {
			checkNS = "*" // reading across namespaces needs a wildcard grant
		}
		if !cfg.ACL.Allow(agentFrom(r), checkNS, false) {
			writeError(w, http.StatusForbidden, "not allowed to read this namespace (specify ?namespace= you have access to)")
			return
		}

		var (
			results []*store.Memory
			err     error
		)
		if query := strings.TrimSpace(q.Get("q")); query != "" {
			results, err = st.Search(query, ns, limit)
		} else {
			results, err = st.List(ns, limit)
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if results == nil {
			results = []*store.Memory{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"memories": results})
	})

	mux.HandleFunc("GET /v1/memories/{id}", func(w http.ResponseWriter, r *http.Request) {
		mem, err := st.Get(r.PathValue("id"))
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		if !cfg.ACL.Allow(agentFrom(r), mem.Namespace, false) {
			writeError(w, http.StatusForbidden, "not allowed to read this namespace")
			return
		}
		writeJSON(w, http.StatusOK, mem)
	})

	mux.HandleFunc("PUT /v1/memories/{id}", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Content string   `json:"content"`
			Tags    []string `json:"tags"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
			return
		}
		if !allowWriteOn(cfg, st, w, r, r.PathValue("id")) {
			return
		}
		mem, err := st.Update(r.PathValue("id"), req.Content, req.Tags)
		if err != nil {
			status := http.StatusBadRequest
			if strings.Contains(err.Error(), "not found") {
				status = http.StatusNotFound
			}
			writeError(w, status, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, mem)
	})

	mux.HandleFunc("DELETE /v1/memories/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !allowWriteOn(cfg, st, w, r, r.PathValue("id")) {
			return
		}
		if err := st.Delete(r.PathValue("id")); err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"deleted": r.PathValue("id")})
	})

	// Memory graph: link two memories.
	mux.HandleFunc("POST /v1/memories/{id}/links", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Rel string `json:"rel"`
			To  string `json:"to"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
			return
		}
		if !allowWriteOn(cfg, st, w, r, r.PathValue("id")) || !allowWriteOn(cfg, st, w, r, req.To) {
			return
		}
		if err := st.Link(r.PathValue("id"), req.Rel, req.To); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, store.Link{From: r.PathValue("id"), Rel: req.Rel, To: req.To})
	})

	// Verification: confirm or dispute a memory.
	mux.HandleFunc("POST /v1/memories/{id}/verify", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Vote string `json:"vote"` // "confirm" | "dispute"
			Note string `json:"note"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
			return
		}
		vote := 0
		switch req.Vote {
		case "confirm":
			vote = 1
		case "dispute":
			vote = -1
		default:
			writeError(w, http.StatusBadRequest, "vote must be 'confirm' or 'dispute'")
			return
		}
		if !allowWriteOn(cfg, st, w, r, r.PathValue("id")) {
			return
		}
		mem, err := st.Verify(r.PathValue("id"), agentFrom(r), vote, req.Note)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, mem)
	})

	// Streamable HTTP MCP transport: agents POST JSON-RPC messages directly
	// to the hub — no local evermemo binary needed on the agent's machine.
	mux.HandleFunc("POST /mcp", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<20))
		if err != nil {
			writeError(w, http.StatusBadRequest, "reading body: "+err.Error())
			return
		}
		agent := agentFrom(r)
		var backend mcp.Backend = st
		if cfg.ACL != nil {
			backend = &aclBackend{st: st, agent: agent, acl: cfg.ACL}
		}
		resp := mcp.HandleRaw(backend, cfg.Version, agent, body)
		if resp == nil { // notification
			w.WriteHeader(http.StatusAccepted)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	})

	limited := withRateLimit(cfg.RatePerMin, mux)
	return withLogging(withAuth(cfg.Auth, limited))
}

// agentFrom returns the caller's identity: the authenticated agent name,
// or the X-Agent header when auth doesn't establish one.
func agentFrom(r *http.Request) string {
	if name, ok := r.Context().Value(agentKey).(string); ok && name != "" {
		return name
	}
	return strings.TrimSpace(r.Header.Get("X-Agent"))
}

// allowWriteOn checks write permission on the namespace of an existing
// memory, writing the HTTP error itself when denied/missing.
func allowWriteOn(cfg Config, st *store.Store, w http.ResponseWriter, r *http.Request, id string) bool {
	if cfg.ACL == nil {
		return true
	}
	mem, err := st.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return false
	}
	if !cfg.ACL.Allow(agentFrom(r), mem.Namespace, true) {
		writeError(w, http.StatusForbidden, "not allowed to write this namespace")
		return false
	}
	return true
}

// aclBackend enforces namespace ACLs on MCP tool calls made over HTTP.
type aclBackend struct {
	st    *store.Store
	agent string
	acl   *ACL
}

func (b *aclBackend) errDenied(ns string) error {
	return fmt.Errorf("agent %q is not allowed to access namespace %q", b.agent, ns)
}

func (b *aclBackend) Add(req store.AddRequest) (*store.Memory, error) {
	if !b.acl.Allow(b.agent, req.Namespace, true) {
		return nil, b.errDenied(req.Namespace)
	}
	return b.st.Add(req)
}

func (b *aclBackend) writeCheck(id string) error {
	m, err := b.st.Get(id)
	if err != nil {
		return err
	}
	if !b.acl.Allow(b.agent, m.Namespace, true) {
		return b.errDenied(m.Namespace)
	}
	return nil
}

func (b *aclBackend) Update(id, content string, tags []string) (*store.Memory, error) {
	if err := b.writeCheck(id); err != nil {
		return nil, err
	}
	return b.st.Update(id, content, tags)
}

func (b *aclBackend) Get(id string) (*store.Memory, error) {
	m, err := b.st.Get(id)
	if err != nil {
		return nil, err
	}
	if !b.acl.Allow(b.agent, m.Namespace, false) {
		return nil, b.errDenied(m.Namespace)
	}
	return m, nil
}

func (b *aclBackend) Search(query, ns string, limit int) ([]*store.Memory, error) {
	check := ns
	if check == "" {
		check = "*"
	}
	if !b.acl.Allow(b.agent, check, false) {
		return nil, b.errDenied(check)
	}
	return b.st.Search(query, ns, limit)
}

func (b *aclBackend) List(ns string, limit int) ([]*store.Memory, error) {
	check := ns
	if check == "" {
		check = "*"
	}
	if !b.acl.Allow(b.agent, check, false) {
		return nil, b.errDenied(check)
	}
	return b.st.List(ns, limit)
}

func (b *aclBackend) Delete(id string) error {
	if err := b.writeCheck(id); err != nil {
		return err
	}
	return b.st.Delete(id)
}

func (b *aclBackend) Link(from, rel, to string) error {
	if err := b.writeCheck(from); err != nil {
		return err
	}
	if err := b.writeCheck(to); err != nil {
		return err
	}
	return b.st.Link(from, rel, to)
}

func (b *aclBackend) Verify(id, _ string, vote int, note string) (*store.Memory, error) {
	if err := b.writeCheck(id); err != nil {
		return nil, err
	}
	return b.st.Verify(id, b.agent, vote, note)
}

func protected(path string) bool {
	return strings.HasPrefix(path, "/v1/") || path == "/mcp"
}

func withAuth(auth *Auth, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !protected(r.URL.Path) || auth == nil || !auth.enabled() {
			next.ServeHTTP(w, r)
			return
		}
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		// Per-agent keys: identify the caller.
		for name, key := range auth.agentKeys() {
			if subtle.ConstantTimeCompare([]byte(got), []byte(key)) == 1 {
				next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), agentKey, name)))
				return
			}
		}
		// Shared key: authenticated but anonymous.
		if auth.SharedKey != "" && subtle.ConstantTimeCompare([]byte(got), []byte(auth.SharedKey)) == 1 {
			next.ServeHTTP(w, r)
			return
		}
		writeError(w, http.StatusUnauthorized, "missing or invalid API key")
	})
}

// withRateLimit applies a simple token-bucket limit per caller (agent name,
// bearer token, or client IP) to protected routes.
func withRateLimit(perMin int, next http.Handler) http.Handler {
	if perMin <= 0 {
		return next
	}
	var (
		mu      sync.Mutex
		buckets = map[string]*bucket{}
	)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !protected(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		id := agentFrom(r)
		if id == "" {
			id = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		}
		if id == "" {
			if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
				id = host
			} else {
				id = r.RemoteAddr
			}
		}
		mu.Lock()
		b, ok := buckets[id]
		if !ok {
			b = &bucket{tokens: float64(perMin), last: time.Now()}
			buckets[id] = b
		}
		allowed := b.take(float64(perMin))
		mu.Unlock()
		if !allowed {
			w.Header().Set("Retry-After", "60")
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		next.ServeHTTP(w, r)
	})
}

type bucket struct {
	tokens float64
	last   time.Time
}

// take refills at ratePerMin tokens/minute (capped at ratePerMin) and
// consumes one token; reports whether the request is allowed.
func (b *bucket) take(ratePerMin float64) bool {
	now := time.Now()
	b.tokens += now.Sub(b.last).Minutes() * ratePerMin
	if b.tokens > ratePerMin {
		b.tokens = ratePerMin
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		log.Printf("%s %s", r.Method, r.URL.Path)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
