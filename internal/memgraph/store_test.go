package memgraph_test

// NOTE: These tests require a live Memgraph instance and are skipped in unit
// CI runs that don't provide one. Run them manually or in integration CI.
//
// To run: MEMGRAPH_URI=bolt://localhost:7687 go test ./internal/memgraph/...

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/openclaw/cortex/internal/memgraph"
	"github.com/openclaw/cortex/internal/models"
	"github.com/openclaw/cortex/internal/store"
)

func boltURI(t *testing.T) string {
	t.Helper()
	uri := os.Getenv("MEMGRAPH_URI")
	if uri == "" {
		t.Skip("MEMGRAPH_URI not set — skipping integration test")
	}
	return uri
}

func newTestStore(t *testing.T) *memgraph.MemgraphStore {
	t.Helper()
	s, err := memgraph.New(boltURI(t), "", "")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// seedMemory creates a memory and returns its UUID.
func seedMemory(t *testing.T, s *memgraph.MemgraphStore, content string) string {
	t.Helper()
	mem := &models.Memory{
		Content: content,
		Type:    "fact",
		Scope:   "session",
	}
	id, err := s.Create(context.Background(), mem)
	if err != nil {
		t.Fatalf("seed memory: %v", err)
	}
	return id
}

// TestDelete_ExactUUID verifies the baseline exact-match behaviour is unchanged.
func TestDelete_ExactUUID(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id := seedMemory(t, s, "exact-delete test")
	if err := s.Delete(ctx, id); err != nil {
		t.Fatalf("Delete(exact): %v", err)
	}

	// Second delete should return ErrNotFound.
	err := s.Delete(ctx, id)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound on second delete, got: %v", err)
	}
}

// TestDelete_PrefixMatch verifies that a short prefix resolves to the right memory.
func TestDelete_PrefixMatch(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id := seedMemory(t, s, "prefix-delete test")
	// Use first 8 chars as a short prefix.
	prefix := id[:8]

	if err := s.Delete(ctx, prefix); err != nil {
		t.Fatalf("Delete(prefix %q): %v", prefix, err)
	}

	// Verify the memory is gone via exact UUID lookup.
	_, err := s.Get(ctx, id)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after prefix delete, got: %v", err)
	}
}

// TestDelete_AmbiguousPrefix verifies that a prefix matching >1 memory returns an error.
func TestDelete_AmbiguousPrefix(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Seed two memories — their UUIDs will share the empty prefix "".
	// Use a very short prefix that is guaranteed to match both (empty string
	// would match everything, but len("") == 0 < 36 triggers the prefix path).
	// We can't force a shared prefix on real UUIDs, so instead we seed two
	// memories and use a prefix of "" (length 0) which starts-with matches all.
	_ = seedMemory(t, s, "ambiguous A")
	_ = seedMemory(t, s, "ambiguous B")

	err := s.Delete(ctx, "") // prefix="" matches every memory
	if err == nil {
		t.Fatal("expected ambiguous prefix error, got nil")
	}
	// Should not be ErrNotFound; should mention "ambiguous".
	if errors.Is(err, store.ErrNotFound) {
		t.Fatalf("got ErrNotFound instead of ambiguous error: %v", err)
	}
	t.Logf("got expected error: %v", err)
}

// TestDelete_NotFound verifies ErrNotFound is returned for a non-existent full UUID.
func TestDelete_NotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	err := s.Delete(ctx, "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing UUID, got: %v", err)
	}
}
