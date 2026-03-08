# Changelog

All notable changes to OpenClaw Cortex will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

## [0.3.0] - 2026-03-09

### Added
- **Threshold-gated LLM re-ranking**: `ShouldRerank` fires only when the top-4 score spread is ≤ 0.15 (low-confidence results), enforcing latency budgets of 100 ms (hook) and 3000 ms (CLI) via `context.WithTimeout`; ~10–30% of recalls trigger re-ranking
- **Session pre-warm cache**: goroutine in PostTurnHook writes ranked results to `~/.cortex/rerank_cache/<sid>.json` (5-min TTL); PreTurnHook reads cache for zero-latency injection on session resume
- **Conflict engine**: ConflictDetector identifies contradicting memories on capture → tags both with `ConflictGroupID` and `status="active"`; `FormatWithConflictAnnotations` surfaces conflicts inline during recall; `consolidate` resolves groups by confidence (loser → `status="resolved"`)
- **Confidence reinforcement**: memories with 0.80 ≤ similarity < 0.92 trigger `UpdateReinforcement` (confidence += 0.05, ReinforcedCount++); memories at ≥ 0.92 continue to be dedup-skipped
- **Multi-turn capture**: `ExtractWithContext` reads the last N turns from the JSONL transcript and passes them to Claude Haiku for richer memory extraction
- **`UpdateConflictFields` store method**: atomically updates `ConflictGroupID`, `status`, and `contradicts_id` payload fields
- **`UpdateReinforcement` store method**: atomically increments `confidence` and `reinforced_count`
- **Config keys**: `recall.rerank_threshold`, `recall.hook_latency_budget_ms`, `recall.cli_latency_budget_ms`; `capture_quality.context_window_turns`, `capture_quality.reinforcement_threshold_low`, `capture_quality.reinforcement_threshold_high`

### Changed
- `consolidate` command adds conflict-resolution pass (phase 4): groups memories by `ConflictGroupID`, keeps highest-confidence copy, marks losers `status="resolved"`
- Recall output annotates conflicts inline when `FormatWithConflictAnnotations` is used

## [0.2.0] - 2026-02-28

### Added
- **HTTP API server**: `cortex serve` exposes REST endpoints (`POST /v1/remember`, `POST /v1/recall`, `GET /v1/memories/{id}`, `DELETE /v1/memories/{id}`, `POST /v1/search`, `GET /v1/stats`, `GET /healthz`); optional `Authorization: Bearer` token auth
- **MCP server**: `cortex mcp` implements Model Context Protocol over stdio (remember/recall/forget/search/stats tools) for Claude Desktop integration
- **OpenAI-compatible embedder**: configurable `embedder.provider: openai` alongside the existing Ollama provider; configurable `openai_dim` for embedding dimension
- **Entity model**: `models.Entity` with type hierarchy (person/project/system/decision/concept); `UpsertEntity`, `GetEntity`, `SearchEntities`, `LinkMemoryToEntity` store methods; `cortex entities` CLI subcommand
- **Temporal model**: `Memory.SupersedesID` and `Memory.ValidUntil` fields; `GetChain` store method to follow supersedes history; `cortex store --supersedes` and `--valid-until` flags; lifecycle phase retires expired facts
- **Re-ranking**: optional Claude-powered result re-ranking in `internal/recall/reasoner.go` with graceful degradation on API error
- **Prometheus-compatible metrics**: in-process counters via `expvar` (recall total, capture total, dedup skipped, lifecycle events)
- **`cortex health`** command: pings Qdrant and Ollama; checks `ANTHROPIC_API_KEY` presence

### Changed
- Makefile replaced with Taskfile.yml (go-task)
- Test coverage floor raised to 50%
- Dedup threshold configurable (`memory.dedup_threshold_hook`, default 0.95 for hooks; 0.92 for CLI)

## [0.1.0] - 2026-02-25

### Added
- **Phase 1 — Foundation**: Config (Viper), memory models, Ollama embedder, Qdrant gRPC store, markdown indexer, CLI scaffold (Cobra)
- **Phase 2 — Smart Capture**: Claude Haiku extraction, heuristic classifier, cosine-similarity dedup
- **Phase 3 — Smart Recall**: Multi-factor ranking (similarity, recency, frequency, type, scope), token budgeting
- **Phase 4 — Integration**: Lifecycle management (TTL/decay/consolidation), pre/post-turn hooks, OpenClaw skill definition, stats command
- Docker Compose for local Qdrant
- Kubernetes manifests for Qdrant StatefulSet
- Multi-stage Dockerfile
- Comprehensive test suite with mocked Qdrant
- golangci-lint configuration
