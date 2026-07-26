package store

import (
	"path/filepath"
	"testing"
)

func TestBackup(t *testing.T) {
	st := testStore(t)
	st.Add(AddRequest{Content: "precious data"})

	dest := filepath.Join(t.TempDir(), "backup.db")
	if err := st.Backup(dest); err != nil {
		t.Fatalf("backup: %v", err)
	}
	// Backing up onto an existing file must fail.
	if err := st.Backup(dest); err == nil {
		t.Error("overwrite should fail")
	}

	// The snapshot is a fully working database.
	bk, err := Open(dest)
	if err != nil {
		t.Fatalf("open backup: %v", err)
	}
	defer bk.Close()
	if n, _ := bk.Count(); n != 1 {
		t.Errorf("backup count = %d, want 1", n)
	}
	if res, _ := bk.Search("precious", "", 5); len(res) != 1 {
		t.Error("backup FTS index broken")
	}
}

func TestLinksAndGet(t *testing.T) {
	st := testStore(t)
	a, _ := st.Add(AddRequest{Content: "old fact"})
	b, _ := st.Add(AddRequest{Content: "new fact"})

	if err := st.Link(b.ID, "supersedes", a.ID); err != nil {
		t.Fatalf("link: %v", err)
	}
	// Duplicate links are ignored.
	if err := st.Link(b.ID, "supersedes", a.ID); err != nil {
		t.Fatalf("dup link: %v", err)
	}
	if err := st.Link(b.ID, "invalid_rel", a.ID); err == nil {
		t.Error("invalid relation should fail")
	}
	if err := st.Link(b.ID, "relates_to", "mem_missing"); err == nil {
		t.Error("link to missing memory should fail")
	}

	got, _ := st.Get(a.ID)
	if len(got.Links) != 1 || got.Links[0].Rel != "supersedes" || got.Links[0].From != b.ID {
		t.Errorf("links = %+v", got.Links)
	}
}

func TestVerifyConfidence(t *testing.T) {
	st := testStore(t)
	m, _ := st.Add(AddRequest{Content: "disputed fact"})
	if m.Confidence != 0.6 {
		t.Errorf("initial confidence = %v, want 0.6", m.Confidence)
	}

	got, err := st.Verify(m.ID, "agent1", 1, "checked")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if got.Confidence != 0.7 {
		t.Errorf("after confirm = %v, want 0.7", got.Confidence)
	}

	got, _ = st.Verify(m.ID, "agent2", -1, "wrong")
	if got.Confidence != 0.55 {
		t.Errorf("after confirm+dispute = %v, want 0.55", got.Confidence)
	}

	// Same agent revotes: latest wins, no double count.
	got, _ = st.Verify(m.ID, "agent2", 1, "actually right")
	if got.Confidence != 0.8 {
		t.Errorf("after revote = %v, want 0.8", got.Confidence)
	}

	if _, err := st.Verify(m.ID, "", 1, ""); err == nil {
		t.Error("verify without agent should fail")
	}
	if _, err := st.Verify(m.ID, "a", 0, ""); err == nil {
		t.Error("vote 0 should fail")
	}
}

func TestArchiveHidesFromSearch(t *testing.T) {
	st := testStore(t)
	m, _ := st.Add(AddRequest{Content: "obsolete deploy info"})

	if err := st.Archive(m.ID); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if res, _ := st.Search("deploy", "", 10); len(res) != 0 {
		t.Error("archived memory still in search")
	}
	if res, _ := st.List("", 10); len(res) != 0 {
		t.Error("archived memory still in list")
	}
	// Get still works (audit trail).
	if _, err := st.Get(m.ID); err != nil {
		t.Errorf("get archived: %v", err)
	}
}
