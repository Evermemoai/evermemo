# Changelog

All notable changes to evermemo are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com); versions follow
[SemVer](https://semver.org).

## [Unreleased]

### Changed
- Module path is now `github.com/Evermemoai/evermemo` (`go install` works).
- Release binaries report their tagged version (`-X main.version`).

### Added
- Dockerfile + multi-arch image published to `ghcr.io/evermemoai/evermemo`.
- Deployment examples: systemd unit, Docker Compose (`deploy/`).
- Community files: CONTRIBUTING, SECURITY, CODE_OF_CONDUCT, issue/PR templates.

## [0.2.0] - 2026-07-26

### Added
- **Hub mode**: central shared memory for all agents, per-agent API keys,
  write provenance on every memory.
- **Interactive TUI** with line editing and history; launches on bare `evermemo`.
- **Semantic search**: hybrid BM25 + embeddings (Ollama / OpenAI-compatible)
  fused with RRF; graceful keyword-only fallback.
- **Memory consolidation** (`evermemo consolidate`): LLM merges duplicates,
  resolves contradictions, archives stale memories — audit-safe (archive+link,
  never delete).
- **Auto-recall proxy** (`evermemo proxy`): injects relevant memories into
  OpenAI/Anthropic chat requests transparently.
- **Memory graphs**: `supersedes` / `relates_to` / `derived_from` links.
- **Confidence + verification**: agents confirm/dispute memories; votes move
  a confidence score.
- **Namespace ACLs** (`EVERMEMO_ACL`) enforced on REST and MCP transports.
- **Streamable HTTP MCP** (`POST /mcp`) — no local binary needed.
- TTL / expiry, update endpoint + tool, JSONL export/import, per-caller rate
  limiting, TLS (`--cert/--key`), hot-reloadable key file, online backup.
- CI + cross-platform release workflows; 40+ tests.

## [0.1.0] - 2026-07-26

### Added
- Initial release: CLI, HTTP API, MCP server over stdio, SQLite+FTS5 store
  with BM25 search.
