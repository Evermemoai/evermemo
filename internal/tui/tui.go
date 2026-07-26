// Package tui implements evermemo's interactive terminal UI: a Claude
// Code-style REPL with a boxed welcome banner and slash commands.
// Plain text input stores a memory; /commands search and manage them.
package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"evermemo/internal/store"
)

// ANSI styles (truecolor-free, 256-color safe).
const (
	reset  = "\x1b[0m"
	bold   = "\x1b[1m"
	dim    = "\x1b[2m"
	orange = "\x1b[38;5;209m"
	green  = "\x1b[38;5;114m"
	red    = "\x1b[38;5;203m"
	gray   = "\x1b[38;5;245m"
	cyan   = "\x1b[38;5;117m"
)

const boxWidth = 66 // inner width of the banner box

// Run starts the interactive UI and blocks until the user exits.
func Run(st *store.Store, version string) error {
	printBanner(st, version)

	rd := newLineReader()
	ns := "default"

	for {
		raw, ok := rd.readLine(orange + bold + "❯ " + reset)
		if !ok {
			fmt.Println()
			bye()
			return nil
		}
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "/") {
			if quit := runCommand(st, &ns, line); quit {
				bye()
				return nil
			}
			fmt.Println()
			continue
		}
		// Plain text → remember it.
		mem, err := st.Add(store.AddRequest{Content: line, Namespace: ns, Agent: os.Getenv("EVERMEMO_AGENT")})
		if err != nil {
			errorf("%v", err)
		} else {
			fmt.Printf("%s●%s Stored %s%s%s %s(ns: %s)%s\n", green, reset, cyan, mem.ID, reset, dim, ns, reset)
		}
		fmt.Println()
	}
}

// runCommand executes a /command line; returns true if the user wants to quit.
func runCommand(st *store.Store, ns *string, line string) bool {
	fields := strings.Fields(line)
	cmd, rest := fields[0], strings.TrimSpace(strings.TrimPrefix(line, fields[0]))

	switch cmd {
	case "/exit", "/quit", "/q":
		return true

	case "/help", "/h", "/?":
		printHelp()

	case "/search", "/s":
		if rest == "" {
			errorf("usage: /search <query>")
			return false
		}
		results, err := st.Search(rest, *ns, 10)
		if err != nil {
			errorf("%v", err)
			return false
		}
		if len(results) == 0 {
			fmt.Printf("%s●%s no results\n", gray, reset)
			return false
		}
		fmt.Printf("%s●%s %d result(s)\n", green, reset, len(results))
		for _, m := range results {
			fmt.Printf("  %s%s%s  %s[%.2f]%s  %s\n", cyan, m.ID, reset, dim, m.Score, reset, oneline(m.Content, 80))
		}

	case "/list", "/ls", "/l":
		limit := 20
		if rest != "" {
			if n, err := strconv.Atoi(rest); err == nil {
				limit = n
			}
		}
		results, err := st.List(*ns, limit)
		if err != nil {
			errorf("%v", err)
			return false
		}
		if len(results) == 0 {
			fmt.Printf("%s●%s no memories yet — type anything to remember it\n", gray, reset)
			return false
		}
		for _, m := range results {
			fmt.Printf("  %s%s%s  %s%s%s  %s\n", cyan, m.ID, reset, dim, m.CreatedAt.Format("2006-01-02 15:04"), reset, oneline(m.Content, 72))
		}

	case "/get", "/g":
		if rest == "" {
			errorf("usage: /get <id>")
			return false
		}
		m, err := st.Get(rest)
		if err != nil {
			errorf("%v", err)
			return false
		}
		fmt.Printf("  %sid%s         %s\n", gray, reset, m.ID)
		fmt.Printf("  %snamespace%s  %s\n", gray, reset, m.Namespace)
		if len(m.Tags) > 0 {
			fmt.Printf("  %stags%s       %s\n", gray, reset, strings.Join(m.Tags, ", "))
		}
		if m.Agent != "" {
			fmt.Printf("  %sagent%s      %s\n", gray, reset, m.Agent)
		}
		fmt.Printf("  %screated%s    %s\n", gray, reset, m.CreatedAt.Format("2006-01-02 15:04:05"))
		fmt.Printf("  %scontent%s    %s\n", gray, reset, m.Content)

	case "/delete", "/rm", "/d":
		if rest == "" {
			errorf("usage: /delete <id>")
			return false
		}
		if err := st.Delete(rest); err != nil {
			errorf("%v", err)
			return false
		}
		fmt.Printf("%s●%s Deleted %s\n", red, reset, rest)

	case "/ns":
		if rest == "" {
			fmt.Printf("%s●%s namespace: %s%s%s\n", gray, reset, cyan, *ns, reset)
			return false
		}
		*ns = rest
		fmt.Printf("%s●%s switched to namespace %s%s%s\n", green, reset, cyan, *ns, reset)

	default:
		errorf("unknown command %s — try /help", cmd)
	}
	return false
}

func printBanner(st *store.Store, version string) {
	n, _ := st.Count()
	title := " evermemo v" + version + " "

	top := "╭─" + title + strings.Repeat("─", max(0, boxWidth-len([]rune(title))-1)) + "╮"
	bot := "╰" + strings.Repeat("─", boxWidth) + "╯"

	fmt.Println(orange + top + reset)
	row("")
	row(bold + "  Welcome back!" + reset)
	row(dim + "  " + strconv.Itoa(n) + " memories · " + tildify(st.Path()) + reset)
	row("")
	row(orange + "  Tips for getting started" + reset)
	row("  Type anything to remember it across sessions.")
	row(dim + "  /search <query>   find memories" + reset)
	row(dim + "  /list             recent memories" + reset)
	row(dim + "  /help             all commands" + reset)
	row("")
	fmt.Println(orange + bot + reset)
	fmt.Println()
}

func printHelp() {
	type item struct{ cmd, desc string }
	items := []item{
		{"<any text>", "store it as a memory"},
		{"/search <query>", "full-text search (BM25 ranked)"},
		{"/list [n]", "list recent memories"},
		{"/get <id>", "show a memory in full"},
		{"/delete <id>", "delete a memory"},
		{"/ns [name]", "show or switch namespace"},
		{"/help", "this help"},
		{"/exit", "leave (memories persist)"},
	}
	for _, it := range items {
		fmt.Printf("  %s%-17s%s %s%s%s\n", cyan, it.cmd, reset, dim, it.desc, reset)
	}
}

// row prints one banner line, padded to the box width (ANSI-aware).
func row(content string) {
	pad := boxWidth - visibleLen(content)
	if pad < 0 {
		pad = 0
	}
	fmt.Println(orange + "│" + reset + content + strings.Repeat(" ", pad) + orange + "│" + reset)
}

// visibleLen returns the printable rune count of s, skipping ANSI sequences.
func visibleLen(s string) int {
	n := 0
	inEsc := false
	for _, r := range s {
		switch {
		case inEsc:
			if r == 'm' {
				inEsc = false
			}
		case r == '\x1b':
			inEsc = true
		default:
			n++
		}
	}
	return n
}

func tildify(p string) string {
	if home, err := os.UserHomeDir(); err == nil {
		if rel, err := filepath.Rel(home, p); err == nil && !strings.HasPrefix(rel, "..") {
			return "~/" + rel
		}
	}
	return p
}

func oneline(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > max {
		return string(r[:max-1]) + "…"
	}
	return s
}

func errorf(format string, args ...any) {
	fmt.Printf("%s●%s %s\n", red, reset, fmt.Sprintf(format, args...))
}

func bye() {
	fmt.Println(dim + "goodbye — your memories are saved." + reset)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
