package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"evermemo/internal/client"
	"evermemo/internal/consolidate"
	"evermemo/internal/embed"
	"evermemo/internal/llm"
	"evermemo/internal/mcp"
	"evermemo/internal/proxy"
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
  link      Link two memories: evermemo link <from> <rel> <to>
  verify    Vote on a memory: evermemo verify <id> confirm|dispute
  export    Write all memories as JSONL to stdout
  import    Read JSONL memories from stdin
  backup    Snapshot the database safely while serving: evermemo backup dest.db
  consolidate  LLM-powered memory hygiene: merge duplicates, resolve contradictions
  proxy     Auto-recall proxy in front of an LLM API (injects relevant memories)
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
  EVERMEMO_LLM_URL         Chat LLM for 'consolidate' (Ollama or OpenAI-compatible)
  EVERMEMO_LLM_MODEL       Chat model (default: llama3.2 / gpt-4o-mini)
  EVERMEMO_LLM_API_KEY     Bearer key for OpenAI-compatible chat providers
  EVERMEMO_ACL             Namespace ACLs "agent:ns:perm" (perm r|rw, * wildcards),
                           e.g. "claude:eng:rw,cursor:eng:r,auditor:*:r"

Examples:
  evermemo add "User prefers dark mode and tabs over spaces"
  echo "Deploy runs at 6pm UTC" | evermemo add --tags ops,deploy --ttl 7d
  evermemo search "deploy time"
  evermemo serve --addr :7777
  evermemo export > memories.jsonl
  evermemo consolidate --ns default --dry-run
  evermemo proxy --target https://api.openai.com --addr :8788
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
	case "link":
		err = cmdLink(args)
	case "verify":
		err = cmdVerify(args)
	case "export":
		err = cmdExport(args)
	case "import":
		err = cmdImport(args)
	case "backup":
		err = cmdBackup(args)
	case "consolidate":
		err = cmdConsolidate(args)
	case "proxy":
		err = cmdProxy(args)
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
	cert := fs.String("cert", os.Getenv("EVERMEMO_TLS_CERT"), "TLS certificate file (enables HTTPS with --key)")
	key := fs.String("key", os.Getenv("EVERMEMO_TLS_KEY"), "TLS private key file")
	keysFile := fs.String("keys-file", os.Getenv("EVERMEMO_KEYS_FILE"), "agent keys file (agent:key per line), hot-reloaded on change")
	fs.Parse(args)

	if (*cert == "") != (*key == "") {
		return fmt.Errorf("--cert and --key must be set together")
	}

	st, err := openStore(*db)
	if err != nil {
		return err
	}
	defer st.Close()

	scheme := "http"
	if *cert != "" {
		scheme = "https"
	}
	fmt.Printf("evermemo %s serving %s on %s (db: %s)\n", version, scheme, *addr, st.Path())
	rate, _ := strconv.Atoi(os.Getenv("EVERMEMO_RATE"))
	return server.Run(*addr, st, server.Config{
		Auth: &server.Auth{
			SharedKey: os.Getenv("EVERMEMO_API_KEY"),
			AgentKeys: server.ParseAgentKeys(os.Getenv("EVERMEMO_AGENT_KEYS")),
			KeysFile:  *keysFile,
		},
		ACL:        server.ParseACL(os.Getenv("EVERMEMO_ACL")),
		RatePerMin: rate,
		Version:    version,
		CertFile:   *cert,
		KeyFile:    *key,
	})
}

func cmdBackup(args []string) error {
	fs := flag.NewFlagSet("backup", flag.ExitOnError)
	db := fs.String("db", "", "database path")
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: evermemo backup <destination.db>")
	}

	st, err := openStore(*db)
	if err != nil {
		return err
	}
	defer st.Close()

	if err := st.Backup(fs.Arg(0)); err != nil {
		return err
	}
	fmt.Println("backup written to", fs.Arg(0))
	return nil
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

