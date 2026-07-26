// Package server implements evermemo's HTTP API.
package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"

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

// Run starts the HTTP API on addr with the given auth configuration.
func Run(addr string, st *store.Store, auth Auth) error {
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
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
			return
		}
		mem, err := st.Add(req.Content, req.Tags, req.Namespace, req.Metadata, agentFrom(r))
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

	mux.HandleFunc("DELETE /v1/memories/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := st.Delete(r.PathValue("id")); err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"deleted": r.PathValue("id")})
	})

	handler := withLogging(withAuth(auth, mux))
	return http.ListenAndServe(addr, handler)
}

// agentFrom returns the caller's identity: the authenticated agent name,
// or the X-Agent header when auth doesn't establish one.
func agentFrom(r *http.Request) string {
	if name, ok := r.Context().Value(agentKey).(string); ok && name != "" {
		return name
	}
	return strings.TrimSpace(r.Header.Get("X-Agent"))
}

func withAuth(auth Auth, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v1/") || (auth.SharedKey == "" && len(auth.AgentKeys) == 0) {
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
