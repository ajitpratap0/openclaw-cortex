//go:build integration

package tests

// NOTE: These tests require a live Memgraph instance and are skipped in unit
// CI runs that don't provide one. Run them manually or in integration CI.
//
// To run: go test -tags integration -run TestDelete ./tests/...

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/ajitpratap0/openclaw-cortex/internal/embedder"
	"github.com/ajitpratap0/openclaw-cortex/internal/memgraph"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

// seedDeleteMemory creates a memory via Upsert and returns its UUID.
func seedDeleteMemory(t *testing.T, s *memgraph.MemgraphStore, emb embedder.Embedder, content string) string {
	t.Helper()
	mem := newMemory(models.MemoryTypeFact, content)
	vec := mustEmbed(t, emb, content)
	if err := s.Upsert(context.Background(), mem, vec); err != nil {
		t.Fatalf("seed memory: %v", err)
	}
	return mem.ID
}

// TestDelete_ExactUUID verifies the baseline exact-match behaviour is unchanged.
func TestDelete_ExactUUID(t *testing.T) {
	s := newIntegrationMemgraph(t)
	emb := newTestEmbedder(t)
	ctx := context.Background()

	id := seedDeleteMemory(t, s, emb, "exact-delete test")
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
	s := newIntegrationMemgraph(t)
	emb := newTestEmbedder(t)
	ctx := context.Background()

	id := seedDeleteMemory(t, s, emb, "prefix-delete test")
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
	s := newIntegrationMemgraph(t)
	emb := newTestEmbedder(t)
	ctx := context.Background()

	// Seed two memories and use an empty prefix (starts-with "" matches all).
	seedDeleteMemory(t, s, emb, "ambiguous A")
	seedDeleteMemory(t, s, emb, "ambiguous B")

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
	s := newIntegrationMemgraph(t)
	ctx := context.Background()

	err := s.Delete(ctx, uuid.Nil.String())
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing UUID, got: %v", err)
	}
}
