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

// unit32Vec returns a normalised float32 slice that is all-zero except for the
// first element, making it easy to produce orthogonal or identical vectors in
// tests without running a real embedder.
func makeDedupVec(val float32) []float32 {
	v := make([]float32, 4)
	v[0] = val
	return v
}

// storeMemory is a helper that upserts a memory with a known vector.
func storeMemory(t *testing.T, st *store.MockStore, id, content string, vec []float32) {
	t.Helper()
	now := time.Now().UTC()
	mem := models.Memory{
		ID:           id,
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopePermanent,
		Visibility:   models.VisibilityShared,
		Content:      content,
		Confidence:   0.9,
		Source:       "test",
		CreatedAt:    now,
		UpdatedAt:    now,
		LastAccessed: now,
	}
	require.NoError(t, st.Upsert(context.Background(), mem, vec))
}

// TestDedupNearIdenticalContentSkipped verifies that when a new memory's
// content is the same length (or shorter) as an existing near-duplicate, the
// check signals a duplicate and the caller should skip storing.
func TestDedupNearIdenticalContentSkipped(t *testing.T) {
	st := store.NewMockStore()
	ctx := context.Background()

	existingVec := makeDedupVec(1.0)
	storeMemory(t, st, "existing-1", "Go uses goroutines for concurrency.", existingVec)

	// Identical vector → cosine similarity = 1.0 (well above 0.92 threshold).
	newVec := makeDedupVec(1.0)
	newContent := "Go uses goroutines for concurrency." // same length

	res, err := store.CheckAndHandleDuplicate(ctx, st, newVec, newContent, 0.92)
	require.NoError(t, err)
	assert.True(t, res.IsDuplicate, "same-length content should be flagged as duplicate")
	assert.False(t, res.IsUpdated, "should not update when content is not richer")
	assert.Equal(t, "existing-1", res.ExistingID)
}

// TestDedupShorterContentSkipped verifies that content shorter than the
// existing duplicate is skipped (not richer).
func TestDedupShorterContentSkipped(t *testing.T) {
	st := store.NewMockStore()
	ctx := context.Background()

	existingVec := makeDedupVec(1.0)
	storeMemory(t, st, "existing-2", "Go uses goroutines for lightweight concurrency.", existingVec)

	newVec := makeDedupVec(1.0)
	newContent := "Go uses goroutines." // shorter

	res, err := store.CheckAndHandleDuplicate(ctx, st, newVec, newContent, 0.92)
	require.NoError(t, err)
	assert.True(t, res.IsDuplicate, "shorter content should be flagged as duplicate")
	assert.False(t, res.IsUpdated)
	assert.Equal(t, "existing-2", res.ExistingID)
}

// TestDedupRicherContentUpdatesExisting verifies that when the new content is
// longer than the existing duplicate, CheckAndHandleDuplicate updates the
// existing memory in place and returns IsUpdated=true.
func TestDedupRicherContentUpdatesExisting(t *testing.T) {
	st := store.NewMockStore()
	ctx := context.Background()

	existingVec := makeDedupVec(1.0)
	existingContent := "Go uses goroutines for concurrency."
	storeMemory(t, st, "existing-3", existingContent, existingVec)

	newVec := makeDedupVec(1.0)
	richerContent := "Go uses goroutines for concurrency. Goroutines are multiplexed onto OS threads by the Go runtime scheduler."

	res, err := store.CheckAndHandleDuplicate(ctx, st, newVec, richerContent, 0.92)
	require.NoError(t, err)
	assert.False(t, res.IsDuplicate, "richer content should not be reported as a skip")
	assert.True(t, res.IsUpdated, "richer content should trigger an in-place update")
	assert.Equal(t, "existing-3", res.ExistingID)

	// Verify the stored memory was actually updated.
	updated, getErr := st.Get(ctx, "existing-3")
	require.NoError(t, getErr)
	assert.Equal(t, richerContent, updated.Content, "stored content should reflect the richer version")
}

// TestDedupNoMatchProceedsNormally verifies that when no existing memory
// exceeds the similarity threshold, CheckAndHandleDuplicate returns a zero
// DedupResult and the caller should proceed with a normal store.
func TestDedupNoMatchProceedsNormally(t *testing.T) {
	st := store.NewMockStore()
	ctx := context.Background()

	// Store a memory with an orthogonal vector.
	existingVec := makeDedupVec(1.0) // [1,0,0,0]
	storeMemory(t, st, "unrelated-1", "Python uses the GIL for thread safety.", existingVec)

	// New vector is orthogonal → cosine similarity = 0.
	newVec := []float32{0, 1, 0, 0}
	newContent := "Rust uses ownership for memory safety."

	res, err := store.CheckAndHandleDuplicate(ctx, st, newVec, newContent, 0.92)
	require.NoError(t, err)
	assert.False(t, res.IsDuplicate, "orthogonal vector should not be flagged as duplicate")
	assert.False(t, res.IsUpdated, "orthogonal vector should not trigger an update")
	assert.Empty(t, res.ExistingID)
}
