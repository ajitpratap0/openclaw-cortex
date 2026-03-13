# Native Entity-Relationship Graph Integration — Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a native entity-relationship graph layer using Neo4j + Claude Haiku with bi-temporal facts, three-stage entity resolution, and hybrid search with RRF fusion.

**Architecture:** Phase 1 wires the existing orphaned `EntityExtractor` into the capture pipeline and exposes entities via API/MCP. Phase 2 adds a new `internal/graph/` package with Neo4j Bolt driver, fact extraction, entity resolution, fact resolution, and hybrid graph search integrated into recall.

**Tech Stack:** Go 1.23+, Neo4j 5.13+ (Bolt via `neo4j-go-driver/v5`), Claude Haiku (Anthropic SDK), Ollama nomic-embed-text (768-dim), Cobra/Viper CLI

**Spec:** `docs/superpowers/specs/2026-03-14-graphiti-entity-graph-design.md`

---

## File Structure

### Phase 1 — Wire Entity Pipeline (modify existing files)

| File | Responsibility |
|------|---------------|
| `internal/models/entity.go` | Add `Project`, `Summary`, `NameEmbedding`, `CommunityID` fields |
| `cmd/openclaw-cortex/cmd_capture.go` | Wire EntityExtractor after Capturer.Extract() |
| `internal/api/server.go` | Add `GET /v1/entities` and `GET /v1/entities/{id}` routes |
| `internal/mcp/server.go` | Add `entity_search` and `entity_get` MCP tools |
| `cmd/openclaw-cortex/cmd_stats.go` | Add entity count to stats output |
| `tests/entity_pipeline_test.go` | Capture + entity extraction integration test |
| `tests/entity_api_test.go` | Entity API endpoint tests |
| `tests/entity_mcp_test.go` | Entity MCP tool tests |

### Phase 2 — Native Graph Layer (new `internal/graph/` package)

| File | Responsibility |
|------|---------------|
| `internal/models/fact.go` | `Fact` struct with bi-temporal fields |
| `internal/graph/types.go` | `FactAction`, `EntityResult`, `FactResult` shared types |
| `internal/graph/client.go` | `Client` interface (storage operations only) |
| `internal/graph/mock_client.go` | `MockGraphClient` for tests |
| `internal/graph/neo4j.go` | `Neo4jClient` — Bolt driver, schema, CRUD, search queries |
| `internal/graph/entity_resolver.go` | Three-stage entity resolution (fulltext → similarity → Claude) |
| `internal/graph/fact_extractor.go` | Claude Haiku fact extraction from conversation text |
| `internal/graph/fact_resolver.go` | Fact dedup + contradiction detection |
| `internal/graph/search.go` | Hybrid search (BM25 + cosine + BFS) with RRF fusion |
| `internal/config/config.go` | Add `GraphConfig` struct + defaults |
| `internal/recall/recall.go` | Graph recall merge: Qdrant + graph → dedup → Rank() |
| `cmd/openclaw-cortex/main.go` | Wire `graph.Client` when `graph.enabled` |
| `cmd/openclaw-cortex/cmd_capture.go` | Add fact extraction + graph write when graph enabled |
| `cmd/openclaw-cortex/cmd_health.go` | Report Neo4j health as optional service |
| `docker-compose.yml` | Add Neo4j service (commented out) |
| `tests/graph_client_test.go` | MockGraphClient contract tests |
| `tests/entity_resolution_test.go` | Three-stage resolution tests |
| `tests/fact_extraction_test.go` | Fact extractor tests |
| `tests/fact_resolution_test.go` | Fact resolution + invalidation tests |
| `tests/graph_search_test.go` | Hybrid search + RRF tests |
| `tests/graph_recall_merge_test.go` | Recall merge + dedup tests |
| `tests/graph_degradation_test.go` | Graceful degradation tests |

---

## Chunk 1: Phase 1 — Entity Pipeline

### Task 1: Extend Entity Model

**Files:**
- Modify: `internal/models/entity.go`
- Test: `tests/entity_pipeline_test.go`

- [ ] **Step 1: Add new fields to Entity struct**

In `internal/models/entity.go`, add four fields after `Metadata`:

```go
type Entity struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Type      EntityType     `json:"type"`
	Aliases   []string       `json:"aliases,omitempty"`
	MemoryIDs []string       `json:"memory_ids,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	Metadata  map[string]any `json:"metadata,omitempty"`

	// Graph integration fields (v0.5.0)
	Project       string    `json:"project,omitempty"`
	Summary       string    `json:"summary,omitempty"`
	NameEmbedding []float32 `json:"name_embedding,omitempty"`
	CommunityID   string    `json:"community_id,omitempty"`
}
```

- [ ] **Step 2: Verify tests still pass**

Run: `go test -short -count=1 ./...`
Expected: All tests pass (additive change, no breakage)

- [ ] **Step 3: Commit**

```bash
git add internal/models/entity.go
git commit -m "feat(models): add Project, Summary, NameEmbedding, CommunityID to Entity"
```

---

### Task 2: Wire EntityExtractor into Capture Command

**Files:**
- Modify: `cmd/openclaw-cortex/cmd_capture.go`
- Ref: `internal/capture/entity_extractor.go` (existing, read-only)

- [ ] **Step 1: Write the failing test**

Create `tests/entity_pipeline_test.go`:

```go
package tests

