// Package store implements evermemo's persistence layer: SQLite with FTS5
// full-text search, embedded in the binary via the pure-Go driver.
package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	Score     float64        `json:"score,omitempty"` // relevance, only set on search results
}

type Store struct {
	db   *sql.DB
	path string
}

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
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_memories_namespace ON memories(namespace);

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
	// Migration for pre-provenance databases; harmless if the column exists.
	db.Exec(`ALTER TABLE memories ADD COLUMN agent TEXT NOT NULL DEFAULT ''`)
	return &Store{db: db, path: path}, nil
}

func (s *Store) Close() error { return s.db.Close() }
func (s *Store) Path() string { return s.path }

func newID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return "mem_" + hex.EncodeToString(b)
}

// Add stores a new memory and returns it. agent records who wrote it
// (provenance); it may be empty.
func (s *Store) Add(content string, tags []string, namespace string, metadata map[string]any, agent string) (*Memory, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, fmt.Errorf("content must not be empty")
	}
	if namespace == "" {
		namespace = "default"
	}
	metaJSON := "{}"
	if len(metadata) > 0 {
		b, err := json.Marshal(metadata)
		if err != nil {
			return nil, fmt.Errorf("encoding metadata: %w", err)
		}
		metaJSON = string(b)
	}
	now := time.Now().UTC()
	m := &Memory{
		ID:        newID(),
		Content:   content,
		Tags:      tags,
		Namespace: namespace,
		Metadata:  metadata,
		Agent:     agent,
		CreatedAt: now,
		UpdatedAt: now,
	}
	_, err := s.db.Exec(
		`INSERT INTO memories (id, content, tags, namespace, metadata, agent, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.Content, strings.Join(tags, ","), m.Namespace, metaJSON, m.Agent,
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
	)
	if err != nil {
		return nil, err
	}
	return m, nil
}

// Get returns a memory by id.
func (s *Store) Get(id string) (*Memory, error) {
	row := s.db.QueryRow(
		`SELECT id, content, tags, namespace, metadata, agent, created_at, updated_at
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
		`SELECT id, content, tags, namespace, metadata, agent, created_at, updated_at
		 FROM memories
		 WHERE (? = '' OR namespace = ?)
		 ORDER BY created_at DESC
		 LIMIT ?`, namespace, namespace, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collect(rows, false)
}

// Search runs a BM25-ranked full-text search over content and tags.
func (s *Store) Search(query, namespace string, limit int) ([]*Memory, error) {
	if limit <= 0 {
		limit = 10
	}
	match := ftsQuery(query)
	if match == "" {
		return nil, fmt.Errorf("empty search query")
	}
	rows, err := s.db.Query(
		`SELECT m.id, m.content, m.tags, m.namespace, m.metadata, m.agent, m.created_at, m.updated_at,
		        -bm25(memories_fts) AS score
		 FROM memories_fts f
		 JOIN memories m ON m.rowid = f.rowid
		 WHERE memories_fts MATCH ?
		   AND (? = '' OR m.namespace = ?)
		 ORDER BY bm25(memories_fts)
		 LIMIT ?`, match, namespace, namespace, limit)
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
	dest := []any{&m.ID, &m.Content, &tags, &m.Namespace, &metaJSON, &m.Agent, &createdAt, &updatedAt}
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
