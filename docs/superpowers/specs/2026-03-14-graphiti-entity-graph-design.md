# Native Entity-Relationship Graph Integration

> **Issue:** #43
> **Status:** Design approved, pending implementation
> **Date:** 2026-03-14
> **Supersedes:** Earlier Graphiti-based design (rejected: OpenAI dependency conflicts with Claude/Anthropic ecosystem)

## Goal

Add a native entity-relationship graph layer to openclaw-cortex using Neo4j and Claude Haiku, enabling relationship-aware recall alongside existing vector search. The design adopts key innovations from Graphiti (by Zep) — bi-temporal facts, three-stage entity resolution, hybrid search with RRF fusion — but replaces Graphiti's OpenAI dependency with Claude Haiku to stay within the OpenClaw ecosystem.

## Background

### Existing Entity Infrastructure (orphaned)

openclaw-cortex has entity infrastructure built in v0.2.0 but never wired into the capture pipeline:

- `internal/capture/entity_extractor.go` — `EntityExtractor` using Claude Haiku to extract entities from conversation text
- `internal/models/entity.go` — `Entity` model with 5 types (person, project, system, decision, concept), aliases, metadata, memory ID links
- `internal/store/store.go` — Store interface with `UpsertEntity`, `GetEntity`, `SearchEntities`, `LinkMemoryToEntity`
- `internal/store/qdrant.go` / `mock_store.go` — Both implement entity store methods

This code compiles and is tested in isolation, but `capture.go` never calls `EntityExtractor`.

### Why Not Graphiti

Graphiti (by Zep) is a Python graph engine with excellent algorithms. However:

1. **Hard OpenAI dependency** — Graphiti uses OpenAI for entity extraction, relationship extraction, entity resolution, and community summarization. openclaw-cortex is built on Claude/Anthropic end-to-end.
2. **Redundant extraction** — We already have `EntityExtractor` and `ConflictDetector` using Claude Haiku. Graphiti would duplicate this with a different LLM.
3. **Double LLM cost** — Paying both Anthropic (capture) and OpenAI (Graphiti) for overlapping work.
4. **Opaque sidecar** — An extra container between us and Neo4j that we don't control.

### What We Adopt From Graphiti

