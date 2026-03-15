# Memgraph Migration Design Spec

**Date:** 2026-03-15
**Status:** Approved
**Version:** v0.7.0

## Goal

Replace Qdrant (vector DB) and Neo4j (graph DB) with a single Memgraph instance that handles both vector search and graph traversal. Clean replacement — no dead code, no fallback to old backends.

## Architecture

### Current (v0.6.0)

```
Qdrant (gRPC:6334)          Neo4j (Bolt:7687)
  ├─ memories (vectors)       ├─ Entity nodes
  ├─ entities (dual-write)    ├─ RELATES_TO edges (facts)
  └─ keyword filters          └─ fulltext + graph traversal
```

Two containers, two drivers, two config sections, entities duplicated across both.

### Target (v0.7.0)

```
Memgraph (Bolt:7687)
  ├─ :Memory nodes (vectors + properties)
  ├─ :Entity nodes (graph + vector)
  ├─ :RELATES_TO edges (facts)
  ├─ vector indexes (cosine, 768-dim)
  ├─ property indexes (type, scope, project)
  └─ text search indexes (entity name, fact text)
```

One container, one driver (`neo4j-go-driver/v5` — Bolt compatible), one config section.

### Implementation Pattern

Single `MemgraphStore` struct implements both `store.Store` (17 methods) and `graph.Client` (15 methods). Shared Bolt driver and session pool.

```
cmd/main.go
  │
  newStore()       → *memgraph.MemgraphStore (implements store.Store)
  newGraphClient() → same instance           (implements graph.Client)
  │
  All existing callers work unchanged — they accept interfaces
```

## Data Model

### Memory Nodes (replaces Qdrant points)

```cypher
(:Memory {
  uuid:               STRING,       -- primary key
  type:               STRING,       -- rule|fact|episode|procedure|preference
  scope:              STRING,       -- permanent|project|session|ttl
  visibility:         STRING,
  content:            STRING,
  confidence:         FLOAT,
  source:             STRING,
  project:            STRING,
  ttl_seconds:        INTEGER,
  created_at:         STRING,       -- RFC3339Nano (passed as param, not datetime())
  updated_at:         STRING,
  last_accessed:      STRING,
  access_count:       INTEGER,
  supersedes_id:      STRING,
  conflict_group_id:  STRING,
  conflict_status:    STRING,
  valid_until:        STRING,
  reinforced_at:      STRING,
  reinforced_count:   INTEGER,
  tags:               LIST OF STRING,
  metadata:           STRING,       -- JSON-marshaled
  embedding:          LIST OF FLOAT -- 768-dim vector
})
```

All timestamps are STRING (RFC3339Nano) passed as Go parameters via `time.Now().UTC().Format(time.RFC3339Nano)`. Do NOT use Memgraph's `datetime()` function — keep timestamps as strings for consistency with the existing data model.

### Entity Nodes (unified — no more dual-write)

```cypher
(:Entity:Person|:Entity:System|:Entity:Concept|... {
  uuid:            STRING,
  name:            STRING,       -- unique constraint
  type:            STRING,
  project:         STRING,
  summary:         STRING,
  aliases:         LIST OF STRING,
  memory_ids:      LIST OF STRING,
  name_embedding:  LIST OF FLOAT,
  community_id:    STRING,
  created_at:      STRING,
  updated_at:      STRING
})
```

### Fact Relationships (unchanged from Neo4j)

```cypher
(s:Entity)-[:RELATES_TO {
  uuid:              STRING,
  relation_type:     STRING,
  fact:              STRING,
  fact_embedding:    LIST OF FLOAT,
  created_at:        STRING,
  expired_at:        STRING,       -- NULL if active
  valid_at:          STRING,
  invalid_at:        STRING,
  source_memory_ids: LIST OF STRING,
  episodes:          LIST OF STRING,
  confidence:        FLOAT
}]->(t:Entity)
```

## Schema (Indexes & Constraints)

Created by `EnsureSchema()` on startup. Memgraph does NOT support `IF NOT EXISTS` on constraints — handle "already exists" errors gracefully (log and continue).

