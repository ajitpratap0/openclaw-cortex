package tests

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/hooks"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

// pipelineTestInput returns a minimal PostTurnInput for pipeline tests.
func pipelineTestInput() hooks.PostTurnInput {
	return hooks.PostTurnInput{
		UserMessage:      "how do I deploy to prod?",
		AssistantMessage: "use kubectl apply -f",
		SessionID:        "pipeline-test-session",
		Project:          "test-project",
	}
}

// selectiveEmbedder fails on embed calls for memories at specific indices.
// Thread-safe: uses atomic counter to track call order.
type selectiveEmbedder struct {
	dim      int
	failIdxs map[int]bool
	calls    atomic.Int32
}

func (e *selectiveEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	idx := int(e.calls.Add(1)) - 1
	if e.failIdxs[idx] {
		return nil, errors.New("embed service unavailable for index")
	}
	// One-hot vectors: orthogonal, so dedup threshold (0.95) is never triggered
	// between distinct memories.
	vec := make([]float32, e.dim)
	vec[idx%e.dim] = 1.0
	return vec, nil
}

func (e *selectiveEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	result := make([][]float32, len(texts))
	for i := range texts {
		v, err := e.Embed(context.Background(), texts[i])
		if err != nil {
			return nil, err
		}
		result[i] = v
	}
	return result, nil
}

func (e *selectiveEmbedder) Dimension() int {
	return e.dim
}

// TestPostTurnHook_Pipeline_SkipsEmbedErrors verifies that when embedding fails
// for memory at index 1, memories at indices 0 and 2 are still stored.
func TestPostTurnHook_Pipeline_SkipsEmbedErrors(t *testing.T) {
	ctx := context.Background()
	ms := store.NewMockStore()
	logger := slog.Default()

	cap := &hookMockCapturer{
		memories: []models.CapturedMemory{
			{Content: "memory zero", Type: models.MemoryTypeFact, Confidence: 0.9},
			{Content: "memory one — embed will fail", Type: models.MemoryTypeFact, Confidence: 0.9},
			{Content: "memory two", Type: models.MemoryTypeFact, Confidence: 0.9},
		},
	}
	cls := &hookMockClassifier{memType: models.MemoryTypeFact}
	// Index 1 will fail; indices 0 and 2 succeed.
	emb := &selectiveEmbedder{dim: 8, failIdxs: map[int]bool{1: true}}

	// concurrency=1 for deterministic ordering of embed calls.
	hook := hooks.NewPostTurnHook(cap, cls, emb, ms, logger, 0.95, 1)
	err := hook.Execute(ctx, pipelineTestInput())
	require.NoError(t, err, "Execute must not return error even when individual embeds fail")

	stats, statsErr := ms.Stats(ctx)
	require.NoError(t, statsErr)
	// Memories 0 and 2 stored; memory 1 skipped.
	assert.Equal(t, int64(2), stats.TotalMemories)
}

// TestPostTurnHook_Pipeline_ContextCancellation verifies that when the context
// is canceled before Execute is called, Extract propagates the cancellation
// (returns an error) and Execute returns a non-nil error. No memories are stored.
func TestPostTurnHook_Pipeline_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately before Execute

	ms := store.NewMockStore()
	logger := slog.Default()

	cap := &hookMockCapturer{
		memories: []models.CapturedMemory{
			{Content: "should not be stored", Type: models.MemoryTypeFact, Confidence: 0.9},
		},
		// Simulate capturer respecting ctx cancellation.
		err: context.Canceled,
	}
	cls := &hookMockClassifier{memType: models.MemoryTypeFact}
	emb := &hookMockEmbedder{dim: 8}

	hook := hooks.NewPostTurnHook(cap, cls, emb, ms, logger, 0.95, 1)
	err := hook.Execute(ctx, pipelineTestInput())
	// Extract fails due to canceled context, so Execute must return an error.
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)

	// Stats call on a canceled ctx may also fail; use a fresh context.
	freshCtx := context.Background()
	stats, statsErr := ms.Stats(freshCtx)
	require.NoError(t, statsErr)
	assert.Equal(t, int64(0), stats.TotalMemories)
}

