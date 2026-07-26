package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"evermemo/internal/client"
	"evermemo/internal/mcp"
	"evermemo/internal/server"
	"evermemo/internal/store"
	"evermemo/internal/tui"
)

const version = "0.1.0"

const usage = `evermemo — a tiny, universal memory engine for humans and AI agents

Usage:
  evermemo [command] [flags] [args]

Running with no command starts the interactive UI.

Commands:
  serve     Start the HTTP API server
  add       Add a memory (from arg or stdin)
  search    Full-text search memories
  list      List recent memories
  get       Get a memory by id
  delete    Delete a memory by id
  mcp       Run as an MCP server over stdio (for Claude Code, Cursor, etc.)
  ui        Start the interactive terminal UI (default when no command given)
  version   Print version

Environment:
  EVERMEMO_DB          Path to the database file (default: ~/.evermemo/evermemo.db)
  EVERMEMO_API_KEY     If set, the HTTP API requires "Authorization: Bearer <key>"
  EVERMEMO_AGENT_KEYS  Per-agent keys "alice:key1,bob:key2" — identifies writers
  EVERMEMO_REMOTE      URL of a central memory hub (mcp connects there instead of a local db)
  EVERMEMO_AGENT       This process's agent name, recorded as provenance

Examples:
  evermemo add "User prefers dark mode and tabs over spaces"
  echo "Deploy runs at 6pm UTC" | evermemo add --tags ops,deploy
  evermemo search "deploy time"
  evermemo serve --addr :7777
  evermemo mcp --remote https://memory.internal:7777 --agent claude-code
`

