# Graphiti-Inspired Features Design Spec
**Date:** 2026-03-15  
**Version:** draft-1.0  
**Author:** Jarvis (Subagent)  
**Target:** openclaw-cortex v0.8.0  

---

## Executive Summary

This spec designs four Graphiti-inspired capabilities for openclaw-cortex: temporal versioning, contradiction detection, episodic→semantic triple extraction, and graph-aware recall. 

**Critical finding:** The codebase is further along than the task implies. Several foundations already exist:
- `models.Fact` already has bi-temporal fields (`ValidAt`, `InvalidAt`, `ExpiredAt`, `CreatedAt`)
- `graph.Client` has `InvalidateFact`, `RecallByGraph`, `GetFactsForEntity` 
- `graph.FactResolver` already does duplicate/contradiction detection via LLM
- `graph.RRFMerge` / `HybridSearch` already merge ranked lists
- `graph.EntityResolver` does three-stage entity resolution
- `models.Memory` has `SupersedesID`, `ConflictGroupID`, `ConflictStatus`

This means the spec is mostly about **wiring and completing** partially-built systems, not building from scratch. Effort estimates reflect this.

---

## 1. Current Architecture Snapshot

### Data Model
```
Memory (node, :Memory label)
  uuid, type, scope, visibility, content, confidence, source
  tags[], project, ttl_seconds
  created_at, updated_at, last_accessed, access_count
  reinforced_at, reinforced_count
  supersedes_id, valid_until
  conflict_group_id, conflict_status
  embedding: float[768]

Entity (node, :Entity label)
  uuid, name, type, aliases[], summary
  project, community_id
  name_embedding: float[768]

Fact (edge via :RELATES_TO relationship)
  id, source_entity_id, target_entity_id
  relation_type, fact
  created_at, expired_at, valid_at, invalid_at
  source_memory_ids[], episodes[]
  confidence
  fact_embedding: float[768]
```

### Vector Indexes (Memgraph)
- `memory_embedding ON :Memory(embedding)` — dim 768, cosine
- `entity_name_embedding ON :Entity(name_embedding)` — dim 768, cosine

### Key Interfaces
- `store.Store` — CRUD + vector search for Memory nodes
- `graph.Client` — Entity/Fact graph operations
- `internal/recall/recall.go` — multi-factor ranking engine
- `internal/capture/` — LLM-driven memory extraction from conversations
- `internal/graph/fact_resolver.go` — duplicate/contradiction detection
- `internal/graph/entity_resolver.go` — entity dedup via 3-stage resolution

---

## 2. Gap Analysis vs. Graphiti

| Graphiti Feature | Cortex Status | Gap |
|---|---|---|
| Temporal fact validity windows | ✅ `Fact.ValidAt/InvalidAt` model exists | Not persisted/queried in Memgraph; Memory nodes lack `valid_from/valid_to` |
| Fact invalidation (not deletion) | ✅ `graph.Client.InvalidateFact` exists | Not called from store/capture pipeline |
| Episode provenance | ✅ `Fact.Episodes[]` field exists | Not populated from `capture` command |
| Contradiction detection | ✅ `FactResolver.Resolve` exists | Memory-level contradiction (not fact-level) uses ConflictGroupID; wiring incomplete |
| Triple extraction from conversations | ✅ `FactExtractor` + `EntityResolver` exist | `capture` command only stores blob Memories, doesn't call graph pipeline |
| Graph-aware recall | ✅ `graph.Client.RecallByGraph` interface exists | Not implemented in `MemgraphStore`; recall.go doesn't merge graph results |
| Memory temporal versioning | ⚠️ `SupersedesID` chain exists | No `valid_from/valid_to` on Memory nodes; no invalidation on store |
| BM25 full-text search for facts | ❌ | Planned (text index exists on :Entity); fact text search uses CONTAINS |

---

## 3. Schema Changes

### 3.1 Memory Node — Temporal Fields

Add two new properties to all `:Memory` nodes:

```cypher
-- New properties on :Memory
valid_from: datetime   -- when this memory became valid (= created_at for existing)
valid_to:   datetime   -- when this memory was superseded (null = currently valid)
```

**Reasoning:** `valid_until` already exists but means TTL expiry (time-to-live). These new fields mean "worldtime validity" — when did this fact become/stop being true. Keep both distinct.

**New index:**
```cypher
CREATE INDEX ON :Memory(valid_from);
CREATE INDEX ON :Memory(valid_to);
```

### 3.2 Fact Edge — Persisted Temporal Fields

The `Fact` model already has `ValidAt/InvalidAt/ExpiredAt`. Ensure these are written to the `:RELATES_TO` edge in Memgraph (currently not confirmed in store.go):

```cypher
-- Ensure :RELATES_TO edges store:
valid_at:   datetime   -- world-time when relationship became true
invalid_at: datetime   -- world-time when relationship ended (null = currently true)
expired_at: datetime   -- system-time when we recorded the invalidation
```

**New index:**
```cypher
CREATE INDEX ON :RELATES_TO(valid_at);
CREATE INDEX ON :RELATES_TO(invalid_at);
CREATE INDEX ON :RELATES_TO(id);
```

Note: Memgraph supports relationship property indexes since 2.5.

### 3.3 Episode Node (New)

Add a lightweight `:Episode` node to capture conversation provenance:

```cypher
(:Episode {
  uuid:        string,   -- UUID
  session_id:  string,   -- OpenClaw session ID
  user_msg:    string,   -- user message (truncated at 2000 chars)
  assistant_msg: string, -- assistant response (truncated at 2000 chars)
  captured_at: datetime,
  memory_ids:  string[], -- Memory UUIDs derived from this episode
  fact_ids:    string[]  -- Fact IDs extracted from this episode
})
```

