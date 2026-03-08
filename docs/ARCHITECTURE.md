# Architecture

OpenClaw Cortex is a hybrid semantic memory system. It stores memories as both structured metadata and high-dimensional vectors, then retrieves them using a multi-factor scoring algorithm that combines semantic similarity with recency, frequency, type priority, and project scope.

## System Diagram

```
┌──────────────────────────────────────────────────────────┐
│                   OpenClaw Agent                          │
│                                                          │
│   Pre-Turn Hook ──> Recall ──> Inject context            │
│   Post-Turn Hook ──> Capture ──> Store memories          │
└──────────┬───────────────────────────────┬───────────────┘
           │                               │
           ▼                               ▼
┌──────────────────┐            ┌──────────────────────┐
│   CLI Interface  │            │   Hook / API / MCP   │
│   (Cobra)        │            │   (Pre/Post Turn)    │
└────────┬─────────┘            └──────────┬───────────┘
         │                                 │
         └──────────────┬──────────────────┘
                        │
                        ▼
┌─────────────────────────────────────────────────────────┐
│                    Core Engine                            │
│                                                          │
│  ┌──────────┐  ┌───────────┐  ┌──────────────────────┐ │
│  │ Indexer  │  │ Capturer  │  │      Recaller        │ │
│  │ (scan +  │  │ (Claude   │  │  (multi-factor rank) │ │
│  │  chunk)  │  │  Haiku)   │  │                      │ │
│  └─────┬────┘  └─────┬─────┘  └──────────┬───────────┘ │
│        │              │                   │              │
│  ┌─────▼──────────────▼───────────────────▼─────────┐   │
│  │              Classifier                           │   │
│  │     (heuristic keyword scoring -> MemoryType)     │   │
│  └──────────────────────┬────────────────────────────┘   │
│                         │                                 │
│  ┌──────────────────────▼────────────────────────────┐   │
│  │            Lifecycle Manager                       │   │
│  │     (TTL expiry, session decay, consolidation)     │   │
│  └────────────────────────────────────────────────────┘  │
└──────────┬──────────────────────────┬───────────────────┘
           │                          │
           ▼                          ▼
┌──────────────────┐       ┌──────────────────────┐
│    Embedder      │       │       Store           │
│  (Ollama HTTP)   │       │   (Qdrant gRPC)       │
│  nomic-embed-text│       │   768-dim vectors     │
└──────────────────┘       └──────────────────────┘
```

## Layered Call Flows

### Recall Flow (`cortex recall`)

```
cmd/cmd_recall.go
  -> recall.Recaller          (internal/recall/)
       -> embedder.Embed()    (internal/embedder/)  -- Ollama HTTP, 768-dim
       -> store.Search()      (internal/store/)     -- Qdrant gRPC vector search
       -> recaller.Rank()                           -- multi-factor scoring
  -> tokenizer.FormatMemoriesWithBudget()           -- trim to token budget
```

1. The query is embedded via Ollama (`nomic-embed-text`).
2. Qdrant returns the top-50 candidates by cosine similarity.
3. The multi-factor scorer re-ranks them.
4. Results are trimmed to fit the token budget (default: 2000 tokens).
5. Access metadata (`last_accessed`, `access_count`) is updated for returned memories.

### Capture Flow (`cortex capture`)

```
cmd/cmd_capture.go
  -> capture.Capturer.Extract()  (internal/capture/)
       -- Claude Haiku extracts JSON memories from conversation text
  -> classifier.Classifier       (internal/classifier/)
       -- heuristic keyword scoring assigns MemoryType if LLM left it empty
  -> embedder.Embed()
  -> store.FindDuplicates()      -- cosine similarity dedup (threshold: 0.92)
  -> store.Upsert()
```

User and assistant message content is XML-escaped before interpolation into the Claude prompt to prevent prompt injection.

### Index Flow (`cortex index`)

