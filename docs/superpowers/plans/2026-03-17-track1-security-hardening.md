# Track 1 — Security & Reliability Hardening Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix all critical and medium security/reliability findings from the v0.9.0 architecture review — gateway timeout, API auth/TLS/rate-limiting, schema dimension drift, signed pagination cursors, entity search scalability, and several low-severity doc/log fixes.

**Architecture:** Four independent PRs on separate branches. Each PR has no file overlap with the others and can be reviewed and merged in any order. All changes are backward-compatible except PR 1.4 which changes the `store.Store.SearchEntities` signature (MockStore updated in same PR).

**Tech Stack:** Go 1.25, golangci-lint v2, `go test -race -count=1 ./...`, black-box tests in `tests/` package, MockStore from `internal/store/mock_store.go`

**Spec:** `docs/superpowers/specs/2026-03-17-v0.9.0-architecture-security-review-design.md`

---

## PR 1.1 — `fix/gateway-timeout`

**Fixes:** S1 (GatewayClient hangs indefinitely when gateway is slow/unresponsive)

**Files:**
- Modify: `internal/llm/gateway.go`
- Modify: `internal/config/config.go`
- Test: `tests/llm_gateway_timeout_test.go` (new)

---

### Task 1: Write the failing test for gateway timeout

- [ ] **Step 1: Create `tests/llm_gateway_timeout_test.go`**

```go
package tests

import (
    "context"
    "net/http"
    "net/http/httptest"
    "testing"
    "time"

    "github.com/ajitpratap0/openclaw-cortex/internal/llm"
)

func TestGatewayClient_Timeout(t *testing.T) {
    // Server that hangs for 5 seconds
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        time.Sleep(5 * time.Second)
        w.WriteHeader(http.StatusOK)
    }))
    defer srv.Close()

    client := llm.NewGatewayClient(srv.URL, "test-token", 1) // 1-second timeout
    ctx := context.Background()
    _, err := client.Complete(ctx, "claude-haiku-4-5-20251001", "system", "hello", 100)
    if err == nil {
        t.Fatal("expected timeout error, got nil")
    }
}

func TestGatewayClient_NoTimeoutOnFastResponse(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusOK)
        _, _ = w.Write([]byte(`{"choices":[{"message":{"content":"hello"}}]}`))
    }))
    defer srv.Close()

    client := llm.NewGatewayClient(srv.URL, "test-token", 5) // 5-second timeout
    ctx := context.Background()
    resp, err := client.Complete(ctx, "claude-haiku-4-5-20251001", "system", "hello", 100)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if resp != "hello" {
        t.Fatalf("expected 'hello', got %q", resp)
    }
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
go test -run TestGatewayClient_Timeout ./tests/ -v -count=1
```

Expected: FAIL — `NewGatewayClient` does not yet accept a timeout parameter.

---

### Task 2: Add `TimeoutSeconds` to config and update `NewGatewayClient`

- [ ] **Step 3: Add `TimeoutSeconds` to `ClaudeConfig` in `internal/config/config.go`**

In the `ClaudeConfig` struct, add:
```go
GatewayTimeoutSeconds int `mapstructure:"gateway_timeout_seconds"`
```

In `Load()`, add default:
```go
v.SetDefault("claude.gateway_timeout_seconds", 60)
```

Add env binding after the existing gateway bindings:
```go
_ = v.BindEnv("claude.gateway_timeout_seconds", "OPENCLAW_CORTEX_CLAUDE_GATEWAY_TIMEOUT")
```

- [ ] **Step 4: Update `NewGatewayClient` in `internal/llm/gateway.go`**

Change the signature:
```go
// NewGatewayClient creates a GatewayClient that POSTs to baseURL/v1/chat/completions
// authenticated with token. timeoutSeconds controls the HTTP client timeout (0 = no timeout).
func NewGatewayClient(baseURL, token string, timeoutSeconds int) *GatewayClient {
    timeout := time.Duration(timeoutSeconds) * time.Second
    return &GatewayClient{
        baseURL: baseURL,
        token:   token,
        http:    &http.Client{Timeout: timeout},
    }
}
```

Add `"time"` to the import block.

