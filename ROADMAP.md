# Roadmap

OpenClaw Cortex follows a milestone-based release cadence. Features ship when stable, not on a fixed calendar.

## v0.3.x — Current (Stability & Contribution Onboarding)

- [ ] `good-first-issue` backlog on GitHub — entry points for new contributors
- [ ] Improve error messages for misconfigured Qdrant/Ollama
- [ ] `cortex hook install` command (auto-writes `.claude/settings.json`)

## v0.4.0 — Multi-User & Observability

- [ ] Multi-user namespace isolation (per-user Qdrant collection or tenant prefix)
- [ ] Web UI memory browser (read-only, local-only)
- [ ] Prometheus metrics endpoint (`/metrics`) via optional exporter
- [ ] `cortex export` command (JSON/NDJSON dump of all memories)

## v0.5.0 — Pluggable Providers & Streaming

- [ ] Pluggable embedding providers: Cohere, Gemini, OpenAI-compatible endpoints
- [ ] Streaming recall (SSE endpoint for progressive context injection)
- [ ] Batch capture from chat export files (JSON, Markdown)

## Community

- [ ] GitHub Discussions (enable when community reaches 50+ stars)
- [ ] Discord server (TBD)

---

Contributions toward any roadmap item are welcome. Open an issue first to discuss scope.
