package tests

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/capture"
	"github.com/ajitpratap0/openclaw-cortex/internal/hooks"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

// TestConflictDetector_EmptyCandidates verifies that when no candidate memories
// are provided, Detect immediately returns false without calling the API.
func TestConflictDetector_EmptyCandidates(t *testing.T) {
	cd := capture.NewConflictDetector("fake-api-key", "claude-haiku-4-5-20251001", slog.Default())
	contradicts, id, reason, err := cd.Detect(context.Background(), "some new memory", nil)
	require.NoError(t, err)
	assert.False(t, contradicts, "empty candidates should return false immediately")
	assert.Empty(t, id)
	assert.Empty(t, reason)
}

// TestConflictDetector_EmptySliceCandidates verifies that an empty (non-nil) slice
// is treated the same as nil — returns false immediately.
func TestConflictDetector_EmptySliceCandidates(t *testing.T) {
	cd := capture.NewConflictDetector("fake-api-key", "claude-haiku-4-5-20251001", slog.Default())
	contradicts, id, reason, err := cd.Detect(context.Background(), "some new memory", []models.Memory{})
	require.NoError(t, err)
	assert.False(t, contradicts)
	assert.Empty(t, id)
	assert.Empty(t, reason)
}

// TestConflictDetector_InvalidAPIKey_GracefulDegradation verifies that when the
// Claude API call fails (e.g., invalid API key), Detect returns false without
// propagating an error — safe default is to store the memory anyway.
func TestConflictDetector_InvalidAPIKey_GracefulDegradation(t *testing.T) {
	cd := capture.NewConflictDetector("invalid-api-key-xxx", "claude-haiku-4-5-20251001", slog.Default())

	candidates := []models.Memory{
		newTestMemory("cand-1", models.MemoryTypeFact, "Python is a fast language"),
		newTestMemory("cand-2", models.MemoryTypeRule, "Always use type hints in Python"),
	}

	// With an invalid key the API will return an auth error; Detect must degrade gracefully.
	contradicts, id, reason, err := cd.Detect(context.Background(), "Python is a slow language", candidates)
	require.NoError(t, err, "Detect must not propagate API errors")
	assert.False(t, contradicts, "graceful degradation returns false on API error")
	assert.Empty(t, id)
	assert.Empty(t, reason)
}

// TestConflictDetector_Constructor verifies the constructor returns a non-nil detector.
func TestConflictDetector_Constructor(t *testing.T) {
	cd := capture.NewConflictDetector("fake-key", "claude-haiku-4-5-20251001", slog.Default())
	assert.NotNil(t, cd)
}

// TestPostTurnHook_WithConflictDetector_GracefulDegradation verifies that when a
// ConflictDetector is attached to PostTurnHook and the API call fails (fake key),
// the hook still stores the memory without error.
func TestPostTurnHook_WithConflictDetector_GracefulDegradation(t *testing.T) {
	ctx := context.Background()
	ms := store.NewMockStore()
	logger := slog.Default()

	cap := &hookMockCapturer{
		memories: []models.CapturedMemory{
			{Content: "Python is a slow language", Type: models.MemoryTypeFact, Confidence: 0.9},
		},
	}
	cls := &hookMockClassifier{memType: models.MemoryTypeFact}
	emb := &hookMockEmbedder{dim: 8}

	// ConflictDetector with a fake key — API call will fail, must degrade gracefully.
	cd := capture.NewConflictDetector("invalid-api-key-xxx", "claude-haiku-4-5-20251001", logger)

	hook := hooks.NewPostTurnHook(cap, cls, emb, ms, logger, 0.95).
		WithConflictDetector(cd)

	err := hook.Execute(ctx, hookTestInput())
	require.NoError(t, err, "Execute must not error even when ConflictDetector API fails")

	// Memory should be stored despite the conflict check failure.
	stats, statsErr := ms.Stats(ctx)
	require.NoError(t, statsErr)
	assert.Equal(t, int64(1), stats.TotalMemories, "memory should be stored even when conflict check fails")
}

// TestPostTurnHook_WithConflictDetector_NilDetector verifies that when no
// ConflictDetector is attached (nil), the hook behaves exactly as before.
func TestPostTurnHook_WithConflictDetector_NilDetector(t *testing.T) {
	ctx := context.Background()
	ms := store.NewMockStore()
	logger := slog.Default()

	cap := &hookMockCapturer{
		memories: []models.CapturedMemory{
			{Content: "Go uses goroutines", Type: models.MemoryTypeFact, Confidence: 0.9},
		},
	}
	cls := &hookMockClassifier{memType: models.MemoryTypeFact}
	emb := &hookMockEmbedder{dim: 8}

	// No conflict detector attached — hook should work normally.
	hook := hooks.NewPostTurnHook(cap, cls, emb, ms, logger, 0.95)

	err := hook.Execute(ctx, hookTestInput())
	require.NoError(t, err)

	stats, statsErr := ms.Stats(ctx)
	require.NoError(t, statsErr)
	assert.Equal(t, int64(1), stats.TotalMemories)
}

// TestPostTurnHook_WithConflictDetector_WithExistingMemories verifies that when
// existing memories are in the store and the conflict detector is attached (but
// the API key is fake so it degrades), all new memories are still stored.
func TestPostTurnHook_WithConflictDetector_WithExistingMemories(t *testing.T) {
	ctx := context.Background()
	ms := store.NewMockStore()
	logger := slog.Default()

	// Pre-populate the store with a memory that could be a candidate.
	existingVec := newHookMockVec()
	existing := newTestMemory("existing-fact", models.MemoryTypeFact, "Python is a fast language")
	require.NoError(t, ms.Upsert(ctx, existing, existingVec))

	// New memory uses a different vector so dedup doesn't trigger.
	cap := &hookMockCapturer{
		memories: []models.CapturedMemory{
			{Content: "Python is a slow language", Type: models.MemoryTypeFact, Confidence: 0.9},
		},
	}
	cls := &hookMockClassifier{memType: models.MemoryTypeFact}
	// Use dim-based embedder so new memory gets a distinct vector from the existing one.
	emb := &hookMockEmbedder{dim: 8}

	cd := capture.NewConflictDetector("invalid-api-key-xxx", "claude-haiku-4-5-20251001", logger)

	hook := hooks.NewPostTurnHook(cap, cls, emb, ms, logger, 0.50).
		WithConflictDetector(cd)

	err := hook.Execute(ctx, hookTestInput())
	require.NoError(t, err)

	// Both memories (existing + new) should be in the store.
	stats, statsErr := ms.Stats(ctx)
	require.NoError(t, statsErr)
	assert.Equal(t, int64(2), stats.TotalMemories)
}

// TestPostTurnHook_WithConflictDetector_ChainedCall verifies that
// WithConflictDetector returns the same hook for method chaining.
func TestPostTurnHook_WithConflictDetector_ChainedCall(t *testing.T) {
	ms := store.NewMockStore()
	logger := slog.Default()
	cap := &hookMockCapturer{}
	cls := &hookMockClassifier{}
	emb := &hookMockEmbedder{dim: 8}
	cd := capture.NewConflictDetector("fake-key", "claude-haiku-4-5-20251001", logger)

	hook := hooks.NewPostTurnHook(cap, cls, emb, ms, logger, 0.95)
	returned := hook.WithConflictDetector(cd)
	assert.Equal(t, hook, returned, "WithConflictDetector should return the same hook instance")
}