Edges:
```
(:Episode)-[:PRODUCED]->(:Memory)
(:Episode)-[:PRODUCED_FACT]->(:Memory)  // via :RELATES_TO
```

This satisfies Graphiti's "episodes as provenance" without changing Memory or Fact models.

**New constraint + index:**
```cypher
CREATE CONSTRAINT ON (e:Episode) ASSERT e.uuid IS UNIQUE;
CREATE INDEX ON :Episode(session_id);
CREATE INDEX ON :Episode(captured_at);
```

### 3.4 Full Schema DDL Summary (additions only)

```cypher
-- Temporal indexes on Memory
CREATE INDEX ON :Memory(valid_from);
CREATE INDEX ON :Memory(valid_to);

-- Relationship indexes (requires Memgraph 2.5+, we have 2.21)
CREATE INDEX ON :RELATES_TO(id);
CREATE INDEX ON :RELATES_TO(invalid_at);

-- Episode nodes
CREATE CONSTRAINT ON (e:Episode) ASSERT e.uuid IS UNIQUE;
CREATE INDEX ON :Episode(session_id);
CREATE INDEX ON :Episode(captured_at);

-- Fact embedding vector index (if not already created)
CREATE VECTOR INDEX fact_embedding ON :RELATES_TO(fact_embedding) 
  WITH CONFIG {"dimension": 768, "metric": "cos", "capacity": 50000};
```

---

## 4. Feature Design

### 4.1 Temporal Versioning for Memory Nodes

#### Goal
When `store memory-update` or `capture` produces a new memory that supersedes an old one, the old memory gets `valid_to = now()` set. Recall by default returns only `valid_to IS NULL` memories, with `--include-history` flag to include expired.

#### Model Changes

```go
// internal/models/memory.go — additions
type Memory struct {
    // ... existing fields ...
    
    // ValidFrom is when this memory version became the current truth.
    // Set to CreatedAt on first write. Immutable after initial store.
    ValidFrom time.Time `json:"valid_from,omitempty"`
    
    // ValidTo is set when this memory is superseded. Zero = currently valid.
    ValidTo *time.Time `json:"valid_to,omitempty"`
    
    // IsCurrentVersion is derived (not stored): ValidTo == nil
    // Used for display only.
    IsCurrentVersion bool `json:"is_current_version,omitempty"`
}
```

#### store.Store Interface Changes

```go
// Add to store.Store interface:

// InvalidateMemory sets valid_to on a memory without deleting it.
// Used when a superseding memory is stored.
InvalidateMemory(ctx context.Context, id string, validTo time.Time) error

// GetHistory returns all versions of a memory chain, including invalidated ones.
// Uses SupersedesID chain traversal.
GetHistory(ctx context.Context, id string) ([]Memory, error)
```

#### Upsert Logic Change

When `store.Upsert` is called with `memory.SupersedesID != ""`:
1. Set `valid_to = now()` on the superseded memory (call `InvalidateMemory`)
2. Set `valid_from = now()` on the new memory
3. Store as normal

```go
// internal/memgraph/store.go — Upsert modification
func (s *MemgraphStore) Upsert(ctx context.Context, memory models.Memory, vector []float32) error {
    // ... existing code ...
    
    // NEW: if this supersedes another memory, invalidate it
    if memory.SupersedesID != "" {
        now := time.Now().UTC()
        if err := s.InvalidateMemory(ctx, memory.SupersedesID, now); err != nil {
            s.logger.Warn("failed to invalidate superseded memory", 
                "superseded_id", memory.SupersedesID, "error", err)
            // non-fatal: still store the new memory
        }
    }
    
    // Set valid_from if not set
    if memory.ValidFrom.IsZero() {
        memory.ValidFrom = time.Now().UTC()
    }
}
```

#### Search/Recall Filtering

Default behavior: exclude `valid_to IS NOT NULL` memories from all search/recall results.

```cypher
-- Default recall query addition
WHERE m.valid_to IS NULL OR m.valid_to > datetime()
```

Flag: `--include-history` or filter `IncludeInvalidated: true` in `SearchFilters`.

```go
// internal/store/store.go — SearchFilters addition
type SearchFilters struct {
    // ... existing fields ...
    
    // IncludeInvalidated includes memories with valid_to set (historical versions).
    // Default: false (only return currently-valid memories)
    IncludeInvalidated bool `json:"include_invalidated,omitempty"`
    
    // AsOf returns the state of memories valid at a specific point in time.
    // When set, returns memories where valid_from <= AsOf AND (valid_to IS NULL OR valid_to > AsOf)
    AsOf *time.Time `json:"as_of,omitempty"`
}
```

### 4.2 Contradiction Detection

#### Current State
`FactResolver.Resolve` already detects contradictions between `Fact` edges. `ConflictDetector` exists for `Memory` nodes but uses a different mechanism (ConflictGroupID). The gap: capture pipeline doesn't call either consistently.

#### Design: Unified Contradiction Pipeline

**Stage 1: Candidate Retrieval (fast, ~10ms)**
```
new_memory_content → embed → vector_search(top_20, similarity > 0.75)
                   + entity_extract → entity_lookup → GetMemoryFacts() for matching entities
Merge candidates by ID (dedup)
```

**Stage 2: Heuristic Filter (fast, ~2ms)**
For each candidate memory, check:
- Same extracted entities (>1 shared entity) AND
- Conflicting predicate signals (e.g., "works at" → extract org names, check if different)

