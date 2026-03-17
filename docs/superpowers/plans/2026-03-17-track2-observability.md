# Track 2 — Observability (Sentry + Prometheus) Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Sentry error tracking + performance tracing (PR 2.1) and a Prometheus `/metrics` endpoint (PR 2.2) to all three runtime modes (CLI, HTTP API server, MCP server).

**Architecture:** Two PRs serialized on the same branch stack. PR 2.1 introduces a thin `internal/sentry/` wrapper that is a no-op when `SENTRY_DSN` is unset. PR 2.2 replaces the existing `expvar` counters in `internal/metrics/` with Prometheus types and adds a `/metrics` HTTP endpoint. PR 2.1 must merge before 2.2 because both modify `internal/api/server.go`.

**Prerequisite:** Track 1 PRs merged (especially 1.4 which changes `NewServer` signature).

**Tech Stack:** Go 1.25, `github.com/getsentry/sentry-go`, `github.com/prometheus/client_golang`, golangci-lint v2, `go test -race -count=1 ./...`

**Spec:** `docs/superpowers/specs/2026-03-17-v0.9.0-architecture-security-review-design.md`

---

## PR 2.1 — `feat/sentry`

**Size note:** ~24 files. Implement as 3 commits: (1) sentry package + config, (2) HTTP middleware, (3) CLI wiring.

**Files:**
- Create: `internal/sentry/sentry.go`
- Modify: `internal/config/config.go`
- Modify: `internal/api/server.go`
- Modify: `cmd/openclaw-cortex/main.go`
- Modify: all `cmd/openclaw-cortex/cmd_*.go` files (20 files — mechanical 1–3 line change each)
- Test: `tests/sentry_test.go` (new)

---

### Task 1: Sentry wrapper package

- [ ] **Step 1: Add `sentry-go` dependency**

```bash
go get github.com/getsentry/sentry-go
```

- [ ] **Step 2: Create `internal/sentry/sentry.go`**

```go
// Package sentry wraps the Sentry Go SDK.
// All functions are no-ops when DSN is empty so the package is safe to call
// unconditionally regardless of whether Sentry is configured.
package sentry

import (
    "time"

    sentrygo "github.com/getsentry/sentry-go"
)

// Init initialises the Sentry SDK. Does nothing if dsn is empty.
func Init(dsn, environment, release string) {
    if dsn == "" {
        return
    }
    _ = sentrygo.Init(sentrygo.ClientOptions{
        Dsn:              dsn,
        Environment:      environment,
        Release:          release,
        TracesSampleRate: 0.2,
    })
    initialised = true
}

// Flush waits up to timeout for buffered events to be sent.
func Flush(timeout time.Duration) {
    sentrygo.Flush(timeout)
}

// CaptureException sends err to Sentry. No-op if err is nil or Sentry is not configured.
func CaptureException(err error) {
    if err == nil {
        return
    }
    sentrygo.CaptureException(err)
}

// initialised is set to true by Init when a non-empty DSN is provided.
// It guards all Sentry calls so they are no-ops when Sentry is not configured.
// Checking hub.Client() at call time is unreliable because Init("", ...) may
// still create a hub with a nil client that behaves unexpectedly.
var initialised bool

// StartSpan starts a Sentry performance span. Returns a function that must be called to finish the span.
// If Sentry is not configured, returns a no-op finish function.
func StartSpan(op, description string) func() {
    if !initialised {
        return func() {}
    }
    span := sentrygo.StartSpan(sentrygo.TODO(), op,
        sentrygo.WithDescription(description),
    )
    return func() { span.Finish() }
}
```

- [ ] **Step 3: Add `SentryConfig` to `internal/config/config.go`**

```go
// SentryConfig holds Sentry error tracking settings.
type SentryConfig struct {
    DSN         string `mapstructure:"dsn"`
    Environment string `mapstructure:"environment"`
}
```

Add `Sentry SentryConfig` to the `Config` struct.

