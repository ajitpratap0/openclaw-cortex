# Changelog

All notable changes to OpenClaw Cortex will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Changed
- Replaced Makefile with Taskfile.yml (go-task)

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
