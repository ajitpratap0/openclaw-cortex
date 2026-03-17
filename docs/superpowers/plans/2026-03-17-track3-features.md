# Track 3 — Features (User Namespacing, Streaming Recall, Batch Capture) Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship three user-facing features: multi-user namespace isolation (`user_id` field throughout), server-sent events streaming for `/v1/recall`, and a `capture-batch` CLI command for bulk JSONL import.

**Architecture:** Three PRs serialized by dependency. PR 3.1 (user namespacing) changes the `Store` interface and `models.Memory` — all subsequent PRs depend on it. PR 3.2 (streaming recall) and PR 3.3 (batch capture) are independent of each other but must follow 3.1. PR 3.1 is split into 3.1a (model + store layer, ~8 files) and 3.1b (propagation to API/MCP/CLI, ~22 files).

**Prerequisite:** Track 1 PRs merged (especially 1.4 which changes `NewServer` and `SearchEntities` signatures).

**Tech Stack:** Go 1.25, `net/http` (SSE via `http.Flusher`), golangci-lint v2, black-box tests in `tests/`

**Spec:** `docs/superpowers/specs/2026-03-17-v0.9.0-architecture-security-review-design.md`

---

## PR 3.1a — `feat/user-namespacing` (model + store layer)

**Files:**
- Modify: `internal/models/memory.go`
- Modify: `internal/models/entity.go`
- Modify: `internal/store/store.go`
- Modify: `internal/store/mock_store.go`
- Modify: `internal/memgraph/store.go`
- Modify: `internal/memgraph/graph.go`
- Modify: `internal/config/config.go`
- Modify: `cmd/openclaw-cortex/cmd_migrate.go`
- Test: `tests/user_isolation_test.go` (new)

---

### Task 1: Add `UserID` to models and store interface

- [ ] **Step 1: Write failing isolation test in `tests/user_isolation_test.go`**

```go
package tests

import (
    "context"
    "testing"
    "time"

    "github.com/ajitpratap0/openclaw-cortex/internal/models"
    "github.com/ajitpratap0/openclaw-cortex/internal/store"
)

func TestUserIsolation_UserACannotRecallUserBMemories(t *testing.T) {
    ctx := context.Background()
    st := newTestStore(t) // helper that returns a MockStore

    userA := "alice"
    userB := "bob"

    memA := models.Memory{
        ID:           "mem-a",
        UserID:       userA,
        Content:      "Alice's secret project details",
        Type:         models.MemoryTypeFact,
        Scope:        models.ScopeSession,
        Confidence:   0.9,
        CreatedAt:    time.Now(),
        UpdatedAt:    time.Now(),
        LastAccessed: time.Now(),
    }

    if err := st.Upsert(ctx, memA, make([]float32, 768)); err != nil {
        t.Fatalf("upsert: %v", err)
    }

    // Search as userB — should not find userA's memory
    filterB := &store.SearchFilters{UserID: &userB}
    results, err := st.Search(ctx, make([]float32, 768), 10, filterB)
    if err != nil {
        t.Fatalf("search: %v", err)
    }
    for _, r := range results {
        if r.Memory.UserID == userA {
            t.Fatalf("user B received user A's memory: %s", r.Memory.ID)
        }
    }
}

func TestUserIsolation_SameUserCanRecallOwnMemories(t *testing.T) {
    ctx := context.Background()
    st := newTestStore(t)

    userA := "alice"
    mem := models.Memory{
        ID:           "mem-aa",
        UserID:       userA,
        Content:      "Alice remembers this",
        Type:         models.MemoryTypeFact,
        Scope:        models.ScopeSession,
        Confidence:   0.9,
        CreatedAt:    time.Now(),
        UpdatedAt:    time.Now(),
        LastAccessed: time.Now(),
    }
    _ = st.Upsert(ctx, mem, make([]float32, 768))

    filterA := &store.SearchFilters{UserID: &userA}
    results, err := st.Search(ctx, make([]float32, 768), 10, filterA)
    if err != nil {
        t.Fatalf("search: %v", err)
    }
    found := false
    for _, r := range results {
        if r.Memory.ID == "mem-aa" {
            found = true
        }
    }
    if !found {
        t.Fatal("expected to find alice's own memory, did not")
    }
}

// newTestStore returns a MockStore for unit tests.
func newTestStore(t *testing.T) store.Store {
    t.Helper()
    return store.NewMockStore()
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test -run TestUserIsolation ./tests/ -v -count=1
```

Expected: FAIL — `models.Memory` has no `UserID` field, `store.SearchFilters` has no `UserID` field.

---

### Task 2: Add `UserID` to models

