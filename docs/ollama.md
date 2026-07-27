# Running evermemo fully offline with Ollama

evermemo works with zero AI dependencies out of the box (SQLite FTS5 + BM25).
Point it at [Ollama](https://ollama.com) and you unlock **semantic search**,
**LLM-powered consolidation**, and the **auto-recall proxy** — all running
100% locally. Nothing leaves your machine, no API keys, no cloud, no cost.

```
┌───────────┐        ┌───────────┐        ┌──────────────┐
│  evermemo │  HTTP  │  Ollama   │  runs  │  local models │
│  (binary) ├───────►│ :11434    ├───────►│ embed + chat  │
└───────────┘        └───────────┘        └──────────────┘
        everything stays on localhost — fully offline
```

---

## 1. Install Ollama

**macOS / Windows:** download the installer from <https://ollama.com/download>.

**Linux:**

```sh
curl -fsSL https://ollama.com/install.sh | sh
```

Verify it's running (Ollama serves on `http://localhost:11434` by default):

```sh
ollama --version
curl http://localhost:11434/api/tags   # should return JSON
```

If the server isn't running, start it with `ollama serve` (macOS/Windows apps
start it automatically).

---

## 2. Pull the models

evermemo uses two kinds of models. Pull the ones for the features you want.

| Feature                     | Model kind | Recommended (default)      | Pull command                        |
| --------------------------- | ---------- | -------------------------- | ----------------------------------- |
| Semantic search             | Embedding  | `nomic-embed-text`         | `ollama pull nomic-embed-text`      |
| Consolidation / proxy chat  | Chat       | `llama3.2`                 | `ollama pull llama3.2`              |

```sh
# semantic search (small, fast, ~275MB)
ollama pull nomic-embed-text

# chat model for consolidate + proxy (~2GB; use a bigger one if you have RAM)
ollama pull llama3.2
```

Good alternatives:

- **Embeddings:** `mxbai-embed-large` (higher quality), `all-minilm` (tiny)
- **Chat:** `qwen2.5`, `mistral`, `gpt-oss:20b` (needs more RAM, great quality)

> **Note on switching embedding models:** the vector dimension is part of the
> index. If you change the embedding model after storing memories, re-embed by
> re-importing (`evermemo export > m.jsonl && evermemo import < m.jsonl`) so old
> and new vectors are consistent.

---

## 3. Configure evermemo

Set the environment variables for the features you want. `EVERMEMO_*_PROVIDER`
defaults to `ollama` when no API key is set, so you usually only need the URL.

### Semantic search (embeddings)

```sh
export EVERMEMO_EMBED_URL=http://localhost:11434
export EVERMEMO_EMBED_MODEL=nomic-embed-text   # optional; this is the default
```

Memories are embedded on write. Search becomes **hybrid**: BM25 keyword ranking
fused with cosine similarity (Reciprocal Rank Fusion), so "when do we deploy"
matches "release schedule is thursdays". If Ollama is unreachable, search
silently falls back to keyword-only — it never breaks.

### Consolidation (LLM hygiene)

```sh
export EVERMEMO_LLM_URL=http://localhost:11434
export EVERMEMO_LLM_MODEL=llama3.2             # optional; this is the default
```

### Auto-recall proxy

The proxy uses the embedding config above to find relevant memories and inject
them into every chat request. No extra model needed beyond the embedder.

---

## 4. Try it

```sh
# add a few memories (these get embedded via Ollama automatically)
evermemo add "Release schedule is every Thursday at 6pm UTC" --tags ops
evermemo add "Payments run on Stripe, invoices are net-30" --tags billing

# semantic search — matches by meaning, not just keywords
evermemo search "when do we ship"        # finds the Thursday release memory
evermemo search "how do customers pay"   # finds the Stripe memory

# LLM-powered cleanup: merge duplicates, resolve contradictions
evermemo consolidate --ns default --dry-run   # preview the plan
evermemo consolidate --ns default             # apply it

# auto-recall proxy in front of a local Ollama chat endpoint
evermemo proxy --target http://localhost:11434 --addr :8788
# now point your client at http://localhost:8788 and relevant memories
# are injected into every request automatically
```