```cypher
-- Uniqueness constraints (handle AlreadyExists errors)
CREATE CONSTRAINT ON (m:Memory) ASSERT m.uuid IS UNIQUE;
CREATE CONSTRAINT ON (e:Entity) ASSERT e.name IS UNIQUE;

-- Vector indexes (Memgraph native DDL syntax)
CREATE VECTOR INDEX memory_embedding ON :Memory(embedding)
  WITH CONFIG {"dimension": 768, "metric": "cosine", "capacity": 10000};
CREATE VECTOR INDEX entity_name_embedding ON :Entity(name_embedding)
  WITH CONFIG {"dimension": 768, "metric": "cosine", "capacity": 10000};

-- Property indexes for filtering
CREATE INDEX ON :Memory(type);
CREATE INDEX ON :Memory(scope);
CREATE INDEX ON :Memory(project);
CREATE INDEX ON :Memory(source);
CREATE INDEX ON :Memory(uuid);
CREATE INDEX ON :Entity(uuid);
CREATE INDEX ON :Entity(project);

-- Text search indexes (Memgraph DDL syntax)
CREATE TEXT INDEX entity_text ON :Entity;
CREATE TEXT INDEX fact_text ON :RELATES_TO;
```

Note: Memgraph text indexes apply to all string properties on the label/edge type. Field-level specification is not supported — filtering by specific fields is done in the `WHERE` clause after the text search call.

## Search Strategy

### Vector Search (replaces Qdrant Search)

```cypher
CALL vector_search.search("memory_embedding", $limit, $query_vector)
YIELD node, similarity
WITH node, similarity
WHERE node.type = $type_filter AND node.scope = $scope_filter  -- optional filters
RETURN node, similarity AS score
ORDER BY score DESC
```

Parameter order: `(index_name, limit, query_vector)`. Yields `node` and `similarity` (0-1 range, higher = more similar). No need to compute `1 - distance`.

### Keyword Filtering (replaces Qdrant payload filters)

Cypher `WHERE` clauses on indexed properties:
- `WHERE m.type = $type` (exact match)
- `WHERE m.scope = $scope` (exact match)
- `WHERE m.project = $project` (exact match)
- `WHERE ANY(t IN m.tags WHERE t = $tag)` (tag containment)
- `WHERE m.conflict_status = $status` (exact match)

### Fulltext Search (replaces Neo4j db.index.fulltext)

Entity text search:
```cypher
CALL text_search.search("entity_text", $query)
YIELD node, score
RETURN node, score
LIMIT $limit
```

Fact/relationship text search (uses `search_edges` for edge indexes):
```cypher
CALL text_search.search_edges("fact_text", $query)
YIELD edge, score
WITH edge, score, startNode(edge) AS s, endNode(edge) AS t
RETURN edge, s, t, score
LIMIT $limit
```

### Graph Traversal (same Cypher as Neo4j)

```cypher
-- Facts between entities (bidirectional)
MATCH (n:Entity)-[r:RELATES_TO]-(m:Entity)
WHERE n.uuid = $source_id AND m.uuid = $target_id AND r.expired_at IS NULL
RETURN r, startNode(r) AS s, endNode(r) AS t

-- Recall by graph
MATCH (s:Entity)-[r:RELATES_TO]->(t:Entity)
WHERE r.expired_at IS NULL
RETURN DISTINCT r.source_memory_ids AS mids
```

## File Changes

### New Files

| File | Purpose |
|------|---------|
| `internal/memgraph/store.go` | `MemgraphStore` struct, constructor, `store.Store` methods |
| `internal/memgraph/graph.go` | `graph.Client` methods on `MemgraphStore` |
| `internal/memgraph/schema.go` | `EnsureSchema()` — indexes, constraints, vector indexes |
| `internal/memgraph/serialize.go` | Cypher record ↔ `models.Memory` / `models.Entity` / `models.Fact` |

### Files to Delete

| File | Reason |
|------|--------|
| `internal/store/qdrant.go` | Replaced by `memgraph/store.go` |
| `internal/graph/neo4j.go` | Replaced by `memgraph/graph.go` |
| `tests/qdrant_store_test.go` | No more Qdrant |