// TestPostTurnHook_Pipeline_ConcurrencyOne verifies that concurrency=1
// (fully sequential) still processes all memories correctly.
func TestPostTurnHook_Pipeline_ConcurrencyOne(t *testing.T) {
	ctx := context.Background()
	ms := store.NewMockStore()
	logger := slog.Default()

	cap := &hookMockCapturer{
		memories: []models.CapturedMemory{
			{Content: "seq memory alpha", Type: models.MemoryTypeFact, Confidence: 0.9},
			{Content: "seq memory beta", Type: models.MemoryTypeRule, Confidence: 0.85},
			{Content: "seq memory gamma", Type: models.MemoryTypeProcedure, Confidence: 0.8},
		},
	}
	cls := &hookMockClassifier{memType: models.MemoryTypeFact}
	emb := &hookMockEmbedder{dim: 8} // distinct vectors — no dedup triggers

	// concurrency=1: sequential execution
	hook := hooks.NewPostTurnHook(cap, cls, emb, ms, logger, 0.95, 1)
	err := hook.Execute(ctx, pipelineTestInput())
	require.NoError(t, err)

	stats, statsErr := ms.Stats(ctx)
	require.NoError(t, statsErr)
	assert.Equal(t, int64(3), stats.TotalMemories)
}

// atomicSeqEmbedder generates distinct orthogonal vectors using an atomic counter.
// Thread-safe: uses atomic.Int32 instead of a plain int.
type atomicSeqEmbedder struct {
	dim int
	seq atomic.Int32
}

func (e *atomicSeqEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	idx := int(e.seq.Add(1)) - 1
	d := e.dim
	v := make([]float32, d)
	v[idx%d] = 1.0
	return v, nil
}

func (e *atomicSeqEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	result := make([][]float32, len(texts))
	for i := range texts {
		v, err := e.Embed(context.Background(), texts[i])
		if err != nil {
			return nil, err
		}
		result[i] = v
	}
	return result, nil
}

func (e *atomicSeqEmbedder) Dimension() int {
	return e.dim
}

// TestPostTurnHook_Pipeline_DefaultConcurrency verifies that passing concurrency=0
// falls back to the default (4) and all memories are stored correctly.
func TestPostTurnHook_Pipeline_DefaultConcurrency(t *testing.T) {
	ctx := context.Background()
	ms := store.NewMockStore()
	logger := slog.Default()

	const numMemories = 10
	mems := make([]models.CapturedMemory, numMemories)
	for i := range mems {
		mems[i] = models.CapturedMemory{
			Type:       models.MemoryTypeFact,
			Confidence: 0.9,
		}
		// Make content unique so dedup does not trigger.
		mems[i].Content = "pipeline default concurrency memory " + string(rune('A'+i))
	}

	cap := &hookMockCapturer{memories: mems}
	cls := &hookMockClassifier{memType: models.MemoryTypeFact}
	// atomicSeqEmbedder is thread-safe: needed because concurrency=0 → 4 goroutines.
	emb := &atomicSeqEmbedder{dim: numMemories}

	// concurrency=0 triggers the default=4 fallback inside runMemoryPipeline.
	hook := hooks.NewPostTurnHook(cap, cls, emb, ms, logger, 0.95, 0)
	err := hook.Execute(ctx, pipelineTestInput())
	require.NoError(t, err)

	stats, statsErr := ms.Stats(ctx)
	require.NoError(t, statsErr)
	assert.Equal(t, int64(numMemories), stats.TotalMemories)
}

