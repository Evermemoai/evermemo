# Contributing to evermemo

Thanks for helping make evermemo better! It's a small codebase on purpose —
one binary, pure Go, no CGO — and we'd like to keep it that way.

## Development

```sh
git clone https://github.com/Evermemoai/evermemo.git
cd evermemo
go build -o evermemo .
go test ./...
```

Requirements: Go 1.25+. No other toolchain needed.

## Environment setup

evermemo needs **no configuration** to build, test, or run — it defaults to a
local SQLite database with keyword (BM25) search. All environment variables are
optional and only enable extra features (semantic search, consolidation, HTTP
auth, hub mode).

A documented [.env.example](.env.example) lists every variable with local dev
defaults. evermemo does **not** auto-load `.env`, so load it into your shell
first:

```sh
cp .env.example .env       # then edit the values you need
set -a; . ./.env; set +a   # bash / zsh
go run . serve
```

For a fully-offline setup with Ollama (semantic search + consolidation), see
[docs/ollama.md](docs/ollama.md). Never commit a real `.env` — it's gitignored.

## Guidelines

- **Keep it small.** New dependencies need a strong reason; pure-Go only
  (the binary must stay CGO-free and cross-compile cleanly).
- **Tests required.** New behavior comes with tests (`internal/*/..._test.go`);
  `go test ./...` and `go vet ./...` must pass.
- **All interfaces move together.** A change to memory operations should be
  wired through all surfaces it applies to: store → HTTP API → MCP tools →
  CLI → remote client (see `mcp.Backend`).
- **No breaking changes** to the HTTP API, MCP tool schemas, or DB schema
  without a migration path (schema changes go through additive
  `ALTER TABLE` migrations in `store.Open`).

## Pull requests

1. Fork, branch from `main`, make your change.
2. `go test ./... && go vet ./...`
3. Open a PR with a clear description of what and why. Small, focused PRs
   review faster.

## Reporting bugs / requesting features

Use the issue templates. For security issues, see [SECURITY.md](SECURITY.md)
— please do not open public issues for vulnerabilities.
