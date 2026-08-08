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
	ID         string         `json:"id"`
	Content    string         `json:"content"`
	Tags       []string       `json:"tags,omitempty"`
	Namespace  string         `json:"namespace"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	Agent      string         `json:"agent,omitempty"` // who wrote it (provenance)
	Confidence float64        `json:"confidence"`      // trust score in [0.05, 0.99]
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
	ExpiresAt  *time.Time     `json:"expires_at,omitempty"` // nil = never expires
	Score      float64        `json:"score,omitempty"`      // relevance, only set on search results
	Links      []Link         `json:"links,omitempty"`      // populated by Get
}

// Link relates two memories. Relations: "supersedes", "relates_to",
// "derived_from".
type Link struct {
	From string `json:"from"`
	Rel  string `json:"rel"`
	To   string `json:"to"`
}

// ValidRels are the allowed link relations.
var ValidRels = map[string]bool{"supersedes": true, "relates_to": true, "derived_from": true}

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
	archived   INTEGER NOT NULL DEFAULT 0,
	confidence REAL NOT NULL DEFAULT 0.6,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_memories_namespace ON memories(namespace);

CREATE TABLE IF NOT EXISTS embeddings (
	id     TEXT PRIMARY KEY,
	vector BLOB NOT NULL
);

CREATE TABLE IF NOT EXISTS links (
	from_id    TEXT NOT NULL,
	rel        TEXT NOT NULL,
	to_id      TEXT NOT NULL,
	created_at TEXT NOT NULL,
	PRIMARY KEY (from_id, rel, to_id)
);

CREATE INDEX IF NOT EXISTS idx_links_to ON links(to_id);

CREATE TABLE IF NOT EXISTS verifications (
	memory_id  TEXT NOT NULL,
	agent      TEXT NOT NULL,
	vote       INTEGER NOT NULL, -- +1 confirm, -1 dispute
	note       TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	PRIMARY KEY (memory_id, agent)
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

CREATE TABLE IF NOT EXISTS kv (
	key        TEXT PRIMARY KEY,
	value      TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
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
	db.Exec(`ALTER TABLE memories ADD COLUMN archived INTEGER NOT NULL DEFAULT 0`)
	db.Exec(`ALTER TABLE memories ADD COLUMN confidence REAL NOT NULL DEFAULT 0.6`)
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
	m.Confidence = 0.6
	_, err := s.db.Exec(
		`INSERT INTO memories (id, content, tags, namespace, metadata, agent, expires_at, confidence, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.Content, strings.Join(req.Tags, ","), m.Namespace, metaJSON, m.Agent, expires, m.Confidence,
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

// Get returns a memory by id, including its links (both directions).
func (s *Store) Get(id string) (*Memory, error) {
	row := s.db.QueryRow(
		`SELECT id, content, tags, namespace, metadata, agent, expires_at, confidence, created_at, updated_at
		 FROM memories WHERE id = ?`, id)
	m, err := scanMemory(row)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("memory %q not found", id)
	}
	if err != nil {
		return nil, err
	}
	m.Links, err = s.LinksFor(id)
	return m, err
}

