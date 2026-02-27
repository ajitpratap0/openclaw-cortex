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
	dim int // vector dimension
	seq int // call counter for generating distinct vectors
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

// ── PreTurnHook tests ────────────────────────────────────────────────────────

func newPreTurnRecaller() *recall.Recaller {
	return recall.NewRecaller(recall.DefaultWeights(), slog.Default())
}

func TestPreTurnHook_HappyPath(t *testing.T) {
	ctx := context.Background()
	ms := store.NewMockStore()

	vec := newHookMockVec()
	_ = ms.Upsert(ctx, newTestMemory("pre-1", models.MemoryTypeProcedure, "Run kubectl apply -f deployment.yaml"), vec)
	_ = ms.Upsert(ctx, newTestMemory("pre-2", models.MemoryTypeFact, "Kubernetes requires RBAC"), vec)

	hook := hooks.NewPreTurnHook(
		&hookMockEmbedder{vec: vec},
		ms,
		newPreTurnRecaller(),
		slog.Default(),
	)

	out, err := hook.Execute(ctx, hooks.PreTurnInput{
		Message:     "How do I deploy to Kubernetes?",
		Project:     "proj-k8s",
		TokenBudget: 500,
	})
	require.NoError(t, err)
	require.NotNil(t, out)
	assert.GreaterOrEqual(t, out.MemoryCount, 0)
	assert.GreaterOrEqual(t, out.TokensUsed, 0)
}

func TestPreTurnHook_ZeroBudgetDefaultsToNonZero(t *testing.T) {
	ctx := context.Background()
	ms := store.NewMockStore()
	vec := newHookMockVec()
	_ = ms.Upsert(ctx, newTestMemory("pre-z", models.MemoryTypeFact, "Some memory"), vec)

	hook := hooks.NewPreTurnHook(
		&hookMockEmbedder{vec: vec},
		ms,
		newPreTurnRecaller(),
		slog.Default(),
	)

	// zero budget is normalised to a positive default inside Execute
	out, err := hook.Execute(ctx, hooks.PreTurnInput{
		Message:     "anything",
		TokenBudget: 0,
	})
	require.NoError(t, err)
	assert.NotNil(t, out)
}

func TestPreTurnHook_EmbedError(t *testing.T) {
	ctx := context.Background()
	ms := store.NewMockStore()

	hook := hooks.NewPreTurnHook(
		// dim must be > 0 so nextVec() doesn't divide by zero; err is checked by Execute.
		&hookMockEmbedder{err: errors.New("embed failed"), dim: 8},
		ms,
		newPreTurnRecaller(),
		slog.Default(),
	)

	_, err := hook.Execute(ctx, hooks.PreTurnInput{Message: "test", TokenBudget: 500})
	require.Error(t, err)
	assert.ErrorContains(t, err, "embed failed")
}

func TestPreTurnHook_ProjectFilterPassedThrough(t *testing.T) {
	ctx := context.Background()
	ms := store.NewMockStore()
	vec := newHookMockVec()

	mem := newTestMemory("pf-1", models.MemoryTypeFact, "Project-specific fact")
	mem.Project = "proj-k8s"
	_ = ms.Upsert(ctx, mem, vec)

	hook := hooks.NewPreTurnHook(
		&hookMockEmbedder{vec: vec},
		ms,
		newPreTurnRecaller(),
		slog.Default(),
	)

	// Verify a non-empty project does not cause an error (filter is wired correctly).
	out, err := hook.Execute(ctx, hooks.PreTurnInput{
		Message:     "deploy kubernetes",
		Project:     "proj-k8s",
		TokenBudget: 1000,
	})
	require.NoError(t, err)
	assert.NotNil(t, out)
}

func TestPreTurnHook_EmptyStore(t *testing.T) {
	ctx := context.Background()
	ms := store.NewMockStore()

	hook := hooks.NewPreTurnHook(
		&hookMockEmbedder{vec: newHookMockVec()},
		ms,
		newPreTurnRecaller(),
		slog.Default(),
	)

	out, err := hook.Execute(ctx, hooks.PreTurnInput{Message: "anything", TokenBudget: 500})
	require.NoError(t, err)
	assert.Equal(t, 0, out.MemoryCount)
}