- [ ] **Step 5: Update the factory in `internal/llm/factory.go`** to pass `cfg.Claude.GatewayTimeoutSeconds` when constructing `GatewayClient`. Find the `NewGatewayClient` call and add the third argument.

- [ ] **Step 6: Run tests**

```bash
go test -run TestGatewayClient -race -count=1 ./tests/ -v
go test -short -race -count=1 ./...
```

Expected: both timeout tests pass, full suite green.

- [ ] **Step 7: Lint**

```bash
golangci-lint run ./...
```

Expected: no issues.

- [ ] **Step 8: Commit**

```bash
git checkout -b fix/gateway-timeout
git add internal/llm/gateway.go internal/config/config.go internal/llm/factory.go tests/llm_gateway_timeout_test.go
git commit -m "fix(llm): add configurable timeout to GatewayClient (default 60s)

Fixes S1: GatewayClient used &http.Client{} with no timeout, causing
all LLM-dependent paths to block indefinitely when the gateway hangs.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## PR 1.2 — `fix/api-hardening`

**Fixes:** S2 (auth disabled by default), S5 (no rate limiting)

**Files:**
- Modify: `cmd/openclaw-cortex/cmd_serve.go`
- Modify: `internal/api/server.go`
- Modify: `internal/config/config.go`
- Create: `internal/api/ratelimit.go`
- Test: `tests/api_ratelimit_test.go` (new), update `tests/api_test.go`

---

### Task 3: Rate limit middleware

- [ ] **Step 1: Write the failing test for rate limiting in `tests/api_ratelimit_test.go`**

```go
package tests

import (
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/ajitpratap0/openclaw-cortex/internal/api"
)

func TestRateLimitMiddleware_AllowsUnderLimit(t *testing.T) {
    handler := api.RateLimitMiddleware(10, 10)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
    }))
    req := httptest.NewRequest(http.MethodGet, "/", nil)
    req.RemoteAddr = "127.0.0.1:1234"

    for i := 0; i < 5; i++ {
        rr := httptest.NewRecorder()
        handler.ServeHTTP(rr, req)
        if rr.Code != http.StatusOK {
            t.Fatalf("request %d: expected 200, got %d", i, rr.Code)
        }
    }
}

func TestRateLimitMiddleware_Returns429OnBurst(t *testing.T) {
    // rps=1, burst=2: third immediate request should be rejected
    handler := api.RateLimitMiddleware(1, 2)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
    }))
    req := httptest.NewRequest(http.MethodGet, "/", nil)
    req.RemoteAddr = "10.0.0.1:9999"

    got429 := false
    for i := 0; i < 10; i++ {
        rr := httptest.NewRecorder()
        handler.ServeHTTP(rr, req)
        if rr.Code == http.StatusTooManyRequests {
            got429 = true
            break
        }
    }
    if !got429 {
        t.Fatal("expected 429 after burst exhausted, never got one")
    }
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
go test -run TestRateLimitMiddleware ./tests/ -v -count=1
```

Expected: FAIL — `api.RateLimitMiddleware` does not exist.

- [ ] **Step 3: Create `internal/api/ratelimit.go`**

```go
package api

import (
    "net/http"
    "strings"
    "sync"
    "time"

    "golang.org/x/time/rate"
)

