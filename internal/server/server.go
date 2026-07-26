// Package server implements evermemo's HTTP API and the streamable-HTTP MCP
// transport, with per-agent authentication, provenance, and rate limiting.
package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
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

	"evermemo/internal/mcp"
	"evermemo/internal/store"
)

type ctxKey int

const agentKey ctxKey = 0

// Auth describes how the API authenticates callers.
//
//   - SharedKey: one bearer key for everyone (legacy, $EVERMEMO_API_KEY).
//   - AgentKeys: per-agent identities, "name -> key" ($EVERMEMO_AGENT_KEYS,
//     format "alice:key1,bob:key2"). The authenticated agent name is recorded
//     as provenance on every memory it writes.
//
// If both are empty the API is open (agent name may still be supplied via
// the X-Agent header).
type Auth struct {
	SharedKey string
	AgentKeys map[string]string
}

// Config configures the HTTP server.
type Config struct {
	Auth       Auth
	RatePerMin int    // per-caller requests/minute on /v1 and /mcp; 0 = unlimited
	Version    string // reported by the MCP transport
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
	go func() { errCh <- srv.ListenAndServe() }()

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
		if err := st.Delete(r.PathValue("id")); err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"deleted": r.PathValue("id")})
	})

	// Streamable HTTP MCP transport: agents POST JSON-RPC messages directly
	// to the hub — no local evermemo binary needed on the agent's machine.
	mux.HandleFunc("POST /mcp", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<20))
		if err != nil {
			writeError(w, http.StatusBadRequest, "reading body: "+err.Error())
			return
		}
		resp := mcp.HandleRaw(st, cfg.Version, agentFrom(r), body)
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

func protected(path string) bool {
	return strings.HasPrefix(path, "/v1/") || path == "/mcp"
}

func withAuth(auth Auth, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !protected(r.URL.Path) || (auth.SharedKey == "" && len(auth.AgentKeys) == 0) {
			next.ServeHTTP(w, r)
			return
		}
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		// Per-agent keys: identify the caller.
		for name, key := range auth.AgentKeys {
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
