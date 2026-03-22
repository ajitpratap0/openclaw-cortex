# Roadmap

OpenClaw Cortex follows a milestone-based release cadence. Features ship when stable, not on a fixed calendar.

## v0.4.0 — Enhanced Recall & Tooling

- [x] 8-factor recall scoring with configurable weights
- [x] Supersession and conflict penalties in ranking
- [x] Tag, type, scope, and project filtering on recall/search
- [x] Memory update with lineage preservation (`SupersedesID`)
- [x] `cortex lifecycle` command with `--dry-run` and `--json`
- [x] `cortex store-batch` for bulk memory ingestion via stdin
- [x] Pre-capture quality filtering (min length, blocklist)
- [x] Health metrics in `cortex stats` (reinforcement tiers, conflicts, TTL)
- [x] Plugin expansion (update, lifecycle, store-batch tools)
- [ ] `good-first-issue` backlog on GitHub
- [ ] Improve error messages for misconfigured Qdrant/Ollama
- [ ] `cortex hook install` command (auto-writes `.claude/settings.json`)

## v0.5.0 — LLM Abstraction & Plugin Improvements

- [x] LLM gateway abstraction (provider-agnostic interface for capture and re-ranking)
- [x] Plugin versioning (typed version negotiation in the plugin manifest)
- [ ] Multi-user namespace isolation (per-user Qdrant collection or tenant prefix)
- [ ] Web UI memory browser (read-only, local-only)
- [ ] Prometheus metrics endpoint (`/metrics`) via optional exporter

## v0.6.0 — Knowledge Graph Layer (Complete)

- [x] Neo4j graph backend integration (entity and relation storage)
- [x] Entity extraction from conversation turns (name, type, attributes)
- [x] Fact extraction linking entities via typed relations
- [x] Graph-augmented recall (entity-neighborhood expansion of vector results)
- [x] Entity name-to-UUID resolution (stable identity across captures)

## v0.7.0 — Memgraph Migration (Complete)

- [x] Replaced Qdrant + Neo4j with single Memgraph instance
- [x] Unified vector search + graph traversal in one container
- [x] Graph features always enabled (removed `graph.enabled` flag)
- [x] Simplified config: `memgraph.uri` replaces `qdrant.*` and `graph.neo4j.*`
- [ ] Pluggable embedding providers: Cohere, Gemini, OpenAI-compatible endpoints
- [ ] Streaming recall (SSE endpoint for progressive context injection)
- [ ] Batch capture from chat export files (JSON, Markdown)

## v0.8.0 — Complete (Temporal Versioning, Triple Extraction, Contradiction Detection, Graph-Aware Recall)

- [x] Phase 1: Temporal versioning — valid_from/valid_to fields, supersession auto-invalidates old versions, as-of point-in-time queries
- [x] Phase 2: Episodic→Semantic triple extraction — Episode provenance nodes, automatic fact extraction from captured memories, entity linking
- [x] Phase 3: Contradiction detection — vector similarity + keyword heuristic to detect contradicting memories during capture, auto-flagging with conflict groups
- [x] Phase 4: Graph-aware recall — configurable traversal depth, RRF merge of vector + graph results, entity-seeded graph walks
- [x] InvalidateMemory and GetHistory methods on store.Store interface
- [x] MigrateTemporalFields for backfilling existing memories
- [x] ContradictionDetector interface and MemoryContradictionDetector implementation
- [x] CreateEpisode and GetEpisodesForMemory on graph.Client interface
- [x] SearchFilters extended with IncludeInvalidated and AsOf temporal filters
- [x] MockStore updated to filter invalidated memories by default

## v0.9.0 — Skipped

v0.9.0 was not tagged; all work from this cycle was consolidated into v0.10.0.

## v0.10.0 — Current (Eval Framework, Admin UI, Resilience, Security Hardening)

- [x] LoCoMo + LongMemEval benchmark harness (`eval/` package) with Token-F1, Recall@K, CSV/JSON reports
- [x] `ResettableStore` interface + `openclaw-cortex reset --yes` command for benchmark isolation
- [x] Standalone Next.js 15 admin app (`apps/admin/`) for memory, entity, and conflict management
- [x] `ResilientClient` — circuit breaker, retry with back-off, bounded worker pool for LLM calls
- [x] LM Studio embedder provider; OpenAI embedder removed
- [x] Per-user memory namespacing via `UserID` field on `Memory` and `SearchFilters`
- [x] Sentry error tracking + performance tracing across all CLI commands
- [x] Next.js marketing website + project logo (`web/`)
- [x] Parallel PostTurnHook per-memory pipeline (semaphore-bounded concurrency)
- [x] HTTP API security: `--unsafe-no-auth` required to disable auth, per-IP rate limiting, TLS flags
- [x] Memgraph DDL: vector dimension injected from config; SearchEntities Cypher dialect fix
- [x] Test coverage raised from 56.7 % to 85.3 %

## v0.11.0 — Planned

- [ ] Multi-model embedder support (Cohere, Gemini, OpenAI-compatible endpoints)
- [ ] Streaming recall (SSE endpoint for progressive context injection)
- [ ] Batch capture from chat export files (JSON, Markdown)
- [ ] `cortex hook install` command (auto-writes `.claude/settings.json`)
- [ ] Prometheus metrics endpoint (`/metrics`) with optional exporter
- [ ] Per-user Memgraph namespace isolation (tenant prefix or separate collections)

## Community

- [ ] GitHub Discussions (enable when community reaches 50+ stars)
- [ ] Discord server (TBD)

---

Contributions toward any roadmap item are welcome. Open an issue first to discuss scope.