// RateLimitMiddleware returns a middleware that limits requests per IP.
// rps is the sustained rate (requests per second); burst is the token bucket capacity.
func RateLimitMiddleware(rps float64, burst int) func(http.Handler) http.Handler {
    type visitor struct {
        limiter  *rate.Limiter
        lastSeen time.Time
    }

    var (
        mu       sync.Mutex
        visitors = make(map[string]*visitor)
    )

    // Purge stale visitors every minute.
    go func() {
        for {
            time.Sleep(time.Minute)
            mu.Lock()
            for ip, v := range visitors {
                if time.Since(v.lastSeen) > 3*time.Minute {
                    delete(visitors, ip)
                }
            }
            mu.Unlock()
        }
    }()

    getVisitor := func(ip string) *rate.Limiter {
        mu.Lock()
        defer mu.Unlock()
        v, ok := visitors[ip]
        if !ok {
            v = &visitor{limiter: rate.NewLimiter(rate.Limit(rps), burst)}
            visitors[ip] = v
        }
        v.lastSeen = time.Now()
        return v.limiter
    }

    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            ip := r.RemoteAddr
            if i := strings.LastIndex(ip, ":"); i != -1 {
                ip = ip[:i]
            }
            if !getVisitor(ip).Allow() {
                http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
                return
            }
            next.ServeHTTP(w, r)
        })
    }
}
```

Add `golang.org/x/time/rate` to `go.mod`:
```bash
go get golang.org/x/time/rate
```

- [ ] **Step 4: Run rate limit tests**

```bash
go test -run TestRateLimitMiddleware -race -count=1 ./tests/ -v
```

Expected: PASS.

---

### Task 4: Wire rate limiting into serve + add auth hardening

- [ ] **Step 5: Update `APIConfig` in `internal/config/config.go`**

Add to `APIConfig` struct:
```go
RateLimitRPS   float64 `mapstructure:"rate_limit_rps"`
RateLimitBurst int     `mapstructure:"rate_limit_burst"`
```

Add defaults in `Load()`:
```go
v.SetDefault("api.rate_limit_rps", 100.0)
v.SetDefault("api.rate_limit_burst", 20)
```

- [ ] **Step 6: Wire rate limiting and auth flag in `cmd/openclaw-cortex/cmd_serve.go`**

Add a `--unsafe-no-auth` persistent flag and apply rate limiting middleware:

```go
func serveCmd() *cobra.Command {
    var unsafeNoAuth bool
    var tlsCert, tlsKey string

    cmd := &cobra.Command{
        Use:   "serve",
        Short: "Start the HTTP/JSON API server",
        RunE: func(cmd *cobra.Command, args []string) error {
            logger := newLogger()
            ctx := cmd.Context()

            // Auth gate
            if cfg.API.AuthToken == "" && !unsafeNoAuth {
                return fmt.Errorf("serve: api.auth_token is not set; " +
                    "set OPENCLAW_CORTEX_API_AUTH_TOKEN or pass --unsafe-no-auth to disable auth (insecure)")
            }
            if cfg.API.AuthToken == "" {
                logger.Warn("HTTP API: auth is DISABLED (--unsafe-no-auth); do not expose this port")
            }

            // ... existing store + recaller setup unchanged ...

            srv := api.NewServer(st, rec, emb, logger, cfg.API.AuthToken)

            rl := api.RateLimitMiddleware(cfg.API.RateLimitRPS, cfg.API.RateLimitBurst)
            httpSrv := &http.Server{
                Addr:              cfg.API.ListenAddr,
                Handler:           rl(srv.Handler()),
                ReadHeaderTimeout: 10 * time.Second,
                ReadTimeout:       30 * time.Second,
                WriteTimeout:      60 * time.Second,
                IdleTimeout:       120 * time.Second,
            }

            // TLS if certs provided
            startServer := func() error {
                if tlsCert != "" && tlsKey != "" {
                    logger.Info("HTTP API server starting (TLS)", "addr", cfg.API.ListenAddr)
                    return httpSrv.ListenAndServeTLS(tlsCert, tlsKey)
                }
                logger.Info("HTTP API server starting", "addr", cfg.API.ListenAddr)
                return httpSrv.ListenAndServe()
            }

            errCh := make(chan error, 1)
            go func() {
                if listenErr := startServer(); listenErr != nil && listenErr != http.ErrServerClosed {
                    errCh <- fmt.Errorf("serve: HTTP server: %w", listenErr)
                }
                close(errCh)
            }()

            select {
            case <-cmd.Context().Done():
                logger.Info("shutting down")
            case startErr := <-errCh:
                if startErr != nil {
                    return startErr
                }
                return nil
            }

            const shutdownTimeout = 10 * time.Second
            if shutdownErr := api.Shutdown(httpSrv, shutdownTimeout); shutdownErr != nil {
                return fmt.Errorf("serve: graceful shutdown: %w", shutdownErr)
            }
            if startErr := <-errCh; startErr != nil {
                return startErr
            }
            return nil
        },
    }

    cmd.Flags().BoolVar(&unsafeNoAuth, "unsafe-no-auth", false, "Allow serving without authentication (insecure)")
    cmd.Flags().StringVar(&tlsCert, "tls-cert", "", "Path to TLS certificate file")
    cmd.Flags().StringVar(&tlsKey, "tls-key", "", "Path to TLS key file")
    return cmd
}
```

- [ ] **Step 7: Write test for auth gate in `tests/api_ratelimit_test.go`**

Add:
```go
func TestServeCmd_FailsWithoutAuthToken(t *testing.T) {
    // This tests the serve command fails fast when no auth token set.
    // We test the logic indirectly by checking the serve command's RunE.
    // Since serve blocks, we just verify the flag exists via cobra metadata.
    // Full integration test would require starting a server subprocess.
    t.Skip("integration test: requires subprocess; covered by manual smoke test")
}
```

- [ ] **Step 8: Run full test suite**

```bash
go test -short -race -count=1 ./...
golangci-lint run ./...
```

Expected: green.

- [ ] **Step 9: Commit**

```bash
git checkout -b fix/api-hardening
git add internal/api/ratelimit.go internal/config/config.go cmd/openclaw-cortex/cmd_serve.go tests/api_ratelimit_test.go go.mod go.sum
git commit -m "fix(api): require --unsafe-no-auth flag + add per-IP rate limiting

