# Graphiti Entity-Relationship Graph Integration

> **Issue:** #43
> **Status:** Design approved, pending implementation
> **Date:** 2026-03-14

## Goal

Add graph-based entity traversal to openclaw-cortex as an optional sidecar, enabling relationship-aware recall alongside existing vector search. Delivered in two phases: first wire the existing orphaned entity system into the capture pipeline, then layer Graphiti (by Zep) on top for full graph intelligence.

## Background

openclaw-cortex already has entity infrastructure that was built in v0.2.0 but never wired into the capture pipeline:

- `internal/capture/entity_extractor.go` — `EntityExtractor` that uses Claude Haiku to extract entities from conversation text
- `internal/models/entity.go` — `Entity` model with 5 types (person, project, system, decision, concept), aliases, metadata, memory ID links
- `internal/store/store.go` — Store interface with `UpsertEntity`, `GetEntity`, `SearchEntities`, `LinkMemoryToEntity` methods
- `internal/store/qdrant.go` / `mock_store.go` — Both implement entity store methods

This code compiles and is tested in isolation, but `capture.go` never calls `EntityExtractor`. Phase 1 completes this pipeline. Phase 2 adds Graphiti for relationship-aware graph queries.

## Architecture

```
Capture Flow (Phase 1):
  capture.Capturer.Extract()
    → store.Upsert(memory)
    → EntityExtractor.Extract(content)        # NEW: wired in
    → store.UpsertEntity(entity)              # NEW: wired in
    → store.LinkMemoryToEntity(memID, entID)  # NEW: wired in

Capture Flow (Phase 2, graphiti.enabled=true):
  capture.Capturer.Extract()
    → store.Upsert(memory)
    → graph.Client.AddEpisode(content, entities)  # Graphiti handles extraction
    → (EntityExtractor skipped — Graphiti owns entity extraction)

Capture Flow (Phase 2, graphiti.enabled=false):
  → Same as Phase 1 (EntityExtractor fallback)

Recall Flow (Phase 2):
  recall.Recaller.Recall(query)
    1. embedder.Embed(query)
    2. store.Search(vector, top-50)           # Qdrant vector search
    3. graph.Client.SearchEntities(query)     # Graphiti entity lookup (with latency budget)
    4. Merge results, dedup by memory ID
    5. recaller.Rank(merged candidates)       # Full 8-factor scoring
    6. tokenizer.FormatMemoriesWithBudget()
```

## Phase 1: Complete Entity Pipeline

### Changes to `internal/capture/capture.go`

After `store.Upsert(memory)` succeeds, call `EntityExtractor.Extract(content)` to get entities, then `store.UpsertEntity()` and `store.LinkMemoryToEntity()` for each. Entity extraction failures are logged and skipped — the memory is stored regardless.

The `Capturer` struct gains an optional `EntityExtractor` field. When nil (default for hooks where latency matters), entity extraction is skipped. The CLI `capture` command and API endpoint wire it in.

### API Endpoints

Add to `internal/api/server.go`:

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/entities?query=<text>&type=<type>&limit=<n>` | Search entities |
| `GET` | `/v1/entities/{id}` | Get entity by ID |

Entity creation happens implicitly through capture — no standalone create endpoint needed.

### MCP Tools

Add to `internal/mcp/server.go`:

| Tool | Parameters | Description |
|------|-----------|-------------|
| `entity_search` | `query`, `type` (optional), `limit` (optional) | Search entities by name/alias |
| `entity_get` | `id` | Get entity details + linked memories |

## Phase 2: Graphiti Integration

### New Package: `internal/graph/`

**`client.go`** — Interface and implementation:

```go
type Client interface {
    AddEpisode(ctx context.Context, content string, sessionID string) error
    SearchEntities(ctx context.Context, query string, limit int) ([]EntityResult, error)
    GetEntityMemories(ctx context.Context, entityID string) ([]string, error) // returns memory IDs
    DeleteEpisode(ctx context.Context, sessionID string) error
    Healthy(ctx context.Context) bool
}

type EntityResult struct {
    Name     string
    Type     string
    MemoryIDs []string
    Score    float64
}
```

**`graphiti.go`** — `GraphitiClient` implements `Client` via Graphiti's REST API:

- `POST /v1/episodes` for write path
- `POST /v1/search` for entity search
- `DELETE /v1/episodes/{id}` for cleanup
- `GET /v1/health` for health checks
- All calls wrapped with `context.WithTimeout` using configured budgets

**`mock_client.go`** — `MockGraphClient` for tests.

### Write Path (Capture)

When `graphiti.enabled=true`:

1. After `store.Upsert(memory)`, call `graph.Client.AddEpisode()` with the conversation content and session ID
2. Graphiti extracts entities and relationships internally (using its own LLM calls)
3. Skip `EntityExtractor` — Graphiti owns entity extraction
4. On Graphiti failure: log warning, memory is already stored in Qdrant

When `graphiti.enabled=false`:

1. Fall back to Phase 1 behavior (EntityExtractor + store entity methods)

### Read Path (Recall)

Sequential merge with dedup:

1. Qdrant vector search returns top-50 candidates (existing behavior)
2. Extract entity names from query (simple NER or keyword match)
3. `graph.Client.SearchEntities(query)` returns entity-linked memory IDs with latency budget
4. Fetch any graph-sourced memories not already in Qdrant results via `store.GetByID()`
5. Merge into candidate list, dedup by memory ID (Qdrant results take priority)
6. Run full `Recaller.Rank()` on merged set
7. `tokenizer.FormatMemoriesWithBudget()` as usual

### Delete Path

When a memory is deleted via CLI/API/MCP:

1. `store.Delete(id)` removes from Qdrant (existing)
2. If `graphiti.enabled`, `graph.Client.DeleteEpisode()` removes associated graph data
3. Graphiti failure on delete: log error and return it to caller (delete failures should surface)

### Configuration

```yaml
graphiti:
  enabled: false
  base_url: http://localhost:8000
  timeout_ms: 500
  recall_budget_ms: 50      # hook context latency budget
  recall_budget_cli_ms: 500  # CLI context latency budget
