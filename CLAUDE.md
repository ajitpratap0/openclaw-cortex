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

# Local services (Memgraph required)
docker compose up -d
# Health check (verifies Memgraph, Ollama, Claude LLM)
openclaw-cortex health
```

## Architecture

OpenClaw Cortex is a hybrid semantic memory system for AI agents. It stores memories and entity-relationship facts in Memgraph (graph DB with vector search) and retrieves them using multi-factor ranking. All external services (Memgraph, Ollama, Claude/Gateway) are wired together only in `cmd/`; `internal/` packages accept interfaces.

**Layered call flow for a `recall` command (graph-aware, RRF merge):**
```
cmd/cmd_recall.go
  → recall.Recaller          (internal/recall/)      — multi-factor scoring + RRF graph merge
  → memgraph.Client          (internal/memgraph/)    — entity-seeded graph walks (configurable depth)
  → embedder.Embedder        (internal/embedder/)    — Ollama HTTP, 768-dim nomic-embed-text
  → memgraph.Client          (internal/memgraph/)    — vector search + graph traversal
  → recall.RRFMerge          (internal/recall/)      — Reciprocal Rank Fusion of vector + graph results
  → tokenizer.FormatMemoriesWithBudget (pkg/tokenizer/) — trim to token budget
```

**Capture flow** (`cmd capture`) with contradiction detection and temporal versioning:
```
cmd/cmd_capture.go
  → capture.Capturer         (internal/capture/)     — Claude Haiku extracts JSON memories
  → capture.EntityExtractor  (internal/capture/)     — Claude Haiku extracts entities
  → capture.ContradictionDetector (internal/capture/) — vector similarity + keyword heuristic contradiction check
  → memgraph.FactExtractor   (internal/memgraph/)    — Claude Haiku extracts relationship facts (triple extraction)
  → memgraph.Client          (internal/memgraph/)    — CreateEpisode (episode provenance node)
  → classifier.Classifier    (internal/classifier/)  — heuristic keyword scoring → MemoryType
  → memgraph.Client          (internal/memgraph/)    — dedup via cosine similarity, then upsert (sets valid_from, invalidates superseded)
  → memgraph.Client          (internal/memgraph/)    — entity/fact upsert + GetEpisodesForMemory linking
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
| `memgraph.Client` | `internal/memgraph/client.go` | `MemgraphClient` (Bolt), `MockMemgraphClient` (tests) |
| `embedder.Embedder` | `internal/embedder/embedder.go` | `OllamaEmbedder` (HTTP) |
| `classifier.Classifier` | `internal/classifier/classifier.go` | `HeuristicClassifier` |
| `capture.Capturer` | `internal/capture/capture.go` | `ClaudeCapturer` |
| `capture.ContradictionDetector` | `internal/capture/contradiction.go` | `MemoryContradictionDetector` |
| `llm.LLMClient` | `internal/llm/client.go` | `AnthropicClient`, `GatewayClient` |

New in v0.8.0 — methods added to `memgraph.Client`:
- `InvalidateMemory(ctx, id)` — marks a memory as invalidated (sets valid_to)
- `GetHistory(ctx, id)` — returns supersession chain for a memory
- `MigrateTemporalFields(ctx)` — backfills valid_from on existing memories
- `CreateEpisode(ctx, episode)` — creates an Episode provenance node
- `GetEpisodesForMemory(ctx, memoryID)` — returns episodes linked to a memory

### Core Data Model (`internal/models/memory.go`)

`Memory` is the central struct. Key fields:
- `Type` — `rule | fact | episode | procedure | preference` (affects recall boost)
- `Scope` — `permanent | project | session | ttl` (affects expiry + recall boost)
- `Confidence` — `0.0–1.0`; memories below 0.5 are filtered on capture
- `LastAccessed` / `AccessCount` — updated on every retrieval for recency/frequency scoring
- `Project` — used to boost `scope=project` memories in context
- `ValidFrom` / `ValidTo` — temporal versioning fields (v0.8.0); `ValidTo` set on invalidation
- `ConflictGroupID` / `ConflictStatus` — contradiction detection (v0.8.0); auto-flagged on capture
- `EpisodeIDs` — provenance links to Episode nodes created during capture (v0.8.0)

`SearchFilters` extensions (v0.8.0):
- `IncludeInvalidated bool` — include memories with a non-zero ValidTo (default: false)
- `AsOf time.Time` — point-in-time query; returns memories valid at that instant

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
- **Context propagation**: every function that touches Memgraph, Ollama, or Claude accepts `ctx context.Context` as the first argument.
- **Tests live in `tests/`**: all test files are in the top-level `tests/` package (black-box testing), not co-located with the package under test. Use `MockMemgraphClient` from `internal/memgraph/mock_client.go` to avoid requiring live Memgraph.
- **Prompt injection prevention**: user/assistant content is XML-escaped in `internal/capture/capture.go` before interpolation into the Claude prompt. Maintain this for any new LLM-calling code.
- **Linter**: golangci-lint v2 with `linters.settings` (not top-level `settings`) and `linters.exclusions.rules` (not `issues.exclude-rules`). Test files are excluded from `errcheck` and `unparam`.

## External Service Dependencies

| Service | Default address | Purpose |
|---------|----------------|---------|
| Memgraph | `bolt://localhost:7687` | Vector storage, graph traversal, entity-relationship storage |
| Ollama | `http://localhost:11434` | Embeddings (`nomic-embed-text`, 768-dim) |
| Claude LLM | Anthropic API or OpenClaw gateway | Memory extraction, entity/fact extraction, resolution |

### LLM Authentication (choose one)

1. **API key**: Set `ANTHROPIC_API_KEY` env var or `claude.api_key` in config
2. **OpenClaw gateway** (for Max plan users): Set `claude.gateway_url` and `claude.gateway_token` in config — routes through `http://127.0.0.1:18789/v1/chat/completions`

Config is loaded from `~/.openclaw-cortex/config.yaml` with env var overrides prefixed `OPENCLAW_CORTEX_` (e.g., `OPENCLAW_CORTEX_MEMGRAPH_URI`).

### Versioning

Plugin version (`PLUGIN_VERSION` in `extensions/openclaw-plugin/index.ts`) and binary version (`version` in `cmd/openclaw-cortex/main.go`) should be kept in sync. On startup, the plugin logs both versions and warns on mismatch. Check with `openclaw cortex version`.