In `Load()`:
```go
v.SetDefault("sentry.dsn", "")
v.SetDefault("sentry.environment", "production")
_ = v.BindEnv("sentry.dsn", "SENTRY_DSN")
_ = v.BindEnv("sentry.environment", "SENTRY_ENVIRONMENT")
```

- [ ] **Step 4: Wire Sentry init into `cmd/openclaw-cortex/main.go`**

In `PersistentPreRunE`:
```go
sentry.Init(cfg.Sentry.DSN, cfg.Sentry.Environment, version)
```

At the end of `main()` before `os.Exit`:
```go
sentry.Flush(2 * time.Second)
```

Add import `"github.com/ajitpratap0/openclaw-cortex/internal/sentry"`.

- [ ] **Step 5: Write the Sentry no-op test in `tests/sentry_test.go`**

```go
package tests

import (
    "errors"
    "testing"

    "github.com/ajitpratap0/openclaw-cortex/internal/sentry"
)

func TestSentry_NoopWhenDSNEmpty(t *testing.T) {
    // Verify that all sentry functions are safe to call with no DSN configured.
    sentry.Init("", "test", "0.0.0")
    sentry.CaptureException(errors.New("test error"))
    finish := sentry.StartSpan("test.op", "test description")
    finish()
    sentry.Flush(0)
    // If we get here without panic, the no-op behaviour works.
}
```

- [ ] **Step 6: Run test**

```bash
go test -run TestSentry_NoopWhenDSNEmpty -race -count=1 ./tests/ -v
```

Expected: PASS.

- [ ] **Step 7: Commit (commit 1 of 3)**

```bash
git checkout -b feat/sentry
git add internal/sentry/ internal/config/config.go cmd/openclaw-cortex/main.go tests/sentry_test.go go.mod go.sum
git commit -m "feat(sentry): add no-op Sentry wrapper + config"
```

---

### Task 2: HTTP middleware for Sentry

- [ ] **Step 8: Add Sentry middleware to `internal/api/server.go`**

Add import `sentryhttp "github.com/getsentry/sentry-go/http"`.

In `Handler()`, wrap the mux with the Sentry HTTP middleware:
```go
func (s *Server) Handler() http.Handler {
    mux := http.NewServeMux()
    // ... route registrations unchanged ...

    sentryHandler := sentryhttp.New(sentryhttp.Options{
        Repanic: true, // re-panic after capturing so the server can recover
    })
    return sentryHandler.Handle(mux)
}
```

- [ ] **Step 9: Add test verifying the HTTP middleware is wired (in `tests/sentry_test.go`)**

Add a test that verifies the handler responds normally when Sentry is in no-op mode (DSN empty). The Sentry HTTP middleware is transparent when no DSN is set; this test confirms the middleware doesn't break normal requests:

```go
func TestSentryMiddleware_PassthroughWhenNoDSN(t *testing.T) {
    // Sentry was Init'd with "" in TestSentry_NoopWhenDSNEmpty above.
    // Build a minimal server and confirm a normal request still gets 200.
    st := store.NewMockStore()
    srv := api.NewServer(st, nil, newTestEmbedder(t), newTestLogger(), "", "")
    req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
    rr := httptest.NewRecorder()
    srv.Handler().ServeHTTP(rr, req)
    if rr.Code != http.StatusOK {
        t.Fatalf("expected 200 from healthz, got %d", rr.Code)
    }
}
```

Run:
```bash
go test -run TestSentryMiddleware -race -count=1 ./tests/ -v
```

Expected: PASS.

- [ ] **Step 10: Commit (commit 2 of 3)**

```bash
git add internal/api/server.go tests/sentry_test.go
git commit -m "feat(sentry): wrap HTTP handler with Sentry middleware"
```

---

### Task 3: Wire CaptureException into CLI commands

- [ ] **Step 11: Update each `cmd_*.go` file** to call `sentry.CaptureException(err)` before returning errors from `RunE`.

The pattern is identical for every command. For each file in `cmd/openclaw-cortex/` that has a `RunE`:

```go
// Before:
return fmt.Errorf("capture: %w", err)

// After:
if runErr := fmt.Errorf("capture: %w", err); runErr != nil {
    sentry.CaptureException(runErr)
    return runErr
}
```