```
cmd/cmd_index.go
  -> indexer.Index()   (internal/indexer/)
       -- walks the memory directory tree
       -- parses markdown into H1-H4 section hierarchy (ParseMarkdownTree)
       -- optional: generates per-section summaries via Claude Haiku (--summarize)
       -> classifier.Classify() per chunk
       -> embedder.Embed() per chunk
       -> store.Upsert() per chunk
```

### Lifecycle Flow (`cortex consolidate`)

```
cmd/cmd_lifecycle.go
  -> lifecycle.Manager.Run()  (internal/lifecycle/)
       -- TTL expiry: delete memories past their time-to-live
       -- session decay: expire session-scoped memories after 24h inactivity
       -- consolidation: merge near-duplicate memories
       -- conflict resolution: group by ConflictGroupID, keep highest confidence, mark losers resolved
```

## Recall Intelligence (v0.3.0)

### Threshold-Gated Re-Ranking

Standard multi-factor scoring is deterministic but cannot reason about semantic nuance. When the top-4 result scores are tightly clustered (spread ≤ 0.15), the ranking is ambiguous and a stronger signal is needed.

`ShouldRerank` in `internal/recall/recall.go` computes `max_score − min_score` over the top-4 candidates. When the spread falls at or below the threshold, it dispatches the candidates to Claude (via `internal/recall/reasoner.go`) with a latency budget enforced by `context.WithTimeout`:

| Context | Budget |
|---------|--------|
| Hook (PreTurnHook) | 100 ms |
| CLI (`cortex recall`) | 3000 ms |

On timeout or API error, the original multi-factor ranking is used — graceful degradation is guaranteed. In practice, re-ranking fires on ~10–30% of recall operations.

### Session Pre-Warm Cache

A goroutine in `PostTurnHook` writes the ranked recall results for the current session to `~/.cortex/rerank_cache/<session_id>.json` immediately after each turn (5-minute TTL). On the next turn, `PreTurnHook` reads the cache before calling Qdrant, providing zero-latency context injection for session-resumed conversations.

Cache files are invalidated after 5 minutes or when the session ends.

## Conflict Engine (v0.3.0)

Contradicting facts accumulate in long-running agent sessions. The conflict engine detects, surfaces, and resolves them across three phases:

### Detect (write path — capture)

`ConflictDetector` in `internal/capture/conflict_detector.go` compares new memory content against top-K similar existing memories. When Claude identifies a semantic contradiction:
- Both the new memory and the contradicted memory are tagged with a shared `ConflictGroupID` (UUID)
- Both receive `status = "active"` and cross-reference each other via `contradicts_id`
- The new memory is stored with `SupersedesID` pointing to the older one

### Surface (read path — recall)

`FormatWithConflictAnnotations` in `pkg/tokenizer` appends `[conflicts with: <short-id>]` to any memory whose `status = "active"` and `ConflictGroupID` is non-empty. This surfaces unresolved conflicts inline in the context injected into Claude's system prompt.

### Resolve (lifecycle — consolidate)

Phase 4 of `lifecycle.Manager.Run()` groups memories by `ConflictGroupID`, sorts each group by `Confidence` descending, and marks all but the highest-confidence member as `status = "resolved"`. Resolved memories are excluded from future recall results.

## Confidence Reinforcement (v0.3.0)

When a new capture closely resembles an existing memory but not closely enough to trigger dedup (0.80 ≤ similarity < 0.92), the existing memory is reinforced rather than duplicating it:

- `store.UpdateReinforcement(id)` atomically increments `confidence` by 0.05 (capped at 1.0) and `reinforced_count` by 1
- The new candidate is discarded (not stored)
- At similarity ≥ 0.92, the existing dedup skip continues as before

This ensures frequently-observed facts converge toward maximum confidence over time without growing the collection.

## Data Model

The central struct is `models.Memory` in `internal/models/memory.go`.

### Memory Types

| Type | Recall Multiplier | Description |
|------|-------------------|-------------|
| `rule` | 1.5x | Operating principles, hard constraints, policies |
| `procedure` | 1.3x | How-to steps, workflows, processes |
| `fact` | 1.0x | Declarative knowledge, definitions |
| `episode` | 0.8x | Specific events with temporal context |
| `preference` | 0.7x | User preferences, style choices |