- [ ] **Step 3: Add `UserID string` to `models.Memory` in `internal/models/memory.go`**

Add the field after `Project`:
```go
// UserID is the owner of this memory. Used for namespace isolation.
// Empty string means the memory belongs to the default user ("default").
UserID string `json:"user_id,omitempty"`
```

- [ ] **Step 4: Add `UserID string` to `models.Entity` in `internal/models/entity.go`**

```go
UserID string `json:"user_id,omitempty"`
```

- [ ] **Step 5: Add `UserID *string` to `store.SearchFilters` in `internal/store/store.go`**

```go
// UserID filters results to a specific user namespace.
// nil means no user filter (returns results for all users).
UserID *string `json:"user_id,omitempty"`
```

---

### Task 3: Update MockStore to enforce user isolation

- [ ] **Step 6: Update `internal/store/mock_store.go` `Search` and `List` to filter by `UserID`**

In `MockStore.Search`, after existing filters, add:
```go
if filters != nil && filters.UserID != nil {
    var filtered []models.SearchResult
    for _, r := range results {
        if r.Memory.UserID == *filters.UserID {
            filtered = append(filtered, r)
        }
    }
    results = filtered
}
```

Apply the same pattern to `MockStore.List`.

- [ ] **Step 7: Run isolation tests**

```bash
go test -run TestUserIsolation -race -count=1 ./tests/ -v
```

Expected: PASS.

---

### Task 4: Update MemgraphStore to filter by `UserID`

- [ ] **Step 8: Update `buildWhereClause` in `internal/memgraph/store.go`** to handle `UserID` filter

In `buildWhereClause(filters *store.SearchFilters, alias string)`:
```go
if filters.UserID != nil {
    clauses = append(clauses, fmt.Sprintf("%s.user_id = $user_id", alias))
    params["user_id"] = *filters.UserID
}
```

- [ ] **Step 9: Update `memoryToParams` to include `user_id`**

```go
params["user_id"] = memory.UserID
```

Add `m.user_id = $user_id` to the `SET` clause in `Upsert`.

- [ ] **Step 10: Update Memgraph schema** in `internal/memgraph/graph.go` — add user_id index:

```go
"CREATE INDEX ON :Memory(user_id)",
"CREATE INDEX ON :Entity(user_id)",
```

- [ ] **Step 11: Update entity upsert/search** in `MemgraphStore` to persist and filter `UserID`.

---

### Task 5: Config + migration

- [ ] **Step 12: Add default user config in `internal/config/config.go`**

```go
// UserConfig holds user identity settings.
type UserConfig struct {
    DefaultUserID string `mapstructure:"default_user_id"`
}
```

Add `User UserConfig` to `Config` struct.

In `Load()`:
```go
v.SetDefault("user.default_user_id", "default")
_ = v.BindEnv("user.default_user_id", "OPENCLAW_CORTEX_USER_ID")
```

- [ ] **Step 13: Add `--add-user-id` migration subcommand to `cmd/openclaw-cortex/cmd_migrate.go`**

```go
// migrateAddUserIDCmd backfills user_id = "default" on all Memory and Entity nodes
// that have no user_id set.
var migrateAddUserIDCmd = &cobra.Command{
    Use:   "add-user-id",
    Short: "Backfill user_id='default' on all memories and entities without one",
    RunE: func(cmd *cobra.Command, args []string) error {
        // Run Cypher:
        // MATCH (m:Memory) WHERE m.user_id IS NULL OR m.user_id = ""
        // SET m.user_id = "default"
        // MATCH (e:Entity) WHERE e.user_id IS NULL OR e.user_id = ""
        // SET e.user_id = "default"
        logger := newLogger()
        ctx := cmd.Context()
        st, err := newMemgraphStore(ctx, logger)
        if err != nil {
            return fmt.Errorf("migrate: %w", err)
        }
        defer func() { _ = st.Close() }()
        return st.MigrateAddUserID(ctx)
    },
}
```

Add `MigrateAddUserID(ctx context.Context) error` to `MemgraphStore`.

- [ ] **Step 14: Run full test suite**

```bash
go test -short -race -count=1 ./...
golangci-lint run ./...
```

Expected: green.

- [ ] **Step 15: Commit 3.1a**