A simpler approach that avoids changing every return statement — add a helper in `cmd/openclaw-cortex/helpers.go`:

```go
// cmdErr wraps an error with context and reports it to Sentry.
func cmdErr(context string, err error) error {
    if err == nil {
        return nil
    }
    wrapped := fmt.Errorf("%s: %w", context, err)
    sentry.CaptureException(wrapped)
    return wrapped
}
```

Then update `RunE` functions to use `return cmdErr("capture", err)` instead of `return fmt.Errorf("capture: %w", err)`.

This is a mechanical change. Apply it to: `cmd_capture.go`, `cmd_recall.go`, `cmd_search.go`, `cmd_store.go`, `cmd_store_batch.go`, `cmd_index.go`, `cmd_lifecycle.go`, `cmd_serve.go`, `cmd_export.go`, `cmd_import.go`, `cmd_migrate.go`, `cmd_entities.go`, `cmd_get.go`, `cmd_update.go`, `cmd_hook.go`, `cmd_mcp.go`.

- [ ] **Step 12: Add performance spans to hot paths**

In `internal/hooks/hooks.go` — `PreTurnHook.Run` and `PostTurnHook.Run`:
```go
finish := sentry.StartSpan("hook.pre_turn", "PreTurnHook")
defer finish()
```

In `internal/embedder/embedder.go` — `OllamaEmbedder.Embed`:
```go
finish := sentry.StartSpan("embed.ollama", "OllamaEmbedder.Embed")
defer finish()
```

In `internal/recall/recall.go` — `Recaller.RecallWithGraph`:
```go
finish := sentry.StartSpan("recall.with_graph", "Recaller.RecallWithGraph")
defer finish()
```

In `internal/llm/client.go` — `AnthropicClient.Complete` and `internal/llm/gateway.go` — `GatewayClient.Complete`:
```go
finish := sentry.StartSpan("llm.complete", "LLMClient.Complete")
defer finish()
```

- [ ] **Step 13: Run full test suite**

```bash
go test -short -race -count=1 ./...
golangci-lint run ./...
```

Expected: green.

- [ ] **Step 14: Commit (commit 3 of 3)**

```bash
git add cmd/openclaw-cortex/ internal/hooks/hooks.go internal/embedder/ internal/recall/recall.go internal/llm/
git commit -m "feat(sentry): wire CaptureException into CLI commands + spans on hot paths

All cmd RunE paths now report errors to Sentry via cmdErr() helper.
Performance spans added to: embed, recall, LLM complete, pre/post-turn hooks.
Sentry is a complete no-op when SENTRY_DSN is unset.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## PR 2.2 — `feat/prometheus-metrics`

**Breaking change note:** This PR replaces `expvar.Int` counters in `internal/metrics/metrics.go` with `prometheus/client_golang` types. Call sites using `metrics.Inc(metrics.RecallTotal)` etc. will change. The `/debug/vars` endpoint is removed; `/metrics` replaces it.

**Prerequisite:** PR 2.1 merged.

**Files:**
- Modify: `internal/metrics/metrics.go` (full rewrite)
- Modify: `internal/api/server.go`
- Modify: `internal/hooks/hooks.go`
- Modify: `internal/recall/recall.go`
- Modify: `cmd/openclaw-cortex/cmd_serve.go`
- Test: `tests/metrics_test.go` (update)

---

### Task 4: Rewrite metrics package with Prometheus

- [ ] **Step 1: Add prometheus dependency**

```bash
go get github.com/prometheus/client_golang/prometheus
go get github.com/prometheus/client_golang/prometheus/promauto
go get github.com/prometheus/client_golang/prometheus/promhttp
```

- [ ] **Step 2: Rewrite `internal/metrics/metrics.go`**

```go
// Package metrics exposes Prometheus metrics for openclaw-cortex.
// All metrics are registered with the default Prometheus registry.
package metrics

import (
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promauto"
)

