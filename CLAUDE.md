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

# Local Qdrant (required for full integration tests)
docker compose up -d
```

## Architecture

OpenClaw Cortex is a hybrid semantic memory system for AI agents. It stores memories in Qdrant (vector DB over gRPC) and retrieves them using multi-factor ranking. All external services (Qdrant, Ollama, Claude API) are wired together only in `cmd/`; `internal/` packages accept interfaces.

**Layered call flow for a `recall` command:**
```
cmd/cmd_recall.go
  → recall.Recaller          (internal/recall/)   — multi-factor scoring
  → embedder.Embedder        (internal/embedder/) — Ollama HTTP, 768-dim nomic-embed-text
  → store.Store              (internal/store/)    — Qdrant gRPC CRUD + vector search
  → tokenizer.FormatMemoriesWithBudget (pkg/tokenizer/) — trim to token budget
```

**Capture flow** (`cmd capture`):
```
cmd/cmd_capture.go
  → capture.Capturer         (internal/capture/)  — Claude Haiku extracts JSON memories
  → classifier.Classifier    (internal/classifier/) — heuristic keyword scoring → MemoryType
  → store.Store              — dedup via cosine similarity, then upsert
```

**Lifecycle flow** (`cmd consolidate`):
```
cmd/cmd_lifecycle.go
  → lifecycle.Manager        (internal/lifecycle/) — TTL expiry + session decay (24h)
```

### Key Interfaces

| Interface | File | Implementations |
|-----------|------|-----------------|
| `store.Store` | `internal/store/store.go` | `QdrantStore` (gRPC), `MockStore` (tests) |
| `embedder.Embedder` | `internal/embedder/embedder.go` | `OllamaEmbedder` (HTTP) |
| `classifier.Classifier` | `internal/classifier/classifier.go` | `HeuristicClassifier` |
| `capture.Capturer` | `internal/capture/capture.go` | `ClaudeCapturer` |

### Core Data Model (`internal/models/memory.go`)

`Memory` is the central struct. Key fields:
- `Type` — `rule | fact | episode | procedure | preference` (affects recall boost)
- `Scope` — `permanent | project | session | ttl` (affects expiry + recall boost)
- `Confidence` — `0.0–1.0`; memories below 0.5 are filtered on capture
- `LastAccessed` / `AccessCount` — updated on every retrieval for recency/frequency scoring
- `Project` — used to boost `scope=project` memories in context

### Recall Scoring (`internal/recall/recall.go`)

Final score is a weighted sum:
```
0.5 × similarity + 0.2 × recency + 0.1 × frequency + 0.1 × typeBoost + 0.1 × scopeBoost
```
Type multipliers: rule=1.5, procedure=1.3, fact=1.0, episode=0.8, preference=0.7.
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
| Anthropic API | HTTPS | Memory extraction via Claude Haiku |

Set `ANTHROPIC_API_KEY` for capture commands. Config is loaded from `~/.cortex/config.yaml` with env var overrides prefixed `OPENCLAW_CORTEX_` (e.g., `OPENCLAW_CORTEX_QDRANT_HOST`).
