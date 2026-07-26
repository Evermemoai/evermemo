# evermemo

**A tiny, universal memory engine for humans and AI agents.**

One small Go binary. No external services. It's a CLI, an HTTP API, and an MCP server — so *anything* can remember things: your terminal, your scripts, Claude Code, Cursor, or any agent in any industry.

```
┌──────────────┐   ┌─────────────┐   ┌──────────────────┐
│  You (CLI)   │   │  Any app    │   │  AI agents (MCP)  │
│ evermemo add │   │  HTTP API   │   │  Claude, Cursor…  │
└──────┬───────┘   └──────┬──────┘   └────────┬─────────┘
       └──────────────────┼──────────────────┘
                  ┌───────▼──────┐
                  │   evermemo   │  single binary
                  │ SQLite+FTS5  │  BM25 full-text search
                  └──────────────┘
```

## Install

```sh
go build -o evermemo .
# optionally: mv evermemo /usr/local/bin/
```

## CLI

```sh
evermemo add "User prefers dark mode and tabs over spaces" --tags prefs,ui
echo "Deploy runs at 6pm UTC" | evermemo add --tags ops
evermemo search "deploy time"
evermemo list
evermemo get mem_a1b2c3d4e5f60718
evermemo delete mem_a1b2c3d4e5f60718
```

## Interactive UI

Run `evermemo` with no arguments (or `evermemo ui`) for a Claude Code-style terminal UI:
type anything to remember it, use `/search`, `/list`, `/get`, `/delete`, `/ns` to manage
memories, `/help` for everything, `/exit` to leave.

## HTTP API

```sh
evermemo serve --addr :7777
```

| Method | Path                 | Description                          |
| ------ | -------------------- | ------------------------------------ |
| GET    | `/health`            | Health check + memory count          |
| POST   | `/v1/memories`       | Create: `{content, tags?, namespace?, metadata?}` |
| GET    | `/v1/memories?q=...` | Search (BM25 ranked); omit `q` to list |
| GET    | `/v1/memories/{id}`  | Get one                              |
| DELETE | `/v1/memories/{id}`  | Delete                               |

```sh
curl -X POST localhost:7777/v1/memories \
  -d '{"content":"Invoices are due net-30","tags":["billing"]}'

curl "localhost:7777/v1/memories?q=invoice+due"
```

Set `EVERMEMO_API_KEY=secret` to require `Authorization: Bearer secret` on all `/v1` routes.

## MCP (Claude Code, Cursor, any agent)

evermemo speaks the Model Context Protocol over stdio, exposing four tools:
`add_memory`, `search_memory`, `list_memories`, `delete_memory`.

**Claude Code:**

```sh
claude mcp add evermemo -- /path/to/evermemo mcp
```

**Cursor / generic MCP config:**

```json
{
  "mcpServers": {
    "evermemo": {
      "command": "/path/to/evermemo",
      "args": ["mcp"]
    }
  }
}
```

Now your agent can remember things across sessions — automatically.

## Shared memory for all your agents (hub mode)

Run one evermemo as your organization's memory hub, and point every agent at it.
All agents share, search, and build on the same trusted knowledge — and every
memory records **which agent wrote it**.

```sh
# On the hub machine: one key per agent identity
EVERMEMO_AGENT_KEYS='claude:key1,cursor:key2' evermemo serve --addr :7777

# On each agent's machine: MCP proxies to the hub instead of a local file
evermemo mcp --remote https://memory.internal:7777 --key key1 --agent claude
```

Anything one agent stores is instantly searchable by all the others, with
provenance (`"agent": "claude"`) on every memory. Requests with unknown keys
are rejected. `EVERMEMO_REMOTE`, `EVERMEMO_API_KEY`, and `EVERMEMO_AGENT` env
vars work as flag defaults.

## Configuration

| Env var            | Default                  | Description                      |
| ------------------ | ------------------------ | -------------------------------- |
| `EVERMEMO_DB`      | `~/.evermemo/evermemo.db` | Database file path              |
| `EVERMEMO_API_KEY` | *(unset)*                | If set, HTTP API requires bearer auth |

Every command also accepts `--db` to point at a specific database, and `--ns`/`namespace` to partition memories (per project, per user, per agent — your call).

## Why

- **Small**: one binary, one SQLite file, zero dependencies to run.
- **Universal**: CLI for humans, HTTP for any language, MCP for any agent.
- **Fast**: SQLite FTS5 with BM25 ranking — millisecond search on millions of rows.
- **Yours**: local-first, no cloud, no telemetry. `scp` the file to back it up.

## Roadmap

- [ ] Semantic (vector) search via optional embedding providers
- [ ] Memory expiry / TTL
- [ ] Import/export (JSONL)
- [ ] Next.js dashboard (if people ask for it)

## License

MIT