### Files to Modify

| File | Changes |
|------|---------|
| `internal/config/config.go` | Remove `QdrantConfig`, `Neo4jConfig`. Replace `GraphConfig` with `MemgraphConfig` + keep `EntityResolutionConfig` and `FactExtractionConfig` as top-level or under new `extraction:` section. Move `recall_budget_ms` under `RecallConfig`. Bump version to v0.7.0 in main.go. |
| `cmd/openclaw-cortex/main.go` | `newStore()` returns `MemgraphStore`. `newGraphClient()` returns same instance. Remove Qdrant/Neo4j imports. Remove conditional graph client init. Bump version. |
| `cmd/openclaw-cortex/cmd_health.go` | Single Memgraph health check. Remove separate Qdrant/Neo4j checks. |
| `cmd/openclaw-cortex/cmd_capture.go` | Remove `cfg.Graph.Enabled` guards. Graph always available. Simplify entity/fact extraction flow. |
| `cmd/openclaw-cortex/cmd_recall.go` | Remove conditional graph wiring. Graph always available via store. |
| `cmd/openclaw-cortex/cmd_serve.go` | Remove conditional graph init. |
| `cmd/openclaw-cortex/cmd_hook.go` | Remove conditional graph init. |
| `cmd/openclaw-cortex/cmd_mcp.go` | Remove conditional graph init. |
| `docker-compose.yml` | Replace qdrant+neo4j with single memgraph service. |
| `go.mod` / `go.sum` | Remove `qdrant/go-client`. Keep `neo4j-go-driver/v5`. Run `go mod tidy`. |
| `CLAUDE.md` | Rewrite architecture section for Memgraph. |
| `README.md` | Update service dependencies and install instructions. |
| `extensions/openclaw-plugin/index.ts` | Bump to v0.7.0, update service message (Memgraph not Qdrant). |
| `extensions/openclaw-plugin/package.json` | Bump to v0.7.0. |
| `extensions/openclaw-plugin/openclaw.plugin.json` | Bump to v0.7.0. |

### Files Unchanged

| File | Reason |
|------|--------|
| `internal/store/store.go` | Interface unchanged |
| `internal/graph/client.go` | Interface unchanged |
| `internal/store/mock_store.go` | Tests use mocks |
| `internal/graph/mock_client.go` | Tests use mocks |
| `internal/graph/entity_resolver.go` | Consumes `graph.Client` interface |
| `internal/graph/fact_resolver.go` | Consumes `graph.Client` interface |
| `internal/graph/fact_extractor.go` | Consumes `llm.LLMClient` interface |
| `internal/graph/search.go` | RRF merge logic, no DB dependency |
| `internal/graph/types.go` | Data types only |
| `internal/capture/*.go` | Consumes interfaces |
| `internal/recall/*.go` | Consumes interfaces |
| `internal/llm/*.go` | No DB dependency |
| `internal/classifier/*.go` | No DB dependency |
| `internal/embedder/*.go` | No DB dependency |
| `internal/models/*.go` | Data structs unchanged |
| `tests/*.go` (except qdrant_store_test.go) | Use mocks |

## Config Changes

### Remove

```yaml
qdrant:
  host: localhost
  grpc_port: 6334
  http_port: 6333
  collection: cortex_memories
  use_tls: false

graph:
  enabled: true          # removed — graph is always on
  neo4j:                 # removed — replaced by memgraph
    uri: bolt://localhost:7687
    username: neo4j
    password: ""
    database: neo4j
```

### Add

```yaml
memgraph:
  uri: bolt://localhost:7687
  username: ""
  password: ""
  database: ""           # empty string — Memgraph does not support multi-database

entity_resolution:       # moved from graph.entity_resolution
  similarity_threshold: 0.95
  max_candidates: 10

fact_extraction:         # moved from graph.fact_extraction
  enabled: true
```

### Move

```yaml
recall:
  graph_budget_ms: 50        # was graph.recall_budget_ms
  graph_budget_cli_ms: 500   # was graph.recall_budget_cli_ms
  # existing weights, rerank settings unchanged
```

### Environment Variables

