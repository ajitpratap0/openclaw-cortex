# Architecture

## Overview

Cortex is a hybrid layered memory system that augments OpenClaw AI agents with persistent, semantically searchable memory. It bridges file-based structured memory (markdown files, git-backed) with vector-based semantic memory (Qdrant).

## System Diagram

```
┌──────────────────────────────────────────────────────────┐
│                   OpenClaw Agent                          │
│                                                          │
│   Pre-Turn Hook ──→ Recall ──→ Inject context            │
│   Post-Turn Hook ──→ Capture ──→ Store memories          │
└──────────┬───────────────────────────────┬───────────────┘
           │                               │
           ▼                               ▼
┌──────────────────┐            ┌──────────────────────┐
│   CLI Interface  │            │   Hook Integration   │
│   (Cobra)        │            │   (Pre/Post Turn)    │
└────────┬─────────┘            └──────────┬───────────┘
         │                                 │
         ▼                                 ▼
┌─────────────────────────────────────────────────────────┐
│                    Core Engine                            │
│                                                          │
│  ┌──────────┐  ┌───────────┐  ┌──────────┐             │
│  │ Indexer   │  │ Capturer  │  │ Recaller │             │
│  │ (scan +   │  │ (Claude   │  │ (multi-  │             │
│  │  chunk)   │  │  Haiku)   │  │  factor) │             │
│  └─────┬─────┘  └─────┬─────┘  └────┬─────┘             │
│        │              │              │                    │
│  ┌─────▼──────────────▼──────────────▼─────┐             │
│  │           Classifier                     │             │
│  │   (heuristic + LLM type detection)       │             │
│  └─────────────────┬───────────────────────┘             │
│                    │                                      │
│  ┌─────────────────▼───────────────────────┐             │
│  │          Lifecycle Manager               │             │
│  │   (TTL, decay, consolidation)            │             │
│  └─────────────────────────────────────────┘             │
└──────────┬──────────────────────────┬───────────────────┘
           │                          │
           ▼                          ▼
┌──────────────────┐       ┌──────────────────────┐
│    Embedder      │       │       Store           │
│  (Ollama HTTP)   │       │   (Qdrant gRPC)       │
│  nomic-embed-text│       │   768-dim vectors     │
└──────────────────┘       └──────────────────────┘
```

## Data Flow

### Indexing (Batch Import)
1. **Indexer** scans markdown files from the memory directory
2. Files are split into chunks (512 tokens, 64 overlap)
3. **Classifier** assigns type (rule/fact/episode/procedure/preference) and scope
4. **Embedder** generates 768-dim vectors via Ollama
5. **Store** upserts into Qdrant with metadata payload

### Capture (Per-Turn)
1. Post-turn hook sends user + assistant messages to **Capturer**
2. Claude Haiku extracts structured memories (content, type, tags, importance)
3. **Classifier** validates/adjusts the type assignment
4. Dedup check: cosine similarity against existing memories (threshold: 0.92)
5. New memories are embedded and stored

### Recall (Per-Turn)
1. Pre-turn hook sends current message to **Recaller**
2. Message is embedded via Ollama
3. Qdrant returns top-K candidates by vector similarity
4. Multi-factor scoring applied:
   - Similarity (50%) — cosine distance
   - Recency (20%) — exponential decay, 7-day half-life
   - Frequency (10%) — log-scale access count
   - Type boost (10%) — priority multiplier per type
   - Scope boost (10%) — project-match bonus
5. Results are token-budgeted and returned as context

### Lifecycle (Periodic)
1. **TTL expiry** — remove memories past their time-to-live
2. **Session decay** — expire session-scoped memories after inactivity
3. **Consolidation** — merge similar memories, promote frequently accessed ones

## Key Design Decisions

| Decision | Rationale |
|----------|-----------|
| Qdrant over Chroma/Pinecone | gRPC performance, self-hosted, rich filtering |
| Ollama over OpenAI embeddings | Local, free, no API dependency for core path |
| Claude Haiku for extraction | Best cost/quality ratio for structured extraction |
| Cosine dedup at 0.92 | Catches near-duplicates without false positives |
| 768-dim nomic-embed-text | Good balance of quality vs. storage/compute |
| Cobra + Viper | Standard Go CLI stack, config from env/file/flags |
| gRPC for Qdrant | 2-3x faster than HTTP for batch operations |

## Dependencies

| Component | Version | Purpose |
|-----------|---------|---------|
| Go | 1.25+ | Language runtime |
| Qdrant | 1.12+ | Vector storage |
| Ollama | latest | Local embeddings |
| Claude Haiku | claude-haiku-4-5 | Memory extraction |
| Cobra | v1.8+ | CLI framework |
| Viper | v1.18+ | Configuration |
| Testify | v1.9+ | Test assertions |
