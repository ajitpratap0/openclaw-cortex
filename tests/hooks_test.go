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
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

// --- mock capturer ---

type hookMockCapturer struct {
	memories []models.CapturedMemory
	err      error
}

func (m *hookMockCapturer) Extract(_ context.Context, _, _ string) ([]models.CapturedMemory, error) {
	return m.memories, m.err
}

// --- mock classifier ---

type hookMockClassifier struct {
	memType models.MemoryType
}

func (m *hookMockClassifier) Classify(_ string) models.MemoryType {
	return m.memType
}

// --- mock embedder ---

type hookMockEmbedder struct {
	vec []float32
	err error
	dim int   // vector dimension
	seq int   // call counter for generating distinct vectors
}

func (m *hookMockEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return m.nextVec(), m.err
}

func (m *hookMockEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	result := make([][]float32, len(texts))
	for i := range texts {
		result[i] = m.nextVec()
	}
	return result, m.err
}

func (m *hookMockEmbedder) Dimension() int {
	if m.dim > 0 {
		return m.dim
	}
	return len(m.vec)
}

// nextVec returns the next embedding vector. When vec is set, it always returns
// that fixed vector (useful for dedup tests). When only dim is set, it returns
// distinct one-hot vectors so successive embeddings are orthogonal.
func (m *hookMockEmbedder) nextVec() []float32 {
	if m.vec != nil {
		return m.vec
	}
	d := m.Dimension()
	v := make([]float32, d)
	v[m.seq%d] = 1.0
	m.seq++
	return v
}

// --- helpers ---

func hookTestInput() hooks.PostTurnInput {
	return hooks.PostTurnInput{
		UserMessage:      "How do I deploy?",
		AssistantMessage: "Run kubectl apply.",
		SessionID:        "sess-1",
		Project:          "proj-1",
	}
}

func newHookMockVec() []float32 {
	v := make([]float32, 8)
	for i := range v {
		v[i] = float32(i) * 0.1
	}
	return v
}

// --- tests ---

func TestPostTurnHook_HappyPath(t *testing.T) {
	ctx := context.Background()
	ms := store.NewMockStore()
	logger := slog.Default()

	cap := &hookMockCapturer{
		memories: []models.CapturedMemory{
			{Content: "Deploy with kubectl apply", Type: models.MemoryTypeProcedure, Confidence: 0.9, Tags: []string{"k8s"}},
			{Content: "Always use --dry-run first", Type: models.MemoryTypeRule, Confidence: 0.85, Tags: []string{"safety"}},
		},
	}
	cls := &hookMockClassifier{memType: models.MemoryTypeFact}
	emb := &hookMockEmbedder{dim: 8} // distinct vectors per call — no false dedup

	hook := hooks.NewPostTurnHook(cap, cls, emb, ms, logger)
	err := hook.Execute(ctx, hookTestInput())
	require.NoError(t, err)

	// Both memories should be stored.
	stats, err := ms.Stats(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(2), stats.TotalMemories)
}

func TestPostTurnHook_DedupSkip(t *testing.T) {
	ctx := context.Background()
	ms := store.NewMockStore()
	logger := slog.Default()
	vec := newHookMockVec()

	// Pre-populate store with an existing memory using the same vector.
	existing := newTestMemory("existing-1", models.MemoryTypeFact, "Deploy with kubectl apply")
	require.NoError(t, ms.Upsert(ctx, existing, vec))

	cap := &hookMockCapturer{
		memories: []models.CapturedMemory{
			{Content: "Deploy with kubectl apply", Type: models.MemoryTypeProcedure, Confidence: 0.9},
		},
	}
	cls := &hookMockClassifier{memType: models.MemoryTypeFact}
	emb := &hookMockEmbedder{vec: vec} // Same vector -> cosine similarity = 1.0 -> dedup triggers.

	hook := hooks.NewPostTurnHook(cap, cls, emb, ms, logger)
	err := hook.Execute(ctx, hookTestInput())
	require.NoError(t, err)

	// Only the pre-existing memory should remain; the duplicate should NOT be stored.
	stats, err := ms.Stats(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), stats.TotalMemories)
}

func TestPostTurnHook_EmptyExtraction(t *testing.T) {
	ctx := context.Background()
	ms := store.NewMockStore()
	logger := slog.Default()

	cap := &hookMockCapturer{memories: nil}
	cls := &hookMockClassifier{memType: models.MemoryTypeFact}
	emb := &hookMockEmbedder{vec: newHookMockVec()}

	hook := hooks.NewPostTurnHook(cap, cls, emb, ms, logger)
	err := hook.Execute(ctx, hookTestInput())
	require.NoError(t, err)

	stats, err := ms.Stats(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), stats.TotalMemories)
}

func TestPostTurnHook_CaptureError(t *testing.T) {
	ctx := context.Background()
	ms := store.NewMockStore()
	logger := slog.Default()

	capErr := errors.New("claude API down")
	cap := &hookMockCapturer{err: capErr}
	cls := &hookMockClassifier{memType: models.MemoryTypeFact}
	emb := &hookMockEmbedder{vec: newHookMockVec()}

	hook := hooks.NewPostTurnHook(cap, cls, emb, ms, logger)
	err := hook.Execute(ctx, hookTestInput())
	require.Error(t, err)
	assert.ErrorContains(t, err, "claude API down")
}