### Memory Scopes

| Scope | Behavior |
|-------|----------|
| `permanent` | Persists indefinitely |
| `project` | Boosted when project context matches; does not expire |
| `session` | Auto-expires after 24 hours without access |
| `ttl` | Expires after the configured TTL (default: 720 hours) |

### Key Fields

| Field | Type | Description |
|-------|------|-------------|
| `ID` | string (UUID) | Unique identifier |
| `Content` | string | The memory text |
| `Type` | MemoryType | Classification (rule/fact/episode/procedure/preference) |
| `Scope` | MemoryScope | Lifecycle policy (permanent/project/session/ttl) |
| `Confidence` | float64 | 0.0-1.0; memories below 0.5 are filtered on capture |
| `Tags` | []string | User-defined labels |
| `Project` | string | Project name for scope=project memories |
| `CreatedAt` | time.Time | When the memory was first stored |
| `LastAccessed` | time.Time | Updated on every recall |
| `AccessCount` | int | Total recall count |
| `SupersedesID` | string | ID of the memory this one replaces (conflict resolution) |

## Recall Scoring

The multi-factor scoring formula combines five signals:

```
score = 0.5 * similarity
      + 0.2 * recency
      + 0.1 * frequency
      + 0.1 * typeBoost
      + 0.1 * scopeBoost
```

**Similarity** (50%): Cosine similarity from Qdrant. The primary signal.

**Recency** (20%): Exponential decay with a 7-day half-life:

```
recency = exp(-ln(2) * daysSinceAccess / 7)
```

**Frequency** (10%): Log₂-scale access count, capped at 1.0:

```
frequency = min(1.0, log2(1 + accessCount) / 10)
```

This saturates at ~1000 accesses (log₂(1001) ≈ 10).

**Type boost** (10%): Multiplier based on memory type priority (see table above).

**Scope boost** (10%): Normalized multiplier. Project-scoped memories whose project matches the query receive a score of 1.0 (vs. 0.67 for `permanent`-scope and 0.53 for `session`/`ttl`). This surfaces project-specific context over global memories when a project is specified.

## Key Design Decisions

| Decision | Rationale |
|----------|-----------|
| Qdrant over Chroma/Pinecone | gRPC performance, self-hosted, rich payload filtering |
| Ollama for embeddings | Local, free, no external API dependency on the critical recall path |
| Claude Haiku for extraction | Best cost/quality ratio for structured JSON extraction |
| 768-dim nomic-embed-text | Good quality-to-storage-cost balance; well-suited for semantic similarity |
| gRPC for Qdrant | 2-3x faster than HTTP for batch operations |
| Cosine dedup at 0.92 | Catches near-duplicates without false positives on related-but-distinct memories |
| Black-box tests in `tests/` | Prevents tests from coupling to internal implementation details |
| XML-escape before prompt interpolation | Prevents prompt injection when user content is embedded in Claude prompts |

## Package Layout

```
internal/
  api/         -- HTTP API server (REST endpoints)
  capture/     -- Claude Haiku memory extraction + conflict detection
  classifier/  -- Heuristic keyword scoring -> MemoryType
  config/      -- Viper-based configuration loading
  embedder/    -- Embedder interface + Ollama HTTP implementation
  hooks/       -- Pre/post-turn hook handlers
  indexer/     -- Markdown tree walker + section summarizer
  lifecycle/   -- TTL expiry, session decay, consolidation
  mcp/         -- MCP server (remember/recall/forget/search/stats tools)
  metrics/     -- In-process counters
  models/      -- Memory struct and type definitions
  recall/      -- Multi-factor ranker + optional Claude re-ranker
  store/       -- Store interface, Qdrant gRPC implementation, MockStore

pkg/
  tokenizer/   -- Token estimation and budget-aware formatting

cmd/
  openclaw-cortex/  -- CLI entrypoint (Cobra)
```