import (
	"context"
	"testing"

	"github.com/ajitpratap0/openclaw-cortex/internal/capture"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

// TestCaptureWithEntityExtraction verifies that when entity extraction
// is wired into the capture flow, entities are upserted and linked.
// This is a unit test using MockStore — no live services needed.
func TestCaptureWithEntityExtraction(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping entity pipeline test in short mode")
	}

	ms := store.NewMockStore()
	ctx := context.Background()

	// Simulate what cmd_capture.go does after Capturer.Extract():
	// 1. Upsert memory
	mem := models.Memory{
		ID:      "mem-1",
		Content: "Alice from Acme Corp prefers Go for backend services",
		Type:    models.MemoryTypeFact,
		Scope:   models.ScopePermanent,
	}
	vec := make([]float32, 768)
	if err := ms.Upsert(ctx, mem, vec); err != nil {
		t.Fatalf("upsert memory: %v", err)
	}

	// 2. Simulate entity extraction results (would come from EntityExtractor)
	entities := []models.Entity{
		{ID: "ent-1", Name: "Alice", Type: models.EntityTypePerson},
		{ID: "ent-2", Name: "Acme Corp", Type: models.EntityTypeProject},
	}

	// 3. Wire: upsert entities and link to memory
	for i := range entities {
		if err := ms.UpsertEntity(ctx, entities[i]); err != nil {
			t.Fatalf("upsert entity %s: %v", entities[i].Name, err)
		}
		if err := ms.LinkMemoryToEntity(ctx, entities[i].ID, mem.ID); err != nil {
			t.Fatalf("link entity %s to memory: %v", entities[i].Name, err)
		}
	}

	// Verify entities were stored
	found, err := ms.SearchEntities(ctx, "Alice")
	if err != nil {
		t.Fatalf("search entities: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("expected 1 entity for 'Alice', got %d", len(found))
	}
	if found[0].Name != "Alice" {
		t.Errorf("expected entity name 'Alice', got %q", found[0].Name)
	}

	// Verify memory link
	ent, err := ms.GetEntity(ctx, "ent-1")
	if err != nil {
		t.Fatalf("get entity: %v", err)
	}
	if len(ent.MemoryIDs) != 1 || ent.MemoryIDs[0] != "mem-1" {
		t.Errorf("expected entity linked to mem-1, got %v", ent.MemoryIDs)
	}

	_ = capture.NewEntityExtractor // ensure import compiles (real test needs API key)
}
```

- [ ] **Step 2: Run test to verify it passes (mock-only test)**

Run: `go test -v -run TestCaptureWithEntityExtraction ./tests/`
Expected: PASS

- [ ] **Step 3: Add entity extraction to cmd_capture.go**

In `cmd/openclaw-cortex/cmd_capture.go`, after the dedup/upsert loop, add entity extraction. Import `capture` package (already imported). Add a `--entities` flag (default true for CLI, skipped in hooks):

After the `for i := range memories` loop that upserts each memory, add:

```go
// Entity extraction (optional, skipped in hook mode for latency)
if cfg.Claude.APIKey != "" {
	extractor := capture.NewEntityExtractor(cfg.Claude.APIKey, cfg.Claude.Model, logger)
	for i := range memories {
		entities, extractErr := extractor.Extract(ctx, memories[i].Content)
		if extractErr != nil {
			logger.Warn("entity extraction failed, skipping", "error", extractErr)
			continue
		}
		for j := range entities {
			if upsertErr := s.UpsertEntity(ctx, entities[j]); upsertErr != nil {
				logger.Warn("upsert entity failed", "entity", entities[j].Name, "error", upsertErr)
				continue
			}
			if linkErr := s.LinkMemoryToEntity(ctx, entities[j].ID, storedIDs[i]); linkErr != nil {
				logger.Warn("link entity to memory failed", "entity", entities[j].Name, "error", linkErr)
			}
		}
	}
}
```

Note: `storedIDs` is a `[]string` that must be collected during the upsert loop. Add `storedIDs := make([]string, 0, len(memories))` before the loop and `storedIDs = append(storedIDs, id)` inside it for each successfully upserted memory.

- [ ] **Step 4: Run tests**

Run: `go test -short -count=1 ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/openclaw-cortex/cmd_capture.go tests/entity_pipeline_test.go
git commit -m "feat(capture): wire EntityExtractor into capture command"
```

---

### Task 3: Add Entity API Endpoints

**Files:**
- Modify: `internal/api/server.go`
- Create: `tests/entity_api_test.go`

- [ ] **Step 1: Write the failing test**

Create `tests/entity_api_test.go`:

```go
package tests

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ajitpratap0/openclaw-cortex/internal/api"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

func TestEntitySearchEndpoint(t *testing.T) {
	ms := store.NewMockStore()
	ctx := context.Background()

	// Seed entities
	ms.UpsertEntity(ctx, models.Entity{ID: "e1", Name: "Alice", Type: models.EntityTypePerson})
	ms.UpsertEntity(ctx, models.Entity{ID: "e2", Name: "Bob", Type: models.EntityTypePerson})
	ms.UpsertEntity(ctx, models.Entity{ID: "e3", Name: "Acme", Type: models.EntityTypeProject})

	srv := api.NewServer(ms, nil, nil, nil, "test-token")
	handler := srv.Handler()

	// Search for "Alice"
	req := httptest.NewRequest(http.MethodGet, "/v1/entities?query=Alice", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result struct {
		Entities []models.Entity `json:"entities"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(result.Entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(result.Entities))
	}
	if result.Entities[0].Name != "Alice" {
		t.Errorf("expected 'Alice', got %q", result.Entities[0].Name)
	}
}

func TestEntitySearchWithTypeFilter(t *testing.T) {
	ms := store.NewMockStore()
	ctx := context.Background()

	ms.UpsertEntity(ctx, models.Entity{ID: "e1", Name: "Alice", Type: models.EntityTypePerson})
	ms.UpsertEntity(ctx, models.Entity{ID: "e2", Name: "Alice Project", Type: models.EntityTypeProject})

	srv := api.NewServer(ms, nil, nil, nil, "test-token")
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/v1/entities?query=Alice&type=person", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var result struct {
		Entities []models.Entity `json:"entities"`
	}
	json.NewDecoder(rec.Body).Decode(&result)
	if len(result.Entities) != 1 {
		t.Fatalf("expected 1 entity (type-filtered), got %d", len(result.Entities))
	}
}

func TestEntityGetEndpoint(t *testing.T) {
	ms := store.NewMockStore()
	ctx := context.Background()

	ms.UpsertEntity(ctx, models.Entity{ID: "e1", Name: "Alice", Type: models.EntityTypePerson})

	srv := api.NewServer(ms, nil, nil, nil, "test-token")
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/v1/entities/e1", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var entity models.Entity
	json.NewDecoder(rec.Body).Decode(&entity)
	if entity.Name != "Alice" {
		t.Errorf("expected 'Alice', got %q", entity.Name)
	}
}

func TestEntityGetNotFound(t *testing.T) {
	ms := store.NewMockStore()
	srv := api.NewServer(ms, nil, nil, nil, "test-token")
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/v1/entities/nonexistent", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -v -run TestEntity ./tests/`
Expected: FAIL (routes don't exist yet)

- [ ] **Step 3: Implement entity endpoints in server.go**

In `internal/api/server.go`, add two new routes in `Handler()`:

```go
mux.HandleFunc("GET /v1/entities/{id}", s.auth(s.handleGetEntity))
mux.HandleFunc("GET /v1/entities", s.auth(s.handleSearchEntities))
```

Add handler implementations:

```go
func (s *Server) handleSearchEntities(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("query")
	typeFilter := r.URL.Query().Get("type")
	limitStr := r.URL.Query().Get("limit")

	limit := 10
	if limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			if parsed > 100 {
				parsed = 100
			}
			limit = parsed
		}
	}

	entities, err := s.store.SearchEntities(r.Context(), query)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "search failed: %s"}`, err), http.StatusInternalServerError)
		return
	}

	// In-process type filtering (store.SearchEntities has no type param)
	if typeFilter != "" {
		filtered := make([]models.Entity, 0, len(entities))
		for i := range entities {
			if string(entities[i].Type) == typeFilter {
				filtered = append(filtered, entities[i])
			}
		}
		entities = filtered
	}

	// Truncate to limit
	if len(entities) > limit {
		entities = entities[:limit]
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"entities": entities})
}