// Link records a relation between two memories. Valid rels: supersedes,
// relates_to, derived_from.
func (s *Store) Link(from, rel, to string) error {
	if !ValidRels[rel] {
		return fmt.Errorf("invalid relation %q (want supersedes, relates_to, or derived_from)", rel)
	}
	for _, id := range []string{from, to} {
		var one int
		if err := s.db.QueryRow(`SELECT 1 FROM memories WHERE id = ?`, id).Scan(&one); err != nil {
			return fmt.Errorf("memory %q not found", id)
		}
	}
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO links (from_id, rel, to_id, created_at) VALUES (?, ?, ?, ?)`,
		from, rel, to, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

// LinksFor returns all links where id is either endpoint.
func (s *Store) LinksFor(id string) ([]Link, error) {
	rows, err := s.db.Query(
		`SELECT from_id, rel, to_id FROM links WHERE from_id = ? OR to_id = ?`, id, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Link
	for rows.Next() {
		var l Link
		if err := rows.Scan(&l.From, &l.Rel, &l.To); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// Verify records an agent's confirm (+1) or dispute (-1) vote on a memory
// and recomputes its confidence: 0.6 + 0.10·confirms − 0.15·disputes,
// clamped to [0.05, 0.99]. One vote per agent (latest wins).
func (s *Store) Verify(id, agent string, vote int, note string) (*Memory, error) {
	if agent == "" {
		return nil, fmt.Errorf("verification requires an agent identity")
	}
	if vote != 1 && vote != -1 {
		return nil, fmt.Errorf("vote must be +1 (confirm) or -1 (dispute)")
	}
	if _, err := s.Get(id); err != nil {
		return nil, err
	}
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO verifications (memory_id, agent, vote, note, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		id, agent, vote, note, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	var confirms, disputes int
	if err := s.db.QueryRow(
		`SELECT COALESCE(SUM(vote = 1), 0), COALESCE(SUM(vote = -1), 0)
		 FROM verifications WHERE memory_id = ?`, id).Scan(&confirms, &disputes); err != nil {
		return nil, err
	}
	conf := 0.6 + 0.10*float64(confirms) - 0.15*float64(disputes)
	conf = math.Round(conf*100) / 100
	if conf > 0.99 {
		conf = 0.99
	}
	if conf < 0.05 {
		conf = 0.05
	}
	if _, err := s.db.Exec(`UPDATE memories SET confidence = ? WHERE id = ?`, conf, id); err != nil {
		return nil, err
	}
	return s.Get(id)
}

// Archive hides a memory from list/search without deleting it (audit trail
// for consolidation). Get still returns archived memories.
func (s *Store) Archive(id string) error {
	res, err := s.db.Exec(`UPDATE memories SET archived = 1 WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("memory %q not found", id)
	}
	return nil
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
		`SELECT id, content, tags, namespace, metadata, agent, expires_at, confidence, created_at, updated_at
		 FROM memories
		 WHERE (? = '' OR namespace = ?)
		   AND (expires_at = 0 OR expires_at > ?)
		   AND archived = 0
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
		`SELECT m.id, m.content, m.tags, m.namespace, m.metadata, m.agent, m.expires_at, m.confidence, m.created_at, m.updated_at,
		        -bm25(memories_fts) AS score
		 FROM memories_fts f
		 JOIN memories m ON m.rowid = f.rowid
		 WHERE memories_fts MATCH ?
		   AND (? = '' OR m.namespace = ?)
		   AND (m.expires_at = 0 OR m.expires_at > ?)
		   AND m.archived = 0
		 ORDER BY bm25(memories_fts)
		 LIMIT ?`, match, namespace, namespace, time.Now().Unix(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collect(rows, true)
}

// KVGet returns the value stored under key, or "" when absent.
func (s *Store) KVGet(key string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM kv WHERE key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}

// KVSet stores value under key, replacing any previous value.
func (s *Store) KVSet(key, value string) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO kv (key, value, updated_at) VALUES (?, ?, ?)`,
		key, value, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

// Count returns the total number of memories.
func (s *Store) Count() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM memories`).Scan(&n)
	return n, err
}

// Backup writes a consistent snapshot of the database to dest using SQLite's
// VACUUM INTO — safe to run while the hub is serving traffic. Fails if dest
// already exists.
func (s *Store) Backup(dest string) error {
	if dest == "" {
		return fmt.Errorf("backup destination required")
	}
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("destination %q already exists", dest)
	}
	if dir := filepath.Dir(dest); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating backup directory: %w", err)
		}
	}
	_, err := s.db.Exec(`VACUUM INTO ?`, dest)
	return err
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
	dest := []any{&m.ID, &m.Content, &tags, &m.Namespace, &metaJSON, &m.Agent, &expires, &m.Confidence, &createdAt, &updatedAt}
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
		`SELECT m.id, m.content, m.tags, m.namespace, m.metadata, m.agent, m.expires_at, m.confidence, m.created_at, m.updated_at,
		        e.vector
		 FROM embeddings e
		 JOIN memories m ON m.id = e.id
		 WHERE (? = '' OR m.namespace = ?)
		   AND (m.expires_at = 0 OR m.expires_at > ?)
		   AND m.archived = 0`,
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
		if err := rows.Scan(&m.ID, &m.Content, &tags, &m.Namespace, &metaJSON, &m.Agent, &expires, &m.Confidence, &createdAt, &updatedAt, &blob); err != nil {
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
		if m.Confidence == 0 {
			m.Confidence = 0.6
		}
		_, err := s.db.Exec(
			`INSERT OR REPLACE INTO memories (id, content, tags, namespace, metadata, agent, expires_at, confidence, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			m.ID, m.Content, strings.Join(m.Tags, ","), m.Namespace, metaJSON, m.Agent, expires, m.Confidence,
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
