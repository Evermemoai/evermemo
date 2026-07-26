package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"evermemo/internal/client"
	"evermemo/internal/embed"
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
  search    Full-text (and semantic, if configured) search
  list      List recent memories
  get       Get a memory by id
  update    Update a memory's content/tags by id
  delete    Delete a memory by id
  export    Write all memories as JSONL to stdout
  import    Read JSONL memories from stdin
  mcp       Run as an MCP server over stdio (for Claude Code, Cursor, etc.)
  ui        Start the interactive terminal UI (default when no command given)
  version   Print version

Environment:
  EVERMEMO_DB              Path to the database file (default: ~/.evermemo/evermemo.db)
  EVERMEMO_API_KEY         If set, the HTTP API requires "Authorization: Bearer <key>"
  EVERMEMO_AGENT_KEYS      Per-agent keys "alice:key1,bob:key2" — identifies writers
  EVERMEMO_RATE            Max requests/minute per caller on the HTTP API (0 = off)
  EVERMEMO_REMOTE          URL of a central memory hub (mcp connects there instead of a local db)
  EVERMEMO_AGENT           This process's agent name, recorded as provenance
  EVERMEMO_EMBED_URL       Embedding provider URL (enables semantic search),
                           e.g. http://localhost:11434 for Ollama
  EVERMEMO_EMBED_MODEL     Embedding model (default: nomic-embed-text / text-embedding-3-small)
  EVERMEMO_EMBED_API_KEY   Bearer key for OpenAI-compatible embedding providers
  EVERMEMO_EMBED_PROVIDER  "ollama" (default) or "openai"

Examples:
  evermemo add "User prefers dark mode and tabs over spaces"
  echo "Deploy runs at 6pm UTC" | evermemo add --tags ops,deploy --ttl 7d
  evermemo search "deploy time"
  evermemo serve --addr :7777
  evermemo export > memories.jsonl
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
	case "update":
		err = cmdUpdate(args)
	case "delete", "rm":
		err = cmdDelete(args)
	case "export":
		err = cmdExport(args)
	case "import":
		err = cmdImport(args)
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
	st, err := store.Open(dbPath)
	if err != nil {
		return nil, err
	}
	if e := embed.FromEnv(); e != nil {
		st.SetEmbedder(e)
	}
	return st, nil
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
	rate, _ := strconv.Atoi(os.Getenv("EVERMEMO_RATE"))
	return server.Run(*addr, st, server.Config{
		Auth: server.Auth{
			SharedKey: os.Getenv("EVERMEMO_API_KEY"),
			AgentKeys: server.ParseAgentKeys(os.Getenv("EVERMEMO_AGENT_KEYS")),
		},
		RatePerMin: rate,
		Version:    version,
	})
}

func cmdAdd(args []string) error {
	fs := flag.NewFlagSet("add", flag.ExitOnError)
	db := fs.String("db", "", "database path")
	tags := fs.String("tags", "", "comma-separated tags")
	ns := fs.String("ns", "default", "namespace")
	meta := fs.String("meta", "", "metadata as JSON object")
	ttl := fs.String("ttl", "", "time-to-live like 90m, 24h or 7d (memory expires after)")
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
	ttlDur, err := store.ParseTTL(*ttl)
	if err != nil {
		return err
	}

	st, err := openStore(*db)
	if err != nil {
		return err
	}
	defer st.Close()

	mem, err := st.Add(store.AddRequest{
		Content:   content,
		Tags:      splitTags(*tags),
		Namespace: *ns,
		Metadata:  metadata,
		Agent:     os.Getenv("EVERMEMO_AGENT"),
		TTL:       ttlDur,
	})
	if err != nil {
		return err
	}
	fmt.Println(mem.ID)
	return nil
}

func cmdUpdate(args []string) error {
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	db := fs.String("db", "", "database path")
	tags := fs.String("tags", "", "comma-separated tags (replaces existing when set)")
	fs.Parse(args)
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: evermemo update [flags] <id> [new content]")
	}

	st, err := openStore(*db)
	if err != nil {
		return err
	}
	defer st.Close()

	var newTags []string
	if *tags != "" {
		newTags = splitTags(*tags)
	}
	mem, err := st.Update(fs.Arg(0), strings.Join(fs.Args()[1:], " "), newTags)
	if err != nil {
		return err
	}
	fmt.Println("updated", mem.ID)
	return nil
}

func cmdExport(args []string) error {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	db := fs.String("db", "", "database path")
	fs.Parse(args)

	st, err := openStore(*db)
	if err != nil {
		return err
	}
	defer st.Close()

	n, err := st.ExportJSONL(os.Stdout)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "exported %d memories\n", n)
	return nil
}

func cmdImport(args []string) error {
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	db := fs.String("db", "", "database path")
	fs.Parse(args)

	st, err := openStore(*db)
	if err != nil {
		return err
	}
	defer st.Close()

	n, err := st.ImportJSONL(os.Stdin)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "imported %d memories\n", n)
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
