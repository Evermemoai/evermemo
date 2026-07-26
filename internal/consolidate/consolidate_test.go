package consolidate

import (
	"path/filepath"
	"testing"

	"evermemo/internal/store"
)

// fakeLLM returns a canned action script.
type fakeLLM struct{ reply string }

func (f *fakeLLM) Chat(system, user string) (string, error) { return f.reply, nil }

func TestConsolidateMerge(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	a, _ := st.Add(store.AddRequest{Content: "deploys at 6pm", Tags: []string{"ops"}})
	b, _ := st.Add(store.AddRequest{Content: "deployment time is 6pm UTC"})
	c, _ := st.Add(store.AddRequest{Content: "cats are great"})

	reply := `Here you go:
[
  {"action":"merge","ids":["` + a.ID + `","` + b.ID + `"],"content":"Deploys run at 6pm UTC","reason":"duplicates"},
  {"action":"keep","ids":["` + c.ID + `"],"reason":"unrelated"},
  {"action":"archive","ids":["mem_hallucinated"],"reason":"should be ignored"}
]`
	rep, err := Run(st, &fakeLLM{reply: reply}, "", false)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rep.Merged != 2 || rep.Archived != 2 || rep.Kept != 1 {
		t.Errorf("report = %+v", rep)
	}

	// Sources archived, merged memory searchable with derived_from links.
	res, _ := st.Search("deploys 6pm UTC", "", 10)
	if len(res) != 1 || res[0].Agent != "consolidator" {
		t.Fatalf("results = %v", res)
	}
	merged, _ := st.Get(res[0].ID)
	if len(merged.Links) != 2 {
		t.Errorf("links = %v", merged.Links)
	}
	if _, err := st.Get(a.ID); err != nil {
		t.Error("source should be archived, not deleted")
	}
}

func TestConsolidateDryRun(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	a, _ := st.Add(store.AddRequest{Content: "fact one"})
	b, _ := st.Add(store.AddRequest{Content: "fact one again"})

	reply := `[{"action":"merge","ids":["` + a.ID + `","` + b.ID + `"],"content":"fact one","reason":"dups"}]`
	rep, err := Run(st, &fakeLLM{reply: reply}, "", true)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !rep.DryRun || rep.Merged != 2 {
		t.Errorf("report = %+v", rep)
	}
	// Nothing actually changed.
	if list, _ := st.List("", 10); len(list) != 2 {
		t.Errorf("dry run modified store: %d memories", len(list))
	}
}