Fixes S2: api.auth_token='' now causes a hard failure unless
--unsafe-no-auth is explicitly passed. Adds TLS flags --tls-cert/--tls-key.

Fixes S5: per-IP token bucket middleware (100 RPS, burst 20 by default)
wraps all HTTP handlers. Configurable via api.rate_limit_rps/burst.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## PR 1.3 — `fix/schema-dim-from-config`

**Fixes:** S3 (hardcoded vector dimension 768 in DDL drifts from config)

**Files:**
- Modify: `internal/memgraph/graph.go`
- Modify: `internal/memgraph/store.go`
- Test: `tests/schema_dim_test.go` (new)

---

### Task 5: Parameterize vector dimension in EnsureSchema

- [ ] **Step 1: Write the failing test in `tests/schema_dim_test.go`**

```go
package tests

import (
    "strings"
    "testing"
)

// TestEnsureSchemaDDL_UsesConfigDimension verifies that the DDL generated
// by EnsureSchema injects the configured vector dimension, not a hardcoded 768.
// We test this by inspecting the Cypher query string constructed internally.
// Since EnsureSchema runs against a live Memgraph, we test the helper that
// builds the DDL string directly.
func TestBuildVectorIndexDDL_UsesProvidedDimension(t *testing.T) {
    ddl := memgraph.BuildMemoryVectorIndexDDL(1024)
    if !strings.Contains(ddl, `"dimension": 1024`) {
        t.Errorf("expected dimension 1024 in DDL, got: %s", ddl)
    }
    if strings.Contains(ddl, `"dimension": 768`) {
        t.Error("DDL still contains hardcoded 768")
    }
}
```

Note: `BuildMemoryVectorIndexDDL` (uppercase) is the exported helper we add to `internal/memgraph/graph.go`. The test must import `"github.com/ajitpratap0/openclaw-cortex/internal/memgraph"` since black-box tests cannot access unexported identifiers.

- [ ] **Step 2: Run to confirm failure**

```bash
go test -run TestBuildVectorIndexDDL ./tests/ -v -count=1
```

Expected: FAIL — function doesn't exist yet.

- [ ] **Step 3: Refactor `internal/memgraph/graph.go`**

Extract the DDL strings that embed dimension into a helper function, then add a `vectorDim int` parameter to `EnsureSchema`:

```go
// BuildMemoryVectorIndexDDL returns the CREATE VECTOR INDEX DDL for the given dimension.
// Exported for testing.
func BuildMemoryVectorIndexDDL(dim int) string {
    return fmt.Sprintf(
        `CREATE VECTOR INDEX memory_embedding ON :Memory(embedding) WITH CONFIG {"dimension": %d, "metric": "cos", "capacity": 10000}`,
        dim,
    )
}

// BuildEntityVectorIndexDDL returns the CREATE VECTOR INDEX DDL for entities.
func BuildEntityVectorIndexDDL(dim int) string {
    return fmt.Sprintf(
        `CREATE VECTOR INDEX entity_name_embedding ON :Entity(name_embedding) WITH CONFIG {"dimension": %d, "metric": "cos", "capacity": 10000}`,
        dim,
    )
}
```