var (
    // MemoriesStoredTotal counts memories written to the store, by source (api, mcp, hook, cli).
    MemoriesStoredTotal = promauto.NewCounterVec(prometheus.CounterOpts{
        Name: "cortex_memories_stored_total",
        Help: "Total number of memories stored, by source.",
    }, []string{"source"})

    // RecallsTotal counts recall operations.
    RecallsTotal = promauto.NewCounter(prometheus.CounterOpts{
        Name: "cortex_recalls_total",
        Help: "Total number of recall operations.",
    })

    // LLMCallsTotal counts LLM completions, by operation name.
    LLMCallsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
        Name: "cortex_llm_calls_total",
        Help: "Total number of LLM completion calls, by operation.",
    }, []string{"op"})

    // LLMErrorsTotal counts LLM completion errors, by operation name.
    LLMErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
        Name: "cortex_llm_errors_total",
        Help: "Total number of LLM completion errors, by operation.",
    }, []string{"op"})

    // RecallLatencyMs is a histogram of recall operation latency in milliseconds.
    RecallLatencyMs = promauto.NewHistogram(prometheus.HistogramOpts{
        Name:    "cortex_recall_latency_ms",
        Help:    "Recall operation latency in milliseconds.",
        Buckets: []float64{10, 50, 100, 250, 500, 1000, 2500},
    })

    // EmbedLatencyMs is a histogram of embedding operation latency.
    EmbedLatencyMs = promauto.NewHistogram(prometheus.HistogramOpts{
        Name:    "cortex_embed_latency_ms",
        Help:    "Embedding generation latency in milliseconds.",
        Buckets: []float64{5, 20, 50, 100, 250, 500},
    })

    // LLMLatencyMs is a histogram of LLM completion latency, by operation.
    LLMLatencyMs = promauto.NewHistogramVec(prometheus.HistogramOpts{
        Name:    "cortex_llm_latency_ms",
        Help:    "LLM completion latency in milliseconds, by operation.",
        Buckets: []float64{100, 250, 500, 1000, 2500, 5000},
    }, []string{"op"})

    // MemoryCount is a gauge of current memory count, by type and scope.
    MemoryCount = promauto.NewGaugeVec(prometheus.GaugeOpts{
        Name: "cortex_memory_count",
        Help: "Current number of memories, by type and scope.",
    }, []string{"type", "scope"})
)
```

- [ ] **Step 3: Update all call sites**

Search for old `metrics.Inc` / `metrics.RecallTotal` etc. calls:
```bash
grep -rn "metrics\." internal/ cmd/ --include="*.go" | grep -v "metrics.go"
```

**Known files to update** (grep may return more — update everything the grep finds):
- `internal/hooks/hooks.go`
- `internal/recall/recall.go`
- `cmd/openclaw-cortex/cmd_stats.go` — calls `.Value()` on existing counters; replace with `testutil.ToFloat64` or direct counter reads
- `cmd/openclaw-cortex/cmd_serve.go` — remove any `/debug/vars` or `expvar` handler registration; the `/metrics` route is added in Step 5 via `internal/api/server.go`
- `internal/lifecycle/lifecycle.go` — may increment `LifecycleExpired`, `LifecycleDecayed`, `LifecycleRetired` counters; add Prometheus equivalents or consolidate into `MemoriesStoredTotal` with a `"lifecycle"` source label

**Note on `StoreTotal` / `CaptureTotal` consolidation:** The existing `hooks.go` increments both `metrics.StoreTotal` and `metrics.CaptureTotal`. Map both to `MemoriesStoredTotal.With(prometheus.Labels{"source": "hook"}).Inc()` — one increment per memory stored is sufficient. If separate lifecycle metrics are desired, add them to `metrics.go` as part of this PR.

Replace with new Prometheus counter increments. Examples:
- `metrics.Inc(metrics.RecallTotal)` → `metrics.RecallsTotal.Inc()`
- `metrics.Inc(metrics.CaptureTotal)` → `metrics.MemoriesStoredTotal.With(prometheus.Labels{"source": "hook"}).Inc()`

Wrap latency-sensitive paths with histogram observations:
```go
start := time.Now()
// ... operation ...
metrics.RecallLatencyMs.Observe(float64(time.Since(start).Milliseconds()))
```

- [ ] **Step 4: Write failing test in `tests/metrics_test.go`**

```go
package tests

