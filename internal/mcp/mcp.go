// Package mcp implements a minimal Model Context Protocol server over stdio
// (JSON-RPC 2.0, newline-delimited), exposing evermemo's memory tools to any
// MCP client: Claude Code, Cursor, Windsurf, etc.
package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"evermemo/internal/store"
)

const protocolVersion = "2024-11-05"

// Backend is the memory operations the MCP server needs. It's satisfied by
// both *store.Store (local database) and *client.Client (remote memory hub),
// so any agent can run against its own file or the organization's shared brain.
type Backend interface {
	Add(req store.AddRequest) (*store.Memory, error)
	Update(id, content string, tags []string) (*store.Memory, error)
	Search(query, namespace string, limit int) ([]*store.Memory, error)
	List(namespace string, limit int) ([]*store.Memory, error)
	Delete(id string) error
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Serve reads JSON-RPC messages from stdin and writes responses to stdout
// until stdin closes. agent is this process's identity, recorded as
// provenance on memories written to a local store.
func Serve(st Backend, version, agent string) error {
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	out := json.NewEncoder(os.Stdout)

	for in.Scan() {
		line := strings.TrimSpace(in.Text())
		if line == "" {
			continue
		}
		var req request
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			continue // ignore malformed input
		}
		// Notifications (no id) get no response.
		if len(req.ID) == 0 || string(req.ID) == "null" {
			continue
		}
		resp := Handle(st, version, agent, req)
		if err := out.Encode(resp); err != nil {
			return err
		}
	}
	return in.Err()
}

// HandleRaw parses a single JSON-RPC message and returns the response, or
// nil for notifications and malformed input. It powers the HTTP transport.
func HandleRaw(st Backend, version, agent string, raw []byte) *response {
	var req request
	if err := json.Unmarshal(raw, &req); err != nil {
		return &response{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "parse error"}}
	}
	if len(req.ID) == 0 || string(req.ID) == "null" {
		return nil
	}
	resp := Handle(st, version, agent, req)
	return &resp
}

// Handle dispatches one request and builds the JSON-RPC response.
func Handle(st Backend, version, agent string, req request) response {
	resp := response{JSONRPC: "2.0", ID: req.ID}
	result, err := handle(st, version, agent, req)
	if err != nil {
		resp.Error = &rpcError{Code: -32603, Message: err.Error()}
	} else {
		resp.Result = result
	}
	return resp
}

func handle(st Backend, version, agent string, req request) (any, error) {
	switch req.Method {
	case "initialize":
		return map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "evermemo", "version": version},
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": toolDefs()}, nil
	case "tools/call":
		return callTool(st, agent, req.Params)
	default:
		return nil, fmt.Errorf("method not supported: %s", req.Method)
	}
}

func toolDefs() []map[string]any {
	obj := func(props map[string]any, required ...string) map[string]any {
		s := map[string]any{"type": "object", "properties": props}
		if len(required) > 0 {
			s["required"] = required
		}
		return s
	}
	str := func(desc string) map[string]any {
		return map[string]any{"type": "string", "description": desc}
	}
	num := func(desc string) map[string]any {
		return map[string]any{"type": "number", "description": desc}
	}

	return []map[string]any{
		{
			"name":        "add_memory",
			"description": "Store a memory (fact, preference, decision, or context) for later recall. Use whenever you learn something worth remembering across sessions.",
			"inputSchema": obj(map[string]any{
				"content":   str("The memory content to store"),
				"tags":      str("Optional comma-separated tags, e.g. 'prefs,ui'"),
				"namespace": str("Optional namespace to group memories (default: 'default')"),
				"ttl":       str("Optional time-to-live like '90m', '24h' or '7d'; the memory expires after this"),
			}, "content"),
		},
		{
			"name":        "update_memory",
			"description": "Update an existing memory's content and/or tags by id. Use when stored knowledge is outdated or wrong.",
			"inputSchema": obj(map[string]any{
				"id":      str("The memory id to update"),
				"content": str("New content (omit to keep current)"),
				"tags":    str("New comma-separated tags (omit to keep current)"),
			}, "id"),
		},
		{
			"name":        "search_memory",
			"description": "Full-text search stored memories and return the most relevant ones. Use before answering questions that may depend on previously stored context.",
			"inputSchema": obj(map[string]any{
				"query":     str("Search query"),
				"namespace": str("Optional namespace to search within"),
				"limit":     num("Max results (default 10)"),
			}, "query"),
		},
		{
			"name":        "list_memories",
			"description": "List the most recently stored memories.",
			"inputSchema": obj(map[string]any{
				"namespace": str("Optional namespace filter"),
				"limit":     num("Max results (default 20)"),
			}),
		},
		{
			"name":        "delete_memory",
			"description": "Delete a memory by its id.",
			"inputSchema": obj(map[string]any{
				"id": str("The memory id, e.g. 'mem_a1b2c3d4e5f60718'"),
			}, "id"),
		},
	}
}

func callTool(st Backend, agent string, params json.RawMessage) (any, error) {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid tools/call params: %w", err)
	}
	args := p.Arguments
	getStr := func(k string) string {
		v, _ := args[k].(string)
		return v
	}
	getInt := func(k string) int {
		v, _ := args[k].(float64)
		return int(v)
	}

	var (
		text string
		err  error
	)
	switch p.Name {
	case "add_memory":
		ttl, terr := store.ParseTTL(getStr("ttl"))
		if terr != nil {
			err = terr
			break
		}
		var mem *store.Memory
		mem, err = st.Add(store.AddRequest{
			Content:   getStr("content"),
			Tags:      splitTags(getStr("tags")),
			Namespace: getStr("namespace"),
			Agent:     agent,
			TTL:       ttl,
		})
		if err == nil {
			text = fmt.Sprintf("Stored memory %s", mem.ID)
		}
	case "update_memory":
		var tags []string
		if _, ok := args["tags"]; ok {
			tags = splitTags(getStr("tags"))
			if tags == nil {
				tags = []string{}
			}
		}
		var mem *store.Memory
		mem, err = st.Update(getStr("id"), getStr("content"), tags)
		if err == nil {
			text = fmt.Sprintf("Updated memory %s", mem.ID)
		}
	case "search_memory":
		var results []*store.Memory
		results, err = st.Search(getStr("query"), getStr("namespace"), getInt("limit"))
		if err == nil {
			text = formatResults(results)
		}
	case "list_memories":
		var results []*store.Memory
		results, err = st.List(getStr("namespace"), getInt("limit"))
		if err == nil {
			text = formatResults(results)
		}
	case "delete_memory":
		err = st.Delete(getStr("id"))
		if err == nil {
			text = fmt.Sprintf("Deleted memory %s", getStr("id"))
		}
	default:
		return nil, fmt.Errorf("unknown tool: %s", p.Name)
	}

	if err != nil {
		return map[string]any{
			"content": []map[string]any{{"type": "text", "text": "Error: " + err.Error()}},
			"isError": true,
		}, nil
	}
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
	}, nil
}

func formatResults(results []*store.Memory) string {
	if len(results) == 0 {
		return "No memories found."
	}
	var b strings.Builder
	for i, m := range results {
		fmt.Fprintf(&b, "%d. [%s] %s", i+1, m.ID, m.Content)
		if len(m.Tags) > 0 {
			fmt.Fprintf(&b, " (tags: %s)", strings.Join(m.Tags, ", "))
		}
		if m.Agent != "" {
			fmt.Fprintf(&b, " (by: %s)", m.Agent)
		}
		if i < len(results)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

func splitTags(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