func (s *Server) handleGetEntity(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	entity, err := s.store.GetEntity(r.Context(), id)
	if err != nil {
		http.Error(w, `{"error": "entity not found"}`, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entity)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -v -run TestEntity ./tests/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/api/server.go tests/entity_api_test.go
git commit -m "feat(api): add entity search and get endpoints"
```

---

### Task 4: Add Entity MCP Tools

**Files:**
- Modify: `internal/mcp/server.go`
- Create: `tests/entity_mcp_test.go`

- [ ] **Step 1: Write the failing test**

Create `tests/entity_mcp_test.go` testing the MCP tool handler functions directly (following existing MCP test patterns):

```go
package tests

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ajitpratap0/openclaw-cortex/internal/mcp"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

func TestMCPEntitySearch(t *testing.T) {
	ms := store.NewMockStore()
	ctx := context.Background()

	ms.UpsertEntity(ctx, models.Entity{ID: "e1", Name: "Alice", Type: models.EntityTypePerson})
	ms.UpsertEntity(ctx, models.Entity{ID: "e2", Name: "Bob", Type: models.EntityTypePerson})

	srv := mcp.NewServer(ms, nil, nil, nil)
	result, err := srv.HandleEntitySearch(ctx, map[string]any{"query": "Alice"})
	if err != nil {
		t.Fatalf("entity search: %v", err)
	}

	var entities []models.Entity
	if jsonErr := json.Unmarshal([]byte(result), &entities); jsonErr != nil {
		t.Fatalf("parse result: %v", jsonErr)
	}
	if len(entities) != 1 || entities[0].Name != "Alice" {
		t.Errorf("expected [Alice], got %v", entities)
	}
}

func TestMCPEntityGet(t *testing.T) {
	ms := store.NewMockStore()
	ctx := context.Background()

	ms.UpsertEntity(ctx, models.Entity{ID: "e1", Name: "Alice", Type: models.EntityTypePerson})

	srv := mcp.NewServer(ms, nil, nil, nil)
	result, err := srv.HandleEntityGet(ctx, map[string]any{"id": "e1"})
	if err != nil {
		t.Fatalf("entity get: %v", err)
	}

	var entity models.Entity
	if jsonErr := json.Unmarshal([]byte(result), &entity); jsonErr != nil {
		t.Fatalf("parse result: %v", jsonErr)
	}
	if entity.Name != "Alice" {
		t.Errorf("expected Alice, got %q", entity.Name)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -v -run TestMCPEntity ./tests/`
Expected: FAIL (HandleEntitySearch/HandleEntityGet don't exist)

- [ ] **Step 3: Add entity tools to MCP server**

In `internal/mcp/server.go`, register two new tools in `NewServer()` and add handler methods `HandleEntitySearch` and `HandleEntityGet`. Follow the exact pattern of existing tools (parameter definitions, `toolResultJSON` helper, graceful nil-check on store).

- [ ] **Step 4: Run tests**

Run: `go test -v -run TestMCPEntity ./tests/`
Expected: PASS

- [ ] **Step 5: Run all tests**

Run: `go test -short -count=1 ./...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/mcp/server.go tests/entity_mcp_test.go
git commit -m "feat(mcp): add entity_search and entity_get tools"
```

---

### Task 5: Add Entity Count to Stats

**Files:**
- Modify: `cmd/openclaw-cortex/cmd_stats.go`

- [ ] **Step 1: Add entity count to stats output**

In `cmd/openclaw-cortex/cmd_stats.go`, after fetching `stats` from `s.Stats(ctx)`, add:

```go
// Entity count (Phase 1: count via SearchEntities with empty query)
entities, entErr := s.SearchEntities(ctx, "")
entityCount := 0
if entErr == nil {
	entityCount = len(entities)
}
```

In the text output section, add a line:

```go
fmt.Fprintf(w, "Entities:      %d\n", entityCount)
```

In the JSON output, add `"entity_count"` field.

- [ ] **Step 2: Run tests**

Run: `go test -short -count=1 ./...`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add cmd/openclaw-cortex/cmd_stats.go
git commit -m "feat(stats): add entity count to stats output"
```

---

### Task 6: Phase 1 Lint + Final Verification

- [ ] **Step 1: Run linter**

Run: `golangci-lint run ./...`
Expected: Clean (no new warnings)

- [ ] **Step 2: Run full test suite**

Run: `go test -short -race -count=1 ./...`
Expected: All PASS

- [ ] **Step 3: Commit any lint fixes**

```bash
git add -A
git commit -m "fix: lint cleanup for Phase 1 entity pipeline"
```

---

## Chunk 2: Phase 2 Foundation — Models, Interface, Config, Mock

### Task 7: Create `models.Fact` Struct

**Files:**
- Create: `internal/models/fact.go`

- [ ] **Step 1: Create fact.go**

```go
package models

import "time"

// Fact represents a relationship between two entities with bi-temporal validity.
// Inspired by Graphiti's EntityEdge — facts are first-class search units with
// their own embeddings, enabling semantic search over relationships.
type Fact struct {
	ID             string  `json:"id"`
	SourceEntityID string  `json:"source_entity_id"`
	TargetEntityID string  `json:"target_entity_id"`
	RelationType   string  `json:"relation_type"`
	Fact           string  `json:"fact"`
	FactEmbedding  []float32 `json:"fact_embedding,omitempty"`

	// Bi-temporal fields: system time vs world time
	CreatedAt time.Time  `json:"created_at"`
	ExpiredAt *time.Time `json:"expired_at,omitempty"`
	ValidAt   *time.Time `json:"valid_at,omitempty"`
	InvalidAt *time.Time `json:"invalid_at,omitempty"`

	// Provenance
	SourceMemoryIDs []string `json:"source_memory_ids,omitempty"`
	Episodes        []string `json:"episodes,omitempty"`
	Confidence      float64  `json:"confidence"`
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/models/`
Expected: Success

- [ ] **Step 3: Commit**

```bash
git add internal/models/fact.go
git commit -m "feat(models): add Fact struct with bi-temporal fields"
```

---

### Task 8: Create `internal/graph/types.go`

**Files:**
- Create: `internal/graph/types.go`

- [ ] **Step 1: Create types.go**

```go
package graph

// FactAction represents the resolution outcome for a new fact.
type FactAction int

const (
	FactActionInsert     FactAction = iota // new fact, no duplicates
	FactActionSkip                         // exact duplicate, append episode
	FactActionInvalidate                   // contradicts existing, invalidate old
)

// EntityResult is a search result from the graph entity index.
type EntityResult struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Type  string  `json:"type"`
	Score float64 `json:"score"`
}

// FactResult is a search result from the graph fact index.
type FactResult struct {
	ID              string   `json:"id"`
	Fact            string   `json:"fact"`
	SourceEntityID  string   `json:"source_entity_id"`
	TargetEntityID  string   `json:"target_entity_id"`
	SourceMemoryIDs []string `json:"source_memory_ids"`
	Score           float64  `json:"score"`
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/graph/`
Expected: Success

- [ ] **Step 3: Commit**

```bash
git add internal/graph/types.go
git commit -m "feat(graph): add shared types (FactAction, EntityResult, FactResult)"
```

---

### Task 9: Create `internal/graph/client.go` Interface

**Files:**
- Create: `internal/graph/client.go`

- [ ] **Step 1: Create client.go**

```go
package graph

import (
	"context"
	"time"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
)

// Client defines the interface for graph storage operations.
// Resolution logic lives in EntityResolver and FactResolver (separate types),
// matching the pattern where ConflictDetector is separate from Store.
type Client interface {
	// EnsureSchema creates indexes and constraints if they don't exist.
	EnsureSchema(ctx context.Context) error

	// UpsertEntity creates or updates an entity node.
	UpsertEntity(ctx context.Context, entity models.Entity) error

	// SearchEntities finds entities by fulltext + embedding similarity.
	SearchEntities(ctx context.Context, query string, embedding []float32, project string, limit int) ([]EntityResult, error)

	// GetEntity retrieves a single entity by ID.
	GetEntity(ctx context.Context, id string) (*models.Entity, error)

	// UpsertFact creates a RELATES_TO edge between two entities.
	UpsertFact(ctx context.Context, fact models.Fact) error

	// SearchFacts finds facts by hybrid search (BM25 + cosine + BFS).
	SearchFacts(ctx context.Context, query string, embedding []float32, limit int) ([]FactResult, error)

	// InvalidateFact sets ExpiredAt and InvalidAt on a fact (never deletes).
	InvalidateFact(ctx context.Context, id string, expiredAt, invalidAt time.Time) error

	// GetFactsBetween returns all active facts between two entities.
	GetFactsBetween(ctx context.Context, sourceID, targetID string) ([]models.Fact, error)

	// GetFactsForEntity returns all active facts involving an entity.
	GetFactsForEntity(ctx context.Context, entityID string) ([]models.Fact, error)

	// AppendEpisode adds an episode/session ID to a fact's episodes list.
	AppendEpisode(ctx context.Context, factID, episodeID string) error

	// AppendMemoryToFact adds a memory ID to a fact's source_memory_ids.
	AppendMemoryToFact(ctx context.Context, factID, memoryID string) error

	// GetMemoryFacts returns all facts derived from a given memory.
	GetMemoryFacts(ctx context.Context, memoryID string) ([]models.Fact, error)

	// RecallByGraph returns memory IDs relevant to a query via graph traversal.
	RecallByGraph(ctx context.Context, query string, embedding []float32, limit int) ([]string, error)

	// Healthy returns true if the graph database is reachable.
	Healthy(ctx context.Context) bool

	// Close releases resources.
	Close() error
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/graph/`
Expected: Success

- [ ] **Step 3: Commit**

```bash
git add internal/graph/client.go
git commit -m "feat(graph): add Client interface for graph storage operations"
```

---

### Task 10: Create `internal/graph/mock_client.go`

**Files:**
- Create: `internal/graph/mock_client.go`
- Create: `tests/graph_client_test.go`

- [ ] **Step 1: Write the failing test**

Create `tests/graph_client_test.go`:

```go
package tests

import (
	"context"
	"testing"
	"time"

	"github.com/ajitpratap0/openclaw-cortex/internal/graph"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
)

func TestMockGraphClient_EntityCRUD(t *testing.T) {
	c := graph.NewMockGraphClient()
	ctx := context.Background()

	entity := models.Entity{
		ID:   "e1",
		Name: "Alice",
		Type: models.EntityTypePerson,
	}

	if err := c.UpsertEntity(ctx, entity); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := c.GetEntity(ctx, "e1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "Alice" {
		t.Errorf("expected Alice, got %q", got.Name)
	}
}

func TestMockGraphClient_FactCRUD(t *testing.T) {
	c := graph.NewMockGraphClient()
	ctx := context.Background()

	fact := models.Fact{
		ID:             "f1",
		SourceEntityID: "e1",
		TargetEntityID: "e2",
		RelationType:   "WORKS_AT",
		Fact:           "Alice works at Acme",
		CreatedAt:      time.Now(),
		Confidence:     0.9,
	}

	if err := c.UpsertFact(ctx, fact); err != nil {
		t.Fatalf("upsert fact: %v", err)
	}

	facts, err := c.GetFactsBetween(ctx, "e1", "e2")
	if err != nil {
		t.Fatalf("get facts: %v", err)
	}
	if len(facts) != 1 {
		t.Fatalf("expected 1 fact, got %d", len(facts))
	}
	if facts[0].Fact != "Alice works at Acme" {
		t.Errorf("unexpected fact text: %q", facts[0].Fact)
	}
}

func TestMockGraphClient_InvalidateFact(t *testing.T) {
	c := graph.NewMockGraphClient()
	ctx := context.Background()

	now := time.Now()
	fact := models.Fact{
		ID:             "f1",
		SourceEntityID: "e1",
		TargetEntityID: "e2",
		Fact:           "Alice works at Acme",
		CreatedAt:      now,
	}
	c.UpsertFact(ctx, fact)

	expiry := now.Add(time.Hour)
	if err := c.InvalidateFact(ctx, "f1", expiry, expiry); err != nil {
		t.Fatalf("invalidate: %v", err)
	}

	// Active facts should be empty after invalidation
	facts, _ := c.GetFactsBetween(ctx, "e1", "e2")
	if len(facts) != 0 {
		t.Errorf("expected 0 active facts after invalidation, got %d", len(facts))
	}
}

func TestMockGraphClient_Healthy(t *testing.T) {
	c := graph.NewMockGraphClient()
	if !c.Healthy(context.Background()) {
		t.Error("mock client should always be healthy")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -v -run TestMockGraphClient ./tests/`
Expected: FAIL (NewMockGraphClient doesn't exist)

- [ ] **Step 3: Implement MockGraphClient**

Create `internal/graph/mock_client.go` implementing all `Client` interface methods with in-memory maps. Follow the same pattern as `internal/store/mock_store.go`:

- `entities map[string]models.Entity`
- `facts map[string]models.Fact`
- Mutex for thread safety
- `SearchEntities`: name-substring match
- `SearchFacts`: returns all non-expired facts (no actual search in mock)
- `GetFactsBetween`: filter by source+target where `ExpiredAt` is nil
- `InvalidateFact`: set `ExpiredAt` and `InvalidAt`
- `RecallByGraph`: return `source_memory_ids` from all matching facts
- `Healthy`: always true

- [ ] **Step 4: Run tests**

Run: `go test -v -run TestMockGraphClient ./tests/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/graph/mock_client.go tests/graph_client_test.go
git commit -m "feat(graph): add MockGraphClient with entity/fact CRUD"
```

---

### Task 11: Add GraphConfig to Config

**Files:**
- Modify: `internal/config/config.go`

- [ ] **Step 1: Add config structs**

In `internal/config/config.go`, add after the existing config structs:

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

Add `Graph GraphConfig` field to the `Config` struct.

In `Load()`, set defaults:

```go
v.SetDefault("graph.enabled", false)
v.SetDefault("graph.neo4j.uri", "bolt://localhost:7687")
v.SetDefault("graph.neo4j.username", "neo4j")
v.SetDefault("graph.neo4j.password", "openclaw-cortex")
v.SetDefault("graph.neo4j.database", "neo4j")
v.SetDefault("graph.entity_resolution.similarity_threshold", 0.95)
v.SetDefault("graph.entity_resolution.max_candidates", 10)
v.SetDefault("graph.fact_extraction.enabled", true)
v.SetDefault("graph.recall_budget_ms", 50)
v.SetDefault("graph.recall_budget_cli_ms", 500)
```

- [ ] **Step 2: Run tests**

Run: `go test -short -count=1 ./...`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/config/config.go
git commit -m "feat(config): add GraphConfig for Neo4j and entity resolution settings"
```

---

## Chunk 3: Phase 2 — Neo4j Client, Resolvers, Fact Extraction

### Task 12: Create Neo4jClient Implementation

**Files:**
- Create: `internal/graph/neo4j.go`
- Modify: `go.mod` (add `neo4j-go-driver/v5`)

- [ ] **Step 1: Add Neo4j driver dependency**

Run: `go get github.com/neo4j/neo4j-go-driver/v5`

- [ ] **Step 2: Implement Neo4jClient**

Create `internal/graph/neo4j.go` implementing `Client` interface. Key patterns:
- Constructor `NewNeo4jClient(ctx, uri, username, password, database, logger)` creates `neo4j.DriverWithContext`
- `EnsureSchema`: creates range indexes, fulltext indexes via `session.ExecuteWrite`
- All writes use `session.ExecuteWrite` with explicit transactions
- All reads use `session.ExecuteRead`
- Entity/Fact CRUD maps Go structs to/from Neo4j node/relationship properties
- `SearchFacts` runs the three Cypher queries from the spec and merges with RRF (delegate to `search.go` in Task 15)
- `RecallByGraph` wraps `SearchFacts` and extracts `source_memory_ids`
- `Healthy` calls `driver.VerifyConnectivity`
- `Close` calls `driver.Close`

- [ ] **Step 3: Verify it compiles**

Run: `go build ./internal/graph/`
Expected: Success

- [ ] **Step 4: Commit**

```bash
git add internal/graph/neo4j.go go.mod go.sum
git commit -m "feat(graph): add Neo4jClient with Bolt driver implementation"
```

---

### Task 13: Create EntityResolver (3-stage)

**Files:**
- Create: `internal/graph/entity_resolver.go`
- Create: `tests/entity_resolution_test.go`

- [ ] **Step 1: Write the failing test**

Create `tests/entity_resolution_test.go` covering:
- `TestEntityResolver_ExactNameMatch` — deterministic fast-path
- `TestEntityResolver_AliasMatch` — alias resolution
- `TestEntityResolver_NoMatch_NewEntity` — no candidates, returns new
- `TestEntityResolver_ClaudeFallback` — mock Claude response for LLM resolution
- `TestEntityResolver_ClaudeError_TreatsAsNew` — graceful degradation

All tests use `MockGraphClient` and mock the Claude API response.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -v -run TestEntityResolver ./tests/`
Expected: FAIL

- [ ] **Step 3: Implement EntityResolver**

Create `internal/graph/entity_resolver.go`:

```go
type EntityResolver struct {
	graph     Client
	client    *anthropic.Client // Claude Haiku for Stage 3
	model     string
	threshold float64 // similarity threshold for fast-path (default 0.95)
	maxCands  int
	logger    *slog.Logger
}

func NewEntityResolver(graph Client, apiKey, model string, threshold float64, maxCands int, logger *slog.Logger) *EntityResolver

// Resolve returns (resolvedID, isNew, err).
// Stage 1: candidate retrieval via graph.SearchEntities
// Stage 2: deterministic fast-path (exact name, alias, embedding > threshold)
// Stage 3: Claude Haiku fallback
// On any error: treats as new entity (safe default)
func (r *EntityResolver) Resolve(ctx context.Context, extracted models.Entity, conversationContext string) (string, bool, error)
```

- [ ] **Step 4: Run tests**

Run: `go test -v -run TestEntityResolver ./tests/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/graph/entity_resolver.go tests/entity_resolution_test.go
git commit -m "feat(graph): add three-stage EntityResolver"
```

---

### Task 14: Create FactExtractor

**Files:**
- Create: `internal/graph/fact_extractor.go`
- Create: `tests/fact_extraction_test.go`

- [ ] **Step 1: Write the failing test**

Create `tests/fact_extraction_test.go` covering:
- `TestFactExtractor_ValidFacts` — extracts facts from conversation text
- `TestFactExtractor_EmptyResult` — no facts to extract
- `TestFactExtractor_MalformedJSON` — graceful degradation
- `TestFactExtractor_APIError` — returns nil, nil (same as EntityExtractor pattern)

Mock Claude responses using httptest.Server or test the prompt construction.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -v -run TestFactExtractor ./tests/`
Expected: FAIL

- [ ] **Step 3: Implement FactExtractor**

Create `internal/graph/fact_extractor.go`:

```go
type FactExtractor struct {
	client *anthropic.Client
	model  string
	logger *slog.Logger
}

func NewFactExtractor(apiKey, model string, logger *slog.Logger) *FactExtractor

// Extract extracts relationship facts from conversation text.
// entityNames is the list of known entity names from the extraction step.
// On API error: logs warning and returns (nil, nil) for graceful degradation.
func (e *FactExtractor) Extract(ctx context.Context, content string, entityNames []string) ([]models.Fact, error)
```

Uses the fact extraction prompt from the spec. XML-escapes content. Parses JSON response. Validates entity names match known list. Generates UUID for each fact.

- [ ] **Step 4: Run tests**

Run: `go test -v -run TestFactExtractor ./tests/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/graph/fact_extractor.go tests/fact_extraction_test.go
git commit -m "feat(graph): add FactExtractor with Claude Haiku"
```

---

### Task 15: Create FactResolver

**Files:**
- Create: `internal/graph/fact_resolver.go`
- Create: `tests/fact_resolution_test.go`

- [ ] **Step 1: Write the failing test**

Create `tests/fact_resolution_test.go` covering:
- `TestFactResolver_ExactDuplicate` — returns FactActionSkip
- `TestFactResolver_Contradiction` — returns FactActionInvalidate
- `TestFactResolver_NewFact` — returns FactActionInsert
- `TestFactResolver_ClaudeError_TreatsAsNew` — graceful degradation
- `TestFactResolver_DuplicateAndContradicted` — both at once

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -v -run TestFactResolver ./tests/`
Expected: FAIL

- [ ] **Step 3: Implement FactResolver**

Create `internal/graph/fact_resolver.go`:

```go
type FactResolver struct {
	graph  Client
	client *anthropic.Client
	model  string
	logger *slog.Logger
}

func NewFactResolver(graph Client, apiKey, model string, logger *slog.Logger) *FactResolver

// Resolve determines if a new fact is a duplicate, contradiction, or new.
// Returns the action to take plus indices of affected existing facts.
func (r *FactResolver) Resolve(ctx context.Context, newFact models.Fact, conversationContext string) (FactAction, []string, error)
```

Follows the 4-step process from the spec: fast path → candidate retrieval → Claude resolution → apply actions.

- [ ] **Step 4: Run tests**

Run: `go test -v -run TestFactResolver ./tests/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/graph/fact_resolver.go tests/fact_resolution_test.go
git commit -m "feat(graph): add FactResolver with dedup and contradiction detection"
```

---

## Chunk 4: Phase 2 — Search, Recall Integration, Wiring

### Task 16: Create Hybrid Graph Search with RRF

**Files:**
- Create: `internal/graph/search.go`
- Create: `tests/graph_search_test.go`

- [ ] **Step 1: Write the failing test**

Create `tests/graph_search_test.go` covering:
- `TestRRFMerge` — unit test of RRF fusion algorithm with known ranked lists
- `TestGraphSearch_MockClient` — search via MockGraphClient returns expected results

```go
func TestRRFMerge(t *testing.T) {
	// Three result lists with overlapping UUIDs
	lists := [][]graph.FactResult{
		{{ID: "a", Score: 0.9}, {ID: "b", Score: 0.8}},
		{{ID: "b", Score: 0.95}, {ID: "c", Score: 0.7}},
		{{ID: "a", Score: 0.85}, {ID: "c", Score: 0.6}},
	}

	merged := graph.RRFMerge(lists, 10)

	// "b" appears in 2 lists at ranks 2,1 → highest RRF score
	// "a" appears in 2 lists at ranks 1,1 → also high
	if len(merged) != 3 {
		t.Fatalf("expected 3 merged results, got %d", len(merged))
	}
	// Verify deduplication
	seen := map[string]bool{}
	for _, r := range merged {
		if seen[r.ID] {
			t.Errorf("duplicate ID in merged results: %s", r.ID)
		}
		seen[r.ID] = true
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -v -run TestRRF ./tests/`
Expected: FAIL

- [ ] **Step 3: Implement search.go**

Create `internal/graph/search.go`:

```go
// RRFMerge implements Reciprocal Rank Fusion (k=60) across multiple ranked lists.
// Each list contains FactResults sorted by their individual method score.
// Returns merged results sorted by combined RRF score, deduped by ID.
func RRFMerge(lists [][]FactResult, limit int) []FactResult

// HybridSearch runs BM25 + cosine + BFS in parallel and merges with RRF.
// This is called by Neo4jClient.SearchFacts and Neo4jClient.RecallByGraph.
func HybridSearch(ctx context.Context, bm25Fn, cosineFn, bfsFn SearchFunc, limit int) ([]FactResult, error)

// SearchFunc is a function that returns a ranked list of fact results.
type SearchFunc func(ctx context.Context, limit int) ([]FactResult, error)
```

- [ ] **Step 4: Run tests**

Run: `go test -v -run TestRRF ./tests/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/graph/search.go tests/graph_search_test.go
git commit -m "feat(graph): add hybrid search with RRF fusion"
```

---

### Task 17: Integrate Graph into Recall Pipeline

**Files:**
- Modify: `internal/recall/recall.go`
- Create: `tests/graph_recall_merge_test.go`

- [ ] **Step 1: Write the failing test**

Create `tests/graph_recall_merge_test.go`:

```go
func TestRecallWithGraphMerge(t *testing.T) {
	// Setup: MockStore with 3 memories, MockGraphClient returning 2 memory IDs
	// (one overlapping with Qdrant results, one graph-only)
	// Verify: merged results have no duplicates, graph-only memory is included,
	// all go through 8-factor ranking
}

func TestRecallWithoutGraph(t *testing.T) {
	// When graph is nil, recall works exactly as before (Qdrant-only)
}

func TestRecallGraphTimeout(t *testing.T) {
	// When graph call exceeds latency budget, Qdrant-only results are used
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -v -run TestRecallWith ./tests/`
Expected: FAIL

- [ ] **Step 3: Modify Recaller to accept optional graph.Client**

In `internal/recall/recall.go`, add `GraphClient graph.Client` field to `Recaller` struct. Modify `Recall()` (or create a new method `RecallWithGraph()`) that:
1. Runs existing Qdrant search
2. If `GraphClient != nil`, calls `RecallByGraph` with latency budget
3. Fetches graph-sourced memories via `store.Get()`
4. Deduplicates by memory ID
5. Passes all candidates through `Rank()`

- [ ] **Step 4: Run tests**

Run: `go test -v -run TestRecallWith ./tests/`
Expected: PASS

- [ ] **Step 5: Run all tests**

Run: `go test -short -count=1 ./...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/recall/recall.go tests/graph_recall_merge_test.go
git commit -m "feat(recall): integrate graph-based recall with dedup and latency budget"
```

---

### Task 18: Wire Graph Client in main.go + Capture

**Files:**
- Modify: `cmd/openclaw-cortex/main.go`
- Modify: `cmd/openclaw-cortex/cmd_capture.go`

- [ ] **Step 1: Add newGraphClient helper to main.go**

```go
func newGraphClient(ctx context.Context, logger *slog.Logger) (graph.Client, error) {
	if !cfg.Graph.Enabled {
		return nil, nil
	}
	client, err := graph.NewNeo4jClient(
		ctx,
		cfg.Graph.Neo4j.URI,
		cfg.Graph.Neo4j.Username,
		cfg.Graph.Neo4j.Password,
		cfg.Graph.Neo4j.Database,
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("graph client: %w", err)
	}
	if schemaErr := client.EnsureSchema(ctx); schemaErr != nil {
		client.Close()
		return nil, fmt.Errorf("graph schema: %w", schemaErr)
	}
	return client, nil
}
```

- [ ] **Step 2: Wire graph into recall and capture commands**

In commands that use recaller (recall, serve, mcp, hook), pass `graph.Client` to `Recaller`. In capture command, add Phase 2 fact extraction + graph write when `cfg.Graph.Enabled`.

- [ ] **Step 3: Run tests**

Run: `go test -short -count=1 ./...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add cmd/openclaw-cortex/main.go cmd/openclaw-cortex/cmd_capture.go
git commit -m "feat(cmd): wire graph.Client into recall and capture pipelines"
```

---

### Task 19: Update Docker Compose + Health Check

**Files:**
- Modify: `docker-compose.yml`
- Modify: `cmd/openclaw-cortex/cmd_health.go`

- [ ] **Step 1: Add Neo4j to docker-compose.yml (commented out)**

```yaml
  # Uncomment to enable entity-relationship graph
  # neo4j:
  #   image: neo4j:5
  #   ports:
  #     - "7474:7474"
  #     - "7687:7687"
  #   environment:
  #     NEO4J_AUTH: neo4j/openclaw-cortex
  #     NEO4J_PLUGINS: '[]'
  #   volumes:
  #     - neo4j_data:/data
  #   restart: unless-stopped
```

Add `neo4j_data:` to the volumes section (also commented).

- [ ] **Step 2: Add Neo4j health check to cmd_health.go**

In `cmd/openclaw-cortex/cmd_health.go`, add an optional Neo4j check when `cfg.Graph.Enabled`:

```go
// Neo4j (optional)
if cfg.Graph.Enabled {
	gc, gcErr := newGraphClient(ctx, logger)
	if gcErr != nil {
		fmt.Fprintf(w, "Neo4j:   FAIL (%v)\n", gcErr)
		allOK = false
	} else {
		if gc.Healthy(ctx) {
			fmt.Fprintf(w, "Neo4j:   OK\n")
		} else {
			fmt.Fprintf(w, "Neo4j:   FAIL (unhealthy)\n")
			allOK = false
		}
		gc.Close()
	}
} else {
	fmt.Fprintf(w, "Neo4j:   SKIP (graph.enabled=false)\n")
}
```

- [ ] **Step 3: Run tests**

Run: `go test -short -count=1 ./...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add docker-compose.yml cmd/openclaw-cortex/cmd_health.go
git commit -m "feat: add Neo4j to docker-compose and health check"
```

---

## Chunk 5: Phase 2 — Degradation Tests + Final Verification

### Task 20: Graceful Degradation Tests

**Files:**
- Create: `tests/graph_degradation_test.go`

- [ ] **Step 1: Write degradation tests**

```go
package tests

func TestRecall_GraphNil_UsesQdrantOnly(t *testing.T) {
	// Recaller with GraphClient=nil should work identically to pre-graph behavior
}

func TestRecall_GraphTimeout_FallsBackToQdrant(t *testing.T) {
	// MockGraphClient that blocks for 200ms + 50ms budget → Qdrant-only results
}

func TestCapture_GraphWriteFailure_MemoryStillStored(t *testing.T) {
	// MockGraphClient that returns error on UpsertEntity
	// Memory should still be in MockStore
}

func TestEntityResolver_ClaudeDown_TreatsAsNew(t *testing.T) {
	// EntityResolver with invalid API key → should not error, should create new entity
}

func TestFactExtractor_ClaudeDown_SkipsFacts(t *testing.T) {
	// FactExtractor with invalid API key → returns nil, nil
}
```

- [ ] **Step 2: Run tests**

Run: `go test -v -run TestRecall_Graph -run TestCapture_Graph -run TestEntityResolver_Claude -run TestFactExtractor_Claude ./tests/`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add tests/graph_degradation_test.go
git commit -m "test: add graceful degradation tests for graph layer"
```

---

### Task 21: Lint + Full Verification

- [ ] **Step 1: Run linter**

Run: `golangci-lint run ./...`
Expected: Clean

- [ ] **Step 2: Run full test suite with race detector**

Run: `go test -short -race -count=1 ./...`
Expected: All PASS

- [ ] **Step 3: Build binary**

Run: `go build -o bin/openclaw-cortex ./cmd/openclaw-cortex`
Expected: Success

- [ ] **Step 4: Fix any issues and commit**

```bash
git add -A
git commit -m "fix: lint cleanup and final verification for entity graph integration"
```

---

### Task 22: Create Feature Branch + PR

- [ ] **Step 1: Ensure all work is on feature branch**

All work should be on `feat/entity-graph-integration` branch.

- [ ] **Step 2: Push and create PR**

```bash
git push -u origin feat/entity-graph-integration
gh pr create --title "feat: native entity-relationship graph integration (Phase 1 + 2)" --body "..."
```

PR body should reference issue #43 and summarize:
- Phase 1: Entity pipeline wired into capture, API/MCP endpoints
- Phase 2: Native Neo4j graph layer with bi-temporal facts, 3-stage entity resolution, fact extraction, hybrid search with RRF
- All Claude Haiku, no OpenAI dependency
- Graceful degradation throughout