| Graphiti Innovation | Our Adaptation |
|---|---|
| **Bi-temporal model** on edges (`created_at`/`expired_at` system time + `valid_at`/`invalid_at` world time) | `models.Fact` with four timestamp fields. Maps to our existing conflict engine pattern. |
| **Facts as first-class search units** with embedded fact text | `models.Fact` with `FactEmbedding` for semantic search over relationships, not just entities |
| **Three-stage entity resolution** (hybrid candidate retrieval → deterministic fast-path → LLM fallback) | `EntityResolver` in `internal/graph/` using Neo4j fulltext + embedding similarity + Claude Haiku fallback |
| **Edge contradiction detection** (invalidate, don't delete) | `FactResolver` extends existing `ConflictDetector` pattern to graph edges |
| **Episode provenance** (MENTIONS edges trace facts to source episodes) | `DERIVED_FROM` relationship links facts to source memory IDs |
| **Hybrid search with RRF fusion** (BM25 + cosine + BFS in parallel) | `GraphSearcher` runs Neo4j fulltext + vector similarity + 1-hop BFS, merges with RRF |
| **Community detection** via label propagation + LLM summaries | Phase 3 (future) — label propagation + Claude Haiku community summaries |

## Architecture Overview

```
┌────────────────────────────────────────────────────────────┐
│                    Capture Pipeline                          │
│                                                              │
│  Capturer.Extract() → memories                               │
│       │                                                      │
│       ├──→ store.Upsert(memory)              [Qdrant]        │
│       │                                                      │
│       ├──→ EntityExtractor.Extract(content)   [Claude Haiku]  │
│       │       │                                              │
│       │       ├──→ EntityResolver.Resolve()    [Neo4j + Haiku]│
│       │       │       (3-stage: fulltext → similarity → LLM) │
│       │       │                                              │
│       │       └──→ graph.UpsertEntity()        [Neo4j]        │
│       │                                                      │
│       ├──→ FactExtractor.Extract(content, entities) [Haiku]  │
│       │       │                                              │
│       │       ├──→ FactResolver.Resolve()      [Neo4j + Haiku]│
│       │       │       (dedup + contradiction detection)      │
│       │       │                                              │
│       │       └──→ graph.UpsertFact()          [Neo4j]        │
│       │                                                      │
│       └──→ graph.LinkMemoryToEntities()        [Neo4j]        │
│                                                              │
└────────────────────────────────────────────────────────────┘

┌────────────────────────────────────────────────────────────┐
│                     Recall Pipeline                          │
│                                                              │
│  1. embedder.Embed(query)                                    │
│  2. store.Search(vector, top-50)          [Qdrant]           │
│  3. graph.SearchFacts(query, budget)      [Neo4j hybrid]     │
│       ├── BM25 fulltext on fact text                         │
│       ├── Cosine similarity on fact embedding                │
│       └── 1-hop BFS from matched entities                    │
│  4. RRF merge graph results → memory IDs                     │
│  5. Fetch graph-sourced memories via store.Get()              │
│  6. Dedup by memory ID (Qdrant results take priority)        │
│  7. recaller.Rank(merged candidates)      [8-factor scoring] │
│  8. tokenizer.FormatMemoriesWithBudget()                     │
│                                                              │
└────────────────────────────────────────────────────────────┘
```

## Data Model

### `models.Fact` (NEW — inspired by Graphiti's EntityEdge)

A fact is a relationship between two entities with bi-temporal validity:

```go
type Fact struct {
    ID              string     `json:"id"`               // UUID
    SourceEntityID  string     `json:"source_entity_id"` // source entity UUID
    TargetEntityID  string     `json:"target_entity_id"` // target entity UUID
    RelationType    string     `json:"relation_type"`    // SCREAMING_SNAKE_CASE, e.g. "WORKS_AT"
    Fact            string     `json:"fact"`             // natural language, e.g. "Alice works at Acme Corp"
    FactEmbedding   []float32  `json:"fact_embedding"`   // 768-dim nomic-embed-text

    // Bi-temporal fields (adopted from Graphiti)
    CreatedAt       time.Time  `json:"created_at"`        // system time: when we first recorded this
    ExpiredAt       *time.Time `json:"expired_at"`        // system time: when we marked this superseded
    ValidAt         *time.Time `json:"valid_at"`          // world time: when this became true
    InvalidAt       *time.Time `json:"invalid_at"`        // world time: when this stopped being true

    // Provenance
    SourceMemoryIDs []string   `json:"source_memory_ids"` // memories this fact was derived from
    Episodes        []string   `json:"episodes"`          // episode/session IDs where observed
    Confidence      float64    `json:"confidence"`        // 0.0-1.0, boosted on re-observation
}
```

**Bi-temporal semantics** (from Graphiti): Every fact tracks two independent time axes:
- **System time** (`CreatedAt` / `ExpiredAt`): When did our system learn/invalidate this fact?
- **World time** (`ValidAt` / `InvalidAt`): When was this actually true in the real world?

When a new fact contradicts an old one, the old fact gets `ExpiredAt` and `InvalidAt` set but is **never deleted** — preserving full history for point-in-time queries.

### `models.Entity` (EXTENDED)

Add to the existing `Entity` struct:

```go
type Entity struct {
    // ... existing fields (ID, Name, Type, Aliases, MemoryIDs, CreatedAt, UpdatedAt, Metadata)

    // NEW: adopted from Graphiti
    Summary       string    `json:"summary,omitempty"`        // LLM-generated evolving summary
    NameEmbedding []float32 `json:"name_embedding,omitempty"` // 768-dim for similarity search
    CommunityID   string    `json:"community_id,omitempty"`   // Phase 3: cluster membership
}
```

### Neo4j Schema

**Node labels:**
- `Entity` — all entity nodes (plus dynamic type labels: `Person`, `Project`, `System`, `Decision`, `Concept`)
- `Community` — entity clusters (Phase 3)

**Relationship types:**
- `RELATES_TO` — `(Entity)-[:RELATES_TO]->(Entity)` — entity-to-entity facts (carries all `Fact` fields as properties)
- `DERIVED_FROM` — `(Entity)-[:RELATES_TO {uuid}]->(Entity)` edge has `source_memory_ids` property linking to Qdrant memory UUIDs. Additionally, a `DERIVED_FROM` relationship `(Entity)-[:DERIVED_FROM]->(Memory)` is created as a lightweight provenance pointer (Memory node is a stub with just `uuid` — actual memory data lives in Qdrant).
- `HAS_MEMBER` — `(Community)-[:HAS_MEMBER]->(Entity)` membership (Phase 3)

**Indexes:**

| Index Type | Target | Fields | Purpose |
|---|---|---|---|
| Range | Entity | `uuid` | Lookup by ID |
| Range | Entity | `project` | Project/tenant isolation (maps to `Memory.Project`) |
| Range | RELATES_TO | `uuid` | Lookup by ID |
| Range | RELATES_TO | `created_at`, `expired_at`, `valid_at`, `invalid_at` | Temporal queries |
| Fulltext | Entity | `name`, `summary` | BM25 entity search |
| Fulltext | RELATES_TO | `relation_type`, `fact` | BM25 fact search |

Vector similarity is computed at query time using stored embeddings (same approach as Graphiti — no declarative vector index needed for Neo4j).

**Neo4j edition note:** All features used (fulltext indexes, Bolt driver, `vector.similarity.cosine()`) are available in Neo4j Community Edition (free, open source). Neo4j 5.13+ required for `vector.similarity.cosine`. The `docker-compose.yml` uses `neo4j:5` which pulls Community — no Enterprise features are required.

## Phase 1: Complete Entity Pipeline

Wire the existing `EntityExtractor` into the capture flow and expose entities via API/MCP.

### Capture Orchestration

The orchestration happens in callers (`cmd/cmd_capture.go`, API handler), not inside `ClaudeCapturer`. `Capturer` is an interface — its `Extract()` method returns memories. The caller then optionally runs:

1. `EntityExtractor.Extract(content)` → list of entities
2. `store.UpsertEntity(entity)` for each
3. `store.LinkMemoryToEntity(entityID, memoryID)` for each

Entity extraction failures are logged and skipped. The memory is stored regardless.

Hook callers skip entity extraction (latency-sensitive). CLI `capture` and API endpoint wire it in.

### API Endpoints

Add to `internal/api/server.go`:

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/entities?query=<text>&type=<type>&limit=<n>` | Search entities (limit default 10, max 100) |
| `GET` | `/v1/entities/{id}` | Get entity by ID |

**Phase 1 limitations:**
- `store.SearchEntities(ctx, name)` takes only a name string — no type filter parameter. The API handler applies `type` and `limit` filtering in-process after the store call returns all matches. Acceptable for Phase 1 entity counts; Phase 2 replaces with Neo4j hybrid search.
- Name-substring matching only, not semantic search.

### MCP Tools

Add to `internal/mcp/server.go`:

| Tool | Parameters | Description |
|------|-----------|-------------|
| `entity_search` | `query`, `type` (optional), `limit` (optional) | Search entities by name/alias |
| `entity_get` | `id` | Get entity details + linked memories |

## Phase 2: Native Graph Layer

### New Package: `internal/graph/`

**`client.go`** — Storage interface (Neo4j CRUD only, no business logic):

```go
// Client defines the interface for graph storage operations.
// All methods accept ctx for timeout/cancellation.
// Resolution logic lives in EntityResolver and FactResolver (separate types),
// matching the pattern where ConflictDetector is separate from Store.
type Client interface {
    // Schema
    EnsureSchema(ctx context.Context) error

    // Entities
    UpsertEntity(ctx context.Context, entity models.Entity) error
    SearchEntities(ctx context.Context, query string, embedding []float32, limit int) ([]EntityResult, error)
    GetEntity(ctx context.Context, id string) (*models.Entity, error)

    // Facts
    UpsertFact(ctx context.Context, fact models.Fact) error
    SearchFacts(ctx context.Context, query string, embedding []float32, limit int) ([]FactResult, error)
    InvalidateFact(ctx context.Context, id string, expiredAt, invalidAt time.Time) error
    GetFactsBetween(ctx context.Context, sourceID, targetID string) ([]models.Fact, error)
    GetFactsForEntity(ctx context.Context, entityID string) ([]models.Fact, error)
    AppendEpisode(ctx context.Context, factID, episodeID string) error

    // Provenance
    LinkFactToMemory(ctx context.Context, factID, memoryID string) error
    GetMemoryFacts(ctx context.Context, memoryID string) ([]models.Fact, error)

    // Recall integration
    RecallByGraph(ctx context.Context, query string, embedding []float32, limit int) ([]string, error) // returns memory IDs

    // Health
    Healthy(ctx context.Context) bool
    Close() error
}
```

**`types.go`** — Shared types used by both Client and resolvers:

```go
type FactAction int
const (
    FactActionInsert      FactAction = iota // new fact, no duplicates
    FactActionSkip                          // exact duplicate, append episode
    FactActionInvalidate                    // contradicts existing, invalidate old
)
```

**`neo4j.go`** — `Neo4jClient` implementation using the official `neo4j-go-driver/v5` Bolt driver. Each write operation uses an auto-commit write transaction via `session.ExecuteWrite`; parallel graph search queries run in a single read session via `session.ExecuteRead`. Schema creation (`EnsureSchema`) uses schema transactions as required by Neo4j 5 for index creation.

**`mock_client.go`** — `MockGraphClient` for tests.

**`entity_resolver.go`** — `EntityResolver` (standalone type, like `ConflictDetector`). Takes `Client` for candidate retrieval + Claude Haiku for LLM fallback.

**`fact_resolver.go`** — `FactResolver` (standalone type). Takes `Client` for candidate retrieval + Claude Haiku for contradiction detection.

**Package layering note:** `EntityExtractor` remains in `internal/capture/` (existing code). `FactExtractor`, `EntityResolver`, and `FactResolver` live in `internal/graph/` (new code). The capture orchestrator in `cmd/cmd_capture.go` imports both packages. This mirrors how `cmd/` already imports both `internal/capture` and `internal/store`.

### Entity Resolution (3-stage, adapted from Graphiti)

When a new entity is extracted, it must be resolved against existing entities to prevent duplicates:

**Stage 1: Candidate retrieval**
Run Neo4j fulltext search on `name` + `summary` and cosine similarity on `name_embedding` in parallel. Deduplicate candidates by UUID. This narrows the search space.

**Stage 2: Deterministic fast-path**
- Exact name match (case-insensitive) → resolve immediately
- Alias match → resolve immediately
- Name embedding cosine similarity > 0.95 → resolve immediately
This avoids Claude API calls for obvious matches.

**Stage 3: Claude Haiku fallback**
For unresolved candidates, ask Claude:

```
You are an entity resolution system. Determine if the NEW ENTITY is a duplicate
of any EXISTING ENTITY. Entities are duplicates only if they refer to the same
real-world object or concept. Semantic equivalence is allowed (e.g., "the CEO"
= "John Smith" if context makes it clear).

<new_entity>Name: {name}, Type: {type}</new_entity>
<existing_entities>{numbered list}</existing_entities>
<context>{recent conversation text}</context>

Return JSON: {"is_duplicate": bool, "existing_id": "id or empty"}
```

On Claude API error or malformed response: treat as new entity (safe default). This is the same graceful degradation pattern used throughout openclaw-cortex.

### Fact Extraction (NEW — adapted from Graphiti)

New `FactExtractor` in `internal/graph/fact_extractor.go`, backed by Claude Haiku:

```
You are a fact extractor. Extract relationship facts from the conversation text.

For each fact provide:
- source_entity_name: must match one of the KNOWN ENTITIES exactly
- target_entity_name: must match one of the KNOWN ENTITIES exactly
- relation_type: SCREAMING_SNAKE_CASE (e.g., WORKS_AT, DEPENDS_ON, DECIDED_TO)
- fact: natural language description, paraphrased (not verbatim quotes)
- valid_at: ISO 8601 if the text states when the fact became true, null if ongoing or unknown
- invalid_at: ISO 8601 if the text states when the fact ended, null otherwise

Rules:
- Only extract facts between two DISTINCT known entities
- Do not invent entities not in the provided list
- Set valid_at to null for ongoing facts with no known start date
- Only set invalid_at when the text explicitly states something has ended
- Do not hallucinate temporal bounds — leave null when uncertain

<known_entities>{entity names from extraction step}</known_entities>
<content>{XML-escaped conversation text}</content>
<reference_time>{current timestamp}</reference_time>

Return JSON array of facts. Return [] if no relationship facts found.
```

### Fact Resolution (adapted from Graphiti's edge resolution)

When a new fact is extracted, resolve it against existing facts between the same entity pair:

1. **Fast path**: If fact text + endpoints match an existing fact exactly, append the new episode/memory ID to the existing fact's `SourceMemoryIDs` and `Episodes`. Return `FactActionSkip`.

2. **Candidate retrieval**: Query all `RELATES_TO` edges between the same source-target pair (duplicate candidates) + broader semantic search across all edges from either entity (contradiction candidates).

3. **Claude Haiku resolution** (adapted from Graphiti's two-list prompt):

```
You are a fact resolution system. Determine if the NEW FACT duplicates or
contradicts any existing facts.

EXISTING FACTS (same entity pair):
{numbered list with indices 0..n-1}

BROADER FACTS (related entities):
{numbered list with indices n..m}

NEW FACT: {fact text}

A duplicate means semantically the same fact, possibly with updated details.
A contradiction means the new fact directly invalidates an old one.
A fact can be BOTH a duplicate AND contradicted (e.g., same relationship but updated).

Return JSON: {"duplicate_indices": [int], "contradicted_indices": [int]}
```

4. **Apply actions**:
   - Contradicted facts: set `ExpiredAt = now()`, `InvalidAt = now()` (never deleted)
   - Duplicate facts: append episode to existing fact's `Episodes` list
   - If neither: insert as new fact (`FactActionInsert`)

### Hybrid Graph Search with RRF (adapted from Graphiti)

`GraphSearcher` in `internal/graph/search.go` runs three retrieval methods in parallel, each returning `2 × limit` candidates:

**Method 1: BM25 fulltext**
```cypher
CALL db.index.fulltext.queryRelationships("fact_fulltext", $query, {limit: $limit})
YIELD relationship, score
WITH relationship AS r, score
MATCH (s:Entity)-[r]->(t:Entity)
WHERE r.expired_at IS NULL
RETURN r.uuid, r.fact, r.source_memory_ids, score
```

**Method 2: Cosine similarity**
```cypher
MATCH (s:Entity)-[r:RELATES_TO]->(t:Entity)
WHERE r.expired_at IS NULL
WITH r, vector.similarity.cosine(r.fact_embedding, $query_embedding) AS score
WHERE score > 0.6
RETURN r.uuid, r.fact, r.source_memory_ids, score
ORDER BY score DESC LIMIT $limit
```

**Performance note:** Method 2 computes cosine similarity at query time over all `RELATES_TO` edges (no vector index on relationships in Neo4j 5). This is a full scan — acceptable for graphs under ~100K edges. At larger scale, pre-filter by entity proximity or switch to application-side vector comparison. Will revisit when Neo4j adds vector index support for relationships.

**Method 3: 1-hop BFS from entity matches (undirected)**
```cypher
MATCH (n:Entity)
WHERE vector.similarity.cosine(n.name_embedding, $query_embedding) > 0.7
MATCH (n)-[r:RELATES_TO]-(m:Entity)
WHERE r.expired_at IS NULL
RETURN DISTINCT r.uuid, r.fact, r.source_memory_ids
LIMIT $limit
```

Note: Method 3 uses undirected traversal (`-[r]-` not `-[r]->`) intentionally — BFS should find facts where the matched entity is either source or target.

**RRF Merge** (from Graphiti):
```go
// Reciprocal Rank Fusion with k=60 (Cormack et al. 2009)
// For each fact UUID appearing in any result list:
// score[uuid] += 1.0 / (rank + 60)  // rank is 1-based, summed across all methods
// Sort by score descending, take top limit
```

The merged results yield `source_memory_ids` — these are the memory IDs to fetch from Qdrant and merge into the recall candidate set.

### Recall Integration

Modify `internal/recall/recall.go`:

```
Recall(ctx context.Context, query string, ...) ([]RecallResult, error):
  1. embedder.Embed(query)
  2. store.Search(vector, top-50, filters)              // existing Qdrant path
  3. IF graph != nil:
       graphMemIDs := graph.RecallByGraph(ctx, query, vector, 20)  // with latency budget
       for each ID not already in Qdrant results:
           mem := store.Get(ctx, id)
           append to candidates with similarity=0 (graph-sourced)
  4. recaller.Rank(allCandidates, query)                // full 8-factor scoring
  5. tokenizer.FormatMemoriesWithBudget()
```

Graph-sourced memories enter ranking with `similarity=0` since they weren't retrieved by vector search. They compete on other factors (recency, type, confidence, tag affinity, etc.) and on whatever graph relevance they carry. This is conservative — graph results must earn their place.

**Note:** The recall scoring formula is the 8-factor weighted sum from v0.4.0 (`internal/recall/recall.go`), not the 5-factor formula in CLAUDE.md (which is stale). The authoritative formula: `0.35*similarity + 0.15*recency + 0.10*frequency + 0.10*typeBoost + 0.08*scopeBoost + 0.10*confidence + 0.07*reinforcement + 0.05*tagAffinity` plus supersession and conflict penalties.

### Entity Summary Generation

When an entity is updated (new facts added), regenerate its summary via Claude Haiku:

```
Summarize this entity based on its known facts. Keep it under 200 characters.

Entity: {name} ({type})
Known facts:
{list of active facts involving this entity}

Return a single concise summary sentence.
```

Summaries are regenerated lazily — on entity access, if facts have changed since last summary. This avoids unnecessary API calls.

### Configuration

```yaml
graph:
  enabled: false
  neo4j:
    uri: bolt://localhost:7687
    username: neo4j
    password: openclaw-cortex
    database: neo4j
  entity_resolution:
    similarity_threshold: 0.95    # deterministic fast-path threshold
    max_candidates: 10            # max candidates for LLM resolution
  fact_extraction:
    enabled: true                 # can disable fact extraction independently
  recall_budget_ms: 50            # hook context latency budget (entire graph call)
  recall_budget_cli_ms: 500       # CLI context latency budget (entire graph call)
```

**Go config struct** (added to `internal/config/config.go`):

```go
type GraphConfig struct {
    Enabled          bool                   `mapstructure:"enabled"`
    Neo4j            Neo4jConfig            `mapstructure:"neo4j"`
    EntityResolution EntityResolutionConfig `mapstructure:"entity_resolution"`
    FactExtraction   FactExtractionConfig   `mapstructure:"fact_extraction"`
    RecallBudgetMs   int                    `mapstructure:"recall_budget_ms"`
    RecallBudgetCLIMs int                   `mapstructure:"recall_budget_cli_ms"`
}

type Neo4jConfig struct {
    URI      string `mapstructure:"uri"`
    Username string `mapstructure:"username"`
    Password string `mapstructure:"password"`
    Database string `mapstructure:"database"`
}

type EntityResolutionConfig struct {
    SimilarityThreshold float64 `mapstructure:"similarity_threshold"`
    MaxCandidates       int     `mapstructure:"max_candidates"`
}

type FactExtractionConfig struct {
    Enabled bool `mapstructure:"enabled"`
}
```

A `Graph GraphConfig` field is added to the top-level `Config` struct.

Environment variable overrides:
- `OPENCLAW_CORTEX_GRAPH_ENABLED`
- `OPENCLAW_CORTEX_GRAPH_NEO4J_URI`
- `OPENCLAW_CORTEX_GRAPH_NEO4J_USERNAME`
- `OPENCLAW_CORTEX_GRAPH_NEO4J_PASSWORD`
- `OPENCLAW_CORTEX_GRAPH_RECALL_BUDGET_MS`
- `OPENCLAW_CORTEX_GRAPH_RECALL_BUDGET_CLI_MS`

### Docker Compose

Add to existing `docker-compose.yml` (commented out by default):

```yaml
# Uncomment to enable entity-relationship graph (Phase 2)
# neo4j:
#   image: neo4j:5
#   ports:
#     - "7474:7474"   # HTTP browser
#     - "7687:7687"   # Bolt protocol
#   environment:
#     NEO4J_AUTH: neo4j/openclaw-cortex
#     NEO4J_PLUGINS: '[]'
#   volumes:
#     - neo4j_data:/data
```

No OpenAI key. No Graphiti container. Just Neo4j — everything else runs in-process with Claude Haiku.

## Error Handling & Graceful Degradation

Both phases follow the established pattern — optional services never block core operations.

**Phase 1 (Entity Pipeline):**
- `EntityExtractor` failures during capture: logged and skipped, memory still stored
- API/MCP entity endpoints: standard error responses (404 missing, 500 store errors)

**Phase 2 (Graph Layer):**
- All `graph.Client` calls use `context.WithTimeout`
- **Entity resolution**: Claude API error → treat as new entity (safe default)
- **Fact extraction**: Claude API error → skip fact extraction, memory still stored
- **Fact resolution**: Claude API error → treat as new fact (insert, don't invalidate)
- **Write path** (capture): Neo4j failure → log warning, skip graph write. Memory stored in Qdrant regardless.
- **Read path** (recall): Neo4j failure → log warning, return empty graph results. Qdrant results proceed through ranking normally.
- **Delete path**: Neo4j failure → log error, return error to caller (orphaned graph data should surface because it can't be auto-cleaned).
- **Health**: `cortex stats` / `cortex health` reports Neo4j + graph status. Phase 1 adds entity count to `cortex stats`. Phase 2 adds fact count, graph health. Unhealthy graph doesn't affect overall health — marked as "optional: degraded".

**Delete path asymmetry rationale:** Write-path failures are silenced because the memory is safely in Qdrant — no data loss. Delete-path failures surface because orphaned Neo4j data can't be automatically cleaned.

**Latency budgets (recall):**

| Context | Graph Budget (entire call) | Total Recall Budget |
|---------|---------------------------|---------------------|
| Hook (PreTurnHook) | 50ms | 100ms |
| CLI (`cortex recall`) | 500ms | 3000ms |

On timeout: use Qdrant-only results.

## Migration & Backfill

Existing memories in Qdrant have no entity links or graph data. Both phases only process new captures. A future `cortex reindex-entities` command could backfill by re-running entity and fact extraction over existing memories, but this is not required for the integration to be useful. New captures progressively build the entity graph.

## Phase 3: Community Intelligence (Future)

Adopted from Graphiti's community detection, deferred to a future spec:

- **Label propagation** clusters densely connected entities into communities
- **Claude Haiku** summarizes each community (hierarchical pair-wise reduction)
- **Community-aware recall**: high-level context injection ("This conversation involves the Authentication team, which handles OAuth, SSO, and API keys")
- **Community search**: BM25 + cosine on community `name` + `summary`

This is noted here for architectural awareness but is NOT part of the current implementation scope.

## Testing Strategy

All tests in top-level `tests/` package per project convention. All use `MockStore` for Qdrant and `MockGraphClient` for Neo4j — no live services needed for `go test -short`.

**Phase 1 tests:**

| File | Coverage |
|------|----------|
| `tests/entity_pipeline_test.go` | Capture with entity extraction → verify `MockStore.UpsertEntity` and `LinkMemoryToEntity` called |
| `tests/entity_api_test.go` | HTTP endpoint tests for entity search and get |
| `tests/entity_mcp_test.go` | MCP tool contract tests for `entity_search`, `entity_get` |

**Phase 2 tests:**

| File | Coverage |
|------|----------|
| `tests/graph_client_test.go` | Neo4j client with mock driver — entity/fact CRUD, schema creation |
| `tests/entity_resolution_test.go` | Three-stage resolution: exact match, alias match, similarity fast-path, Claude fallback, graceful degradation on API error |
| `tests/fact_extraction_test.go` | Fact extractor with mock Claude responses — valid facts, empty results, malformed JSON, API errors |
| `tests/fact_resolution_test.go` | Fact resolution: exact dedup, contradiction detection, two-list prompt, edge invalidation |
| `tests/graph_search_test.go` | Hybrid search: BM25 + cosine + BFS results, RRF merge, dedup by UUID |
| `tests/graph_recall_merge_test.go` | Recall integration: Qdrant + graph results merged, deduped by memory ID, ranked |
| `tests/graph_degradation_test.go` | Graceful degradation: Neo4j down → Qdrant-only recall; Claude API down → safe defaults |

**Integration test** (behind build tag):

| File | Coverage |
|------|----------|
| `tests/integration/graph_integration_test.go` | `//go:build integration` — real Neo4j via testcontainers (`github.com/testcontainers/testcontainers-go`), full capture-to-recall round-trip |

## Files Changed

### Phase 1

| Action | File | What |
|--------|------|------|
| Modify | `cmd/openclaw-cortex/cmd_capture.go` | Wire EntityExtractor call after Capturer.Extract() |
| Modify | `internal/api/server.go` | Add entity search/get routes |
| Modify | `internal/mcp/server.go` | Add entity_search, entity_get tools |
| Modify | `cmd/openclaw-cortex/cmd_stats.go` | Add entity count to stats output (calls `store.SearchEntities(ctx, "")` and counts results; Phase 2 replaces with `graph.Client` count query) |
| Create | `tests/entity_pipeline_test.go` | Capture + entity extraction tests |
| Create | `tests/entity_api_test.go` | HTTP endpoint tests |
| Create | `tests/entity_mcp_test.go` | MCP tool contract tests |

### Phase 2

| Action | File | What |
|--------|------|------|
| Create | `internal/models/fact.go` | `Fact` struct with bi-temporal fields |
| Modify | `internal/models/entity.go` | Add `Summary`, `NameEmbedding`, `CommunityID` fields |
| Create | `internal/graph/client.go` | `Client` interface (storage only) |
| Create | `internal/graph/types.go` | `FactAction`, `EntityResult`, `FactResult` shared types |
| Create | `internal/graph/neo4j.go` | `Neo4jClient` implementation (Bolt driver) |
| Create | `internal/graph/entity_resolver.go` | Three-stage entity resolution |
| Create | `internal/graph/fact_extractor.go` | Claude Haiku fact extraction |
| Create | `internal/graph/fact_resolver.go` | Fact dedup + contradiction detection |
| Create | `internal/graph/search.go` | Hybrid search (BM25 + cosine + BFS) with RRF |
| Create | `internal/graph/mock_client.go` | MockGraphClient for tests |
| Modify | `internal/config/config.go` | `graph.*` config keys |
| Modify | `internal/capture/capture.go` | Fact extraction + graph write when enabled |
| Modify | `internal/recall/recall.go` | Graph recall merge: Qdrant + graph → dedup → Rank() |
| Modify | `cmd/openclaw-cortex/main.go` | Wire graph.Client when enabled |
| Modify | `cmd/openclaw-cortex/cmd_health.go` | Report Neo4j/graph health as optional service |
| Modify | `docker-compose.yml` | Add Neo4j service (commented out) |
| Create | `tests/graph_client_test.go` | Neo4j client tests |
| Create | `tests/entity_resolution_test.go` | Three-stage resolution tests |
| Create | `tests/fact_extraction_test.go` | Fact extractor tests |
| Create | `tests/fact_resolution_test.go` | Fact resolution + invalidation tests |
| Create | `tests/graph_search_test.go` | Hybrid search + RRF tests |
| Create | `tests/graph_recall_merge_test.go` | Recall merge + dedup tests |
| Create | `tests/graph_degradation_test.go` | Graceful degradation tests |
| Create | `tests/integration/graph_integration_test.go` | Full round-trip integration test |
| Modify | `go.mod` / `go.sum` | Add `neo4j-go-driver/v5`, `testcontainers-go` |

## Dependencies

| Dependency | Purpose | Notes |
|---|---|---|
| `github.com/neo4j/neo4j-go-driver/v5` | Neo4j Bolt protocol driver | Official Go driver, well-maintained |
| `github.com/testcontainers/testcontainers-go` | Integration tests only | Behind `//go:build integration` tag |
| Neo4j 5 (Docker) | Graph database | Self-hosted, no external API dependency |
| Claude Haiku (Anthropic API) | Entity resolution, fact extraction, fact resolution, entity summaries | Already a dependency for capture — no new API keys needed |
| Ollama / nomic-embed-text | Embeddings for entity names and fact text | Already a dependency — no new services |

**No OpenAI. No Graphiti. No new API keys.** The entire graph layer runs on the same Claude + Ollama + self-hosted infrastructure that openclaw-cortex already uses.
