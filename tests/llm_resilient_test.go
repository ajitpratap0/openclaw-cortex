package tests

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ajitpratap0/openclaw-cortex/internal/llm"
)

// resilientMockLLMClient is a controllable LLMClient for resilience tests.
type resilientMockLLMClient struct {
	mu        sync.Mutex
	calls     []resilientMockCall // responses in order; last entry repeats if exhausted
	callsMade atomic.Int64
}

type resilientMockCall struct {
	result string
	err    error
}

func newMockLLM(calls ...resilientMockCall) *resilientMockLLMClient {
	return &resilientMockLLMClient{calls: calls}
}

func (m *resilientMockLLMClient) Complete(_ context.Context, _, _, _ string, _ int) (string, error) {
	// Compute idx atomically before acquiring the mutex to avoid the
	// Add/Load race: capture the post-increment value in one operation.
	n := int(m.callsMade.Add(1)) - 1
	m.mu.Lock()
	defer m.mu.Unlock()
	idx := n
	if idx >= len(m.calls) {
		idx = len(m.calls) - 1
	}
	return m.calls[idx].result, m.calls[idx].err
}

// httpErr is a convenience constructor for *llm.HTTPError.
func httpErr(status int) error {
	return &llm.HTTPError{StatusCode: status, Body: "test body"}
}

// ---- Tests ----

// TestResilientClient_RetriesOnRateLimit verifies that a 429 response is retried
// and that on the third attempt (after two 429s) the success is returned.
func TestResilientClient_RetriesOnRateLimit(t *testing.T) {
	mock := newMockLLM(
		resilientMockCall{err: httpErr(429)},
		resilientMockCall{err: httpErr(429)},
		resilientMockCall{result: "ok", err: nil},
	)
	client := llm.NewResilientClient(mock,
		/*maxConcurrent=*/ 4,
		/*cbFailureThreshold=*/ 5,
		/*cbRecoverySeconds=*/ 30,
		/*maxRetries=*/ 3,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := client.Complete(ctx, "model", "sys", "user", 100)
	if err != nil {
		t.Fatalf("expected success after retries, got error: %v", err)
	}
	if result != "ok" {
		t.Fatalf("expected result 'ok', got %q", result)
	}
	if got := mock.callsMade.Load(); got != 3 {
		t.Fatalf("expected 3 calls total, got %d", got)
	}
}

// TestResilientClient_FastFailsOn401 verifies that a 401 error is NOT retried.
func TestResilientClient_FastFailsOn401(t *testing.T) {
	mock := newMockLLM(
		resilientMockCall{err: httpErr(401)},
	)
	client := llm.NewResilientClient(mock, 4, 5, 30, 3)

	ctx := context.Background()
	_, err := client.Complete(ctx, "model", "sys", "user", 100)
	if err == nil {
		t.Fatal("expected error for 401, got nil")
	}
	var httpError *llm.HTTPError
	if !errors.As(err, &httpError) || httpError.StatusCode != 401 {
		t.Fatalf("expected HTTPError with status 401, got %v", err)
	}
	if got := mock.callsMade.Load(); got != 1 {
		t.Fatalf("expected exactly 1 call (no retries on 401), got %d", got)
	}
}

// TestResilientClient_FastFailsOn400 verifies that a 400 error is NOT retried.
func TestResilientClient_FastFailsOn400(t *testing.T) {
	mock := newMockLLM(resilientMockCall{err: httpErr(400)})
	client := llm.NewResilientClient(mock, 4, 5, 30, 3)

	_, err := client.Complete(context.Background(), "model", "sys", "user", 100)
	if err == nil {
		t.Fatal("expected error for 400, got nil")
	}
	if got := mock.callsMade.Load(); got != 1 {
		t.Fatalf("expected exactly 1 call (no retries on 400), got %d", got)
	}
}

// TestResilientClient_FastFailsOn403 verifies that a 403 error is NOT retried.
func TestResilientClient_FastFailsOn403(t *testing.T) {
	mock := newMockLLM(resilientMockCall{err: httpErr(403)})
	client := llm.NewResilientClient(mock, 4, 5, 30, 3)

	_, err := client.Complete(context.Background(), "model", "sys", "user", 100)
	if err == nil {
		t.Fatal("expected error for 403, got nil")
	}
	if got := mock.callsMade.Load(); got != 1 {
		t.Fatalf("expected exactly 1 call (no retries on 403), got %d", got)
	}
}

// TestResilientClient_CircuitOpensAfterThreshold verifies that after
// cbFailureThreshold consecutive failures the circuit opens and subsequent
// calls return ErrCircuitOpen without hitting the inner client.
func TestResilientClient_CircuitOpensAfterThreshold(t *testing.T) {
	alwaysFail := newMockLLM(resilientMockCall{err: errors.New("server error")})
	// Repeat last entry indefinitely — already the default behavior of resilientMockLLMClient.

	const threshold = 5
	client := llm.NewResilientClient(alwaysFail,
		/*maxConcurrent=*/ 4,
		/*cbFailureThreshold=*/ threshold,
		/*cbRecoverySeconds=*/ 60,
		/*maxRetries=*/ 0, // no retries so each call counts as one failure
	)

	ctx := context.Background()

	// Drive threshold failures into the circuit breaker.
	for i := 0; i < threshold; i++ {
		_, err := client.Complete(ctx, "model", "sys", "user", 100)
		if err == nil {
			t.Fatalf("call %d: expected error, got nil", i+1)
		}
		// Must NOT be ErrCircuitOpen yet (circuit still closed during ramp-up).
		if errors.Is(err, llm.ErrCircuitOpen) {
			t.Fatalf("call %d: circuit opened too early", i+1)
		}
	}

	// Next call must return ErrCircuitOpen.
	_, err := client.Complete(ctx, "model", "sys", "user", 100)
	if !errors.Is(err, llm.ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen after %d failures, got %v", threshold, err)
	}
}

// TestResilientClient_WorkerPoolLimitsParallelism verifies that no more than
// maxConcurrent calls execute simultaneously.
func TestResilientClient_WorkerPoolLimitsParallelism(t *testing.T) {
	const (
		poolSize   = 2
		goroutines = 10
	)

	var (
		active    atomic.Int64
		maxActive atomic.Int64
	)

	// slow inner client that tracks concurrent executions
	slow := &slowMockLLM{
		onStart: func() {
			cur := active.Add(1)
			for {
				prev := maxActive.Load()
				if cur <= prev || maxActive.CompareAndSwap(prev, cur) {
					break
				}
			}
		},
		onEnd: func() { active.Add(-1) },
		delay: 50 * time.Millisecond,
	}

	client := llm.NewResilientClient(slow, poolSize, 100, 30, 0)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			_, _ = client.Complete(ctx, "model", "sys", "user", 100)
		}()
	}
	wg.Wait()

	if got := maxActive.Load(); got > int64(poolSize) {
		t.Fatalf("max concurrent calls = %d, want <= %d", got, poolSize)
	}
}