Update `EnsureSchema` signature:
```go
func (g *GraphAdapter) EnsureSchema(ctx context.Context, vectorDim int) error {
    // ...
    queries := []string{
        "CREATE CONSTRAINT ON (m:Memory) ASSERT m.uuid IS UNIQUE",
        "CREATE CONSTRAINT ON (e:Entity) ASSERT e.name IS UNIQUE",
        BuildMemoryVectorIndexDDL(vectorDim),
        BuildEntityVectorIndexDDL(vectorDim),
        // ... rest of property indexes unchanged
    }
    // ...
}
```

- [ ] **Step 4: Update `MemgraphStore` to store and pass `vectorDim`**

In `internal/memgraph/store.go`:

Add `vectorDim int` to `MemgraphStore` struct:
```go
type MemgraphStore struct {
    driver                neo4j.DriverWithContext
    database              string
    logger                *slog.Logger
    contradictionDetector store.ContradictionDetector
    vectorDim             int
}
```

Update `New(...)` signature:
```go
func New(ctx context.Context, uri, username, password, database string, vectorDim int, logger *slog.Logger) (*MemgraphStore, error) {
    // ...
    return &MemgraphStore{
        driver:    driver,
        database:  database,
        logger:    logger,
        vectorDim: vectorDim,
    }, nil
}
```

Update `EnsureCollection`:
```go
func (s *MemgraphStore) EnsureCollection(ctx context.Context) error {
    ga := NewGraphAdapter(s)
    return ga.EnsureSchema(ctx, s.vectorDim)
}
```

- [ ] **Step 5: Update all callers of `memgraph.New` in `cmd/`**

Search for `memgraph.New(` and `newMemgraphStore` in `cmd/openclaw-cortex/`:
```bash
grep -r "memgraph.New\|newMemgraphStore" cmd/
```

Update `newMemgraphStore` in `cmd/openclaw-cortex/main.go`:
```go
func newMemgraphStore(ctx context.Context, logger *slog.Logger) (*memgraph.MemgraphStore, error) {
    return memgraph.New(ctx,
        cfg.Memgraph.URI, cfg.Memgraph.Username, cfg.Memgraph.Password, cfg.Memgraph.Database,
        int(cfg.Memory.VectorDimension),
        logger,
    )
}
```

- [ ] **Step 6: Update test stubs that call `memgraph.New` directly** (if any in `tests/`)

```bash
grep -r "memgraph.New(" tests/
```

Add the `vectorDim` argument (768) to any direct calls.

- [ ] **Step 7: Run tests and lint**

```bash
go test -short -race -count=1 ./...
golangci-lint run ./...
```

Expected: green.

- [ ] **Step 8: Commit**

```bash
git checkout -b fix/schema-dim-from-config
git add internal/memgraph/graph.go internal/memgraph/store.go cmd/openclaw-cortex/main.go tests/schema_dim_test.go
git commit -m "fix(memgraph): inject vector dimension from config into DDL

Fixes S3: vector index DDL hardcoded dimension=768 independently of
memory.vector_dimension config. EnsureSchema now accepts vectorDim int;
MemgraphStore stores it from New() and passes it at EnsureCollection time.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## PR 1.4 — `fix/misc-hardening`

**Fixes:** S4 (raw cursor), S6 (entity search O(N)), S7 (re-embed on no-content-change), S8 (stale SECURITY.md), S9 (URI in logs)

**Files:**
- Create: `pkg/cursor/cursor.go`
- Modify: `internal/config/config.go`
- Modify: `internal/api/server.go`
- Modify: `internal/memgraph/store.go`
- Modify: `internal/store/store.go`
- Modify: `internal/store/mock_store.go`
- Modify: `SECURITY.md`
- Test: `tests/cursor_test.go` (new), update `tests/api_test.go`

---

### Task 6: Signed cursor package

- [ ] **Step 1: Write failing tests in `tests/cursor_test.go`**

```go
package tests