```

Environment variable overrides:
- `OPENCLAW_CORTEX_GRAPHITI_ENABLED`
- `OPENCLAW_CORTEX_GRAPHITI_BASE_URL`
- `OPENCLAW_CORTEX_GRAPHITI_TIMEOUT_MS`
- `OPENCLAW_CORTEX_GRAPHITI_RECALL_BUDGET_MS`
- `OPENCLAW_CORTEX_GRAPHITI_RECALL_BUDGET_CLI_MS`

### Docker Compose

Add to existing `docker-compose.yml` (commented out by default):

```yaml
# Uncomment to enable Graphiti entity-relationship graph
# neo4j:
#   image: neo4j:5
#   ports:
#     - "7474:7474"
#     - "7687:7687"
#   environment:
#     NEO4J_AUTH: neo4j/openclaw-cortex
#   volumes:
#     - neo4j_data:/data
#
# graphiti:
#   image: zepai/graphiti:latest
#   ports:
#     - "8000:8000"
#   environment:
#     NEO4J_URI: bolt://neo4j:7687
#     NEO4J_USER: neo4j
#     NEO4J_PASSWORD: openclaw-cortex
#     OPENAI_API_KEY: ${OPENAI_API_KEY}
#   depends_on:
#     - neo4j
```

## Error Handling & Graceful Degradation

Both phases follow the established pattern — optional services never block core operations.

**Phase 1 (Entity Pipeline):**
- `EntityExtractor` failures during capture: logged and skipped, memory still stored
- API/MCP entity endpoints: standard error responses (404 missing, 500 store errors)

**Phase 2 (Graphiti):**
- All `graph.Client` calls use `context.WithTimeout`
- **Write path** (capture): Log warning, skip graph write. Memory stored in Qdrant regardless.
- **Read path** (recall): Log warning, return empty graph results. Qdrant results proceed through ranking normally.
- **Delete path**: Log error, return error to caller (orphaned graph data should surface).
- **Health**: `cortex stats` / `cortex health` reports Graphiti status as "optional: degraded" when unavailable. Does not affect overall health.

**Latency budgets (recall merge):**

| Context | Graphiti Budget | Total Budget |
|---------|----------------|--------------|
| Hook (PreTurnHook) | 50ms | 100ms |
| CLI (`cortex recall`) | 500ms | 3000ms |

On timeout: use Qdrant-only results.

## Testing Strategy

All tests in top-level `tests/` package per project convention. All use `MockStore` for Qdrant and `httptest.Server` for Graphiti — no live services needed for `go test -short`.

**Phase 1 tests:**

| File | Coverage |
|------|----------|
| `tests/entity_pipeline_test.go` | Capture with entity extraction → verify `MockStore.UpsertEntity` and `LinkMemoryToEntity` called |
| `tests/entity_api_test.go` | HTTP endpoint tests for entity search and get |
| `tests/entity_mcp_test.go` | MCP tool contract tests for `entity_search`, `entity_get` |

**Phase 2 tests:**

| File | Coverage |
|------|----------|
| `tests/graph_client_test.go` | `httptest.Server` mocking Graphiti REST API — write/read/delete/timeout/error paths |
| `tests/graph_recall_merge_test.go` | Sequential merge logic — Qdrant + graph results deduped by memory ID, then ranked |
| `tests/graph_degradation_test.go` | Graceful degradation — Graphiti unavailable returns Qdrant-only results without error |
| `tests/graph_capture_test.go` | Dual-write capture: both Qdrant and Graphiti when enabled; Qdrant-only when Graphiti down |

**Integration test** (behind build tag):

| File | Coverage |
|------|----------|
| `tests/integration/graphiti_integration_test.go` | `//go:build integration` — real Graphiti + Neo4j via testcontainers, full capture-to-recall round-trip |

## Files Changed

### Phase 1

| Action | File | What |
|--------|------|------|
| Modify | `internal/capture/capture.go` | Wire EntityExtractor into capture flow |
| Modify | `internal/api/server.go` | Add entity CRUD routes |
| Modify | `internal/mcp/server.go` | Add entity_search, entity_get tools |
| Create | `tests/entity_pipeline_test.go` | Capture + entity extraction tests |
| Create | `tests/entity_api_test.go` | HTTP endpoint tests |
| Create | `tests/entity_mcp_test.go` | MCP tool contract tests |

### Phase 2

| Action | File | What |
|--------|------|------|
| Create | `internal/graph/client.go` | Client interface + GraphitiClient |
| Create | `internal/graph/graphiti.go` | REST implementation with timeouts |
| Create | `internal/graph/mock_client.go` | MockGraphClient for tests |
| Modify | `internal/config/config.go` | graphiti.* config keys |
| Modify | `internal/capture/capture.go` | Conditional Graphiti write path |
| Modify | `internal/recall/recall.go` | Sequential merge: Qdrant + Graphiti + dedup |
| Modify | `cmd/openclaw-cortex/root.go` | Wire graph.Client when enabled |
| Modify | `docker-compose.yml` | Add Neo4j + Graphiti services (commented) |
| Create | `tests/graph_client_test.go` | Graphiti REST mock tests |
| Create | `tests/graph_recall_merge_test.go` | Merge + dedup logic tests |
| Create | `tests/graph_degradation_test.go` | Graceful degradation tests |
| Create | `tests/graph_capture_test.go` | Dual-write capture tests |
