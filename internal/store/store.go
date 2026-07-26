// Package store implements evermemo's persistence layer: SQLite with FTS5
// full-text search, embedded in the binary via the pure-Go driver. An
// optional Embedder upgrades Search to hybrid keyword + semantic retrieval.
package store

import (
	"bufio"
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Memory is a single stored memory.
type Memory struct {
	ID        string         `json:"id"`
	Content   string         `json:"content"`
	Tags      []string       `json:"tags,omitempty"`
	Namespace string         `json:"namespace"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	Agent     string         `json:"agent,omitempty"` // who wrote it (provenance)
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	ExpiresAt *time.Time     `json:"expires_at,omitempty"` // nil = never expires
	Score     float64        `json:"score,omitempty"`      // relevance, only set on search results
}

// AddRequest describes a memory to store.
type AddRequest struct {
	Content   string
	Tags      []string
	Namespace string
	Metadata  map[string]any
	Agent     string        // provenance; may be empty
	TTL       time.Duration // 0 = never expires
}

// Embedder turns text into a vector. When set on a Store, memories are
// embedded on write and Search becomes hybrid (BM25 + cosine similarity).
type Embedder interface {
	Embed(text string) ([]float32, error)
}

type Store struct {
	db       *sql.DB
	path     string
	embedder Embedder
}

// SetEmbedder enables semantic embeddings; call once at startup.
func (s *Store) SetEmbedder(e Embedder) { s.embedder = e }

// DefaultPath returns the database path: $EVERMEMO_DB or ~/.evermemo/evermemo.db.
func DefaultPath() string {
	if p := os.Getenv("EVERMEMO_DB"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "evermemo.db"
	}
	return filepath.Join(home, ".evermemo", "evermemo.db")
}

const schema = `
CREATE TABLE IF NOT EXISTS memories (
	id         TEXT PRIMARY KEY,
	content    TEXT NOT NULL,
	tags       TEXT NOT NULL DEFAULT '',
	namespace  TEXT NOT NULL DEFAULT 'default',
	metadata   TEXT NOT NULL DEFAULT '{}',
	agent      TEXT NOT NULL DEFAULT '',
	expires_at INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_memories_namespace ON memories(namespace);

CREATE TABLE IF NOT EXISTS embeddings (
	id     TEXT PRIMARY KEY,
	vector BLOB NOT NULL
);

CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(
	content, tags,
	content='memories', content_rowid='rowid'
);

CREATE TRIGGER IF NOT EXISTS memories_ai AFTER INSERT ON memories BEGIN
	INSERT INTO memories_fts(rowid, content, tags) VALUES (new.rowid, new.content, new.tags);
END;

CREATE TRIGGER IF NOT EXISTS memories_ad AFTER DELETE ON memories BEGIN
	INSERT INTO memories_fts(memories_fts, rowid, content, tags) VALUES ('delete', old.rowid, old.content, old.tags);
END;

CREATE TRIGGER IF NOT EXISTS memories_au AFTER UPDATE ON memories BEGIN
	INSERT INTO memories_fts(memories_fts, rowid, content, tags) VALUES ('delete', old.rowid, old.content, old.tags);
	INSERT INTO memories_fts(rowid, content, tags) VALUES (new.rowid, new.content, new.tags);
END;

CREATE TRIGGER IF NOT EXISTS embeddings_ad AFTER DELETE ON memories BEGIN
	DELETE FROM embeddings WHERE id = old.id;
END;
`

// Open opens (or creates) the database at path and runs migrations.
func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("creating db directory: %w", err)
		}
	}
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// modernc.org/sqlite works best with a single writer connection.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrating schema: %w", err)
	}
	// Migrations for older databases; harmless if the columns exist.
	db.Exec(`ALTER TABLE memories ADD COLUMN agent TEXT NOT NULL DEFAULT ''`)
	db.Exec(`ALTER TABLE memories ADD COLUMN expires_at INTEGER NOT NULL DEFAULT 0`)
	// Purge expired memories on open.
	db.Exec(`DELETE FROM memories WHERE expires_at > 0 AND expires_at <= ?`, time.Now().Unix())
	return &Store{db: db, path: path}, nil
}

func (s *Store) Close() error { return s.db.Close() }
func (s *Store) Path() string { return s.path }

func newID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return "mem_" + hex.EncodeToString(b)
}

// ParseTTL parses a time-to-live like "90m", "24h" or "7d" (days).
func ParseTTL(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	if strings.HasSuffix(s, "d") {
		n, err := strconv.ParseFloat(strings.TrimSuffix(s, "d"), 64)
		if err != nil {
			return 0, fmt.Errorf("invalid ttl %q", s)
		}
		return time.Duration(n * 24 * float64(time.Hour)), nil
	}
	return time.ParseDuration(s)
}

// Add stores a new memory and returns it.
func (s *Store) Add(req AddRequest) (*Memory, error) {
	content := strings.TrimSpace(req.Content)
	if content == "" {
		return nil, fmt.Errorf("content must not be empty")
	}
	namespace := req.Namespace
	if namespace == "" {
		namespace = "default"
	}
	metaJSON := "{}"
	if len(req.Metadata) > 0 {
		b, err := json.Marshal(req.Metadata)
		if err != nil {
			return nil, fmt.Errorf("encoding metadata: %w", err)
		}
		metaJSON = string(b)
	}
	now := time.Now().UTC()
	var expires int64
	m := &Memory{
		ID:        newID(),
		Content:   content,
		Tags:      req.Tags,
		Namespace: namespace,
		Metadata:  req.Metadata,
		Agent:     req.Agent,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if req.TTL != 0 {
		t := now.Add(req.TTL)
		expires = t.Unix()
		m.ExpiresAt = &t
	}
	_, err := s.db.Exec(
		`INSERT INTO memories (id, content, tags, namespace, metadata, agent, expires_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.Content, strings.Join(req.Tags, ","), m.Namespace, metaJSON, m.Agent, expires,
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
	)
	if err != nil {
		return nil, err
	}
	s.embed(m.ID, m.Content)
	return m, nil
}

// Update modifies a memory's content and/or tags. Empty content keeps the
// old content; nil tags keep the old tags.
func (s *Store) Update(id, content string, tags []string) (*Memory, error) {
	m, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if c := strings.TrimSpace(content); c != "" {
		m.Content = c
	}
	if tags != nil {
		m.Tags = tags
	}
	m.UpdatedAt = time.Now().UTC()
	_, err = s.db.Exec(
		`UPDATE memories SET content = ?, tags = ?, updated_at = ? WHERE id = ?`,
		m.Content, strings.Join(m.Tags, ","), m.UpdatedAt.Format(time.RFC3339Nano), id,
	)
	if err != nil {
		return nil, err
	}
	s.embed(m.ID, m.Content)
	return m, nil
}

// Get returns a memory by id.
func (s *Store) Get(id string) (*Memory, error) {
	row := s.db.QueryRow(
		`SELECT id, content, tags, namespace, metadata, agent, expires_at, created_at, updated_at
		 FROM memories WHERE id = ?`, id)
	m, err := scanMemory(row)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("memory %q not found", id)
	}
	return m, err
}

// Delete removes a memory by id.
func (s *Store) Delete(id string) error {
	res, err := s.db.Exec(`DELETE FROM memories WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("memory %q not found", id)
	}
	return nil
}

// List returns the most recent memories, optionally filtered by namespace.
func (s *Store) List(namespace string, limit int) ([]*Memory, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.Query(
		`SELECT id, content, tags, namespace, metadata, agent, expires_at, created_at, updated_at
		 FROM memories
		 WHERE (? = '' OR namespace = ?)
		   AND (expires_at = 0 OR expires_at > ?)
		 ORDER BY created_at DESC
		 LIMIT ?`, namespace, namespace, time.Now().Unix(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collect(rows, false)
}

// Search returns the most relevant memories for query. It always runs a
// BM25-ranked full-text search; when an Embedder is configured it also runs
// a semantic (cosine similarity) search and fuses the two rankings with
// Reciprocal Rank Fusion.
func (s *Store) Search(query, namespace string, limit int) ([]*Memory, error) {
	if limit <= 0 {
		limit = 10
	}
	kw, err := s.keywordSearch(query, namespace, limit*3)
	if err != nil {
		return nil, err
	}
	if s.embedder == nil {
		if len(kw) > limit {
			kw = kw[:limit]
		}
		return kw, nil
	}
	sem, err := s.semanticSearch(query, namespace, limit*3)
	if err != nil {
		// Embedding provider unavailable: degrade gracefully to keyword-only.
		if len(kw) > limit {
			kw = kw[:limit]
		}
		return kw, nil
	}
	return rrfMerge(kw, sem, limit), nil
}

func (s *Store) keywordSearch(query, namespace string, limit int) ([]*Memory, error) {
	match := ftsQuery(query)
	if match == "" {
		return nil, fmt.Errorf("empty search query")
	}
	rows, err := s.db.Query(
		`SELECT m.id, m.content, m.tags, m.namespace, m.metadata, m.agent, m.expires_at, m.created_at, m.updated_at,
		        -bm25(memories_fts) AS score
		 FROM memories_fts f
		 JOIN memories m ON m.rowid = f.rowid
		 WHERE memories_fts MATCH ?
		   AND (? = '' OR m.namespace = ?)
		   AND (m.expires_at = 0 OR m.expires_at > ?)
		 ORDER BY bm25(memories_fts)
		 LIMIT ?`, match, namespace, namespace, time.Now().Unix(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collect(rows, true)
}

// Count returns the total number of memories.
func (s *Store) Count() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM memories`).Scan(&n)
	return n, err
}

// ftsQuery sanitizes a user query into a safe FTS5 MATCH expression:
// each token is double-quoted (so FTS5 operators/punctuation are treated
// literally) and joined with OR, letting BM25 rank partial matches.
func ftsQuery(q string) string {
	terms := strings.Fields(q)
	quoted := make([]string, 0, len(terms))
	for _, t := range terms {
		t = strings.ReplaceAll(t, `"`, `""`)
		quoted = append(quoted, `"`+t+`"`)
	}
	return strings.Join(quoted, " OR ")
}

type scannable interface{ Scan(dest ...any) error }

func scanMemory(row scannable) (*Memory, error) {
	return scanMemoryScore(row, false)
}

func scanMemoryScore(row scannable, withScore bool) (*Memory, error) {
	var m Memory
	var tags, metaJSON, createdAt, updatedAt string
	var expires int64
	dest := []any{&m.ID, &m.Content, &tags, &m.Namespace, &metaJSON, &m.Agent, &expires, &createdAt, &updatedAt}
	if withScore {
		dest = append(dest, &m.Score)
	}
	if err := row.Scan(dest...); err != nil {
		return nil, err
	}
	if tags != "" {
		m.Tags = strings.Split(tags, ",")
	}
	if metaJSON != "" && metaJSON != "{}" {
		_ = json.Unmarshal([]byte(metaJSON), &m.Metadata)
	}
	if expires > 0 {
		t := time.Unix(expires, 0).UTC()
		m.ExpiresAt = &t
	}
	m.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	m.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return &m, nil
}

func collect(rows *sql.Rows, withScore bool) ([]*Memory, error) {
	var out []*Memory
	for rows.Next() {
		m, err := scanMemoryScore(rows, withScore)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// --- semantic search -------------------------------------------------------

// embed computes and stores the vector for a memory; best-effort.
func (s *Store) embed(id, content string) {
	if s.embedder == nil {
		return
	}
	if v, err := s.embedder.Embed(content); err == nil && len(v) > 0 {
		s.db.Exec(`INSERT OR REPLACE INTO embeddings (id, vector) VALUES (?, ?)`, id, encodeVec(v))
	}
}

func (s *Store) semanticSearch(query, namespace string, limit int) ([]*Memory, error) {
	qv, err := s.embedder.Embed(query)
	if err != nil || len(qv) == 0 {
		return nil, fmt.Errorf("embedding query: %w", err)
	}
	rows, err := s.db.Query(
		`SELECT m.id, m.content, m.tags, m.namespace, m.metadata, m.agent, m.expires_at, m.created_at, m.updated_at,
		        e.vector
		 FROM embeddings e
		 JOIN memories m ON m.id = e.id
		 WHERE (? = '' OR m.namespace = ?)
		   AND (m.expires_at = 0 OR m.expires_at > ?)`,
		namespace, namespace, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Memory
	for rows.Next() {
		var m Memory
		var tags, metaJSON, createdAt, updatedAt string
		var expires int64
		var blob []byte
		if err := rows.Scan(&m.ID, &m.Content, &tags, &m.Namespace, &metaJSON, &m.Agent, &expires, &createdAt, &updatedAt, &blob); err != nil {
			return nil, err
		}
		if tags != "" {
			m.Tags = strings.Split(tags, ",")
		}
		if metaJSON != "" && metaJSON != "{}" {
			_ = json.Unmarshal([]byte(metaJSON), &m.Metadata)
		}
		if expires > 0 {
			t := time.Unix(expires, 0).UTC()
			m.ExpiresAt = &t
		}
		m.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		m.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
		m.Score = cosine(qv, decodeVec(blob))
		out = append(out, &m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// rrfMerge fuses two ranked lists with Reciprocal Rank Fusion (k=60).
func rrfMerge(a, b []*Memory, limit int) []*Memory {
	const k = 60
	scores := map[string]float64{}
	byID := map[string]*Memory{}
	for rank, m := range a {
		scores[m.ID] += 1.0 / float64(k+rank+1)
		byID[m.ID] = m
	}
	for rank, m := range b {
		scores[m.ID] += 1.0 / float64(k+rank+1)
		if _, ok := byID[m.ID]; !ok {
			byID[m.ID] = m
		}
	}
	out := make([]*Memory, 0, len(byID))
	for id, m := range byID {
		m.Score = scores[id]
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func encodeVec(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}

func decodeVec(b []byte) []float32 {
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}

func cosine(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// --- export / import -------------------------------------------------------

// ExportJSONL writes every non-expired memory as one JSON object per line.
func (s *Store) ExportJSONL(w io.Writer) (int, error) {
	mems, err := s.List("", 1<<30)
	if err != nil {
		return 0, err
	}
	enc := json.NewEncoder(w)
	for i, m := range mems {
		if err := enc.Encode(m); err != nil {
			return i, err
		}
	}
	return len(mems), nil
}

// ImportJSONL reads one JSON memory per line, preserving ids and timestamps.
// Existing memories with the same id are replaced.
func (s *Store) ImportJSONL(r io.Reader) (int, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	n := 0
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var m Memory
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			return n, fmt.Errorf("line %d: %w", n+1, err)
		}
		if strings.TrimSpace(m.Content) == "" {
			continue
		}
		if m.ID == "" {
			m.ID = newID()
		}
		if m.Namespace == "" {
			m.Namespace = "default"
		}
		now := time.Now().UTC()
		if m.CreatedAt.IsZero() {
			m.CreatedAt = now
		}
		if m.UpdatedAt.IsZero() {
			m.UpdatedAt = now
		}
		metaJSON := "{}"
		if len(m.Metadata) > 0 {
			b, _ := json.Marshal(m.Metadata)
			metaJSON = string(b)
		}
		var expires int64
		if m.ExpiresAt != nil {
			expires = m.ExpiresAt.Unix()
		}
		_, err := s.db.Exec(
			`INSERT OR REPLACE INTO memories (id, content, tags, namespace, metadata, agent, expires_at, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			m.ID, m.Content, strings.Join(m.Tags, ","), m.Namespace, metaJSON, m.Agent, expires,
			m.CreatedAt.Format(time.RFC3339Nano), m.UpdatedAt.Format(time.RFC3339Nano),
		)
		if err != nil {
			return n, err
		}
		s.embed(m.ID, m.Content)
		n++
	}
	return n, sc.Err()
}
