# Changelog

All notable changes to OpenClaw Cortex will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

## [0.11.0] - 2026-03-26

### Added

- **`recall --format json|text`** — explicit output format flag; replaces the `--context _` JSON-mode sentinel with a first-class flag; backward-compatible (sentinel still works unless `--format text` is set) ([#101])
- **`recall --limit N`** — hard cap on result count (max 10 000); applied before token-budget trimming for deterministic output ([#101])
- **Post-store entity & fact extraction** (`internal/extract/`) — new `PostStoreExtractor` automatically extracts entities and relationships when storing memories via the `store` command, without requiring the full capture pipeline ([#100])

### Fixed

- **Lucene text search sanitization** (`internal/memgraph/`) — `SanitizeTextSearchQuery` strips Lucene special characters (`+ - & | ! ( ) { } [ ] ^ " ~ * ? : \ /`) before calling `text_search.search_all`, preventing `Unknown exception!` on natural-language queries containing colons, question marks, and parentheses ([#101])
- **`LinkMemoryToEntity` cursor drain** (`internal/memgraph/`) — result cursor is now consumed in the not-found path, preventing a resource leak in the Bolt driver ([#99])
- **LLM health check** (`internal/health/`) — health command now performs a real Claude API ping instead of checking config presence only; detects missing or invalid credentials at startup ([#98])

### Changed

- **Lucene lookup table** — `[128]bool` character set is now a package-level `var` initialized once at startup (was rebuilt on every `SanitizeTextSearchQuery` call) ([#101])
- **Eval runner** — replaced `--context _` sentinel with `--format json --limit` flags in all eval harness invocations; cleaner error messages distinguish flag errors from connectivity failures ([#100])
- **`PostStoreExtract` counters** — `Result.EntitiesExtracted` counts successful `UpsertEntity` calls regardless of subsequent link outcome; `Result.FactsExtracted` counts only fully-linked facts (upsert + link both succeeded) ([#100])

### Tests

- `tests/search_entities_cypher_test.go` — table-driven tests for `SanitizeTextSearchQuery` covering all special chars, `wantExact` assertions, backslash/slash, and plain-query passthrough
- `tests/post_store_extract_test.go` — 8 cases covering entity extraction, fact extraction, cross-memory entity pool, context cancellation, and error paths
- `tests/eval_runner_test.go` — new cases for `--format json`, `--limit` cap, flag precedence, and legacy sentinel compatibility

## [0.10.0] - 2026-03-23

> Note: v0.9.0 was skipped; all post-v0.8.0 work is consolidated here.

### Added

- **LoCoMo + LongMemEval benchmark harness** (`eval/` package) — end-to-end evaluation framework with synthetic datasets, Token-F1 scoring, Recall@K metrics, and CSV/JSON report output ([#88])
- **Eval reset command** — `CortexClient.Reset` wipes the store before each benchmark run; `ResettableStore` interface added to `memgraph.Client`; `openclaw-cortex reset --yes` exits non-zero without the flag ([#88])
- **Standalone Next.js 15 admin app** (`apps/admin/`) — browser UI for browsing memories, entities, and conflict groups with pagination, filtering, and inline resolution ([#81])
- **ResilientClient** (`internal/llm/`) — wraps any `LLMClient` with circuit breaker, configurable retry with exponential back-off, and a bounded worker pool to cap concurrent LLM calls ([#78])
- **LM Studio embedder** — new `lmstudio` provider in `internal/embedder/`; factory selects provider from config; OpenAI embedder removed ([#79])
- **Per-user memory namespacing** — `UserID` field on `Memory` and `SearchFilters`; memories are scoped per user when set ([#71])
- **Sentry integration** — error tracking and performance tracing wired into all CLI commands and hot paths; configurable via `sentry.dsn` and `sentry.environment` in config ([#69])
- **Next.js marketing + documentation website** (`web/`) with product logo, hero, architecture diagram, feature list, and 404 page ([#66])
- **Project logo** — SVG and optimised PNG variants (16 / 32 / 64 / 128 / 256 px) added to `web/public/logo/`
- **Parallel PostTurnHook** — per-memory embed + upsert pipeline now runs concurrently with a semaphore-bounded worker pool, reducing hook latency on multi-memory captures ([#80])

### Changed

- **`ResettableStore` interface** — `DeleteAllMemories(ctx)` moved from `memgraph.MemgraphClient` into the new `memgraph.ResettableStore` interface; callers that need reset must assert or embed this interface
- **`RecallJSONResult` exported** — struct moved to `eval/runner` package and exported for use in tests and external harnesses
- **Eval harness** — `Recall` now runs in JSON mode (`--context _` sentinel), uses separate stdout/stderr capture, applies a per-call timeout, and propagates context cancellation with the original subprocess error preserved
- **API authentication** — auth is now opt-*out* rather than opt-in; a new `--unsafe-no-auth` flag is required to disable the `Authorization: Bearer` check (previously the server started without auth by default)

### Fixed

- **Memgraph DDL** — vector dimension is now injected from `embedder.dimension` config at startup rather than hardcoded, preventing index mismatches when using non-default embedding models ([#67])
- **Pagination cursors** — list endpoints now return signed, opaque cursors; entity search limit and skip-re-embed logic corrected ([#68])
- **API rate limiting** — `RateLimitMiddleware` now receives and propagates the request context correctly ([#82])
- **Memory forget** — `memory_forget` delete now supports prefix matching, allowing bulk deletion by ID prefix ([#83])
- **Graph SearchEntities** — Memgraph Cypher dialect fix: `search_all` procedure requires a `WITH` clause before the `WHERE` filter; previously returned no results silently ([#89])
- **Eval context cancellation** — when a recall subprocess is killed due to context cancellation, both the subprocess error and `ctx.Err()` are wrapped and surfaced; the original error is no longer silently dropped ([#90])
- **LLM client** — unexported `http` field renamed to `httpClient` to avoid shadowing the `net/http` package import; `TestGatewayClient_Timeout` added ([#76])
- **Web visual audit** — skip-nav links, loading/error boundaries, CSS custom-property tokens, contrast ratios, and active nav states corrected ([#85])
- **Admin visual audit** — skip-nav, ARIA dialog roles, loading state spinners, CSS token consistency, and responsive breakpoints corrected ([#86])
- **CI** — `trivy-action` updated from 0.28.0 to 0.35.0; `claude-review` action switched to native action mode and now posts inline comments correctly

### Security

- **Explicit auth opt-out** — HTTP API no longer starts in an unauthenticated state by default; `--unsafe-no-auth` must be passed explicitly, preventing accidental public exposure ([#77])
- **Per-IP rate limiting** — configurable rate limiter applied to all API endpoints to mitigate abuse ([#77])
- **TLS support** — `--tls-cert` and `--tls-key` flags added to `serve` command for HTTPS termination at the binary level ([#77])

### Tests

- Overall test coverage raised from 56.7 % to 85.3 % with targeted unit tests across recall, capture, lifecycle, and API packages ([#75])
- Eval harness: JSON sentinel test, temporal versioning test, `TestRunnerBestCandidate`, store-failure tests, and LoCoMo ground-truth integrity check added
- Compile-time `ResettableStore` assertions added to `failingUpsertStore` test stub

## [0.8.0] - 2026-03-15

### Added
- Phase 1: Temporal versioning — memories have valid_from/valid_to fields, supersession auto-invalidates old versions, as-of point-in-time queries
- Phase 2: Episodic→Semantic triple extraction — Episode provenance nodes, automatic fact extraction from captured memories, entity linking
- Phase 3: Contradiction detection — vector similarity + keyword heuristic to detect contradicting memories during capture, auto-flagging with conflict groups
- Phase 4: Graph-aware recall — configurable traversal depth, RRF merge of vector + graph results, entity-seeded graph walks
- InvalidateMemory and GetHistory methods on store.Store interface
- MigrateTemporalFields for backfilling existing memories
- ContradictionDetector interface and MemoryContradictionDetector implementation
- CreateEpisode and GetEpisodesForMemory on graph.Client interface
- SearchFilters extended with IncludeInvalidated and AsOf temporal filters
- MockStore updated to filter invalidated memories by default

### Changed
- Upsert auto-sets valid_from and invalidates superseded memories
- List and Search exclude invalidated memories by default (respects IncludeInvalidated flag)

## [0.7.2] - 2026-03-15

### Fixed
- Ollama EmbedBatch: replaced N sequential HTTP calls with single `/api/embed` batch call (36x faster — 6 items in 0.5s vs 18s)
- Fixes `memory_store_batch` timeout in OpenClaw plugin

## [0.7.1] - 2026-03-15

### Fixed
- Memgraph Cypher dialect: `WITH` required between `YIELD` and `WHERE` in procedure calls — was causing FindDuplicates to silently fail, leading to unlimited memory duplication
- Memgraph DDL must run in auto-commit transactions (not explicit transactions)
- Vector index metric name: `cos` not `cosine` for Memgraph
- Docker image tag: `memgraph/memgraph:latest` (2.21 tag doesn't exist)

### Added
- 18 Memgraph integration tests covering vector search, dedup, CRUD, entities, and graph operations

## [0.7.0] - 2026-03-15

### Changed
- **BREAKING**: Replaced Qdrant + Neo4j with single Memgraph instance
- Single container for vector search + graph traversal (was two containers)
- Graph features always enabled (removed `graph.enabled` flag)
- Config simplified: `memgraph.uri` replaces `qdrant.*` and `graph.neo4j.*`
- Entity storage unified in Memgraph (was duplicated in Qdrant + Neo4j)

### Removed
- Qdrant dependency (`qdrant/go-client` removed from go.mod)
- `internal/store/qdrant.go` — replaced by `internal/memgraph/`
- `internal/graph/neo4j.go` — replaced by `internal/memgraph/`
- `QdrantConfig`, `Neo4jConfig`, `GraphConfig` from config
- `graph.enabled` configuration flag

## [0.6.0] - 2026-03-15

### Added
- Neo4j entity-relationship graph integration with bi-temporal fact model
- Entity extraction via Claude Haiku during capture pipeline
- Fact extraction and relationship creation in Neo4j
- Three-stage entity resolution (exact match → embedding → Claude LLM fallback)
- Fact resolution with dedup and contradiction detection
- Hybrid graph search with Reciprocal Rank Fusion (RRF)
- Graph-augmented recall with latency budgets (50ms hooks, 500ms CLI)
- Graceful degradation — all graph operations optional, failures logged and skipped
- Entity name→UUID resolution in capture pipeline for Neo4j fact writes
- `llm.StripCodeFences()` utility for gateway model JSON responses
- REST API endpoints: `GET /v1/entities/{id}`, `GET /v1/entities`
- MCP tools: `entity_search`, `entity_get`
- Entity count in `stats` output
- Neo4j service in docker-compose.yml with JVM vector module enabled
- Neo4j health check in `openclaw-cortex health`

## [0.5.0] - 2026-03-15

### Added
- LLM gateway abstraction (`internal/llm/` package) with `LLMClient` interface
- `GatewayClient` for routing Claude calls through OpenClaw gateway (Max plan support)
- `AnthropicClient` wrapping direct Anthropic SDK calls
- Factory function `llm.NewClient()` picks implementation from config
- Plugin versioning: `PLUGIN_VERSION` constant, `openclaw cortex version` CLI command
- Version mismatch detection on plugin startup
- Config options: `claude.gateway_url`, `claude.gateway_token`

### Changed
- All 8 LLM call sites refactored from direct Anthropic SDK to `LLMClient` interface
- Health check recognizes gateway as valid LLM path
- Capture command accepts gateway config (no longer requires `ANTHROPIC_API_KEY`)

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

[Unreleased]: https://github.com/ajitpratap0/openclaw-cortex/compare/v0.10.0...HEAD
[0.10.0]: https://github.com/ajitpratap0/openclaw-cortex/compare/v0.8.0...v0.10.0
[0.8.0]: https://github.com/ajitpratap0/openclaw-cortex/compare/v0.7.2...v0.8.0
[0.7.2]: https://github.com/ajitpratap0/openclaw-cortex/compare/v0.7.1...v0.7.2
[0.7.1]: https://github.com/ajitpratap0/openclaw-cortex/compare/v0.7.0...v0.7.1
[0.7.0]: https://github.com/ajitpratap0/openclaw-cortex/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/ajitpratap0/openclaw-cortex/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/ajitpratap0/openclaw-cortex/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/ajitpratap0/openclaw-cortex/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/ajitpratap0/openclaw-cortex/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/ajitpratap0/openclaw-cortex/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/ajitpratap0/openclaw-cortex/releases/tag/v0.1.0

[#66]: https://github.com/ajitpratap0/openclaw-cortex/pull/66
[#67]: https://github.com/ajitpratap0/openclaw-cortex/pull/67
[#68]: https://github.com/ajitpratap0/openclaw-cortex/pull/68
[#69]: https://github.com/ajitpratap0/openclaw-cortex/pull/69
[#71]: https://github.com/ajitpratap0/openclaw-cortex/pull/71
[#75]: https://github.com/ajitpratap0/openclaw-cortex/pull/75
[#76]: https://github.com/ajitpratap0/openclaw-cortex/pull/76
[#77]: https://github.com/ajitpratap0/openclaw-cortex/pull/77
[#78]: https://github.com/ajitpratap0/openclaw-cortex/pull/78
[#79]: https://github.com/ajitpratap0/openclaw-cortex/pull/79
[#80]: https://github.com/ajitpratap0/openclaw-cortex/pull/80
[#81]: https://github.com/ajitpratap0/openclaw-cortex/pull/81
[#82]: https://github.com/ajitpratap0/openclaw-cortex/pull/82
[#83]: https://github.com/ajitpratap0/openclaw-cortex/pull/83
[#85]: https://github.com/ajitpratap0/openclaw-cortex/pull/85
[#86]: https://github.com/ajitpratap0/openclaw-cortex/pull/86
[#88]: https://github.com/ajitpratap0/openclaw-cortex/pull/88
[#89]: https://github.com/ajitpratap0/openclaw-cortex/pull/89
[#90]: https://github.com/ajitpratap0/openclaw-cortex/pull/90