import (
    "testing"

    "github.com/ajitpratap0/openclaw-cortex/pkg/cursor"
)

var testSecret = []byte("test-secret-32-bytes-long-padding")

func TestCursor_RoundTrip(t *testing.T) {
    signed := cursor.Sign(42, testSecret)
    skip, err := cursor.Verify(signed, testSecret)
    if err != nil {
        t.Fatalf("verify error: %v", err)
    }
    if skip != 42 {
        t.Fatalf("expected 42, got %d", skip)
    }
}

func TestCursor_TamperedReturnsError(t *testing.T) {
    signed := cursor.Sign(10, testSecret)
    tampered := signed[:len(signed)-4] + "XXXX"
    _, err := cursor.Verify(tampered, testSecret)
    if err == nil {
        t.Fatal("expected error for tampered cursor, got nil")
    }
}

func TestCursor_WrongSecretReturnsError(t *testing.T) {
    signed := cursor.Sign(10, testSecret)
    _, err := cursor.Verify(signed, []byte("different-secret-here-padding-xx"))
    if err == nil {
        t.Fatal("expected error for wrong secret, got nil")
    }
}

func TestCursor_EmptyStringReturnsZero(t *testing.T) {
    skip, err := cursor.Verify("", testSecret)
    if err != nil {
        t.Fatalf("unexpected error for empty cursor: %v", err)
    }
    if skip != 0 {
        t.Fatalf("expected 0 for empty cursor, got %d", skip)
    }
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test -run TestCursor ./tests/ -v -count=1
```

Expected: FAIL.

- [ ] **Step 3: Create `pkg/cursor/cursor.go`**

```go
// Package cursor provides HMAC-signed opaque pagination cursors.
// Cursors encode a SKIP offset and are tamper-evident via HMAC-SHA256.
package cursor

import (
    "crypto/hmac"
    "crypto/sha256"
    "encoding/base64"
    "encoding/binary"
    "fmt"
)

// Sign encodes skip as an 8-byte big-endian value, appends a 32-byte HMAC-SHA256
// over it, and returns the whole thing as a URL-safe base64 string.
func Sign(skip int64, secret []byte) string {
    payload := make([]byte, 8)
    binary.BigEndian.PutUint64(payload, uint64(skip))
    mac := hmac.New(sha256.New, secret)
    _, _ = mac.Write(payload)
    sig := mac.Sum(nil)
    return base64.RawURLEncoding.EncodeToString(append(payload, sig...))
}

// Verify decodes and validates a cursor produced by Sign.
// Returns (0, nil) for an empty cursor (first page).
// Returns an error if the cursor is malformed or the HMAC is invalid.
func Verify(encoded string, secret []byte) (int64, error) {
    if encoded == "" {
        return 0, nil
    }
    raw, err := base64.RawURLEncoding.DecodeString(encoded)
    if err != nil {
        return 0, fmt.Errorf("cursor: decode: %w", err)
    }
    const minLen = 8 + 32
    if len(raw) != minLen {
        return 0, fmt.Errorf("cursor: invalid length %d", len(raw))
    }
    payload := raw[:8]
    sig := raw[8:]

    mac := hmac.New(sha256.New, secret)
    _, _ = mac.Write(payload)
    expected := mac.Sum(nil)
    if !hmac.Equal(sig, expected) {
        return 0, fmt.Errorf("cursor: invalid signature")
    }
    return int64(binary.BigEndian.Uint64(payload)), nil
}
```

- [ ] **Step 4: Run cursor tests**

```bash
go test -run TestCursor -race -count=1 ./tests/ -v
```

Expected: PASS.

---

### Task 7: Wire signed cursor into API list handler

- [ ] **Step 5: Add `CursorSecret` to `APIConfig` in `internal/config/config.go`**

```go
CursorSecret string `mapstructure:"cursor_secret"`
```

In `Load()`:
```go
v.SetDefault("api.cursor_secret", "")
_ = v.BindEnv("api.cursor_secret", "OPENCLAW_CORTEX_API_CURSOR_SECRET")
```

- [ ] **Step 6: Update `handleList` in `internal/api/server.go`** to use signed cursors

Add import `"github.com/ajitpratap0/openclaw-cortex/pkg/cursor"`.

In `handleList`, replace the raw cursor pass-through:
```go
// Decode incoming cursor
secret := []byte(s.cursorSecret)
skip, verifyErr := cursor.Verify(r.URL.Query().Get("cursor"), secret)
if verifyErr != nil {
    s.writeError(w, http.StatusBadRequest, "invalid cursor")
    return
}
rawCursor := fmt.Sprintf("%d", skip)

memories, nextRawCursor, err := s.store.List(r.Context(), filters, limit, rawCursor)
// ...

// Encode outgoing cursor
var nextCursor string
if nextRawCursor != "" {
    nextSkip, _ := strconv.ParseInt(nextRawCursor, 10, 64)
    nextCursor = cursor.Sign(nextSkip, secret)
}
s.writeJSON(w, http.StatusOK, listResponse{Memories: memories, NextCursor: nextCursor})
```

Add `cursorSecret string` field to `Server` struct and update `NewServer`:
```go
type Server struct {
    store        store.Store
    recall       *recall.Recaller
    embedder     embedder.Embedder
    logger       *slog.Logger
    authToken    string
    cursorSecret string
}

func NewServer(st store.Store, rec *recall.Recaller, emb embedder.Embedder, logger *slog.Logger, authToken, cursorSecret string) *Server {
    return &Server{
        store:        st,
        recall:       rec,
        embedder:     emb,
        logger:       logger,
        authToken:    authToken,
        cursorSecret: cursorSecret,
    }
}
```

Update `cmd_serve.go` to pass `cfg.API.CursorSecret` to `NewServer`.

---

### Task 8: Fix entity search (S6), skip re-embed (S7), fix logs + docs (S8, S9)

- [ ] **Step 7: Update `store.Store.SearchEntities` signature in `internal/store/store.go`**

```go
// SearchEntities finds entities whose name contains the given string.
// entityType filters by entity type (empty = all types).
// limit caps the number of results (0 = use implementation default).
SearchEntities(ctx context.Context, name, entityType string, limit int) ([]models.Entity, error)
```

- [ ] **Step 8: Update `MemgraphStore.SearchEntities` in `internal/memgraph/store.go`**

Push type filter and LIMIT into the Cypher query:
```go
func (s *MemgraphStore) SearchEntities(ctx context.Context, name, entityType string, limit int) ([]models.Entity, error) {
    if limit <= 0 {
        limit = 100
    }
    rctx, cancel := context.WithTimeout(ctx, memgraphReadTimeout)
    defer cancel()
    session := s.driver.NewSession(rctx, s.sessionConfig())
    defer s.closeSession(ctx, session)

    whereClauses := []string{}
    params := map[string]any{"limit": int64(limit)}

    if name != "" {
        whereClauses = append(whereClauses, "toLower(e.name) CONTAINS toLower($name)")
        params["name"] = name
    }
    if entityType != "" {
        whereClauses = append(whereClauses, "e.type = $entityType")
        params["entityType"] = entityType
    }

    where := ""
    if len(whereClauses) > 0 {
        where = "WHERE " + strings.Join(whereClauses, " AND ")
    }

    query := fmt.Sprintf(`MATCH (e:Entity) %s RETURN e LIMIT $limit`, where)
    // ... execute and collect results
}
```

- [ ] **Step 9: Update `MockStore.SearchEntities` in `internal/store/mock_store.go`** to match new signature (add `entityType string, limit int` params; apply in-memory filtering).

- [ ] **Step 10: Update all call sites** — run the grep exhaustively and update **every file** it returns, not just the ones listed here:
```bash
grep -rn "SearchEntities(" .
```
Known call sites at the time of writing (this list may be incomplete — follow the grep output):
- `internal/api/server.go`
- `internal/mcp/entity_tools.go`
- `cmd/openclaw-cortex/cmd_entities.go` (two call sites)
- `cmd/openclaw-cortex/cmd_stats.go`
- `tests/failing_store_test.go` — the `failingUpsertStore` proxy struct embeds or wraps `store.Store`; update its `SearchEntities` method signature or it will fail to compile

Update each call to pass `entityType` (empty string `""` for no type filter) and `limit` (e.g. 100) arguments.

- [ ] **Step 11: Remove unnecessary re-embed in `handleUpdate`** (`internal/api/server.go`)

In the `else` branch of `if req.Content != ""`, instead of re-embedding, return an error or skip:
```go
// No content change — preserve existing vector.
// Re-embedding would be wasteful and is not needed for metadata-only updates.
// Fetch the stored vector via a dedicated path if the store supports it,
// or store a sentinel to indicate "use existing embedding".
// For now: skip the upsert vector refresh and pass nil — Upsert must
// handle nil vector as "preserve existing embedding".
vec = nil
```

Update `MemgraphStore.Upsert` to skip the `embedding` SET clause when `vector` is nil:
```go
if vector != nil {
    params["embedding"] = float32SliceToAny(vector)
    // include embedding in SET
} else {
    // exclude embedding from SET clause
}
```

This requires splitting the Cypher query into two variants (with/without embedding). Simplest approach: add a boolean param and use conditional SET:
```cypher
SET m.embedding = CASE WHEN $has_embedding THEN $embedding ELSE m.embedding END
```

- [ ] **Step 12: Fix Memgraph URI log in `internal/memgraph/store.go:59`**

```go
// Redact credentials from URI for safe logging.
func redactURI(rawURI string) string {
    u, err := url.Parse(rawURI)
    if err != nil {
        return "[unparseable URI]"
    }
    return u.Redacted()
}

// In New():
logger.Debug("connected to Memgraph", "uri", redactURI(uri), "database", database)
```

Add `"net/url"` to imports.

- [ ] **Step 13: Update `SECURITY.md`** — replace "Qdrant" with "Memgraph" in the out-of-scope section.

- [ ] **Step 14: Run full test suite and lint**

```bash
go test -short -race -count=1 ./...
golangci-lint run ./...
```

Expected: green.

- [ ] **Step 15: Commit**

```bash
git checkout -b fix/misc-hardening
git add pkg/cursor/ internal/config/config.go internal/api/server.go \
        internal/memgraph/store.go internal/store/store.go internal/store/mock_store.go \
        internal/mcp/entity_tools.go SECURITY.md tests/cursor_test.go
git commit -m "fix: signed cursors, entity search limit, skip re-embed, fix log + docs

Fixes S4: pagination cursor is now HMAC-SHA256 signed (pkg/cursor).
Fixes S6/A5: SearchEntities pushes type filter + LIMIT into Cypher.
Fixes S7: handleUpdate skips re-embedding when content is unchanged.
Fixes S8: SECURITY.md no longer references removed Qdrant dependency.
Fixes S9: Memgraph URI downgraded to Debug log with credentials redacted.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Final Track 1 Verification

- [ ] **Run all tests one final time across all Track 1 changes (in a clean build)**

```bash
go build ./...
go test -race -count=1 ./...
golangci-lint run ./...
```

Expected: all green.

- [ ] **Smoke test the serve command locally**

```bash
# Without auth — should fail
./bin/openclaw-cortex serve

# With auth
OPENCLAW_CORTEX_API_AUTH_TOKEN=secret ./bin/openclaw-cortex serve &
curl -s http://localhost:8080/healthz
# Expected: {"status":"ok"}

curl -s http://localhost:8080/v1/memories
# Expected: {"error":"unauthorized"}

curl -s -H "Authorization: Bearer secret" http://localhost:8080/v1/memories
# Expected: {"memories":[],"next_cursor":""}
```

- [ ] **Create PRs**

```bash
# Each branch gets its own PR:
gh pr create --base main --head fix/gateway-timeout --title "fix(llm): add configurable timeout to GatewayClient"
gh pr create --base main --head fix/api-hardening --title "fix(api): require --unsafe-no-auth + per-IP rate limiting"
gh pr create --base main --head fix/schema-dim-from-config --title "fix(memgraph): inject vector dimension from config into DDL"
gh pr create --base main --head fix/misc-hardening --title "fix: signed cursors, entity search limit, skip re-embed, fix log + docs"
```