func main() {
	if len(os.Args) < 2 {
		if err := cmdTUI(nil); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	var err error
	switch cmd {
	case "serve":
		err = cmdServe(args)
	case "add":
		err = cmdAdd(args)
	case "search":
		err = cmdSearch(args)
	case "list":
		err = cmdList(args)
	case "get":
		err = cmdGet(args)
	case "delete", "rm":
		err = cmdDelete(args)
	case "mcp":
		err = cmdMCP(args)
	case "ui", "tui":
		err = cmdTUI(args)
	case "version", "--version", "-v":
		fmt.Println("evermemo " + version)
	case "help", "--help", "-h":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func openStore(dbPath string) (*store.Store, error) {
	if dbPath == "" {
		dbPath = store.DefaultPath()
	}
	return store.Open(dbPath)
}

func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", ":7777", "address to listen on")
	db := fs.String("db", "", "database path (default: ~/.evermemo/evermemo.db)")
	fs.Parse(args)

	st, err := openStore(*db)
	if err != nil {
		return err
	}
	defer st.Close()

	fmt.Printf("evermemo %s serving on %s (db: %s)\n", version, *addr, st.Path())
	return server.Run(*addr, st, server.Auth{
		SharedKey: os.Getenv("EVERMEMO_API_KEY"),
		AgentKeys: server.ParseAgentKeys(os.Getenv("EVERMEMO_AGENT_KEYS")),
	})
}

func cmdAdd(args []string) error {
	fs := flag.NewFlagSet("add", flag.ExitOnError)
	db := fs.String("db", "", "database path")
	tags := fs.String("tags", "", "comma-separated tags")
	ns := fs.String("ns", "default", "namespace")
	meta := fs.String("meta", "", "metadata as JSON object")
	fs.Parse(args)

	content := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if content == "" {
		// read from stdin (piped input)
		stat, _ := os.Stdin.Stat()
		if (stat.Mode() & os.ModeCharDevice) == 0 {
			sc := bufio.NewScanner(os.Stdin)
			sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
			var b strings.Builder
			for sc.Scan() {
				b.WriteString(sc.Text())
				b.WriteString("\n")
			}
			if err := sc.Err(); err != nil {
				return fmt.Errorf("reading stdin: %w", err)
			}
			content = strings.TrimSpace(b.String())
		}
	}
	if content == "" {
		return fmt.Errorf("nothing to add: pass content as an argument or pipe it via stdin")
	}

	var metadata map[string]any
	if *meta != "" {
		if err := json.Unmarshal([]byte(*meta), &metadata); err != nil {
			return fmt.Errorf("invalid --meta JSON: %w", err)
		}
	}

	st, err := openStore(*db)
	if err != nil {
		return err
	}
	defer st.Close()

	mem, err := st.Add(content, splitTags(*tags), *ns, metadata, os.Getenv("EVERMEMO_AGENT"))
	if err != nil {
		return err
	}
	fmt.Println(mem.ID)
	return nil
}

func cmdSearch(args []string) error {
	fs := flag.NewFlagSet("search", flag.ExitOnError)
	db := fs.String("db", "", "database path")
	ns := fs.String("ns", "", "restrict to namespace")
	limit := fs.Int("limit", 10, "max results")
	asJSON := fs.Bool("json", false, "output JSON")
	fs.Parse(args)

	query := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if query == "" {
		return fmt.Errorf("usage: evermemo search [flags] <query>")
	}

	st, err := openStore(*db)
	if err != nil {
		return err
	}
	defer st.Close()

	results, err := st.Search(query, *ns, *limit)
	if err != nil {
		return err
	}
	if *asJSON {
		return json.NewEncoder(os.Stdout).Encode(results)
	}
	if len(results) == 0 {
		fmt.Println("no results")
		return nil
	}
	for _, m := range results {
		fmt.Printf("%s  [%.2f]  %s\n", m.ID, m.Score, oneline(m.Content, 100))
	}
	return nil
}

func cmdList(args []string) error {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	db := fs.String("db", "", "database path")
	ns := fs.String("ns", "", "restrict to namespace")
	limit := fs.Int("limit", 20, "max results")
	asJSON := fs.Bool("json", false, "output JSON")
	fs.Parse(args)

	st, err := openStore(*db)
	if err != nil {
		return err
	}
	defer st.Close()

	results, err := st.List(*ns, *limit)
	if err != nil {
		return err
	}
	if *asJSON {
		return json.NewEncoder(os.Stdout).Encode(results)
	}
	if len(results) == 0 {
		fmt.Println("no memories yet — try: evermemo add \"something worth remembering\"")
		return nil
	}
	for _, m := range results {
		fmt.Printf("%s  %s  %s\n", m.ID, m.CreatedAt.Format("2006-01-02 15:04"), oneline(m.Content, 90))
	}
	return nil
}

func cmdGet(args []string) error {
	fs := flag.NewFlagSet("get", flag.ExitOnError)
	db := fs.String("db", "", "database path")
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: evermemo get <id>")
	}

	st, err := openStore(*db)
	if err != nil {
		return err
	}
	defer st.Close()

	mem, err := st.Get(fs.Arg(0))
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(mem)
}

func cmdDelete(args []string) error {
	fs := flag.NewFlagSet("delete", flag.ExitOnError)
	db := fs.String("db", "", "database path")
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: evermemo delete <id>")
	}

	st, err := openStore(*db)
	if err != nil {
		return err
	}
	defer st.Close()

	if err := st.Delete(fs.Arg(0)); err != nil {
		return err
	}
	fmt.Println("deleted", fs.Arg(0))
	return nil
}

func cmdMCP(args []string) error {
	fs := flag.NewFlagSet("mcp", flag.ExitOnError)
	db := fs.String("db", "", "database path")
	remote := fs.String("remote", os.Getenv("EVERMEMO_REMOTE"), "URL of a central memory hub (use shared memory instead of a local db)")
	key := fs.String("key", os.Getenv("EVERMEMO_API_KEY"), "API key for the remote hub")
	agent := fs.String("agent", os.Getenv("EVERMEMO_AGENT"), "agent name recorded as provenance on writes")
	fs.Parse(args)

	if *remote != "" {
		return mcp.Serve(client.New(*remote, *key, *agent), version, *agent)
	}

	st, err := openStore(*db)
	if err != nil {
		return err
	}
	defer st.Close()

	return mcp.Serve(st, version, *agent)
}

func cmdTUI(args []string) error {
	fs := flag.NewFlagSet("ui", flag.ExitOnError)
	db := fs.String("db", "", "database path")
	fs.Parse(args)

	st, err := openStore(*db)
	if err != nil {
		return err
	}
	defer st.Close()

	return tui.Run(st, version)
}

func splitTags(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func oneline(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > max {
		return s[:max-1] + "…"
	}
	return s
}
