# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Build
go build -o bin/openclaw-cortex ./cmd/openclaw-cortex
# or: task build

# Test (all, with race detector)
go test -v -race -count=1 ./...
# Short tests only (no external services needed):
go test -short -count=1 ./...
# Single package:
go test -v -run TestName ./internal/recall/
# Coverage:
go test -coverprofile=coverage.out -coverpkg=./... ./...

# Lint
golangci-lint run ./...
# or: task lint

# Format
gofmt -w . && goimports -w .

# Local services (Qdrant required, Neo4j optional for graph features)
docker compose up -d
# Health check (verifies Qdrant, Ollama, Claude LLM, Neo4j)
openclaw-cortex health
```

## Architecture

OpenClaw Cortex is a hybrid semantic memory system for AI agents. It stores memories in Qdrant (vector DB over gRPC) and retrieves them using multi-factor ranking. An optional Neo4j entity-relationship graph provides bi-temporal fact storage and graph-augmented recall. All external services (Qdrant, Ollama, Claude/Gateway, Neo4j) are wired together only in `cmd/`; `internal/` packages accept interfaces.

**Layered call flow for a `recall` command:**
```
cmd/cmd_recall.go
  → recall.Recaller          (internal/recall/)   — multi-factor scoring + optional graph merge
  → graph.Client             (internal/graph/)    — Neo4j graph recall (optional, latency-budgeted)
  → embedder.Embedder        (internal/embedder/) — Ollama HTTP, 768-dim nomic-embed-text
  → store.Store              (internal/store/)    — Qdrant gRPC CRUD + vector search
  → tokenizer.FormatMemoriesWithBudget (pkg/tokenizer/) — trim to token budget
```

**Capture flow** (`cmd capture`):
```
cmd/cmd_capture.go
  → capture.Capturer         (internal/capture/)  — Claude Haiku extracts JSON memories
  → capture.EntityExtractor  (internal/capture/)  — Claude Haiku extracts entities
  → graph.FactExtractor      (internal/graph/)    — Claude Haiku extracts relationship facts
  → classifier.Classifier    (internal/classifier/) — heuristic keyword scoring → MemoryType
  → store.Store              — dedup via cosine similarity, then upsert
  → graph.Client             — entity/fact upsert to Neo4j (optional)
```

**Lifecycle flow** (`cmd consolidate`):
```
cmd/cmd_lifecycle.go
  → lifecycle.Manager        (internal/lifecycle/) — TTL expiry + session decay (24h)