func cmdLink(args []string) error {
	fs := flag.NewFlagSet("link", flag.ExitOnError)
	db := fs.String("db", "", "database path")
	fs.Parse(args)
	if fs.NArg() != 3 {
		return fmt.Errorf("usage: evermemo link <from-id> <supersedes|relates_to|derived_from> <to-id>")
	}

	st, err := openStore(*db)
	if err != nil {
		return err
	}
	defer st.Close()

	if err := st.Link(fs.Arg(0), fs.Arg(1), fs.Arg(2)); err != nil {
		return err
	}
	fmt.Printf("linked %s -%s-> %s\n", fs.Arg(0), fs.Arg(1), fs.Arg(2))
	return nil
}

func cmdVerify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	db := fs.String("db", "", "database path")
	note := fs.String("note", "", "reason for the vote")
	fs.Parse(args)
	if fs.NArg() != 2 {
		return fmt.Errorf("usage: evermemo verify <id> confirm|dispute")
	}
	vote := 0
	switch fs.Arg(1) {
	case "confirm":
		vote = 1
	case "dispute":
		vote = -1
	default:
		return fmt.Errorf("vote must be 'confirm' or 'dispute'")
	}
	agent := os.Getenv("EVERMEMO_AGENT")
	if agent == "" {
		agent = "cli"
	}

	st, err := openStore(*db)
	if err != nil {
		return err
	}
	defer st.Close()

	mem, err := st.Verify(fs.Arg(0), agent, vote, *note)
	if err != nil {
		return err
	}
	fmt.Printf("recorded %s — confidence now %.2f\n", fs.Arg(1), mem.Confidence)
	return nil
}

func cmdConsolidate(args []string) error {
	fs := flag.NewFlagSet("consolidate", flag.ExitOnError)
	db := fs.String("db", "", "database path")
	ns := fs.String("ns", "", "namespace to consolidate ('' = all)")
	dryRun := fs.Bool("dry-run", false, "show what would happen without changing anything")
	fs.Parse(args)

	ai := llm.FromEnv()
	if ai == nil {
		return fmt.Errorf("consolidation needs an LLM: set EVERMEMO_LLM_URL (e.g. http://localhost:11434 for Ollama)")
	}

	st, err := openStore(*db)
	if err != nil {
		return err
	}
	defer st.Close()

	rep, err := consolidate.Run(st, ai, *ns, *dryRun)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(rep)
}

func cmdProxy(args []string) error {
	fs := flag.NewFlagSet("proxy", flag.ExitOnError)
	db := fs.String("db", "", "database path")
	addr := fs.String("addr", ":8788", "address to listen on")
	target := fs.String("target", "", "upstream LLM API base URL, e.g. https://api.openai.com")
	ns := fs.String("ns", "", "memory namespace to search ('' = all)")
	limit := fs.Int("limit", 5, "max memories to inject per request")
	remote := fs.String("remote", os.Getenv("EVERMEMO_REMOTE"), "search a central memory hub instead of a local db")
	key := fs.String("key", os.Getenv("EVERMEMO_API_KEY"), "API key for the remote hub")
	fs.Parse(args)
	if *target == "" {
		return fmt.Errorf("usage: evermemo proxy --target <llm-api-url> [--addr :8788]")
	}

	var mem proxy.Searcher
	if *remote != "" {
		mem = client.New(*remote, *key, os.Getenv("EVERMEMO_AGENT"))
	} else {
		st, err := openStore(*db)
		if err != nil {
			return err
		}
		defer st.Close()
		mem = st
	}

	h, err := proxy.Handler(mem, proxy.Config{Target: *target, Namespace: *ns, Limit: *limit})
	if err != nil {
		return err
	}
	fmt.Printf("evermemo %s auto-recall proxy on %s → %s\n", version, *addr, *target)
	return http.ListenAndServe(*addr, h)
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
