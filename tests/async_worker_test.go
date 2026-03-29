package tests

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ajitpratap0/openclaw-cortex/internal/async"
)

// ----------------------------------------------------------------------------
// asyncNoopProcessor — always succeeds.
// ----------------------------------------------------------------------------

type asyncNoopProcessor struct{}

func (asyncNoopProcessor) Process(_ context.Context, _ async.WorkItem) error { return nil }

// ----------------------------------------------------------------------------
// asyncCountingProcessor — succeeds after failN calls then always succeeds.
// ----------------------------------------------------------------------------

type asyncCountingProcessor struct {
	callCount atomic.Int64
	failN     int64 // fail the first failN calls
	err       error // error to return while failing
}

func (p *asyncCountingProcessor) Process(_ context.Context, _ async.WorkItem) error {
	n := p.callCount.Add(1)
	if n <= p.failN {
		return p.err
	}
	return nil
}

// ----------------------------------------------------------------------------
// asyncAlwaysFailProcessor — never succeeds.
// ----------------------------------------------------------------------------

type asyncAlwaysFailProcessor struct {
	err error
}

func (p *asyncAlwaysFailProcessor) Process(_ context.Context, _ async.WorkItem) error {
	return p.err
}

// ----------------------------------------------------------------------------
// helpers
// ----------------------------------------------------------------------------

// asyncNewTestPool creates a Queue + Pool backed by a temp WAL.
func asyncNewTestPool(t *testing.T, proc async.Processor, workers, maxRetries int) (*async.Queue, *async.Pool) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.wal")
	q, err := async.NewQueue(path, 64, 0)
	if err != nil {
		t.Fatalf("NewQueue: %v", err)
	}
	pool := async.NewPool(q, proc, workers, maxRetries, 0, nil)
	return q, pool
}

// asyncWaitFor polls pred every 5 ms until it returns true or the deadline elapses.
func asyncWaitFor(t *testing.T, timeout time.Duration, pred func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if pred() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}

// ----------------------------------------------------------------------------
// TestPool_ProcessesItems
// ----------------------------------------------------------------------------

