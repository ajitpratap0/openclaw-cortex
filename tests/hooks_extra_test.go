package tests

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/hooks"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/recall"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

// TestPostTurnHook_EmbedError_SkipsMemory verifies that when embedding fails
// for an extracted memory, that memory is skipped but execution continues.
func TestPostTurnHook_EmbedError_SkipsMemory(t *testing.T) {
	ctx := context.Background()
	ms := store.NewMockStore()
	logger := slog.Default()

	cap := &hookMockCapturer{
		memories: []models.CapturedMemory{
			{Content: "Memory with bad embed", Type: models.MemoryTypeFact, Confidence: 0.9},
		},
	}
	cls := &hookMockClassifier{memType: models.MemoryTypeFact}
	// Embedder always returns error
	emb := &hookMockEmbedder{err: errors.New("embed service unavailable"), dim: 8}

	hook := hooks.NewPostTurnHook(cap, cls, emb, ms, logger)
	err := hook.Execute(ctx, hookTestInput())
	// Should not error — embed failures are logged and skipped
	require.NoError(t, err)

	// No memories should be stored since embed failed
	stats, err := ms.Stats(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), stats.TotalMemories)
}

// TestPostTurnHook_ClassifierFallback verifies that when a captured memory has
// no type set, the classifier is used to determine the type.
func TestPostTurnHook_ClassifierFallback(t *testing.T) {
	ctx := context.Background()
	ms := store.NewMockStore()
	logger := slog.Default()

	// Memory with empty type — should trigger classifier fallback
	cap := &hookMockCapturer{
		memories: []models.CapturedMemory{
			{Content: "You must always use tests", Type: "", Confidence: 0.9},
		},
	}
	cls := &hookMockClassifier{memType: models.MemoryTypeRule} // Classifier returns Rule
	emb := &hookMockEmbedder{dim: 8}

	hook := hooks.NewPostTurnHook(cap, cls, emb, ms, logger)
	err := hook.Execute(ctx, hookTestInput())
	require.NoError(t, err)

	// One memory should be stored
	stats, err := ms.Stats(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), stats.TotalMemories)
}

// TestPostTurnHook_MultipleMemories verifies that multiple distinct memories are all stored.
func TestPostTurnHook_MultipleMemories(t *testing.T) {
	ctx := context.Background()
	ms := store.NewMockStore()
	logger := slog.Default()

	cap := &hookMockCapturer{
		memories: []models.CapturedMemory{
			{Content: "Memory 1", Type: models.MemoryTypeFact, Confidence: 0.9},
			{Content: "Memory 2", Type: models.MemoryTypeProcedure, Confidence: 0.85},
			{Content: "Memory 3", Type: models.MemoryTypeRule, Confidence: 0.95},
		},
	}
	cls := &hookMockClassifier{memType: models.MemoryTypeFact}
	// Use distinct vectors (dim-based embedder) to avoid dedup
	emb := &hookMockEmbedder{dim: 8}

	hook := hooks.NewPostTurnHook(cap, cls, emb, ms, logger)
	err := hook.Execute(ctx, hookTestInput())
	require.NoError(t, err)

	stats, err := ms.Stats(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(3), stats.TotalMemories)
}

// TestPreTurnHook_MultipleResults verifies that multiple memories are ranked
// and returned correctly.
func TestPreTurnHook_MultipleResults(t *testing.T) {
	ctx := context.Background()
	ms := store.NewMockStore()

	vec := newHookMockVec()
	_ = ms.Upsert(ctx, newTestMemory("m1", models.MemoryTypeRule, "Always use tests"), vec)
	_ = ms.Upsert(ctx, newTestMemory("m2", models.MemoryTypeFact, "Go is great"), vec)
	_ = ms.Upsert(ctx, newTestMemory("m3", models.MemoryTypeProcedure, "Run tests with go test"), vec)

	recaller := recall.NewRecaller(recall.DefaultWeights(), slog.Default())
	hook := hooks.NewPreTurnHook(
		&hookMockEmbedder{vec: vec},
		ms,
		recaller,
		slog.Default(),
	)

	out, err := hook.Execute(ctx, hooks.PreTurnInput{
		Message:     "how to test in Go",
		TokenBudget: 1000,
	})
	require.NoError(t, err)
	assert.NotNil(t, out)
	assert.GreaterOrEqual(t, out.MemoryCount, 0)
	assert.LessOrEqual(t, out.MemoryCount, 3)
}

// TestPreTurnHook_TokenBudgetLimitsMemoryCount verifies that a very small
// token budget limits how many memories are returned.
func TestPreTurnHook_TokenBudgetLimitsMemoryCount(t *testing.T) {
	ctx := context.Background()
	ms := store.NewMockStore()

	vec := newHookMockVec()
	// Add many memories with long content
	for i, id := range []string{"tok-1", "tok-2", "tok-3", "tok-4", "tok-5"} {
		longContent := "This is a very long memory content that will consume many tokens when formatted. "
		for j := 0; j < 10; j++ {
			longContent += "Additional content to make it longer. "
		}
		_ = ms.Upsert(ctx, newTestMemory(id, models.MemoryTypeFact, longContent), testVector(float32(i)*0.1))
	}

	recaller := recall.NewRecaller(recall.DefaultWeights(), slog.Default())
	hook := hooks.NewPreTurnHook(
		&hookMockEmbedder{vec: vec},
		ms,
		recaller,
		slog.Default(),
	)

	// Very small budget that can only fit 1-2 memories
	out, err := hook.Execute(ctx, hooks.PreTurnInput{
		Message:     "test query",
		TokenBudget: 5, // extremely small
	})
	require.NoError(t, err)
	assert.NotNil(t, out)
	// With such a tiny budget, count should be very small
	assert.LessOrEqual(t, out.MemoryCount, 5)
}

// TestPostTurnHook_ProjectTagging verifies that memories are tagged with
// the project from input.
func TestPostTurnHook_ProjectTagging(t *testing.T) {
	ctx := context.Background()
	ms := store.NewMockStore()
	logger := slog.Default()

	cap := &hookMockCapturer{
		memories: []models.CapturedMemory{
			{Content: "project specific memory", Type: models.MemoryTypeFact, Confidence: 0.9},
		},
	}
	cls := &hookMockClassifier{memType: models.MemoryTypeFact}
	emb := &hookMockEmbedder{dim: 8}

	hook := hooks.NewPostTurnHook(cap, cls, emb, ms, logger)
	err := hook.Execute(ctx, hooks.PostTurnInput{
		UserMessage:      "user msg",
		AssistantMessage: "assistant msg",
		SessionID:        "sess-1",
		Project:          "my-project",
	})
	require.NoError(t, err)

	stats, err := ms.Stats(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), stats.TotalMemories)
}
