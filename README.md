# evermemo

[![CI](https://github.com/Evermemoai/evermemo/actions/workflows/ci.yml/badge.svg)](https://github.com/Evermemoai/evermemo/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/Evermemoai/evermemo)](https://github.com/Evermemoai/evermemo/releases)
[![Go Reference](https://pkg.go.dev/badge/github.com/Evermemoai/evermemo.svg)](https://pkg.go.dev/github.com/Evermemoai/evermemo)
[![Go Report Card](https://goreportcard.com/badge/github.com/Evermemoai/evermemo)](https://goreportcard.com/report/github.com/Evermemoai/evermemo)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

**A tiny, universal memory engine for humans and AI agents.**

One small Go binary. No external services. It's a CLI, an HTTP API, and an MCP server — so *anything* can remember things: your terminal, your scripts, Claude Code, Cursor, or any agent in any industry. Run one as a hub and every agent in your organization shares, synchronizes, and reasons over the same trusted knowledge.

```
┌──────────────┐   ┌─────────────┐   ┌──────────────────┐
│  You (CLI)   │   │  Any app    │   │  AI agents (MCP)  │
│ evermemo add │   │  HTTP API   │   │  Claude, Cursor…  │
└──────┬───────┘   └──────┬──────┘   └────────┬─────────┘
       └──────────────────┼──────────────────┘
                  ┌───────▼──────┐
                  │   evermemo   │  single binary
                  │ SQLite+FTS5  │  BM25 + semantic search
                  └──────────────┘
```

## Install

**Prebuilt binaries** (macOS, Linux, Windows — amd64/arm64):
grab one from [Releases](https://github.com/Evermemoai/evermemo/releases).

**Go:**

```sh
go install github.com/Evermemoai/evermemo@latest
```

**Docker:**

```sh
docker run -v evermemo-data:/data -p 7777:7777 ghcr.io/evermemoai/evermemo:latest
```

**From source:**

```sh
git clone https://github.com/Evermemoai/evermemo.git && cd evermemo
go build -o evermemo .
```

For production hubs, see [deploy/](deploy/) for systemd and Docker Compose
examples, and [SECURITY.md](SECURITY.md) for the hardening checklist.

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

evermemo speaks the Model Context Protocol over stdio, exposing eight tools:
`add_memory`, `update_memory`, `search_memory`, `list_memories`, `get_memory`,
`link_memory`, `verify_memory`, `delete_memory`.

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

## Trusted knowledge: provenance, confidence, graphs, ACLs

Every memory records **who wrote it**. On top of that:

- **Verification** — agents confirm or dispute each other's memories
  (`verify_memory` tool, `POST /v1/memories/{id}/verify`). Votes move a
  confidence score (starts 0.6; +0.10 per confirm, −0.15 per dispute).
- **Memory graphs** — link memories with `supersedes`, `relates_to`, or
  `derived_from` (`link_memory` tool, `evermemo link <from> <rel> <to>`)
  to trace how knowledge evolved. Links come back on `GET /v1/memories/{id}`.
- **Namespace ACLs** — restrict which agents can read/write which namespaces:

```sh
EVERMEMO_ACL='finbot:finance:rw,hrbot:hr:rw,hrbot:finance:r,auditor:*:r' \
EVERMEMO_AGENT_KEYS='finbot:key1,hrbot:key2,auditor:key3' \
evermemo serve
```

Enforced on both the REST API and the `/mcp` transport. No ACL set = open.

## Production hub: TLS, key rotation, backups

```sh
# HTTPS (or terminate TLS in Caddy/nginx in front)
evermemo serve --cert cert.pem --key key.pem

# Hot-reloading keys file: add/rotate/revoke agent keys without restart
cat > keys.txt <<EOF
# agent:key, one per line
claude:key1
cursor:key2
EOF
evermemo serve --keys-file keys.txt   # edits picked up automatically

# Consistent online snapshot (safe while serving; uses SQLite VACUUM INTO)
evermemo backup /backups/evermemo-$(date +%F).db
```

⚠️ Bearer keys travel in cleartext over plain HTTP — always use TLS (built-in
or a reverse proxy) when the hub is reachable beyond localhost.

## Memory consolidation (LLM-powered hygiene)

Over time memories accumulate duplicates and contradictions. Point evermemo
at a chat LLM and let it clean up:

```sh
export EVERMEMO_LLM_URL=http://localhost:11434   # Ollama; or any OpenAI-compatible API
evermemo consolidate --ns default --dry-run      # see the plan
evermemo consolidate --ns default                # apply it
```

The LLM merges duplicates, resolves contradictions (newest wins), and
archives stale memories. Nothing is deleted: sources are archived (hidden
from search, kept for audit) and linked to their replacement with
`derived_from`/`supersedes`.

## Auto-recall proxy (memory without tools)

Put evermemo between your app and the LLM API, and relevant memories are
injected into every chat request automatically — no `search_memory` calls
needed:

```sh
evermemo proxy --target https://api.openai.com --addr :8788
# then point your SDK at http://localhost:8788 instead of api.openai.com
```

Works with OpenAI-style (`/v1/chat/completions`) and Anthropic-style
(`/v1/messages`) APIs, streams SSE responses through, and passes all other
routes untouched. Use `--remote https://your-hub:7777` to recall from the
shared hub.

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
| `EVERMEMO_LLM_URL` | *(unset)*                | Chat LLM for `consolidate`       |
| `EVERMEMO_LLM_MODEL` | provider default       | Chat model name                  |
| `EVERMEMO_LLM_API_KEY` | *(unset)*            | Key for OpenAI-compatible chat providers |
| `EVERMEMO_ACL`     | *(unset)*                | Namespace ACLs: `agent:ns:perm` (r/rw, `*` wildcards) |
| `EVERMEMO_TLS_CERT` / `EVERMEMO_TLS_KEY` | *(unset)* | TLS cert/key files for `serve` |
| `EVERMEMO_KEYS_FILE` | *(unset)*              | Hot-reloading agent keys file    |

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
- [ ] Web dashboard (browse, search, audit, graphs)
- [ ] Webhooks / change subscriptions (reactive memory)
- [ ] Local-first replicas with hub sync

## Contributing

Contributions welcome — see [CONTRIBUTING.md](CONTRIBUTING.md).
Found a security issue? Please follow [SECURITY.md](SECURITY.md).

## License

[MIT](LICENSE)
