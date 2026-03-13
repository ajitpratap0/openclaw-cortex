# Changelog

All notable changes to OpenClaw Cortex will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

## [0.4.0] - 2026-03-14

### Added
- **Enhanced recall scoring**: 8-factor weighted formula (similarity 0.35, recency 0.15, frequency 0.10, typeBoost 0.10, scopeBoost 0.08, confidence 0.10, reinforcement 0.07, tagAffinity 0.05) plus multiplicative penalties for superseded memories (x0.3) and active conflicts (x0.8)
- **Recall filters**: `--type`, `--scope`, `--tags`, `--project` flags on `recall` and `search` commands; tag filtering with AND semantics across multiple tags
- **Memory update with lineage**: `cortex update <id>` creates a new memory with `SupersedesID` pointing to the old one, preserving counters and history; superseded memories are penalized in recall ranking
- **Lifecycle CLI**: `cortex lifecycle` exposes TTL expiry, session decay, consolidation, and conflict resolution with `--dry-run` and `--json` flags
- **Batch store**: `cortex store-batch` reads a JSON array from stdin, performs a single `EmbedBatch()` round-trip, per-entry dedup, and outputs JSON results
- **Pre-capture quality filtering**: configurable `min_user_message_length`, `min_assistant_message_length`, and `blocklist_patterns` to skip low-quality captures
- **Health metrics in stats**: `cortex stats` now reports temporal range, top-accessed memories, reinforcement tier distribution, active conflicts, and pending TTL expiry counts; `--json` flag for machine-readable output
- **Configurable recall weights**: `recall.weights.*` in config.yaml and `OPENCLAW_CORTEX_RECALL_WEIGHTS_*` env vars with validation and fallback to defaults
- **`ConflictStatus` typed constant**: promoted from bare string to `models.ConflictStatus` with `IsValid()` method
- **Plugin expansion**: `memory_update`, `memory_lifecycle`, `memory_store_batch` tools; `memory_recall` gains `type`, `scope`, `tags` parameters; auto-capture quality filtering in `agent_end` handler
- **60+ new tests**: recall scoring, tag/type/scope filtering, update lineage, lifecycle commands, quality filtering, batch store, health metrics, and plugin contract tests

### Changed
- Recall scoring formula expanded from 5 factors to 8 factors plus 2 multiplicative penalties
- `Rank()` signature accepts `query string` parameter for tag affinity scoring
- `DefaultWeights()` updated: similarity 0.50 -> 0.35, new weights for confidence, reinforcement, and tag affinity
- `RecallResult` struct gains 5 new fields: `ConfidenceScore`, `ReinforcementScore`, `TagAffinityScore`, `SupersessionPenalty`, `ConflictPenalty`
- `CollectionStats` extended with health metrics (temporal range, reinforcement tiers, conflict/TTL counts)
- DRY refactoring: `buildSearchFilters()` and `parseTags()` helpers eliminate cmd duplication

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