```bash
git checkout -b feat/user-namespacing
git add internal/models/ internal/store/ internal/memgraph/ internal/config/config.go \
        cmd/openclaw-cortex/cmd_migrate.go tests/user_isolation_test.go
git commit -m "feat(user-namespacing) 3.1a: add UserID to models, store, Memgraph

Adds UserID field to Memory and Entity models.
Adds UserID filter to SearchFilters.
MockStore and MemgraphStore enforce user isolation on Search and List.
Memgraph schema gains user_id index on Memory and Entity.
Migration: cortex migrate add-user-id backfills user_id='default'.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## PR 3.1b — `feat/user-namespacing` (API/MCP/CLI propagation)

**Prerequisite:** 3.1a committed on same branch.

**Files:**
- Modify: `internal/api/server.go`
- Modify: `internal/mcp/server.go`
- Modify: `internal/hooks/hooks.go`
- Modify: all `cmd/openclaw-cortex/cmd_*.go` (add `--user` flag)
- Modify: `cmd/openclaw-cortex/main.go`
- Test: `tests/api_user_test.go` (new)

---

### Task 6: HTTP API user propagation

- [ ] **Step 1: Write failing API test in `tests/api_user_test.go`**

```go
package tests

import (
    "bytes"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/ajitpratap0/openclaw-cortex/internal/api"
    // ... imports
)

