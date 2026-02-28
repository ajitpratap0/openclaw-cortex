# OpenClaw Cortex

[![Go Version](https://img.shields.io/github/go-mod/go-version/ajitpratap0/openclaw-cortex)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/ajitpratap0/openclaw-cortex)](https://goreportcard.com/report/github.com/ajitpratap0/openclaw-cortex)
[![GoDoc](https://pkg.go.dev/badge/github.com/ajitpratap0/openclaw-cortex)](https://pkg.go.dev/github.com/ajitpratap0/openclaw-cortex)
[![Docs](https://img.shields.io/badge/docs-online-blue)](https://ajitpratap0.github.io/openclaw-cortex/)

**Persistent, semantically searchable memory for AI agents — across sessions, projects, and context windows.**

OpenClaw Cortex gives Claude and other AI agents long-term memory. It captures important information from conversations, classifies it by type and scope, and retrieves the most relevant context for each new turn — all within your token budget.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/ajitpratap0/openclaw-cortex/main/scripts/install.sh | bash
```

Or build from source (requires Go 1.23+):

```bash
git clone https://github.com/ajitpratap0/openclaw-cortex
cd openclaw-cortex
go build -o bin/openclaw-cortex ./cmd/openclaw-cortex
```

## 3-Command Quickstart

```bash
docker compose up -d                                           # start Qdrant vector store
ollama pull nomic-embed-text                                   # pull the embedding model
openclaw-cortex store "Always run tests before merging" \
  --type rule --scope permanent                                # store your first memory
```

Then recall relevant context in your next session:

```bash
openclaw-cortex recall "What are the testing requirements?" --budget 2000
```

## Why OpenClaw Cortex?

| Feature | Naive conversation history | OpenClaw Cortex |
|---------|--------------------------|-----------------|
| Token limit | Hits context window, truncates | Token-budgeted recall: always fits |
| Search | Sequential scan / none | Semantic vector search |
| Ranking | Chronological only | Similarity + recency + frequency + type + scope |
| Memory expiry | Manual | TTL, session decay, lifecycle consolidation |
| Entity tracking | None | Automatic from conversation capture |
| Cross-session | Context window only | Persists in Qdrant across all sessions |
| API access | None | REST API + MCP server |
| Project isolation | None | Per-project scoping and boosting |

## Architecture

```
┌──────────────────────────────────────────────────────────┐
│                   Claude / AI Agent                       │
│                                                          │
│   Pre-Turn Hook ──> Recall ──> Inject context            │
│   Post-Turn Hook ──> Capture ──> Store memories          │
└──────────┬───────────────────────────────┬───────────────┘
           │                               │
           ▼                               ▼
  CLI / HTTP API / MCP             Hook Integration
  (index search recall             (Pre/Post Turn,
   capture store consolidate)       graceful degradation)
           │                               │
           └──────────────┬───────────────┘
                          │
              ┌───────────▼────────────┐
              │      Core Engine        │
              │  Indexer  Capturer      │
              │  Recaller Classifier    │
              │  Lifecycle Manager      │
              └────────┬────────────────┘
                       │
           ┌───────────▼──────────────┐
           │  Embedder    Store        │
           │  (Ollama)    (Qdrant gRPC)│
           │  768-dim     vectors      │
           └───────────────────────────┘
```

## Features

- **Semantic recall**: Vector similarity search (Qdrant gRPC, 768-dim `nomic-embed-text`)
- **Smart capture**: Claude Haiku extracts structured memories from conversation turns
- **Multi-factor ranking**: Similarity 50% + recency 20% + frequency 10% + type 10% + scope 10%
- **Token-aware output**: Recalled memories trimmed to fit your token budget
- **Deduplication**: Cosine similarity dedup (threshold: 0.92) prevents redundant storage
- **Memory types**: `rule` (1.5x) / `procedure` (1.3x) / `fact` (1.0x) / `episode` (0.8x) / `preference` (0.7x)
- **Lifecycle management**: TTL expiry, session decay, consolidation
- **Claude Code hooks**: Pre/post-turn hooks with graceful degradation
- **HTTP API**: REST endpoints for any LLM pipeline
- **MCP server**: Native Model Context Protocol support for Claude Desktop

## Documentation

Full documentation: **https://ajitpratap0.github.io/openclaw-cortex/**

| Guide | Description |
|-------|-------------|
| [Quickstart](https://ajitpratap0.github.io/openclaw-cortex/quickstart/) | End-to-end setup in 5 minutes |
| [Architecture](https://ajitpratap0.github.io/openclaw-cortex/architecture/) | Layered call flows, data model, scoring formula |
| [Claude Code Hooks](https://ajitpratap0.github.io/openclaw-cortex/hooks/) | Automatic memory for every conversation |
| [HTTP API](https://ajitpratap0.github.io/openclaw-cortex/api/) | REST API reference |
| [MCP Server](https://ajitpratap0.github.io/openclaw-cortex/mcp/) | Claude Desktop integration |
| [Benchmarks](https://ajitpratap0.github.io/openclaw-cortex/benchmarks/) | Latency and throughput characteristics |

## Configuration

Configuration is loaded from (in order of precedence):
1. Environment variables (prefixed `OPENCLAW_CORTEX_`)
2. `~/.openclaw-cortex/config.yaml`
3. Built-in defaults

```yaml
qdrant:
  host: localhost
  grpc_port: 6334

ollama:
  base_url: http://localhost:11434
  model: nomic-embed-text

memory:
  dedup_threshold: 0.92
  default_ttl_hours: 720
```

| Variable | Default | Description |
|----------|---------|-------------|
| `ANTHROPIC_API_KEY` | — | Required for `capture` (Claude Haiku extraction) |
| `OPENCLAW_CORTEX_QDRANT_HOST` | `localhost` | Qdrant hostname |
| `OPENCLAW_CORTEX_QDRANT_GRPC_PORT` | `6334` | Qdrant gRPC port |
| `OPENCLAW_CORTEX_OLLAMA_BASE_URL` | `http://localhost:11434` | Ollama endpoint |

## Claude Code Integration

Add to `.claude/settings.json` in your project:

```json
{
  "hooks": {
    "PreTurn": [{
      "hooks": [{
        "type": "command",
        "command": "echo '{\"message\": \"{{HUMAN_TURN}}\", \"project\": \"my-project\", \"token_budget\": 2000}' | openclaw-cortex hook pre"
      }]
    }],
    "PostTurn": [{
      "hooks": [{
        "type": "command",
        "command": "echo '{\"user_message\": \"{{HUMAN_TURN}}\", \"assistant_message\": \"{{ASSISTANT_TURN}}\", \"session_id\": \"{{SESSION_ID}}\", \"project\": \"my-project\"}' | openclaw-cortex hook post"
      }]
    }]
  }
}
```

Both hooks exit with code 0 even if services are unavailable — Claude is never blocked.

## CLI Reference

```bash
# Store a memory
openclaw-cortex store "Always run tests before merging" --type rule --scope permanent

# Recall with token budget
openclaw-cortex recall "deployment process" --budget 2000 --project myapp

# Capture memories from a conversation turn
openclaw-cortex capture \
  --user "How do I handle errors?" \
  --assistant "Always wrap errors with fmt.Errorf and %w..."

# Index markdown memory files
openclaw-cortex index --path ~/.openclaw/workspace/memory/

# Search (raw similarity, no re-ranking)
openclaw-cortex search "error handling" --type rule --limit 5

# View stats
openclaw-cortex stats

# Run lifecycle management (TTL expiry, decay, consolidation)
openclaw-cortex consolidate

# Start HTTP API server (default :8080, configure via OPENCLAW_CORTEX_API_LISTEN_ADDR)
openclaw-cortex serve

# Start MCP server (for Claude Desktop)
openclaw-cortex mcp
```

## Development

```bash
# Run all tests (race detector enabled)
go test -v -race -count=1 ./...

# Short tests only (no external services needed)
go test -short -count=1 ./...

# Lint
golangci-lint run ./...

# Build
go build -o bin/openclaw-cortex ./cmd/openclaw-cortex

# Using Taskfile
task test
task lint
task build
```

## Project Structure

```
openclaw-cortex/
├── cmd/openclaw-cortex/    # CLI entrypoint (Cobra); all wiring of interfaces
├── internal/
│   ├── api/                # HTTP API server (REST endpoints)
│   ├── capture/            # Claude Haiku memory extraction + conflict detection
│   ├── classifier/         # Heuristic keyword scoring -> MemoryType
│   ├── config/             # Viper-based configuration
│   ├── embedder/           # Embedder interface + Ollama HTTP implementation
│   ├── hooks/              # Pre/post-turn hook handlers
│   ├── indexer/            # Markdown tree walker + section summarizer
│   ├── lifecycle/          # TTL expiry, session decay, consolidation
│   ├── mcp/                # MCP server (remember/recall/forget/search/stats)
│   ├── metrics/            # In-process counters
│   ├── models/             # Memory struct and type definitions
│   ├── recall/             # Multi-factor ranker + optional Claude re-ranker
│   └── store/              # Store interface, Qdrant gRPC, MockStore
├── pkg/tokenizer/          # Token estimation and budget-aware formatting
├── tests/                  # Black-box test suite (no live services needed)
├── docs/                   # MkDocs documentation source
├── scripts/install.sh      # Binary installer
├── k8s/qdrant.yaml         # Kubernetes StatefulSet
├── docker-compose.yml      # Local Qdrant
└── Dockerfile              # Multi-stage build
```

## Tech Stack

- **Go 1.23+** with structured logging (`slog`)
- **Qdrant** vector database (gRPC via `github.com/qdrant/go-client`)
- **Ollama** for local embeddings (`nomic-embed-text`, 768 dimensions)
- **Claude Haiku** for memory extraction (`github.com/anthropics/anthropic-sdk-go`)
- **Cobra** + **Viper** for CLI and configuration
- **mcp-go** for Model Context Protocol server

## Contributing

Contributions are welcome. Please read [CONTRIBUTING.md](CONTRIBUTING.md) first.

```bash
# Start a feature branch
git checkout -b feat/short-description

# Make changes, then verify
go test -short -race -count=1 ./...
golangci-lint run ./...

# Push and open a PR
git push -u origin feat/short-description
gh pr create --title "feat: ..." --body "..."
```

Branch naming: `feat/<topic>`, `fix/<topic>`, `refactor/<topic>`, `test/<topic>`.

`main` is protected — direct pushes are blocked. All changes go through a PR with CI checks.

## License

MIT — see [LICENSE](LICENSE).