---

## 5. Make it permanent

Add the exports to your shell profile so they persist across sessions.

**bash / zsh** (`~/.bashrc` or `~/.zshrc`):

```sh
# evermemo + Ollama (fully local)
export EVERMEMO_EMBED_URL=http://localhost:11434
export EVERMEMO_EMBED_MODEL=nomic-embed-text
export EVERMEMO_LLM_URL=http://localhost:11434
export EVERMEMO_LLM_MODEL=llama3.2
```

**PowerShell** (`$PROFILE`):

```powershell
$env:EVERMEMO_EMBED_URL   = "http://localhost:11434"
$env:EVERMEMO_EMBED_MODEL = "nomic-embed-text"
$env:EVERMEMO_LLM_URL     = "http://localhost:11434"
$env:EVERMEMO_LLM_MODEL   = "llama3.2"
```

### Docker Compose (evermemo + Ollama together)

```yaml
services:
  ollama:
    image: ollama/ollama:latest
    volumes:
      - ollama:/root/.ollama
    # GPU optional — see Ollama docs for device reservations

  evermemo:
    image: ghcr.io/evermemoai/evermemo:latest
    depends_on: [ollama]
    ports:
      - "7777:7777"
    environment:
      EVERMEMO_EMBED_URL: http://ollama:11434
      EVERMEMO_EMBED_MODEL: nomic-embed-text
      EVERMEMO_LLM_URL: http://ollama:11434
      EVERMEMO_LLM_MODEL: llama3.2
    volumes:
      - evermemo-data:/data

volumes:
  ollama:
  evermemo-data:
```

Pull the models into the running Ollama container once:

```sh
docker compose exec ollama ollama pull nomic-embed-text
docker compose exec ollama ollama pull llama3.2
```

---

## Configuration reference

| Env var                  | Default             | Description                                   |
| ------------------------ | ------------------- | --------------------------------------------- |
| `EVERMEMO_EMBED_URL`     | *(unset)*           | Ollama URL — enables semantic search          |
| `EVERMEMO_EMBED_MODEL`   | `nomic-embed-text`  | Embedding model name                          |
| `EVERMEMO_EMBED_PROVIDER`| `ollama`            | `ollama` or `openai`                          |
| `EVERMEMO_LLM_URL`       | *(unset)*           | Ollama URL — enables `consolidate`            |
| `EVERMEMO_LLM_MODEL`     | `llama3.2`          | Chat model name                               |
| `EVERMEMO_LLM_PROVIDER`  | `ollama`            | `ollama` or `openai`                          |

The provider defaults to `ollama` whenever the matching `*_API_KEY` is unset,
so pointing evermemo at a local Ollama needs only the `*_URL` variable.

---

## Troubleshooting

| Symptom                                   | Fix                                                                 |
| ----------------------------------------- | ------------------------------------------------------------------- |
| Search returns only keyword matches       | `EVERMEMO_EMBED_URL` unset, or Ollama not running (it falls back)   |
| `connection refused`                      | Start Ollama (`ollama serve`) and confirm the port (`:11434`)       |
| `model "..." not found`                   | `ollama pull <model>` first                                         |
| Old memories don't match semantically     | They were stored before embeddings were on — re-import to embed     |
| Slow first query                          | Ollama lazy-loads the model into RAM on first call; warms up after  |
| Docker: `connection refused` to Ollama    | Use the service name (`http://ollama:11434`), not `localhost`       |

Check what's loaded:

```sh
ollama list                       # models you've pulled
curl http://localhost:11434/api/tags
```

---

## Why local?

- **Private** — memories and prompts never leave your machine.
- **Free** — no per-token billing, no API keys.
- **Offline** — works on a plane, in an air-gapped network, anywhere.
- **Portable** — one evermemo binary + Ollama; your data is a single SQLite file.

That's the whole point of evermemo: your memory, your machine, no cloud behind it.
```