| Old | New |
|-----|-----|
| `OPENCLAW_CORTEX_QDRANT_HOST` | (removed) |
| `OPENCLAW_CORTEX_QDRANT_GRPC_PORT` | (removed) |
| (new) | `OPENCLAW_CORTEX_MEMGRAPH_URI` |
| (new) | `OPENCLAW_CORTEX_MEMGRAPH_USERNAME` |
| (new) | `OPENCLAW_CORTEX_MEMGRAPH_PASSWORD` |

## Docker Compose

```yaml
services:
  memgraph:
    image: memgraph/memgraph:2.21
    ports:
      - "7687:7687"   # Bolt protocol
      - "7444:7444"   # Monitoring
    command: ["--storage-properties-on-edges=true"]
    volumes:
      - memgraph_data:/var/lib/memgraph
    restart: unless-stopped

volumes:
  memgraph_data:
```

Key flags:
- `--storage-properties-on-edges=true` — required for RELATES_TO edge properties (uuid, fact, confidence, etc.) and edge property indexes
- Image pinned to `2.21` (minimum version supporting vector search + text search)
- ~200MB RAM, sub-second startup, no JVM

## go.mod Changes

**Remove:**
- `github.com/qdrant/go-client`
- All transitive gRPC/protobuf deps only needed by Qdrant

**Keep:**
- `github.com/neo4j/neo4j-go-driver/v5` (Memgraph speaks Bolt)

## Migration

No migration script. Fresh start — existing Qdrant data stays on disk but is not imported. Users re-capture to rebuild memory. Acceptable because:
- System has ~15 memories accumulated over 2 days
- Qdrant Docker volume can be preserved as backup
- Facts will be re-extracted on next capture

## Testing

- All existing tests use `MockStore` / `MockGraphClient` — no changes needed
- Delete `tests/qdrant_store_test.go`
- Add `tests/memgraph_store_test.go` (integration, build-tagged) for basic CRUD against real Memgraph
- `go test -short -count=1 ./...` must pass with zero Qdrant/Neo4j references
- `go build ./...` must produce zero compilation errors

## Version

- Binary: `0.7.0`
- Plugin: `0.7.0`
- CHANGELOG: add `[0.7.0]` section

## Memgraph-Specific Considerations

1. **No `IF NOT EXISTS` on constraints** — `EnsureSchema()` must catch "already exists" errors and continue. Use a helper that runs each DDL statement individually and ignores constraint-already-exists errors.

2. **Timestamps as strings, not datetime()** — All timestamps stored as RFC3339Nano strings, passed as Go parameters. Do not use Memgraph's `datetime()` function. This matches the existing data model and avoids Cypher dialect issues.

3. **`MERGE ... ON CREATE SET ... ON MATCH SET`** — Supported by Memgraph. Used for entity and fact upserts.

4. **`--storage-properties-on-edges=true`** — Must be set in docker-compose `command`. Without this, RELATES_TO edge properties are not persisted or indexable.

5. **Vector search parameter order** — `vector_search.search(index_name, limit, query_vector)` — limit comes before the vector, unlike some other APIs.

6. **Text search is label-wide** — `CREATE TEXT INDEX` applies to all string properties on the label. Field filtering is done in `WHERE` clauses after the search call returns results.

7. **`neo4j-go-driver/v5` compatibility** — Works with Memgraph out of the box. Set `database` to `""` (empty string) in `SessionConfig` since Memgraph does not support multi-database.

8. **In-memory with WAL** — Data survives restarts via write-ahead log. RAM must hold entire dataset. For ~10K memories + entities, this is ~50-100MB — well within limits.

## Risks

1. **Memgraph Cypher dialect differences** — Some Neo4j-specific functions may not exist. Mitigated by using only standard Cypher features and Memgraph-documented procedures.
2. **In-memory storage** — Memgraph is in-memory with WAL persistence. Data survives restarts but RAM must hold entire dataset.
3. **Edge property indexes** — Require `--storage-properties-on-edges=true` flag. Without it, edge properties are ephemeral.
4. **Vector search capacity** — Initial capacity set to 10000. Must be increased if dataset grows beyond this. Memgraph may auto-resize but this needs verification.