// TestPostTurnHook_Pipeline_MaxConcurrencyClamped verifies that concurrency values
// above 16 are clamped to 16 and all memories are still stored.
func TestPostTurnHook_Pipeline_MaxConcurrencyClamped(t *testing.T) {
	ctx := context.Background()
	ms := store.NewMockStore()
	logger := slog.Default()

	cap := &hookMockCapturer{
		memories: []models.CapturedMemory{
			{Content: "clamp test memory one", Type: models.MemoryTypeFact, Confidence: 0.9},
			{Content: "clamp test memory two", Type: models.MemoryTypeFact, Confidence: 0.9},
		},
	}
	cls := &hookMockClassifier{memType: models.MemoryTypeFact}
	// atomicSeqEmbedder is thread-safe; needed because concurrency=100 → 16 goroutines.
	emb := &atomicSeqEmbedder{dim: 8}

	// concurrency=100 must be clamped to 16 internally.
	hook := hooks.NewPostTurnHook(cap, cls, emb, ms, logger, 0.95, 100)
	err := hook.Execute(ctx, pipelineTestInput())
	require.NoError(t, err)

	stats, statsErr := ms.Stats(ctx)
	require.NoError(t, statsErr)
	assert.Equal(t, int64(2), stats.TotalMemories)
}

// slowMockEmbedder is a mock embedder that sleeps for a fixed delay on each
// Embed call to simulate slow LLM/embed work. Used to verify concurrency.
type slowMockEmbedder struct {
	dim   int
	delay time.Duration
	calls atomic.Int32
}

func (e *slowMockEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	time.Sleep(e.delay)
	idx := int(e.calls.Add(1)) - 1
	// Generate orthogonal one-hot vectors so dedup threshold (0.95) is never
	// exceeded between distinct memories, even when processed concurrently.
	vec := make([]float32, e.dim)
	vec[idx%e.dim] = 1.0
	return vec, nil
}

func (e *slowMockEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	result := make([][]float32, len(texts))
	for i := range texts {
		v, err := e.Embed(context.Background(), texts[i])
		if err != nil {
			return nil, err
		}
		result[i] = v
	}
	return result, nil
}

func (e *slowMockEmbedder) Dimension() int {
	return e.dim
}

// TestPostTurnHook_PipelineRunsConcurrently verifies that 4 memories processed
// with concurrency=4 complete in roughly the time of a single embed call, not
// the sequential total. This guards against accidental serialization of the pool.
func TestPostTurnHook_PipelineRunsConcurrently(t *testing.T) {
	const embedDelay = 50 * time.Millisecond
	const concurrency = 4

	slowEmb := &slowMockEmbedder{dim: 8, delay: embedDelay}
	ms := store.NewMockStore()
	logger := slog.Default()

	cap := &hookMockCapturer{
		memories: []models.CapturedMemory{
			{Content: "concurrent memory alpha", Type: models.MemoryTypeFact, Confidence: 0.9},
			{Content: "concurrent memory beta", Type: models.MemoryTypeFact, Confidence: 0.9},
			{Content: "concurrent memory gamma", Type: models.MemoryTypeFact, Confidence: 0.9},
			{Content: "concurrent memory delta", Type: models.MemoryTypeFact, Confidence: 0.9},
		},
	}
	cls := &hookMockClassifier{memType: models.MemoryTypeFact}

	hook := hooks.NewPostTurnHook(cap, cls, slowEmb, ms, logger, 0.95, concurrency)

	start := time.Now()
	err := hook.Execute(context.Background(), pipelineTestInput())
	elapsed := time.Since(start)

	require.NoError(t, err)
	// 4 goroutines running concurrently each sleep 50ms → wall-clock ~50ms.
	// Sequential would be 4 × 50ms = 200ms. Use 3× as a generous upper bound.
	assert.Less(t, elapsed, 3*embedDelay,
		"pipeline should run concurrently: elapsed=%v, sequential would be %v", elapsed, concurrency*int(embedDelay))

	stats, statsErr := ms.Stats(context.Background())
	require.NoError(t, statsErr)
	assert.Equal(t, int64(concurrency), stats.TotalMemories)
}
