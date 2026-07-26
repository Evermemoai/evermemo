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
echo "Deploy runs at 6pm UTC" | evermemo add --tags ops --ttl 7d   # expires in 7 days
evermemo search "deploy time"
evermemo list
evermemo get mem_a1b2c3d4e5f60718
evermemo update mem_a1b2c3d4e5f60718 "Deploy runs at 7pm UTC now"
evermemo delete mem_a1b2c3d4e5f60718
evermemo export > memories.jsonl        # backup / migrate
evermemo import < memories.jsonl
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
| POST   | `/v1/memories`       | Create: `{content, tags?, namespace?, metadata?, ttl?}` |
| GET    | `/v1/memories?q=...` | Search (hybrid ranked); omit `q` to list |
| GET    | `/v1/memories/{id}`  | Get one                              |
| PUT    | `/v1/memories/{id}`  | Update: `{content?, tags?}`          |
| DELETE | `/v1/memories/{id}`  | Delete                               |
| POST   | `/mcp`               | MCP over HTTP (JSON-RPC) — no local binary needed |

```sh
curl -X POST localhost:7777/v1/memories \
  -d '{"content":"Invoices are due net-30","tags":["billing"]}'

curl "localhost:7777/v1/memories?q=invoice+due"
```

Set `EVERMEMO_API_KEY=secret` to require `Authorization: Bearer secret` on all `/v1` routes.

## MCP (Claude Code, Cursor, any agent)

evermemo speaks the Model Context Protocol over stdio, exposing five tools:
`add_memory`, `update_memory`, `search_memory`, `list_memories`, `delete_memory`.

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
vars work as flag defaults. Set `EVERMEMO_RATE=120` to cap each caller at 120
requests/minute. Agents can also talk MCP straight to the hub over HTTP
(`POST /mcp`) — no local binary required.

## Semantic search (optional)

By default search is SQLite FTS5 with BM25 ranking — fast, offline, zero
dependencies. Point evermemo at an embedding provider and search becomes
**hybrid**: BM25 + cosine similarity, fused with Reciprocal Rank Fusion, so
“when do we deploy” finds “release schedule is thursdays”.

```sh
# Ollama (local, free)
export EVERMEMO_EMBED_URL=http://localhost:11434
export EVERMEMO_EMBED_MODEL=nomic-embed-text   # default

# …or any OpenAI-compatible API
export EVERMEMO_EMBED_URL=https://api.openai.com
export EVERMEMO_EMBED_API_KEY=sk-…
```

Memories are embedded on write; if the provider is down, search silently
falls back to keyword-only.

## Configuration

| Env var            | Default                  | Description                      |
| ------------------ | ------------------------ | -------------------------------- |
| `EVERMEMO_DB`      | `~/.evermemo/evermemo.db` | Database file path              |
| `EVERMEMO_API_KEY` | *(unset)*                | If set, HTTP API requires bearer auth |
| `EVERMEMO_AGENT_KEYS` | *(unset)*             | Per-agent keys: `alice:key1,bob:key2` |
| `EVERMEMO_RATE`    | *(unset)*                | Max requests/min per caller (0/unset = off) |
| `EVERMEMO_REMOTE`  | *(unset)*                | Central hub URL for `mcp` mode   |
| `EVERMEMO_AGENT`   | *(unset)*                | Agent name recorded as provenance |
| `EVERMEMO_EMBED_URL` | *(unset)*              | Embedding provider URL (enables semantic search) |
| `EVERMEMO_EMBED_MODEL` | provider default     | Embedding model name             |
| `EVERMEMO_EMBED_API_KEY` | *(unset)*          | Key for OpenAI-compatible providers |
| `EVERMEMO_EMBED_PROVIDER` | `ollama`          | `ollama` or `openai`             |

Every command also accepts `--db` to point at a specific database, and `--ns`/`namespace` to partition memories (per project, per user, per agent — your call).

## Why

- **Small**: one binary, one SQLite file, zero dependencies to run.
- **Universal**: CLI for humans, HTTP for any language, MCP for any agent.
- **Fast**: SQLite FTS5 with BM25 ranking — millisecond search on millions of rows.
- **Yours**: local-first, no cloud, no telemetry. `scp` the file to back it up.

## Roadmap

- [x] Semantic (vector) search via optional embedding providers
- [x] Memory expiry / TTL
- [x] Import/export (JSONL)
- [x] Streamable HTTP MCP transport
- [ ] Next.js dashboard (if people ask for it)

## License

MIT