// TestPool_ProcessesItems verifies that a Pool with an asyncNoopProcessor drains all
// enqueued items and marks them as WALStateDone.
func TestPool_ProcessesItems(t *testing.T) {
	t.Parallel()

	q, pool := asyncNewTestPool(t, asyncNoopProcessor{}, 2, 1)

	items := []async.WorkItem{
		{MemoryID: "m1", Content: "alpha"},
		{MemoryID: "m2", Content: "beta"},
		{MemoryID: "m3", Content: "gamma"},
	}
	for i := range items {
		if err := q.Enqueue(items[i]); err != nil {
			t.Fatalf("Enqueue[%d]: %v", i, err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool.Start(ctx)

	// Wait until the queue has no pending items left.
	asyncWaitFor(t, 3*time.Second, func() bool {
		s := q.Status()
		return s.TotalPending == 0 && s.ChannelDepth == 0
	})

	if err := pool.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	// All items should be done, none failed.
	status := q.Status()
	if status.TotalFailed != 0 {
		t.Errorf("expected 0 failed, got %d", status.TotalFailed)
	}
}

// ----------------------------------------------------------------------------
// TestPool_RetriesOnError
// ----------------------------------------------------------------------------

// TestPool_RetriesOnError verifies that when a processor fails on the first
// attempt and succeeds on subsequent ones, the item is eventually completed
// (not failed).
func TestPool_RetriesOnError(t *testing.T) {
	t.Parallel()

	proc := &asyncCountingProcessor{
		failN: 2,
		err:   errors.New("transient error"),
	}

	// maxRetries=5 so the item gets enough attempts to succeed on the 3rd call.
	q, pool := asyncNewTestPool(t, proc, 1, 5)

	if err := q.Enqueue(async.WorkItem{MemoryID: "retry-mem", Content: "retry-content"}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool.Start(ctx)

	// Wait until the pending count reaches zero (item completed) or failed.
	asyncWaitFor(t, 8*time.Second, func() bool {
		s := q.Status()
		return s.TotalPending == 0 && s.ChannelDepth == 0
	})

	if err := pool.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	// The item should have completed, not failed.
	status := q.Status()
	if status.TotalFailed != 0 {
		t.Errorf("expected 0 failed after eventual success, got %d", status.TotalFailed)
	}

	// The processor should have been called at least 3 times.
	if got := proc.callCount.Load(); got < 3 {
		t.Errorf("expected >= 3 processor calls, got %d", got)
	}
}

// ----------------------------------------------------------------------------
// TestPool_MaxRetriesExceeded
// ----------------------------------------------------------------------------

// TestPool_MaxRetriesExceeded verifies that an item processed by an always-
// failing processor is permanently marked failed after maxRetries attempts.
func TestPool_MaxRetriesExceeded(t *testing.T) {
	t.Parallel()

	proc := &asyncAlwaysFailProcessor{err: errors.New("permanent error")}

	const maxRetries = 3
	q, pool := asyncNewTestPool(t, proc, 1, maxRetries)

	if err := q.Enqueue(async.WorkItem{MemoryID: "fail-mem", Content: "fail-content"}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool.Start(ctx)

	// Wait until the item is no longer pending (it should be failed).
	asyncWaitFor(t, 8*time.Second, func() bool {
		s := q.Status()
		return s.TotalPending == 0 && s.ChannelDepth == 0
	})

	if err := pool.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	// The item must be permanently failed.
	status := q.Status()
	if status.TotalFailed != 1 {
		t.Errorf("expected 1 failed item, got %d", status.TotalFailed)
	}
}

// ----------------------------------------------------------------------------
// TestPool_Shutdown_DrainsInFlight
// ----------------------------------------------------------------------------

// TestPool_Shutdown_DrainsInFlight verifies that a graceful shutdown returns
// once in-flight goroutines finish and does not leak goroutines.
func TestPool_Shutdown_DrainsInFlight(t *testing.T) {
	t.Parallel()

	q, pool := asyncNewTestPool(t, asyncNoopProcessor{}, 4, 1)

	// Enqueue several items before starting the pool.
	for range 10 {
		if err := q.Enqueue(async.WorkItem{MemoryID: "drain-mem", Content: "drain"}); err != nil {
			t.Fatalf("Enqueue: %v", err)
		}
	}

	startCtx := context.Background()
	pool.Start(startCtx)

	// Give workers a moment to start picking up items.
	time.Sleep(20 * time.Millisecond)

	// Shutdown with a generous timeout — all goroutines should exit cleanly.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := pool.Shutdown(shutdownCtx); err != nil {
		t.Errorf("Shutdown returned unexpected error: %v", err)
	}
}

// TestPool_Shutdown_TimesOut verifies that Shutdown returns the context error
// when the provided context expires before all goroutines exit.
func TestPool_Shutdown_TimesOut(t *testing.T) {
	t.Parallel()

	// asyncBlockingProcessor blocks indefinitely until context is canceled.
	proc := asyncProcessorFunc(func(ctx context.Context, _ async.WorkItem) error {
		<-ctx.Done()
		return ctx.Err()
	})

	q, pool := asyncNewTestPool(t, proc, 1, 1)

	if err := q.Enqueue(async.WorkItem{MemoryID: "block-mem", Content: "block"}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// Start pool with a long-lived context so workers don't exit on their own.
	workerCtx, workerCancel := context.WithCancel(context.Background())
	defer workerCancel()
	pool.Start(workerCtx)

	// Give the worker time to pick up the item and block in Process.
	time.Sleep(30 * time.Millisecond)

	// Shutdown with a very short timeout — should time out.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err := pool.Shutdown(shutdownCtx)
	if err == nil {
		t.Fatal("expected Shutdown to return a timeout error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected DeadlineExceeded, got: %v", err)
	}

	// Clean up: cancel the worker context so goroutines exit (prevents goroutine leak).
	workerCancel()
}

// asyncProcessorFunc is an adapter that allows a plain function to satisfy async.Processor.
type asyncProcessorFunc func(ctx context.Context, item async.WorkItem) error

func (f asyncProcessorFunc) Process(ctx context.Context, item async.WorkItem) error {
	return f(ctx, item)
}