// slowMockLLM is a mock that sleeps and calls lifecycle hooks.
type slowMockLLM struct {
	onStart func()
	onEnd   func()
	delay   time.Duration
}

func (s *slowMockLLM) Complete(ctx context.Context, _, _, _ string, _ int) (string, error) {
	s.onStart()
	defer s.onEnd()
	select {
	case <-time.After(s.delay):
		return "ok", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// TestResilientClient_ContextCancelledReleasesPool verifies that a call blocked
// waiting for a pool slot returns immediately when its context is canceled.
func TestResilientClient_ContextCancelledReleasesPool(t *testing.T) {
	const poolSize = 1

	// Blocker occupies the single pool slot indefinitely.
	blocker := make(chan struct{})
	blocking := &blockingMockLLM{gate: blocker}

	client := llm.NewResilientClient(blocking, poolSize, 100, 30, 0)

	// Fill the pool slot.
	go func() {
		_, _ = client.Complete(context.Background(), "model", "sys", "fill", 100)
	}()
	// Give the goroutine time to acquire the slot.
	time.Sleep(20 * time.Millisecond)

	// Try to acquire the slot with an already-canceled context.
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	start := time.Now()
	_, err := client.Complete(canceledCtx, "model", "sys", "blocked", 100)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error for canceled context, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if elapsed > 200*time.Millisecond {
		t.Fatalf("cancellation took too long: %v (want < 200ms)", elapsed)
	}

	// Unblock the occupying goroutine.
	close(blocker)
}

// blockingMockLLM blocks until its gate channel is closed.
type blockingMockLLM struct {
	gate <-chan struct{}
}

func (b *blockingMockLLM) Complete(ctx context.Context, _, _, _ string, _ int) (string, error) {
	select {
	case <-b.gate:
		return "ok", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}