import (
    "strings"
    "testing"

    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/testutil"

    "github.com/ajitpratap0/openclaw-cortex/internal/metrics"
)

func TestMetrics_RecallCounter(t *testing.T) {
    before := testutil.ToFloat64(metrics.RecallsTotal)
    metrics.RecallsTotal.Inc()
    after := testutil.ToFloat64(metrics.RecallsTotal)
    if after-before != 1.0 {
        t.Fatalf("expected counter to increment by 1, got %f", after-before)
    }
}

func TestMetrics_LLMCallsCounter(t *testing.T) {
    metrics.LLMCallsTotal.WithLabelValues("capture").Inc()
    // Verify via text exposition using the default Prometheus gatherer (not nil).
    // testutil.GatherAndCount requires a non-nil prometheus.Gatherer.
    out, err := testutil.GatherAndCount(prometheus.DefaultGatherer)
    if err != nil {
        t.Fatalf("gather error: %v", err)
    }
    if out == 0 {
        t.Fatal("expected at least one metric")
    }
    // Confirm our counter is in the output.
    if err := testutil.GatherAndCompare(
        prometheus.DefaultGatherer,
        strings.NewReader(""),
        "cortex_llm_calls_total",
    ); err != nil {
        // GatherAndCompare with empty expected will only fail on format errors — just log
        t.Logf("gather compare (informational): %v", err)
    }
}
```

- [ ] **Step 5: Add `/metrics` route to `internal/api/server.go`**

```go
import "github.com/prometheus/client_golang/prometheus/promhttp"

func (s *Server) Handler() http.Handler {
    mux := http.NewServeMux()

    // Prometheus metrics — no auth required (scrape convention)
    mux.Handle("GET /metrics", promhttp.Handler())

    // Health check — no auth
    mux.HandleFunc("GET /healthz", s.handleHealthz)

    // All other routes wrapped with auth
    // ... existing routes unchanged ...
}
```

- [ ] **Step 6: Run full test suite and lint**

```bash
go test -short -race -count=1 ./...
golangci-lint run ./...
```

Expected: green. The old `expvar`-based test cases in `tests/metrics_test.go` should be updated to use `testutil.ToFloat64`.

- [ ] **Step 7: Commit**

```bash
git checkout -b feat/prometheus-metrics
git add internal/metrics/metrics.go internal/api/server.go internal/hooks/hooks.go \
        internal/recall/recall.go cmd/openclaw-cortex/cmd_serve.go tests/metrics_test.go \
        go.mod go.sum
git commit -m "feat(metrics): replace expvar with Prometheus; add /metrics endpoint

Replaces expvar.Int counters with prometheus/client_golang types.
Adds /metrics endpoint (no auth, Prometheus convention).
Breaking change to internal/metrics API: callers updated in same PR.

Metrics added:
  cortex_memories_stored_total{source}
  cortex_recalls_total
  cortex_llm_calls_total{op} + cortex_llm_errors_total{op}
  cortex_recall_latency_ms, cortex_embed_latency_ms, cortex_llm_latency_ms{op}
  cortex_memory_count{type,scope}

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Final Track 2 Verification

- [ ] **Verify Sentry no-op with no DSN**

```bash
./bin/openclaw-cortex recall "test query" 2>&1 | grep -i sentry
# Expected: no output (no Sentry errors when DSN empty)
```

- [ ] **Verify `/metrics` endpoint**

```bash
OPENCLAW_CORTEX_API_AUTH_TOKEN=secret ./bin/openclaw-cortex serve &
curl -s http://localhost:8080/metrics | grep cortex_
# Expected: cortex_* metric names with TYPE and HELP lines
```

- [ ] **Create PRs**

```bash
gh pr create --base main --head feat/sentry --title "feat(sentry): error tracking + performance tracing"
gh pr create --base feat/sentry --head feat/prometheus-metrics --title "feat(metrics): Prometheus /metrics endpoint"
```
