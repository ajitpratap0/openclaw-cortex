package async

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// walPath returns a unique WAL path inside t.TempDir().
func walPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "test.wal")
}

// TestQueue_EnqueueAndDrain verifies that enqueued items arrive on C().
func TestQueue_EnqueueAndDrain(t *testing.T) {
	t.Parallel()
	q, err := NewQueue(walPath(t), 16, 0)
	if err != nil {
		t.Fatalf("NewQueue: %v", err)
	}

	items := []WorkItem{
		{MemoryID: "m1", Content: "alpha"},
		{MemoryID: "m2", Content: "beta"},
		{MemoryID: "m3", Content: "gamma"},
	}
	for i := range items {
		if enqErr := q.Enqueue(items[i]); enqErr != nil {
			t.Fatalf("Enqueue[%d]: %v", i, enqErr)
		}
	}

	received := map[string]bool{}
	for range items {
		select {
		case item := <-q.C():
			received[item.Content] = true
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for item from C()")
		}
	}

	for _, item := range items {
		if !received[item.Content] {
			t.Errorf("item %q not received", item.Content)
		}
	}
}

// TestQueue_WALPersistence verifies that items survive a process restart (queue
// close + reopen from same WAL path).
func TestQueue_WALPersistence(t *testing.T) {
	t.Parallel()
	path := walPath(t)

	q, err := NewQueue(path, 16, 0)
	if err != nil {
		t.Fatalf("NewQueue: %v", err)
	}

	items := []WorkItem{
		{MemoryID: "p1", Content: "persist-a"},
		{MemoryID: "p2", Content: "persist-b"},
	}
	for i := range items {
		if enqErr := q.Enqueue(items[i]); enqErr != nil {
			t.Fatalf("Enqueue: %v", enqErr)
		}
	}
	// Drain channel to simulate items processed from channel before restart.
	// We intentionally do NOT call Complete so the WAL still shows pending.
	for range items {
		select {
		case <-q.C():
		case <-time.After(2 * time.Second):
			t.Fatal("timeout draining channel before restart")
		}
	}

	// Reopen from same WAL path; items should replay.
	q2, err := NewQueue(path, 16, 0)
	if err != nil {
		t.Fatalf("NewQueue (reopen): %v", err)
	}

	replayed := map[string]bool{}
	for range items {
		select {
		case item := <-q2.C():
			replayed[item.Content] = true
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for replayed item")
		}
	}
	for _, item := range items {
		if !replayed[item.Content] {
			t.Errorf("item %q not replayed after reopen", item.Content)
		}
	}
}

// TestQueue_ChannelFullDropsToWAL creates a queue with capacity=1, enqueues 3
// items, and asserts only 1 is in the channel while all 3 are durably in the WAL.
func TestQueue_ChannelFullDropsToWAL(t *testing.T) {
	t.Parallel()
	path := walPath(t)

	q, err := NewQueue(path, 1, 0)
	if err != nil {
		t.Fatalf("NewQueue: %v", err)
	}

	for i := range 3 {
		item := WorkItem{MemoryID: "drop-test", Content: "item"}
		_ = i
		if enqErr := q.Enqueue(item); enqErr != nil {
			t.Fatalf("Enqueue: %v", enqErr)
		}
	}

	// Channel was created with capacity 1 and the first item from replay already
	// fills it; subsequent sends go to WAL only.
	if got := len(q.C()); got > 1 {
		t.Errorf("expected channel depth <= 1, got %d", got)
	}

	// All 3 must be in the WAL as pending.
	status := q.Status()
	if status.TotalPending < 3 {
		t.Errorf("expected >= 3 pending in WAL, got %d", status.TotalPending)
	}
}

// TestQueue_Compact enqueues 5 items, completes 3, compacts, reopens the queue,
// and asserts only 2 items replay.
func TestQueue_Compact(t *testing.T) {
	t.Parallel()
	path := walPath(t)

	q, err := NewQueue(path, 16, 0)
	if err != nil {
		t.Fatalf("NewQueue: %v", err)
	}

	ids := make([]string, 5)
	for i := range ids {
		item := WorkItem{MemoryID: "compact-test", Content: "item"}
		if enqErr := q.Enqueue(item); enqErr != nil {
			t.Fatalf("Enqueue[%d]: %v", i, enqErr)
		}
		// Receive so we know the ID assigned.
		received := <-q.C()
		ids[i] = received.ID
	}

	// Complete first 3.
	for i := range 3 {
		q.Complete(ids[i])
	}

	if compactErr := q.Compact(); compactErr != nil {
		t.Fatalf("Compact: %v", compactErr)
	}

	// Reopen — only 2 pending items should replay.
	q2, err := NewQueue(path, 16, 0)
	if err != nil {
		t.Fatalf("NewQueue (after compact): %v", err)
	}

	count := 0
	for {
		select {
		case <-q2.C():
			count++
		default:
			goto done
		}
	}
done:
	if count != 2 {
		t.Errorf("expected 2 replayed items after compact, got %d", count)
	}
}

// TestQueue_Status asserts that Status() returns correct field values.
func TestQueue_Status(t *testing.T) {
	t.Parallel()
	path := walPath(t)

	q, err := NewQueue(path, 16, 0)
	if err != nil {
		t.Fatalf("NewQueue: %v", err)
	}

	// Enqueue 4 items.
	ids := make([]string, 4)
	for i := range ids {
		item := WorkItem{MemoryID: "status-test", Content: "item"}
		if enqErr := q.Enqueue(item); enqErr != nil {
			t.Fatalf("Enqueue: %v", enqErr)
		}
		received := <-q.C()
		ids[i] = received.ID
	}

	// Complete 1, fail 1 — 2 remain pending.
	q.Complete(ids[0])
	q.Fail(ids[1], errors.New("boom"))

	status := q.Status()
	if status.TotalPending != 2 {
		t.Errorf("TotalPending: want 2, got %d", status.TotalPending)
	}
	if status.TotalFailed != 1 {
		t.Errorf("TotalFailed: want 1, got %d", status.TotalFailed)
	}
	// Channel was drained above so depth should be 0.
	if status.ChannelDepth != 0 {
		t.Errorf("ChannelDepth: want 0, got %d", status.ChannelDepth)
	}
}