```

### LLM Client Abstraction (`internal/llm/`)

All LLM calls go through the `llm.LLMClient` interface:
```go
type LLMClient interface {
    Complete(ctx, model, systemPrompt, userMessage string, maxTokens int) (string, error)
}
```
Two implementations:
- `AnthropicClient` — direct Anthropic SDK calls (requires `ANTHROPIC_API_KEY`)
- `GatewayClient` — routes through OpenClaw gateway's OpenAI-compatible endpoint (for Max plan / subscription users)

The factory `llm.NewClient(cfg.Claude)` picks the right implementation based on config.

### Key Interfaces

| Interface | File | Implementations |
|-----------|------|-----------------|
| `store.Store` | `internal/store/store.go` | `QdrantStore` (gRPC), `MockStore` (tests) |
| `embedder.Embedder` | `internal/embedder/embedder.go` | `OllamaEmbedder` (HTTP) |
| `classifier.Classifier` | `internal/classifier/classifier.go` | `HeuristicClassifier` |
| `capture.Capturer` | `internal/capture/capture.go` | `ClaudeCapturer` |
| `llm.LLMClient` | `internal/llm/client.go` | `AnthropicClient`, `GatewayClient` |
| `graph.Client` | `internal/graph/client.go` | `Neo4jClient`, `MockGraphClient` |

### Core Data Model (`internal/models/memory.go`)

`Memory` is the central struct. Key fields:
- `Type` — `rule | fact | episode | procedure | preference` (affects recall boost)
- `Scope` — `permanent | project | session | ttl` (affects expiry + recall boost)
- `Confidence` — `0.0–1.0`; memories below 0.5 are filtered on capture
- `LastAccessed` / `AccessCount` — updated on every retrieval for recency/frequency scoring
- `Project` — used to boost `scope=project` memories in context

### Recall Scoring (`internal/recall/recall.go`)

Final score is a weighted sum of 8 factors:
```
0.35 × similarity + 0.15 × recency + 0.10 × frequency + 0.10 × typeBoost + 0.08 × scopeBoost + 0.10 × confidence + 0.07 × reinforcement + 0.05 × tagAffinity
```
Multiplicative penalties are applied after the weighted sum: superseded memories ×0.3, active conflicts ×0.8.

Type multipliers: rule=1.5, procedure=1.3, fact=1.0, episode=0.8, preference=0.7.
Confidence treats values < 0.01 as "unknown" (legacy memories) and substitutes 0.7. Reinforcement uses log-scaled reinforcement count. Tag affinity measures query-word overlap with memory tags.
Recency uses exponential decay with a 7-day half-life.

## Branch Workflow

**`main` is protected** — direct pushes are blocked. All changes must go through a PR.

```bash
# Start a new feature or fix
git checkout -b feat/short-description   # or fix/short-description
# ... make changes ...
go test -short -race -count=1 ./...      # must pass
golangci-lint run ./...                  # must be clean
git push -u origin feat/short-description
gh pr create --title "..." --body "..."
```

Branch naming: `feat/<topic>`, `fix/<topic>`, `refactor/<topic>`, `test/<topic>`.

Required to merge:
- CI passes: `test (ubuntu-latest, 1.23)` and `test (macos-latest, 1.23)`
- PR must be open (no direct pushes to `main`)
- Force-pushes and branch deletion are blocked
- Linear history required (rebase, no merge commits)

## Conventions

- **Error wrapping**: always `fmt.Errorf("context: %w", err)` — never bare `err` returns from internal functions.
- **Context propagation**: every function that touches Qdrant, Ollama, or Claude accepts `ctx context.Context` as the first argument.
- **Tests live in `tests/`**: all test files are in the top-level `tests/` package (black-box testing), not co-located with the package under test. Use `MockStore` from `internal/store/mock_store.go` to avoid requiring live Qdrant.
- **Prompt injection prevention**: user/assistant content is XML-escaped in `internal/capture/capture.go` before interpolation into the Claude prompt. Maintain this for any new LLM-calling code.
- **Linter**: golangci-lint v2 with `linters.settings` (not top-level `settings`) and `linters.exclusions.rules` (not `issues.exclude-rules`). Test files are excluded from `errcheck` and `unparam`.

## External Service Dependencies

| Service | Default address | Purpose |
|---------|----------------|---------|
| Qdrant | `localhost:6334` (gRPC) | Vector storage and search |
| Ollama | `http://localhost:11434` | Embeddings (`nomic-embed-text`, 768-dim) |
| Claude LLM | Anthropic API or OpenClaw gateway | Memory extraction, entity/fact extraction, resolution |
| Neo4j | `bolt://localhost:7687` | Entity-relationship graph (optional, `graph.enabled`) |

### LLM Authentication (choose one)

1. **API key**: Set `ANTHROPIC_API_KEY` env var or `claude.api_key` in config
2. **OpenClaw gateway** (for Max plan users): Set `claude.gateway_url` and `claude.gateway_token` in config — routes through `http://127.0.0.1:18789/v1/chat/completions`

Config is loaded from `~/.openclaw-cortex/config.yaml` with env var overrides prefixed `OPENCLAW_CORTEX_` (e.g., `OPENCLAW_CORTEX_QDRANT_HOST`).

### Versioning

Plugin version (`PLUGIN_VERSION` in `extensions/openclaw-plugin/index.ts`) and binary version (`version` in `cmd/openclaw-cortex/main.go`) should be kept in sync. On startup, the plugin logs both versions and warns on mismatch. Check with `openclaw cortex version`.
