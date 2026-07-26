// Package consolidate runs memory-hygiene jobs: an LLM reviews stored
// memories, merges duplicates, resolves contradictions, and summarizes,
// keeping full audit trail via archives and links (never hard-deletes).
package consolidate

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Evermemoai/evermemo/internal/store"
)

// Chatter is the LLM capability consolidation needs (satisfied by *llm.Client).
type Chatter interface {
	Chat(system, user string) (string, error)
}

// Action is one instruction returned by the LLM.
type Action struct {
	Action  string   `json:"action"`  // "merge" | "supersede" | "archive" | "keep"
	IDs     []string `json:"ids"`     // sources for merge; single id for others
	Content string   `json:"content"` // new content for merge/supersede
	Reason  string   `json:"reason"`
}

// Report summarizes what a consolidation run did (or would do).
type Report struct {
	Reviewed int      `json:"reviewed"`
	Merged   int      `json:"merged"`
	Archived int      `json:"archived"`
	Kept     int      `json:"kept"`
	Actions  []Action `json:"actions"`
	DryRun   bool     `json:"dry_run"`
}

const systemPrompt = `You are a memory-consolidation engine. You receive a JSON array of memories.
Return ONLY a JSON array of actions, no prose. Each action is one of:

{"action":"merge","ids":["id1","id2"],"content":"<single memory combining them>","reason":"..."}
  - use when memories are duplicates or fragments of the same fact
{"action":"supersede","ids":["old_id"],"content":"<corrected/current fact>","reason":"..."}
  - use when memories contradict: the newest information wins; write the winning fact
{"action":"archive","ids":["id"],"reason":"..."}
  - use for stale/obsolete memories that no longer matter
{"action":"keep","ids":["id"],"reason":"..."}
  - everything else

Rules: every input id must appear in exactly one action. Merged/superseding
content must be concise and self-contained. When in doubt, keep.`

// Run reviews up to 200 memories in namespace and applies the LLM's actions.
// Merged/superseded sources are archived (not deleted) and linked to the new
// memory with derived_from/supersedes relations.
func Run(st *store.Store, ai Chatter, namespace string, dryRun bool) (*Report, error) {
	mems, err := st.List(namespace, 200)
	if err != nil {
		return nil, err
	}
	rep := &Report{Reviewed: len(mems), DryRun: dryRun}
	if len(mems) < 2 {
		return rep, nil
	}

	type slim struct {
		ID        string   `json:"id"`
		Content   string   `json:"content"`
		Tags      []string `json:"tags,omitempty"`
		Agent     string   `json:"agent,omitempty"`
		CreatedAt string   `json:"created_at"`
	}
	batch := make([]slim, len(mems))
	valid := map[string]*store.Memory{}
	for i, m := range mems {
		batch[i] = slim{m.ID, m.Content, m.Tags, m.Agent, m.CreatedAt.Format("2006-01-02T15:04:05Z")}
		valid[m.ID] = m
	}
	input, _ := json.Marshal(batch)

	reply, err := ai.Chat(systemPrompt, string(input))
	if err != nil {
		return nil, fmt.Errorf("consolidation LLM call: %w", err)
	}
	actions, err := parseActions(reply)
	if err != nil {
		return nil, err
	}

	for _, a := range actions {
		// Ignore hallucinated ids.
		ids := a.IDs[:0]
		for _, id := range a.IDs {
			if valid[id] != nil {
				ids = append(ids, id)
			}
		}
		a.IDs = ids
		if len(a.IDs) == 0 {
			continue
		}
		rep.Actions = append(rep.Actions, a)

		switch a.Action {
		case "merge":
			if len(a.IDs) < 2 || strings.TrimSpace(a.Content) == "" {
				rep.Kept += len(a.IDs)
				continue
			}
			rep.Merged += len(a.IDs)
			if dryRun {
				continue
			}
			merged, err := st.Add(store.AddRequest{
				Content:   a.Content,
				Tags:      unionTags(valid, a.IDs),
				Namespace: nsOf(valid, a.IDs, namespace),
				Agent:     "consolidator",
			})
			if err != nil {
				return rep, err
			}
			for _, id := range a.IDs {
				st.Link(merged.ID, "derived_from", id)
				st.Archive(id)
				rep.Archived++
			}
		case "supersede":
			if strings.TrimSpace(a.Content) == "" {
				rep.Kept += len(a.IDs)
				continue
			}
			if dryRun {
				continue
			}
			repl, err := st.Add(store.AddRequest{
				Content:   a.Content,
				Tags:      unionTags(valid, a.IDs),
				Namespace: nsOf(valid, a.IDs, namespace),
				Agent:     "consolidator",
			})
			if err != nil {
				return rep, err
			}
			for _, id := range a.IDs {
				st.Link(repl.ID, "supersedes", id)
				st.Archive(id)
				rep.Archived++
			}
		case "archive":
			if dryRun {
				rep.Archived += len(a.IDs)
				continue
			}
			for _, id := range a.IDs {
				if st.Archive(id) == nil {
					rep.Archived++
				}
			}
		default: // keep
			rep.Kept += len(a.IDs)
		}
	}
	return rep, nil
}

// parseActions extracts the JSON action array, tolerating markdown fences.
func parseActions(reply string) ([]Action, error) {
	reply = strings.TrimSpace(reply)
	if i := strings.Index(reply, "["); i >= 0 {
		if j := strings.LastIndex(reply, "]"); j > i {
			reply = reply[i : j+1]
		}
	}
	var actions []Action
	if err := json.Unmarshal([]byte(reply), &actions); err != nil {
		return nil, fmt.Errorf("parsing LLM actions: %w", err)
	}
	return actions, nil
}

func unionTags(valid map[string]*store.Memory, ids []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, id := range ids {
		for _, t := range valid[id].Tags {
			if !seen[t] {
				seen[t] = true
				out = append(out, t)
			}
		}
	}
	return out
}

func nsOf(valid map[string]*store.Memory, ids []string, fallback string) string {
	if len(ids) > 0 {
		return valid[ids[0]].Namespace
	}
	if fallback == "" {
		return "default"
	}
	return fallback
}
