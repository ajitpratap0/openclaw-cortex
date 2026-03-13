# Roadmap

OpenClaw Cortex follows a milestone-based release cadence. Features ship when stable, not on a fixed calendar.

## v0.4.0 — Current (Enhanced Recall & Tooling)

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

## v0.5.0 — Multi-User & Observability

- [ ] Multi-user namespace isolation (per-user Qdrant collection or tenant prefix)
- [ ] Web UI memory browser (read-only, local-only)
- [ ] Prometheus metrics endpoint (`/metrics`) via optional exporter

## v0.6.0 — Pluggable Providers & Streaming

- [ ] Pluggable embedding providers: Cohere, Gemini, OpenAI-compatible endpoints
- [ ] Streaming recall (SSE endpoint for progressive context injection)
- [ ] Batch capture from chat export files (JSON, Markdown)

## Community

- [ ] GitHub Discussions (enable when community reaches 50+ stars)
- [ ] Discord server (TBD)

---

Contributions toward any roadmap item are welcome. Open an issue first to discuss scope.