Predicate signals are detected via regex patterns on the `relation_type` field of extracted facts:
```go
// internal/capture/contradiction.go
var exclusivePairs = map[string]string{
    "WORKS_AT":    "WORKS_AT",
    "HAS_ROLE":    "HAS_ROLE",
    "LOCATED_IN":  "LOCATED_IN",
    "MARRIED_TO":  "MARRIED_TO",
    "REPORTS_TO":  "REPORTS_TO",
}
```

**Stage 3: LLM Confirmation (optional, ~50-150ms)**
Only invoked when Stage 2 returns candidates with ambiguous conflict signals.

```go
// Reuse FactResolver pattern:
type MemoryContradictionChecker struct {
    client llm.LLMClient
    model  string
    logger *slog.Logger
}

func (c *MemoryContradictionChecker) Check(ctx context.Context, newContent string, candidates []models.Memory) ([]string, error)
// Returns IDs of memories that are contradicted by newContent
```

**Contradiction Resolution Actions:**
1. `contradicted_ids` → set `valid_to = now()` on each (invalidate, don't delete)
2. New memory gets `valid_from = now()`
3. Link via `supersedes_id` chain if the contradiction is a direct update

**Configuration:**
```yaml
# config.yaml additions
contradiction:
  enabled: true
  similarity_threshold: 0.75    # min cosine similarity to be a candidate
  max_candidates: 20            # max memories to check
  llm_confirm_threshold: 0.82   # above this similarity, skip LLM (auto-invalidate)
  llm_timeout_ms: 150           # skip LLM if over budget
```

#### Wire-up in capture pipeline

```go
// cmd/openclaw-cortex/cmd_capture.go — after Extract(), before Upsert loop
if cfg.Contradiction.Enabled {
    detector := capture.NewContradictionDetector(st, emb, llmClient, cfg.Contradiction, logger)
    for i := range memories {
        contradicted, err := detector.FindContradictions(ctx, memories[i])
        for _, cid := range contradicted {
            _ = st.InvalidateMemory(ctx, cid, time.Now())
        }
    }
}
```

### 4.3 Episodic → Semantic Triple Extraction

#### Current State
`capture` command extracts blob Memories via `Capturer.Extract()`. Entity and fact extraction exists in `graph.EntityResolver` and `graph.FactExtractor` but is NOT called from `capture`. It appears to be wired somewhere (cmd_hook.go?) but not in cmd_capture.go.

#### Design: Capture → Graph Pipeline

The capture command should run a two-phase pipeline:

**Phase 1: Memory blob extraction (existing)**
```
conversation → Capturer.Extract() → []CapturedMemory → Upsert into Memory nodes
```

**Phase 2: Triple extraction (new wiring)**
```
conversation → EntityExtractor → []Entity (resolved/deduplicated)
            → FactExtractor(known_entities) → []rawFact
            → FactResolver.Resolve() for each fact → insert/skip/invalidate
            → UpsertFact() with valid_at, episode linkage
            → Create :Episode node linking Memory IDs and Fact IDs
```

The key insight: these phases can run **in parallel** since Phase 1 stores Memories and Phase 2 stores Facts independently. They're linked via `source_memory_ids` on Fact and `MemoryIDs` on Entity.

```go
// cmd/openclaw-cortex/cmd_capture.go — RunE restructure
var (
    memories []models.Memory
    facts    []models.Fact
    wg       sync.WaitGroup
    mu       sync.Mutex
    errs     []error
)

// Phase 1: Memory extraction (existing, runs in goroutine)
wg.Add(1)
go func() {
    defer wg.Done()
    extracted, err := cap.Extract(ctx, userMsg, assistantMsg)
    mu.Lock()
    if err != nil { errs = append(errs, err) }
    memories = extracted
    mu.Unlock()
}()

// Phase 2: Triple extraction (new)
if cfg.Graph.Enabled {
    wg.Add(1)
    go func() {
        defer wg.Done()
        extracted, err := extractTriples(ctx, graphClient, emb, llmClient, userMsg, assistantMsg, cfg)
        mu.Lock()
        if err != nil { errs = append(errs, fmt.Errorf("triple extraction (non-fatal): %w", err)) }
        facts = extracted
        mu.Unlock()
    }()
}

wg.Wait()
// ... store memories, store facts, create Episode node
```

#### New function: `extractTriples`

```go
// internal/capture/triple_extractor.go (new file)
package capture

type TripleExtractor struct {
    graph          graph.Client
    entityResolver *graph.EntityResolver
    factExtractor  *graph.FactExtractor
    factResolver   *graph.FactResolver
    embedder       embed.Embedder
    logger         *slog.Logger
}

func (te *TripleExtractor) Extract(ctx context.Context, content string, sessionID string) ([]models.Fact, error) {
    // 1. Extract entity candidates from content
    // 2. Resolve each entity (EntityResolver.Resolve) → get/create Entity nodes
    // 3. Extract facts given known entities (FactExtractor.Extract)
    // 4. For each fact: FactResolver.Resolve → action (insert/skip/invalidate)
    // 5. Embed fact strings for fact_embedding
    // 6. UpsertFact for inserts; InvalidateFact for contradicted old facts
    // Return []models.Fact of newly created facts
}
```

#### Episode Node Creation

After both phases complete:

```go
// internal/capture/episode.go (new file)
type EpisodeStore interface {
    CreateEpisode(ctx context.Context, ep models.Episode) error
}

// models/episode.go (new file)
type Episode struct {
    UUID         string    `json:"uuid"`
    SessionID    string    `json:"session_id"`
    UserMsg      string    `json:"user_msg"`
    AssistantMsg string    `json:"assistant_msg"`
    CapturedAt   time.Time `json:"captured_at"`
    MemoryIDs    []string  `json:"memory_ids"`
    FactIDs      []string  `json:"fact_ids"`
}
```

Episode creation is **best-effort** — failure doesn't fail the capture.

#### Relation Type Normalization

`FactExtractor` prompts Claude for SCREAMING_SNAKE_CASE relation types. Add a normalization layer with common canonical types to reduce sprawl:

```go
// internal/graph/relation_types.go (new file)
var canonicalRelations = map[string]string{
    "EMPLOYED_BY": "WORKS_AT",
    "EMPLOYEE_OF": "WORKS_AT",
    "EMPLOYED_AT": "WORKS_AT",
    "POSITION_AT": "HAS_ROLE",
    "TITLE_AT":    "HAS_ROLE",
    "LOCATED_AT":  "LOCATED_IN",
    "BASED_IN":    "LOCATED_IN",
    // ... ~30 canonical mappings
}

func NormalizeRelationType(raw string) string {
    if canon, ok := canonicalRelations[raw]; ok {
        return canon
    }
    return raw
}
```

### 4.4 Graph-Aware Recall

#### Current State
`graph.Client.RecallByGraph` interface exists. `recall.go` does vector + multi-factor ranking. They're not connected.

#### Design: Hybrid Recall Pipeline

```
query → embed → [parallel]:
  A) vector_search(Memory nodes, top 30)
  B) RecallByGraph(Entity nodes + graph traversal, top 20)
→ merge via RRF
→ multi-factor re-ranking (existing recall.go weights)
→ return top N
```

**RecallByGraph Implementation (in MemgraphStore)**

```cypher
-- Step 1: Find entities matching query via embedding similarity
CALL vector_search.search("entity_name_embedding", $limit, $query_embedding) 
YIELD node, similarity
WHERE similarity > 0.6
WITH node AS entity, similarity

-- Step 2: BFS from matching entities, 2 hops
MATCH (entity)-[:RELATES_TO*1..2]-(connected_entity)
WHERE connected_entity <> entity

-- Step 3: Find Memory nodes linked to these entities  
MATCH (connected_entity)<-[:MENTIONS]-(m:Memory)
WHERE m.valid_to IS NULL
WITH DISTINCT m.uuid AS memory_id, max(similarity) AS entity_score

RETURN memory_id, entity_score
ORDER BY entity_score DESC
LIMIT $limit
```

**Note:** This requires `:MENTIONS` edges from Memory → Entity. These should be created by `LinkMemoryToEntity` (already exists in `store.Store`). Verify `capture` calls this for extracted entities.

**Merge Strategy**

```go
// internal/recall/recall.go — new function
func (r *Ranker) RecallHybrid(ctx context.Context, query string, embedding []float32, opts RecallOptions) ([]RecallResult, error) {
    var (
        vectorResults []models.SearchResult
        graphMemIDs   []string
        wg            sync.WaitGroup
        mu            sync.Mutex
    )

    // Parallel fetch
    wg.Add(1)
    go func() {
        defer wg.Done()
        res, err := r.store.Search(ctx, embedding, uint64(opts.Limit*3), opts.Filters)
        mu.Lock()
        if err == nil { vectorResults = res }
        mu.Unlock()
    }()

    if r.graph != nil && opts.UseGraph {
        wg.Add(1)
        go func() {
            defer wg.Done()
            ids, err := r.graph.RecallByGraph(ctx, query, embedding, opts.Limit*2)
            mu.Lock()
            if err == nil { graphMemIDs = ids }
            mu.Unlock()
        }()
    }

    wg.Wait()

    // Convert graph IDs to SearchResults (fetch from store)
    graphResults := r.fetchByIDs(ctx, graphMemIDs)
    
    // RRF merge
    merged := rrfMergeMemories(vectorResults, graphResults, opts.Limit*2)
    
    // Apply multi-factor ranking (existing logic)
    return r.rank(ctx, merged, embedding, opts), nil
}
```

**RRF merge for memories:**

```go
// internal/recall/rrf.go (new file)
func rffMergeMemories(lists ...[]models.SearchResult) []models.SearchResult {
    // Same RRF algorithm as graph.RRFMerge but for SearchResult type
    // Dedup by Memory.ID, combine scores
}
```

**Graph Recall Weight in Ranker**

Add a new weight factor: `GraphHopScore` — bonus for memories reachable via graph traversal from query entities. This rewards contextually-connected memories even when vector similarity is moderate.

```go
type Weights struct {
    // ... existing ...
    GraphHop float64 `json:"graph_hop" mapstructure:"graph_hop"` // default: 0.12
}
```

Reduce `Similarity` from 0.35 → 0.28, add `GraphHop: 0.12` (weights must still sum to ~1.0).

---

## 5. API Changes

### 5.1 Modified CLI Commands

#### `recall` — new flags
```
--include-history       Include invalidated/superseded memories in results
--as-of <RFC3339>       Recall state at a specific point in time
--no-graph              Disable graph-aware recall (use vector only)
--graph-hops <int>      Max BFS hops from entity nodes (default: 2)
```

#### `store` — temporal behavior change
```
--supersedes <id>       Mark this memory as superseding the given ID (auto-invalidates old)
```
Behavior change: when `--supersedes` is provided, the superseded memory's `valid_to` is set automatically (currently only `supersedes_id` is stored as metadata).

#### `capture` — new flags
```
--no-triples            Disable triple extraction (blob memories only)
--session-id <id>       Explicit session ID for episode provenance
--episode               Output extracted Episode node summary
```

#### `history` — new command
```
cortex history <memory-id>     Show version chain of a memory
cortex history --entity <name> Show all facts involving an entity over time
```

```go
// cmd/openclaw-cortex/cmd_history.go (new file)
// Calls store.GetHistory(id) or graph.GetFactsForEntity(entityID, includeExpired=true)
```

#### `facts` — enhanced existing `entities` command
```
cortex entities facts <entity-name>    List all facts for entity (currently active)
cortex entities facts <entity-name> --history   Include expired facts
cortex entities facts <entity-name> --as-of <RFC3339>
```

### 5.2 New store.Store Methods

```go
// Additions to store.Store interface

// InvalidateMemory sets valid_to without deleting. Used for temporal versioning.
InvalidateMemory(ctx context.Context, id string, validTo time.Time) error

// SearchWithTemporalFilter is Search but with explicit temporal bounds.
// When asOf is non-nil, returns memories valid at that time.
SearchWithTemporalFilter(ctx context.Context, vector []float32, limit uint64, 
    filters *SearchFilters, asOf *time.Time) ([]SearchResult, error)
```

### 5.3 New graph.Client Methods

```go
// Additions to graph.Client interface

// CreateEpisode persists an Episode node with provenance links.
CreateEpisode(ctx context.Context, episode models.Episode) error

// GetEpisodesForMemory returns episodes that produced a given memory.
GetEpisodesForMemory(ctx context.Context, memoryID string) ([]models.Episode, error)

// GetActiveFactsForEntity returns only valid facts (invalid_at IS NULL).
// Existing GetFactsForEntity returns all.
GetActiveFactsForEntity(ctx context.Context, entityID string) ([]models.Fact, error)
```

---

## 6. Algorithm Details

### 6.1 Contradiction Detection Algorithm

```
Input: new_memory (content, embedding, extracted_entities)
Output: []string (IDs of memories to invalidate)

1. CANDIDATE RETRIEVAL (parallel, ~10ms)
   a. vector_search(new_memory.embedding, top=20, threshold=0.75) → candidates_v
   b. for each entity in new_memory.entities:
        GetMemoryFacts(entity_id) → related_memory_ids
      fetch Memory for each → candidates_g
   c. merge candidates_v + candidates_g, dedup by ID → candidates (max 30)

2. HEURISTIC FILTER (~2ms)
   for each candidate:
     shared_entities = intersect(candidate.entities, new_memory.entities)
     if len(shared_entities) == 0: skip
     
     new_facts = new_memory.extracted_facts  // from triple extraction
     cand_facts = candidate.extracted_facts
     
     for each (nf, cf) in cartesian(new_facts, cand_facts):
       if nf.relation_type == cf.relation_type (exclusive pair):
         if nf.target_entity != cf.target_entity:
           add candidate to heuristic_conflicts
           
3. LLM CONFIRMATION (only for heuristic_conflicts, skip if > budget)
   prompt = contradiction_check_prompt(new_content, candidate_content)
   if llm says contradicted: add to confirmed_contradictions
   if similarity > llm_confirm_threshold (0.82): skip LLM, auto-confirm
   
4. RETURN confirmed_contradictions IDs
```

### 6.2 Graph-Aware Recall Merge Algorithm

```
Input: query_text, query_embedding, limit, opts
Output: []RecallResult (merged, ranked)

1. PARALLEL FETCH
   goroutine A: vector_search(embedding, limit*3) → []SearchResult{Memory, score}
   goroutine B: RecallByGraph(query, embedding, limit*2) → []string{memory_id}
               → fetch each memory → []SearchResult{Memory, score=1/hop_distance}
   
2. RRF MERGE (Reciprocal Rank Fusion, k=60)
   combined_score[id] += 1/(rank + k) for each list
   sort by combined_score desc → merged_list
   
3. MULTI-FACTOR RANKING (existing recall.go)
   for each memory in merged_list[:limit*2]:
     compute: similarity, recency, frequency, type_boost, scope_boost,
              confidence, reinforcement, tag_affinity, graph_hop
     graph_hop = 1.0 if in goroutine B results else 0.0
     final_score = weighted_sum(all factors)
   
4. SORT by final_score DESC, return top limit
   
5. POST-FILTER: exclude valid_to IS NOT NULL (unless include_history)
```

### 6.3 Triple Extraction Algorithm (in capture)

```
Input: user_msg, assistant_msg, session_id
Output: ([]Entity upserted, []Fact upserted, Episode)

1. ENTITY EXTRACTION
   text = concat(user_msg, assistant_msg)
   llm_extract_entities(text) → []raw_entities{name, type, context}
   
2. ENTITY RESOLUTION (per entity, parallel up to 5)
   for each raw_entity:
     EntityResolver.Resolve(raw_entity) →
       stage1: SearchEntities(name, embedding) → candidates
       stage2: exact match / alias match / cosine > 0.92 → fast-path
       stage3: LLM haiku confirmation (if ambiguous)
     result: existing_entity | new_entity
   
3. FACT EXTRACTION
   FactExtractor.Extract(text, known_entities) →
     llm_extract_facts(text, entity_list) → []rawFact
     normalize relation_type → canonical form
     embed each fact string → fact_embedding
   
4. FACT RESOLUTION (per fact)
   for each rawFact:
     GetFactsBetween(source_id, target_id) → existing_facts
     FactResolver.Resolve(new_fact, existing_facts) →
       "insert": UpsertFact (new edge)
       "skip": AppendEpisode(fact_id, session_id)
       "invalidate": InvalidateFact(old_id) + UpsertFact (new edge)

5. EPISODE CREATION
   episode = {uuid, session_id, user_msg[:2000], assistant_msg[:2000],
              memory_ids: [all memories from phase 1],
              fact_ids: [all new facts from step 4]}
   CreateEpisode(episode)
   
6. MEMORY ↔ ENTITY LINKING
   for each memory stored in phase 1:
     for each entity resolved in step 2:
       LinkMemoryToEntity(entity_id, memory_id)
       // creates :MENTIONS edge for graph-aware recall
```

---

## 7. Migration Plan

### 7.1 Backward Compatibility Guarantee

All changes must be backward-compatible:
- Existing memories without `valid_from/valid_to` are treated as currently valid
- Existing facts without `invalid_at` are treated as currently active  
- All existing CLI commands work unchanged (new flags are additive)

### 7.2 Migration Steps

**Step 1: Schema migration (zero-downtime)**
```
cortex migrate --add-temporal-indexes
```

Add new indexes without modifying existing data. Existing memories continue working with `valid_to = NULL` behavior.

Implemented as `cmd_migrate.go` calling `store.MigrateSchema(ctx)`:

```cypher
-- Set valid_from = created_at for all existing memories without valid_from
MATCH (m:Memory) 
WHERE m.valid_from IS NULL
SET m.valid_from = m.created_at
```

**Step 2: Episode backfill (optional, background)**
```
cortex migrate --backfill-episodes
```

For existing memories that have `source = "capture"`, create Episode nodes linking them (using `session_id` from tags if present). Best-effort.

**Step 3: Fact temporal fields (no-op for existing)**

Existing `Fact` edges may not have `valid_at/invalid_at` set. These are treated as:
- `valid_at = NULL` → "unknown start time, assume always valid"
- `invalid_at = NULL` → currently active

No migration needed — just handle NULL in queries.

### 7.3 Migration Command

```go
// cmd/openclaw-cortex/cmd_migrate.go (new file)
var migrateCmd = &cobra.Command{
    Use:   "migrate",
    Short: "Run schema migrations",
    Flags: []Flag{
        "--add-temporal-indexes",
        "--backfill-episodes",
        "--dry-run",
    },
}
```

---

## 8. Performance Considerations

### 8.1 Recall Latency Budget

Target: **< 200ms** at 1000 memories.

Current baseline (estimated from architecture):
- Vector search: ~20-40ms (Memgraph vector index)
- Multi-factor ranking: ~5ms
- Total: ~30-50ms

With graph-aware recall:
- Vector search (parallel): ~20-40ms
- Graph traversal (parallel): ~30-60ms  
- RRF merge: ~2ms
- Multi-factor ranking: ~5ms
- **Total: ~60-80ms** (parallel, bottleneck is graph traversal)

At 1000 memories with proper indexes, this stays well under 200ms. Concern at 10,000+ memories — see §8.4.

### 8.2 Index Strategy

**Critical path indexes for recall:**

```cypher
-- Vector search: covered by existing indexes
-- Temporal filter (new) — needs composite-like behavior:
CREATE INDEX ON :Memory(valid_to);   -- NULL check for active memories

-- Graph traversal from entity:
-- :Entity has name_embedding vector index
-- :RELATES_TO edges indexed by invalid_at (for active filter)
CREATE INDEX ON :RELATES_TO(invalid_at);

-- Memory ↔ Entity join:
-- :MENTIONS relationship needs index on both sides
CREATE INDEX ON :Memory(uuid);   -- already exists
CREATE INDEX ON :Entity(uuid);   -- already exists
```

**Note:** Memgraph relationship property indexes (`:RELATES_TO(invalid_at)`) may have limitations in 2.21 — verify support before relying on them. Fallback: filter in application layer.

### 8.3 Contradiction Detection Latency

Contradiction detection runs during `capture`, not recall — latency is less critical. Budget: **500ms maximum** for contradiction check.

Breakdown:
- Stage 1 (vector search + entity lookup): ~40ms
- Stage 2 (heuristic): ~2ms
- Stage 3 (LLM, if needed): ~150ms
- **Total: ~200ms typical, 500ms worst case**

Skip LLM if `cfg.Contradiction.LLMTimeoutMs` is exceeded via context deadline.

### 8.4 Scalability Projections

| Memory Count | Vector Recall | Graph Recall | Total (parallel) |
|---|---|---|---|
| 1,000 | ~25ms | ~35ms | ~50ms ✅ |
| 10,000 | ~40ms | ~80ms | ~90ms ✅ |
| 100,000 | ~80ms | ~200ms | ~200ms ⚠️ |
| 1M | ~200ms | ~800ms | >200ms ❌ |

For >10K memories, add:
1. **Pre-filter by project/scope** before vector search (already supported via SearchFilters)
2. **Entity community indexes** (`community_id` on Entity already exists) — limit BFS to same community
3. **Fact embedding index** — search fact embeddings directly instead of BFS from entities

### 8.5 Triple Extraction Latency

Triple extraction runs in parallel with memory extraction during `capture`. LLM calls dominate:
- Entity extraction: ~200ms (one LLM call)  
- Fact extraction: ~300ms (one LLM call)
- Fact resolution: ~150ms per fact needing LLM (parallelized)

**Total: ~600ms-1.5s** (acceptable for capture; not in recall path)

Use `--no-triples` flag to skip if latency is critical.

---

## 9. Implementation Phases

### Phase 1: Temporal Versioning (1-2 days)
**Value:** High — immediately improves fact accuracy for evolving information  
**Risk:** Low — additive schema change, no breaking changes

Tasks:
1. Add `ValidFrom`, `ValidTo *time.Time` to `models.Memory`
2. Add `InvalidateMemory` to `store.Store` interface + implement in `MemgraphStore`
3. Add temporal indexes to `EnsureSchema`
4. Modify `Upsert` to set `valid_from` and call `InvalidateMemory` when `supersedes_id` is set
5. Add `IncludeInvalidated` + `AsOf` to `SearchFilters`
6. Filter `valid_to IS NULL` by default in `Search`, `Recall`, `List`
7. Add `--include-history` flag to `recall` and `list` commands
8. Add migration: `SET m.valid_from = m.created_at WHERE m.valid_from IS NULL`
9. Tests: TestTemporalVersioning, TestAsOfQuery, TestInvalidateMemory

### Phase 2: Wire Triple Extraction in Capture (2-3 days)
**Value:** High — unlocks structured knowledge graph from conversations  
**Risk:** Medium — existing FactExtractor/EntityResolver need integration testing

Tasks:
1. Add `models.Episode` struct + `CreateEpisode` to `graph.Client`
2. Create `internal/capture/triple_extractor.go` wrapping existing resolvers
3. Add relation type normalization (`graph/relation_types.go`)
4. Modify `cmd_capture.go` to run triple extraction in parallel
5. Create Episode node after both phases complete
6. Verify `LinkMemoryToEntity` is called for extracted entities (needed for Phase 4)
7. Tests: TestTripleExtraction, TestEpisodeCreation, TestCapturePipeline

### Phase 3: Contradiction Detection (2 days)
**Value:** High — prevents knowledge rot from stale facts  
**Risk:** Medium — LLM dependency, needs careful timeout handling

Tasks:
1. Create `internal/capture/contradiction.go` with `ContradictionDetector`
2. Implement 3-stage pipeline (vector candidates → heuristic → LLM)
3. Add `ContradictionConfig` to config
4. Wire into `cmd_capture.go` after memory extraction, before storage
5. Integrate with temporal versioning: contradicted memories get `valid_to` set
6. Tests: TestContradictionDetection, TestAutoInvalidation, TestContradictionTimeout

### Phase 4: Graph-Aware Recall (2-3 days)
**Value:** High — richer, more connected recall  
**Risk:** Medium — requires Phase 2 for `:MENTIONS` edges to exist; RecallByGraph needs implementation

Tasks:
1. Implement `RecallByGraph` in `MemgraphStore` (Cypher: entity vector search + BFS + memory lookup)
2. Add `rrf.go` to `internal/recall/` with `rffMergeMemories`
3. Add `RecallHybrid` to `recall.Ranker`
4. Add `GraphHop` weight to `Weights` struct; adjust default weights
5. Wire `RecallHybrid` into `cmd_recall.go` (default behavior)
6. Add `--no-graph` flag to disable
7. Benchmark: verify <200ms at 1000 memories
8. Tests: TestGraphAwareRecall, TestRRFMerge, TestRecallHybridWeights

### Phase 5: History & Migration Commands (1 day)
**Value:** Medium — operational completeness  
**Risk:** Low

Tasks:
1. `cmd_history.go` — show Memory version chain + Entity fact history
2. `cmd_migrate.go` — temporal backfill + episode backfill
3. Add `--as-of` to `recall` and `search` commands

---

## 10. Code-Level Design

### 10.1 Files to Modify

| File | Change |
|---|---|
| `internal/models/memory.go` | Add `ValidFrom`, `ValidTo *time.Time` |
| `internal/models/episode.go` | **NEW** — Episode struct |
| `internal/store/store.go` | Add `InvalidateMemory`, `SearchWithTemporalFilter`; extend `SearchFilters` |
| `internal/memgraph/store.go` | Implement `InvalidateMemory`; modify `Upsert` for temporal; modify `Search` for default valid_to filter; implement `RecallByGraph` |
| `internal/memgraph/graph.go` | Add `EnsureSchema` entries for temporal indexes + Episode; implement `CreateEpisode`, `GetActiveFactsForEntity` |
| `internal/graph/client.go` | Add `CreateEpisode`, `GetEpisodesForMemory`, `GetActiveFactsForEntity` |
| `internal/graph/mock_client.go` | Implement new interface methods |
| `internal/graph/types.go` | Add `EpisodeResult` type |
| `internal/graph/relation_types.go` | **NEW** — canonical relation type map + `NormalizeRelationType` |
| `internal/recall/recall.go` | Add `GraphHop` weight; add `RecallHybrid`; add `RecallOptions.UseGraph` |
| `internal/recall/rrf.go` | **NEW** — `rffMergeMemories` |
| `internal/capture/triple_extractor.go` | **NEW** — `TripleExtractor` wrapping existing graph package |
| `internal/capture/contradiction.go` | **NEW** — `ContradictionDetector` |
| `internal/config/config.go` | Add `ContradictionConfig`, `GraphConfig.TripleExtraction`, `RecallConfig.UseGraph` |
| `cmd/openclaw-cortex/cmd_capture.go` | Parallel triple extraction + contradiction detection |
| `cmd/openclaw-cortex/cmd_recall.go` | Use `RecallHybrid`; add `--no-graph`, `--include-history`, `--as-of` |
| `cmd/openclaw-cortex/cmd_history.go` | **NEW** |
| `cmd/openclaw-cortex/cmd_migrate.go` | **NEW** |

### 10.2 New Interfaces

```go
// internal/capture/interfaces.go (or inline in respective files)

type ContradictionDetector interface {
    FindContradictions(ctx context.Context, newMemory models.Memory, embedding []float32) ([]string, error)
}

type TripleExtractor interface {
    Extract(ctx context.Context, userMsg, assistantMsg, sessionID string) ([]models.Fact, []models.Entity, error)
}

type EpisodeStore interface {
    CreateEpisode(ctx context.Context, ep models.Episode) error
    GetEpisodesForMemory(ctx context.Context, memoryID string) ([]models.Episode, error)
}
```

### 10.3 Config Additions

```go
// internal/config/config.go additions

type ContradictionConfig struct {
    Enabled               bool    `mapstructure:"enabled"`
    SimilarityThreshold   float64 `mapstructure:"similarity_threshold"`    // default: 0.75
    MaxCandidates         int     `mapstructure:"max_candidates"`           // default: 20
    LLMConfirmThreshold   float64 `mapstructure:"llm_confirm_threshold"`    // default: 0.82
    LLMTimeoutMs          int     `mapstructure:"llm_timeout_ms"`           // default: 150
}

type GraphConfig struct {
    // ... existing fields ...
    TripleExtraction bool `mapstructure:"triple_extraction"` // default: true (if LLM configured)
    MaxBFSHops       int  `mapstructure:"max_bfs_hops"`      // default: 2
}

type RecallConfig struct {
    // ... existing fields ...
    UseGraph       bool `mapstructure:"use_graph"`        // default: true
    GraphHopWeight float64 `mapstructure:"graph_hop_weight"` // default: 0.12
}
```

### 10.4 Cypher Query Templates

**RecallByGraph (MemgraphStore implementation):**

```cypher
// Step 1: Find entities matching query embedding
CALL vector_search.search("entity_name_embedding", $search_limit, $query_embedding)
YIELD node AS entity, similarity AS entity_score
WHERE entity_score >= $min_entity_score

// Step 2: BFS traversal to related entities (up to $hops hops)
MATCH p = (entity)-[:RELATES_TO*1..$hops]-(related)
WHERE related:Entity

// Step 3: Find memories linked to traversed entities
MATCH (related)<-[:MENTIONS]-(m:Memory)
WHERE m.valid_to IS NULL

// Aggregate: give higher score to closer entities
WITH m.uuid AS memory_id, 
     max(entity_score / length(p)) AS graph_score
ORDER BY graph_score DESC
LIMIT $limit

RETURN memory_id, graph_score
```

**InvalidateMemory:**

```cypher
MATCH (m:Memory {uuid: $id})
SET m.valid_to = $valid_to
RETURN m.uuid
```

**GetHistory (chain traversal):**

```cypher
MATCH (m:Memory {uuid: $start_id})
WITH m
OPTIONAL MATCH chain = (m)-[:SUPERSEDES*]->(older:Memory)
RETURN m, nodes(chain) AS history
ORDER BY m.created_at DESC
```

---

## 11. Testing Strategy

### Unit Tests
- `TestTemporalVersioning` — store/recall with valid_from/valid_to
- `TestContradictionDetector` — heuristic + LLM path
- `TestTripleExtractor` — mock LLM responses, verify entity/fact output
- `TestRecallHybrid` — mock graph client, verify RRF merge
- `TestRRFMerge` — pure algorithmic test
- `TestRelationNormalization` — canonical mapping table

### Integration Tests (against real Memgraph)
- `TestGraphAwareRecallE2E` — store facts, query via graph traversal, verify recall finds connected memories
- `TestTemporalChainE2E` — store v1, supersede with v2, verify v1 is invalidated, recall returns v2
- `TestContradictionE2E` — store "Ajit works at Pixis", then "Ajit works at Booking.com", verify auto-invalidation

### Performance Tests
- `BenchmarkRecallHybrid_1000` — must complete <200ms
- `BenchmarkContradictionCheck_1000` — must complete <500ms

---

## 12. Open Questions

1. **`:MENTIONS` edge population:** Does the current `cmd_capture.go` call `LinkMemoryToEntity`? If not, graph-aware recall Phase 4 won't work until Phase 2 (triple extraction) is wired. Need to verify.

2. **Relationship property indexes in Memgraph 2.21:** The spec assumes `CREATE INDEX ON :RELATES_TO(invalid_at)` works. Memgraph docs show relationship property indexes were added in 2.5. Need to verify query plan with vs without this index.

3. **LLM for contradiction detection:** If the configured model is slow (e.g., claude-opus) and `llm_timeout_ms = 150`, contradiction LLM step will frequently be skipped. Consider defaulting to a faster model (haiku/flash) for contradiction checks specifically. Add `ContradictionConfig.Model` field.

4. **Embedding `fact_embedding`:** The `Fact` model has `FactEmbedding []float32`. Is this currently populated and stored in Memgraph? If yes, direct fact vector search is available as an alternative to BFS-based graph recall. If no, add embedding step in `TripleExtractor.Extract`.

5. **`valid_to` vs `valid_until`:** Two temporal fields with different semantics (`valid_until` = TTL, `valid_to` = supersession). This could confuse users. Consider renaming `valid_until` → `ttl_expires_at` in a future refactor (out of scope for this spec but worth flagging).

---

## Summary

The codebase already has strong foundations for all four features. The primary work is:

1. **Temporal versioning** (~1-2 days): Wire `valid_from/valid_to` to Upsert + Search/Recall queries  
2. **Triple extraction in capture** (~2-3 days): Call existing graph pipeline from `cmd_capture.go`  
3. **Contradiction detection** (~2 days): New `ContradictionDetector` using existing FactResolver pattern  
4. **Graph-aware recall** (~2-3 days): Implement `RecallByGraph` in Memgraph + wire into `recall.Ranker`  

Total estimated effort: **7-10 days** for a single focused engineer working on this sequentially. With parallel agents, phases 1+3 can run simultaneously, and phase 2+4 can follow. Realistic parallel timeline: **4-5 days**.

The highest-value, lowest-risk item to ship first is **Phase 1 (Temporal Versioning)** — it's purely additive, requires no LLM, and immediately prevents stale knowledge from polluting recall results.
