package store

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestAddGetDelete(t *testing.T) {
	st := testStore(t)

	m, err := st.Add(AddRequest{Content: "hello world", Tags: []string{"a", "b"}, Agent: "tester"})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if !strings.HasPrefix(m.ID, "mem_") {
		t.Errorf("id = %q, want mem_ prefix", m.ID)
	}

	got, err := st.Get(m.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Content != "hello world" || got.Agent != "tester" || len(got.Tags) != 2 {
		t.Errorf("got %+v", got)
	}

	if err := st.Delete(m.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := st.Get(m.ID); err == nil {
		t.Error("get after delete should fail")
	}
	if err := st.Delete(m.ID); err == nil {
		t.Error("double delete should fail")
	}
}

func TestAddEmptyContent(t *testing.T) {
	st := testStore(t)
	if _, err := st.Add(AddRequest{Content: "   "}); err == nil {
		t.Error("empty content should fail")
	}
}

func TestSearchBM25(t *testing.T) {
	st := testStore(t)
	st.Add(AddRequest{Content: "deploys happen at 6pm UTC every weekday"})
	st.Add(AddRequest{Content: "the cat sat on the mat"})

	res, err := st.Search("deploy 6pm", "", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res) != 1 || !strings.Contains(res[0].Content, "deploys") {
		t.Errorf("results = %v", res)
	}
}

func TestSearchInjectionSafe(t *testing.T) {
	st := testStore(t)
	st.Add(AddRequest{Content: "plain text"})
	// FTS5 operators and quotes must not break the query.
	for _, q := range []string{`"unbalanced`, `NEAR( AND OR`, `a*b:c^d`, `""`} {
		if _, err := st.Search(q, "", 5); err != nil {
			t.Errorf("search(%q) errored: %v", q, err)
		}
	}
}

func TestNamespaceIsolation(t *testing.T) {
	st := testStore(t)
	st.Add(AddRequest{Content: "alpha fact", Namespace: "proj1"})
	st.Add(AddRequest{Content: "alpha fact two", Namespace: "proj2"})

	res, _ := st.Search("alpha", "proj1", 10)
	if len(res) != 1 {
		t.Errorf("proj1 results = %d, want 1", len(res))
	}
	all, _ := st.Search("alpha", "", 10)
	if len(all) != 2 {
		t.Errorf("all results = %d, want 2", len(all))
	}
}

func TestTTLExpiry(t *testing.T) {
	st := testStore(t)
	st.Add(AddRequest{Content: "ephemeral note", TTL: -time.Hour}) // already expired
	keep, _ := st.Add(AddRequest{Content: "permanent note"})

	list, err := st.List("", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].ID != keep.ID {
		t.Errorf("list = %v, want only permanent", list)
	}
	res, _ := st.Search("note", "", 10)
	if len(res) != 1 {
		t.Errorf("search returned %d, want 1 (expired excluded)", len(res))
	}
}

func TestUpdate(t *testing.T) {
	st := testStore(t)
	m, _ := st.Add(AddRequest{Content: "old content", Tags: []string{"x"}})

	upd, err := st.Update(m.ID, "new content", nil)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if upd.Content != "new content" || len(upd.Tags) != 1 {
		t.Errorf("update = %+v", upd)
	}
	// FTS index must follow the update.
	if res, _ := st.Search("new content", "", 5); len(res) != 1 {
		t.Error("updated content not searchable")
	}
	if res, _ := st.Search("old", "", 5); len(res) != 0 {
		t.Error("old content still searchable")
	}
	if _, err := st.Update("mem_nope", "x", nil); err == nil {
		t.Error("update of missing id should fail")
	}
}

func TestExportImportRoundtrip(t *testing.T) {
	src := testStore(t)
	src.Add(AddRequest{Content: "first", Tags: []string{"t1"}, Agent: "a1"})
	src.Add(AddRequest{Content: "second", Namespace: "work"})

	var buf bytes.Buffer
	n, err := src.ExportJSONL(&buf)
	if err != nil || n != 2 {
		t.Fatalf("export: n=%d err=%v", n, err)
	}

	dst := testStore(t)
	n, err = dst.ImportJSONL(&buf)
	if err != nil || n != 2 {
		t.Fatalf("import: n=%d err=%v", n, err)
	}
	res, _ := dst.Search("first", "", 5)
	if len(res) != 1 || res[0].Agent != "a1" {
		t.Errorf("imported memory lost fields: %v", res)
	}
	// Idempotent: importing again must not duplicate (same ids).
	buf.Reset()
	src.ExportJSONL(&buf)
	dst.ImportJSONL(&buf)
	if c, _ := dst.Count(); c != 2 {
		t.Errorf("count after re-import = %d, want 2", c)
	}
}

// fakeEmbedder maps texts to fixed vectors so we can test hybrid search
// without a network provider.
type fakeEmbedder struct{ vecs map[string][]float32 }

func (f *fakeEmbedder) Embed(text string) ([]float32, error) {
	if v, ok := f.vecs[text]; ok {
		return v, nil
	}
	return []float32{0, 0, 1}, nil
}

func TestHybridSemanticSearch(t *testing.T) {
	st := testStore(t)
	st.SetEmbedder(&fakeEmbedder{vecs: map[string][]float32{
		"release schedule is thursdays": {1, 0, 0},
		"cats enjoy sunbeams":           {0, 1, 0},
		"when do we deploy":             {0.95, 0.05, 0}, // near "release schedule"
	}})
	st.Add(AddRequest{Content: "release schedule is thursdays"})
	st.Add(AddRequest{Content: "cats enjoy sunbeams"})

	// No keyword overlap: only the embedding can find this.
	res, err := st.Search("when do we deploy", "", 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res) == 0 || res[0].Content != "release schedule is thursdays" {
		t.Errorf("semantic search failed, got %v", res)
	}
}

func TestParseTTL(t *testing.T) {
	cases := map[string]time.Duration{
		"":    0,
		"90m": 90 * time.Minute,
		"24h": 24 * time.Hour,
		"7d":  7 * 24 * time.Hour,
	}
	for in, want := range cases {
		got, err := ParseTTL(in)
		if err != nil || got != want {
			t.Errorf("ParseTTL(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	if _, err := ParseTTL("banana"); err == nil {
		t.Error("ParseTTL(banana) should fail")
	}
}
