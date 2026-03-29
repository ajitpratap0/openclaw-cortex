package tests

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

// TestCountZeroEmbeddingMemories verifies that CountZeroEmbeddingMemories
// counts only memories stored with a nil/empty embedding vector.
func TestCountZeroEmbeddingMemories(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	now := time.Now().UTC()
	base := models.Memory{
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Visibility: models.VisibilityShared,
		Confidence: 0.9,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	// Three memories with a valid embedding vector.
	for i, id := range []string{"has-vec-1", "has-vec-2", "has-vec-3"} {
		mem := base
		mem.ID = id
		mem.Content = "memory with embedding " + id
		require.NoError(t, s.Upsert(ctx, mem, testVector(float32(i+1)*0.1)))
	}

	// Two memories with NO embedding (nil / zero-length slice).
	for _, id := range []string{"no-vec-1", "no-vec-2"} {
		mem := base
		mem.ID = id
		mem.Content = "memory without embedding " + id
		require.NoError(t, s.Upsert(ctx, mem, nil))
	}

	n, err := s.CountZeroEmbeddingMemories(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(2), n, "expected exactly 2 memories with no embedding")
}

// TestCountZeroEmbeddingMemories_AllPresent verifies that zero is returned when
// every memory has an embedding.
func TestCountZeroEmbeddingMemories_AllPresent(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	now := time.Now().UTC()
	for i, id := range []string{"e1", "e2", "e3"} {
		mem := models.Memory{
			ID:         id,
			Type:       models.MemoryTypeFact,
			Scope:      models.ScopePermanent,
			Visibility: models.VisibilityShared,
			Content:    "memory " + id,
			Confidence: 0.9,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		require.NoError(t, s.Upsert(ctx, mem, testVector(float32(i+1)*0.2)))
	}

	n, err := s.CountZeroEmbeddingMemories(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), n)
}

// TestCountZeroEmbeddingMemories_Empty verifies that zero is returned on an
// empty store.
func TestCountZeroEmbeddingMemories_Empty(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	n, err := s.CountZeroEmbeddingMemories(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), n)
}

// TestReembed_DryRun verifies that dry-run mode detects zero-embedding memories
// and reports them without modifying the store.
//
// The reembed command (cmd_reembed.go) iterates all memories and calls
// Upsert only when dryRun is false. This test mirrors that logic directly
// using MockStore so we can assert: (a) the initial count is correct, and
// (b) after simulating a dry-run loop (no Upsert calls), the count is
// unchanged. Full CLI-flag integration testing requires a live Memgraph +
// Ollama environment and lives outside the short-test suite.
func TestReembed_DryRun(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	now := time.Now().UTC()

	// Insert two memories without embeddings.
	for _, id := range []string{"dry-no-vec-1", "dry-no-vec-2"} {
		mem := models.Memory{
			ID:         id,
			Type:       models.MemoryTypeFact,
			Scope:      models.ScopePermanent,
			Visibility: models.VisibilityShared,
			Content:    "needs re-embed " + id,
			Confidence: 0.8,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		require.NoError(t, s.Upsert(ctx, mem, nil))
	}

	// Insert one memory that already has an embedding.
	embedded := models.Memory{
		ID:         "dry-has-vec",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Visibility: models.VisibilityShared,
		Content:    "already embedded",
		Confidence: 0.9,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	require.NoError(t, s.Upsert(ctx, embedded, testVector(0.5)))

	// Confirm initial state: 2 memories need re-embedding.
	zerosBefore, err := s.CountZeroEmbeddingMemories(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(2), zerosBefore)

	// Simulate the dry-run loop from cmd_reembed.go: iterate all memories but
	// skip the Upsert call (dryRun=true path). The store must not be modified.
	memories, _, listErr := s.List(ctx, &store.SearchFilters{IncludeInvalidated: true}, 100, "")
	require.NoError(t, listErr)
	var fixed int64
	for i := range memories {
		if memories[i].HasEmbedding {
			continue
		}
		// dry-run: log only, no Upsert
		fixed++
	}
	assert.Equal(t, int64(2), fixed, "dry-run should detect exactly 2 missing-embedding memories")

	// After the dry-run loop, the store must be unmodified.
	zerosAfter, err := s.CountZeroEmbeddingMemories(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(2), zerosAfter, "dry-run must not change zero-embedding count")

	// The memory with a valid embedding is unchanged.
	got, err := s.Get(ctx, "dry-has-vec")
	require.NoError(t, err)
	assert.Equal(t, "already embedded", got.Content)
}

// TestReembed_FixesMissingEmbeddings verifies that upsert with a fresh vector
// causes CountZeroEmbeddingMemories to return zero after re-embedding all
// affected memories.
func TestReembed_FixesMissingEmbeddings(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	now := time.Now().UTC()
	ids := []string{"fix-1", "fix-2", "fix-3"}

	// Insert all three memories without embeddings.
	for _, id := range ids {
		mem := models.Memory{
			ID:         id,
			Type:       models.MemoryTypeFact,
			Scope:      models.ScopePermanent,
			Visibility: models.VisibilityShared,
			Content:    "content for " + id,
			Confidence: 0.85,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		require.NoError(t, s.Upsert(ctx, mem, nil))
	}

	// Confirm all three are zero-embedding.
	n, err := s.CountZeroEmbeddingMemories(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(3), n)

	// Simulate reembed apply path: list all, upsert with fresh vectors.
	memories, _, listErr := s.List(ctx, &store.SearchFilters{IncludeInvalidated: true}, 100, "")
	require.NoError(t, listErr)

	for i := range memories {
		vec := testVector(float32(i+1) * 0.1)
		require.NoError(t, s.Upsert(ctx, memories[i], vec))
	}

	// After re-embedding all three, zero-embedding count must be zero.
	n, err = s.CountZeroEmbeddingMemories(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), n, "all memories should have embeddings after reembed")
}