func TestAPIRemember_StoresUserID(t *testing.T) {
    st := store.NewMockStore()
    srv := api.NewServer(st, nil, newTestEmbedder(t), newTestLogger(), "", "")
    handler := srv.Handler()

    body, _ := json.Marshal(map[string]any{
        "content": "test memory",
        "type":    "fact",
    })
    req := httptest.NewRequest(http.MethodPost, "/v1/remember", bytes.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("X-User-ID", "test-user")

    rr := httptest.NewRecorder()
    handler.ServeHTTP(rr, req)

    if rr.Code != http.StatusOK {
        t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
    }

    // Verify the memory was stored with the correct user_id
    memories, _, _ := st.List(context.Background(), &store.SearchFilters{}, 10, "")
    if len(memories) == 0 {
        t.Fatal("no memories stored")
    }
    if memories[0].UserID != "test-user" {
        t.Fatalf("expected user_id 'test-user', got %q", memories[0].UserID)
    }
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test -run TestAPIRemember_StoresUserID ./tests/ -v -count=1
```

Expected: FAIL — HTTP handler ignores `X-User-ID` header.

- [ ] **Step 3: Update `internal/api/server.go` to read `X-User-ID`**

Add a helper:
```go
// userIDFromRequest extracts the user ID from X-User-ID header.
// Falls back to the server's configured default user ID.
func (s *Server) userIDFromRequest(r *http.Request) string {
    if uid := r.Header.Get("X-User-ID"); uid != "" {
        return uid
    }
    return s.defaultUserID
}
```

Add `defaultUserID string` to `Server` struct and `NewServer` parameters.

Update `handleRemember`, `handleRecall`, `handleSearch`, `handleList`, `handleUpdate`, `handleDeleteMemory`, `handleGetMemory` to call `s.userIDFromRequest(r)` and pass the user ID into the memory or filter.

- [ ] **Step 4: Run API user test**

```bash
go test -run TestAPIRemember_StoresUserID -race -count=1 ./tests/ -v
```

Expected: PASS.

---

### Task 7: MCP and CLI propagation

- [ ] **Step 5: Add `defaultUserID` to the MCP server struct and constructor in `internal/mcp/server.go`**

Read `internal/mcp/server.go` to find the `Server` struct and `NewMCPServer` (or `New`) function. Add the field and update the constructor:

```go
type Server struct {
    // ... existing fields ...
    defaultUserID string
}

// NewMCPServer (or the existing constructor) gains a defaultUserID parameter.
func NewMCPServer(/* existing params */, defaultUserID string) *Server {
    return &Server{
        // ... existing assignments ...
        defaultUserID: defaultUserID,
    }
}
```

Update the call site in `cmd/openclaw-cortex/cmd_mcp.go` to pass `cfg.User.DefaultUserID` (added in Task 5 / 3.1a).

Then add the optional `user_id` parameter to `remember`, `recall`, `search`, `forget` tool definitions:

```go
mcpgo.WithString("user_id",
    mcpgo.Description("User namespace for memory isolation (default: configured default)"),
),
```

In each handler, read: `userID := req.GetString("user_id", s.defaultUserID)` and pass it into the memory or filter.

- [ ] **Step 6: Add `--user` flag to all CLI commands**

In `cmd/openclaw-cortex/main.go`, add a persistent flag on the root command:
```go
var globalUserID string
rootCmd.PersistentFlags().StringVar(&globalUserID, "user", "", "User ID for namespace isolation (default: from config)")
```

In `PersistentPreRunE`, after config load:
```go
if globalUserID == "" {
    globalUserID = cfg.User.DefaultUserID
}
```

Each command's `RunE` uses `globalUserID` when constructing memories or search filters.

- [ ] **Step 7: Update `internal/hooks/hooks.go`** to set `UserID` on memories from `PreTurnInput.SessionID`:

```go
// Use session_id as user namespace when no explicit user set.
userID := input.UserID
if userID == "" {
    userID = input.SessionID
}
```

Add `UserID string` to `PreTurnInput` and `PostTurnInput`.

- [ ] **Step 8: Run full test suite**

```bash
go test -short -race -count=1 ./...
golangci-lint run ./...
```

Expected: green.

- [ ] **Step 9: Commit 3.1b**

```bash
git add internal/api/server.go internal/mcp/server.go internal/hooks/hooks.go \
        cmd/openclaw-cortex/ tests/api_user_test.go
git commit -m "feat(user-namespacing) 3.1b: propagate UserID through API, MCP, CLI, hooks

HTTP API reads X-User-ID header; falls back to server default user.
MCP tools accept optional user_id parameter.
CLI commands accept --user flag; falls back to config user.default_user_id.
Hooks use session_id as user namespace when no explicit user is set.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## PR 3.2 — `feat/streaming-recall`

**Prerequisite:** PR 3.1b merged.

**Files:**
- Modify: `internal/api/server.go`
- Modify: `internal/recall/recall.go`
- Test: `tests/streaming_recall_test.go` (new)

---

### Task 8: Streaming recall endpoint

- [ ] **Step 0: Verify `recall.Recaller` method signatures before writing the test**

Before writing the streaming handler, read `internal/recall/recall.go` to find the method that takes scored results and returns ranked `[]models.SearchResult`. If it is named differently from `RecallWithGraph`, use the actual method name throughout Task 9. If no such synchronous method exists yet, add one:

```go
// RankResults applies multi-factor scoring and RRF graph merge to the given
// search results for query message/vec and returns them sorted by score.
// This is the synchronous variant used by the streaming SSE handler.
func (r *Recaller) RankResults(ctx context.Context, message string, vec []float32, results []models.SearchResult, project string) []models.SearchResult {
    // Delegate to existing internal scoring logic.
    // If the existing Recall method already does this, export its internals
    // or refactor to share the scoring pipeline.
}
```

Commit any changes to `internal/recall/recall.go` as part of this PR (it is already in the Files list for PR 3.2).

- [ ] **Step 1: Write failing test in `tests/streaming_recall_test.go`**

Note: `newTestRecaller(t)` must be defined in the `tests/` package. Add this helper to a shared test file (e.g. `tests/helpers_test.go`). Since `recall.NewRecaller` (or equivalent constructor) requires a store and embedder, create a minimal version:

```go
// Add to tests/helpers_test.go (create file if it does not exist):
func newTestRecaller(t *testing.T) *recall.Recaller {
    t.Helper()
    return recall.NewRecaller(store.NewMockStore(), nil, nil)
    // Adjust constructor arguments to match the actual recall.NewRecaller signature
    // found in internal/recall/recall.go.
}
```

Also note: `api.NewServer` gains a `recall *recall.Recaller` parameter in PR 3.1b (Task 6, Step 3). Ensure the test matches that updated signature — `NewServer(st, rec, emb, logger, authToken, defaultUserID)`.

```go
package tests

import (
    "bufio"
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"

    "github.com/ajitpratap0/openclaw-cortex/internal/api"
    "github.com/ajitpratap0/openclaw-cortex/internal/models"
    "github.com/ajitpratap0/openclaw-cortex/internal/store"
)

func TestStreamingRecall_ReturnsSSEEvents(t *testing.T) {
    st := store.NewMockStore()
    // Seed a memory
    _ = st.Upsert(context.Background(), models.Memory{
        ID: "m1", UserID: "u1", Content: "test content",
        Type: models.MemoryTypeFact, Scope: models.ScopeSession,
        Confidence: 0.9, CreatedAt: time.Now(), UpdatedAt: time.Now(), LastAccessed: time.Now(),
    }, make([]float32, 768))

    srv := api.NewServer(st, newTestRecaller(t), newTestEmbedder(t), newTestLogger(), "", "u1")
    handler := srv.Handler()

    req := httptest.NewRequest(http.MethodGet, "/v1/recall/stream?message=test&budget=2000", nil)
    req.Header.Set("Accept", "text/event-stream")
    req.Header.Set("X-User-ID", "u1")

    rr := httptest.NewRecorder()
    handler.ServeHTTP(rr, req)

    if rr.Code != http.StatusOK {
        t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
    }
    if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
        t.Fatalf("expected text/event-stream, got %q", ct)
    }

    // Parse SSE events
    scanner := bufio.NewScanner(rr.Body)
    eventCount := 0
    for scanner.Scan() {
        line := scanner.Text()
        if strings.HasPrefix(line, "data: ") {
            var mem map[string]any
            if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &mem); err != nil {
                t.Fatalf("invalid SSE data JSON: %v", err)
            }
            eventCount++
        }
    }
    if eventCount == 0 {
        t.Fatal("expected at least one SSE event")
    }
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test -run TestStreamingRecall ./tests/ -v -count=1
```

Expected: FAIL — `/v1/recall/stream` route doesn't exist.

---

### Task 9: Implement streaming recall

- [ ] **Step 3: Add `handleStreamRecall` to `internal/api/server.go`**

Register the route in `Handler()`:
```go
mux.HandleFunc("GET /v1/recall/stream", s.auth(s.handleStreamRecall))
```

Implement the handler:
```go
func (s *Server) handleStreamRecall(w http.ResponseWriter, r *http.Request) {
    flusher, ok := w.(http.Flusher)
    if !ok {
        s.writeError(w, http.StatusInternalServerError, "streaming not supported")
        return
    }

    message := r.URL.Query().Get("message")
    if message == "" {
        s.writeError(w, http.StatusBadRequest, "message is required")
        return
    }

    budgetStr := r.URL.Query().Get("budget")
    budget := 2000
    if budgetStr != "" {
        if b, err := strconv.Atoi(budgetStr); err == nil && b > 0 {
            budget = b
        }
    }

    project := r.URL.Query().Get("project")
    userID := s.userIDFromRequest(r)

    vec, err := s.embedder.Embed(r.Context(), message)
    if err != nil {
        s.writeError(w, http.StatusInternalServerError, "embedding failed")
        return
    }

    var filters *store.SearchFilters
    filters = &store.SearchFilters{UserID: &userID}
    if project != "" {
        filters.Project = &project
    }

    results, err := s.store.Search(r.Context(), vec, 50, filters)
    if err != nil {
        s.writeError(w, http.StatusInternalServerError, "search failed")
        return
    }

    // Use the method verified in Step 0 (RankResults or the actual method name).
    ranked := s.recall.RankResults(r.Context(), message, vec, results, project)

    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")
    w.WriteHeader(http.StatusOK)

    remaining := budget
    for i := range ranked {
        if remaining <= 0 {
            break
        }
        content := ranked[i].Memory.Content
        tokens := tokenizer.EstimateTokens(content)
        if tokens > remaining {
            break
        }
        remaining -= tokens

        data, encErr := json.Marshal(map[string]any{
            "id":      ranked[i].Memory.ID,
            "content": content,
            "score":   ranked[i].Score,
        })
        if encErr != nil {
            continue
        }
        fmt.Fprintf(w, "data: %s\n\n", data)
        flusher.Flush()

        // Update access metadata
        if accessErr := s.store.UpdateAccessMetadata(r.Context(), ranked[i].Memory.ID); accessErr != nil {
            s.logger.Warn("handleStreamRecall: UpdateAccessMetadata", "id", ranked[i].Memory.ID, "error", accessErr)
        }
    }
}
```

- [ ] **Step 4: Run streaming tests**

```bash
go test -run TestStreamingRecall -race -count=1 ./tests/ -v
```

Expected: PASS.

- [ ] **Step 5: Run full suite and lint**

```bash
go test -short -race -count=1 ./...
golangci-lint run ./...
```

Expected: green.

- [ ] **Step 6: Commit**

```bash
git checkout -b feat/streaming-recall
git add internal/api/server.go tests/streaming_recall_test.go
git commit -m "feat(api): add GET /v1/recall/stream SSE endpoint

Server-sent events endpoint that flushes each ranked memory as it's
scored, allowing clients to start rendering before all candidates
are processed. Respects token budget; updates access metadata.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## PR 3.3 — `feat/batch-capture`

**Prerequisite:** PR 3.1b merged. PR 3.2 is independent.

**Relationship to existing `store-batch`:** `store-batch` (`cmd_store_batch.go`) bulk-upserts pre-formed memory records with no LLM extraction. `capture-batch` runs the full extraction pipeline on raw conversation turns (user/assistant messages). They serve different use cases and both are kept.

**Files:**
- Create: `internal/capture/batch.go`
- Create: `cmd/openclaw-cortex/cmd_capture_batch.go`
- Modify: `cmd/openclaw-cortex/main.go`
- Test: `tests/batch_capture_test.go` (new)

---

### Task 10: BatchCapturer

- [ ] **Step 1: Write failing tests in `tests/batch_capture_test.go`**

```go
package tests

import (
    "bytes"
    "context"
    "encoding/json"
    "strings"
    "testing"

    "github.com/ajitpratap0/openclaw-cortex/internal/capture"
    "github.com/ajitpratap0/openclaw-cortex/internal/store"
)

func TestBatchCapturer_ProcessesTurns(t *testing.T) {
    lines := []capture.BatchTurn{
        {User: "What is Go?", Assistant: "Go is a statically typed language.", Project: "test", UserID: "u1"},
        {User: "Who made it?", Assistant: "Google created Go in 2009.", Project: "test", UserID: "u1"},
    }

    var buf bytes.Buffer
    for _, l := range lines {
        b, _ := json.Marshal(l)
        buf.Write(b)
        buf.WriteByte('\n')
    }

    st := store.NewMockStore()
    bc := capture.NewBatchCapturer(newTestCapturer(t), newTestEmbedder(t), newTestClassifier(t), st, newTestLogger())

    stats, err := bc.Run(context.Background(), &buf, capture.BatchOptions{
        Concurrency: 2,
        DryRun:      false,
    })
    if err != nil {
        t.Fatalf("Run: %v", err)
    }
    if stats.ProcessedLines != 2 {
        t.Fatalf("expected 2 processed lines, got %d", stats.ProcessedLines)
    }
}

func TestBatchCapturer_DryRunDoesNotWrite(t *testing.T) {
    line := capture.BatchTurn{User: "test", Assistant: "answer", Project: "p", UserID: "u"}
    var buf bytes.Buffer
    b, _ := json.Marshal(line)
    buf.Write(b)
    buf.WriteByte('\n')

    st := store.NewMockStore()
    bc := capture.NewBatchCapturer(newTestCapturer(t), newTestEmbedder(t), newTestClassifier(t), st, newTestLogger())

    _, err := bc.Run(context.Background(), &buf, capture.BatchOptions{
        Concurrency: 1,
        DryRun:      true,
    })
    if err != nil {
        t.Fatalf("Run: %v", err)
    }

    memories, _, _ := st.List(context.Background(), nil, 100, "")
    if len(memories) != 0 {
        t.Fatalf("dry-run: expected 0 memories stored, got %d", len(memories))
    }
}

func TestBatchCapturer_SkipsMalformedLines(t *testing.T) {
    input := strings.NewReader("not-valid-json\n{\"user\":\"hi\",\"assistant\":\"hello\"}\n")

    st := store.NewMockStore()
    bc := capture.NewBatchCapturer(newTestCapturer(t), newTestEmbedder(t), newTestClassifier(t), st, newTestLogger())

    stats, err := bc.Run(context.Background(), input, capture.BatchOptions{Concurrency: 1})
    if err != nil {
        t.Fatalf("Run: %v", err)
    }
    if stats.SkippedLines != 1 {
        t.Fatalf("expected 1 skipped line, got %d", stats.SkippedLines)
    }
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test -run TestBatchCapturer ./tests/ -v -count=1
```

Expected: FAIL — `capture.BatchCapturer` does not exist.

---

### Task 11: Implement `internal/capture/batch.go`

- [ ] **Step 3: Create `internal/capture/batch.go`**

```go
package capture

import (
    "bufio"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "log/slog"
    "sync"
    "time"

    "github.com/google/uuid"

    "github.com/ajitpratap0/openclaw-cortex/internal/classifier"
    "github.com/ajitpratap0/openclaw-cortex/internal/embedder"
    "github.com/ajitpratap0/openclaw-cortex/internal/models"
    "github.com/ajitpratap0/openclaw-cortex/internal/store"
)

// BatchTurn is one line of JSONL input for capture-batch.
type BatchTurn struct {
    User      string `json:"user"`
    Assistant string `json:"assistant"`
    Project   string `json:"project,omitempty"`
    UserID    string `json:"user_id,omitempty"`
}

// BatchOptions controls batch processing behaviour.
type BatchOptions struct {
    Concurrency int
    DryRun      bool
}

// BatchStats summarizes the result of a batch run.
type BatchStats struct {
    ProcessedLines int
    StoredMemories int
    SkippedLines   int
    Errors         int
}

// BatchCapturer runs the capture pipeline over a stream of JSONL conversation turns.
type BatchCapturer struct {
    capturer   Capturer
    embedder   embedder.Embedder
    classifier classifier.Classifier
    store      store.Store
    logger     *slog.Logger
}

// NewBatchCapturer creates a BatchCapturer.
func NewBatchCapturer(cap Capturer, emb embedder.Embedder, cls classifier.Classifier, st store.Store, logger *slog.Logger) *BatchCapturer {
    return &BatchCapturer{
        capturer:   cap,
        embedder:   emb,
        classifier: cls,
        store:      st,
        logger:     logger,
    }
}

// Run reads JSONL from r and runs the capture pipeline on each turn.
// Progress is logged; errors on individual lines are non-fatal.
func (b *BatchCapturer) Run(ctx context.Context, r io.Reader, opts BatchOptions) (BatchStats, error) {
    concurrency := opts.Concurrency
    if concurrency <= 0 {
        concurrency = 4
    }
    if concurrency > 16 {
        concurrency = 16
    }

    type work struct {
        lineNum int
        turn    BatchTurn
    }

    workCh := make(chan work, concurrency*2)
    var stats BatchStats
    var mu sync.Mutex

    var wg sync.WaitGroup
    for i := 0; i < concurrency; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for w := range workCh {
                stored, err := b.processTurn(ctx, w.turn, opts.DryRun)
                mu.Lock()
                stats.ProcessedLines++
                if err != nil {
                    b.logger.Warn("batch capture: line failed", "line", w.lineNum, "error", err)
                    stats.Errors++
                } else {
                    stats.StoredMemories += stored
                }
                mu.Unlock()
            }
        }()
    }

    scanner := bufio.NewScanner(r)
    lineNum := 0
    for scanner.Scan() {
        lineNum++
        line := scanner.Bytes()
        var turn BatchTurn
        if err := json.Unmarshal(line, &turn); err != nil {
            b.logger.Warn("batch capture: skipping malformed line", "line", lineNum, "error", err)
            mu.Lock()
            stats.SkippedLines++
            mu.Unlock()
            continue
        }
        if turn.User == "" || turn.Assistant == "" {
            mu.Lock()
            stats.SkippedLines++
            mu.Unlock()
            continue
        }
        workCh <- work{lineNum: lineNum, turn: turn}
    }
    close(workCh)
    wg.Wait()

    return stats, scanner.Err()
}

func (b *BatchCapturer) processTurn(ctx context.Context, turn BatchTurn, dryRun bool) (int, error) {
    memories, err := b.capturer.Extract(ctx, turn.User, turn.Assistant)
    if err != nil {
        return 0, fmt.Errorf("extract: %w", err)
    }

    if dryRun {
        return len(memories), nil
    }

    stored := 0
    for _, m := range memories {
        vec, embErr := b.embedder.Embed(ctx, m.Content)
        if embErr != nil {
            return stored, fmt.Errorf("embed: %w", embErr)
        }
        now := time.Now().UTC()
        mem := models.Memory{
            ID:           uuid.NewString(),
            Type:         models.MemoryType(m.Type),
            Scope:        models.ScopeSession,
            Content:      m.Content,
            Confidence:   m.Confidence,
            Tags:         m.Tags,
            Source:       "batch",
            Project:      turn.Project,
            UserID:       turn.UserID,
            CreatedAt:    now,
            UpdatedAt:    now,
            LastAccessed: now,
        }
        if upsertErr := b.store.Upsert(ctx, mem, vec); upsertErr != nil {
            return stored, fmt.Errorf("upsert: %w", upsertErr)
        }
        stored++
    }
    return stored, nil
}
```

- [ ] **Step 4: Run batch capturer tests**

```bash
go test -run TestBatchCapturer -race -count=1 ./tests/ -v
```

Expected: PASS.

---

### Task 12: Add `capture-batch` CLI command

- [ ] **Step 5: Create `cmd/openclaw-cortex/cmd_capture_batch.go`**

```go
package main

import (
    "encoding/json"
    "fmt"
    "os"

    "github.com/spf13/cobra"

    "github.com/ajitpratap0/openclaw-cortex/internal/capture"
    "github.com/ajitpratap0/openclaw-cortex/internal/classifier"
    "github.com/ajitpratap0/openclaw-cortex/internal/llm"
)

func captureBatchCmd() *cobra.Command {
    var concurrency int
    var dryRun bool
    var printStats bool

    cmd := &cobra.Command{
        Use:   "capture-batch",
        Short: "Capture memories from JSONL conversation turns read from stdin",
        Long: `Reads JSONL from stdin. Each line must be: {"user":"...","assistant":"...","project":"...","user_id":"..."}.
Runs the full capture pipeline (LLM extraction, embedding, upsert) on each turn.
See store-batch for bulk-upsert of pre-formed memory records without LLM extraction.`,
        RunE: func(cmd *cobra.Command, args []string) error {
            logger := newLogger()
            ctx := cmd.Context()

            st, err := newMemgraphStore(ctx, logger)
            if err != nil {
                return cmdErr("capture-batch: connecting to store", err)
            }
            defer func() { _ = st.Close() }()

            llmClient := llm.NewClient(cfg.Claude)
            cap := capture.NewCapturer(llmClient, cfg.Claude.Model, logger)
            emb := newEmbedder(logger)
            cls := classifier.New()

            bc := capture.NewBatchCapturer(cap, emb, cls, st, logger)
            stats, err := bc.Run(ctx, os.Stdin, capture.BatchOptions{
                Concurrency: concurrency,
                DryRun:      dryRun,
            })
            if err != nil {
                return cmdErr("capture-batch: run", err)
            }

            fmt.Fprintf(os.Stderr, "processed %d lines, stored %d memories, skipped %d, errors %d\n",
                stats.ProcessedLines, stats.StoredMemories, stats.SkippedLines, stats.Errors)

            if printStats {
                b, _ := json.Marshal(stats)
                fmt.Println(string(b))
            }
            return nil
        },
    }

    cmd.Flags().IntVar(&concurrency, "concurrency", 4, "Number of concurrent workers (max 16)")
    cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Extract memories but do not write to store; prints extracted JSONL to stdout")
    cmd.Flags().BoolVar(&printStats, "stats", false, "Print JSON summary to stdout on completion")
    return cmd
}
```

- [ ] **Step 6: Register the command in `cmd/openclaw-cortex/main.go`**

Add `captureBatchCmd()` to `rootCmd.AddCommand(...)`.

- [ ] **Step 7: Run full test suite**

```bash
go test -short -race -count=1 ./...
golangci-lint run ./...
```

Expected: green.

- [ ] **Step 8: Smoke test**

```bash
go build -o bin/openclaw-cortex ./cmd/openclaw-cortex

echo '{"user":"What is Go?","assistant":"Go is a compiled language."}' | \
  ./bin/openclaw-cortex capture-batch --dry-run --stats
# Expected: stats JSON to stdout; "stored 0 memories" (dry-run)
```

- [ ] **Step 9: Commit**

```bash
git checkout -b feat/batch-capture
git add internal/capture/batch.go cmd/openclaw-cortex/cmd_capture_batch.go cmd/openclaw-cortex/main.go \
        tests/batch_capture_test.go
git commit -m "feat(cli): add capture-batch command for JSONL bulk import

Reads JSONL from stdin (user/assistant/project/user_id per line).
Runs full capture pipeline: LLM extraction, embedding, upsert.
Worker pool (default 4, max 16); --dry-run and --stats flags.
Malformed lines are skipped; processing continues on errors.

Distinct from store-batch which upserts pre-formed records without LLM.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Final Track 3 Verification

- [ ] **Run all tests**

```bash
go build ./...
go test -race -count=1 ./...
golangci-lint run ./...
```

Expected: green.

- [ ] **Smoke test user isolation**

```bash
OPENCLAW_CORTEX_API_AUTH_TOKEN=secret ./bin/openclaw-cortex serve &

# Store memory as alice
curl -s -X POST http://localhost:8080/v1/remember \
  -H "Authorization: Bearer secret" \
  -H "X-User-ID: alice" \
  -H "Content-Type: application/json" \
  -d '{"content":"Alice secret","type":"fact"}'

# Recall as bob — should get 0 memories
curl -s -X POST http://localhost:8080/v1/recall \
  -H "Authorization: Bearer secret" \
  -H "X-User-ID: bob" \
  -H "Content-Type: application/json" \
  -d '{"message":"Alice secret","budget":2000}'
# Expected: {"context":"","memory_count":0,"tokens_used":0}
```

- [ ] **Smoke test streaming recall**

```bash
curl -s -N \
  -H "Authorization: Bearer secret" \
  -H "Accept: text/event-stream" \
  -H "X-User-ID: alice" \
  "http://localhost:8080/v1/recall/stream?message=secret&budget=2000"
# Expected: SSE events with data: {...} lines
```

- [ ] **Smoke test batch capture**

```bash
printf '{"user":"What is TDD?","assistant":"TDD is test-driven development."}\n{"user":"Why use it?","assistant":"It reduces bugs."}\n' | \
  ./bin/openclaw-cortex capture-batch --stats
# Expected: stored N memories (N >= 0), no errors
```

- [ ] **Create PRs**

```bash
gh pr create --base main --head feat/user-namespacing --title "feat: multi-user namespace isolation (user_id field)"
gh pr create --base feat/user-namespacing --head feat/streaming-recall --title "feat(api): SSE streaming for /v1/recall"
gh pr create --base feat/user-namespacing --head feat/batch-capture --title "feat(cli): capture-batch JSONL bulk import"
```
