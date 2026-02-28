package tests

// hook_cmd_test.go exercises the graceful-degradation contract for the
// cortex hook pre/post commands via the internal hook types and MockStore.
// The cmd layer itself is thin glue code; we test the underlying hooks and
// verify that errors do NOT propagate upward (the key safety requirement).

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

// ── helpers ───────────────────────────────────────────────────────────────────

// cmdHookMockEmbedder is a local embedder mock for hook_cmd tests.
// It mirrors hookMockEmbedder defined in hooks_test.go but lives here so the
// hook_cmd tests remain self-contained.
type cmdHookMockEmbedder struct {
	vec []float32
	err error
	dim int
	seq int
}

func (m *cmdHookMockEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return m.cmdNextVec(), m.err
}

func (m *cmdHookMockEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	result := make([][]float32, len(texts))
	for i := range texts {
		result[i] = m.cmdNextVec()
	}
	return result, m.err
}

func (m *cmdHookMockEmbedder) Dimension() int {
	if m.dim > 0 {
		return m.dim
	}
	return len(m.vec)
}

func (m *cmdHookMockEmbedder) cmdNextVec() []float32 {
	if m.vec != nil {
		return m.vec
	}
	d := m.Dimension()
	v := make([]float32, d)
	v[m.seq%d] = 1.0
	m.seq++
	return v
}

// cmdHookMockCapturer is a local capturer mock for hook_cmd tests.
type cmdHookMockCapturer struct {
	memories []models.CapturedMemory
	err      error
}

func (m *cmdHookMockCapturer) Extract(_ context.Context, _, _ string) ([]models.CapturedMemory, error) {
	return m.memories, m.err
}

// cmdHookMockClassifier is a local classifier mock.
type cmdHookMockClassifier struct {
	memType models.MemoryType
}

func (m *cmdHookMockClassifier) Classify(_ string) models.MemoryType {
	return m.memType
}

func newCmdHookRecaller() *recall.Recaller {
	return recall.NewRecaller(recall.DefaultWeights(), slog.Default())
}

// ── PreTurnHook graceful-degradation tests ────────────────────────────────────

// TestHookCmd_Pre_GracefulDegradation_EmbedError verifies that when the embedder
// fails, the pre-turn hook returns an error (the cmd layer catches it and exits 0).
func TestHookCmd_Pre_GracefulDegradation_EmbedError(t *testing.T) {
	ctx := context.Background()
	ms := store.NewMockStore()

	hook := hooks.NewPreTurnHook(
		&cmdHookMockEmbedder{err: errors.New("ollama unavailable"), dim: 8},
		ms,
		newCmdHookRecaller(),
		slog.Default(),
	)

	_, err := hook.Execute(ctx, hooks.PreTurnInput{
		Message:     "how do I deploy?",
		Project:     "proj-1",
		TokenBudget: 2000,
	})
	// The hook itself returns an error; the cmd layer is responsible for
	// catching this and writing an empty JSON response instead of exiting non-zero.
	require.Error(t, err)
	assert.ErrorContains(t, err, "ollama unavailable")
}

// TestHookCmd_Pre_EmptyStore verifies that an empty store returns a valid zero output.
func TestHookCmd_Pre_EmptyStore(t *testing.T) {
	ctx := context.Background()
	ms := store.NewMockStore()

	hook := hooks.NewPreTurnHook(
		&cmdHookMockEmbedder{vec: make([]float32, 8)},
		ms,
		newCmdHookRecaller(),
		slog.Default(),
	)

	out, err := hook.Execute(ctx, hooks.PreTurnInput{
		Message:     "any message",
		TokenBudget: 2000,
	})
	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Equal(t, 0, out.MemoryCount)
	assert.Equal(t, "", out.Context)
}

// TestHookCmd_Pre_HappyPath verifies that the pre-turn hook populates context
// from stored memories when no project filter is applied.
func TestHookCmd_Pre_HappyPath(t *testing.T) {
	ctx := context.Background()
	ms := store.NewMockStore()

	vec := make([]float32, 8)
	vec[0] = 1.0

	// Store memories without a project so they are not filtered out.
	_ = ms.Upsert(ctx, newTestMemory("cmd-pre-1", models.MemoryTypeRule, "Always write tests"), vec)
	_ = ms.Upsert(ctx, newTestMemory("cmd-pre-2", models.MemoryTypeFact, "Go 1.23 is required"), vec)

	hook := hooks.NewPreTurnHook(
		&cmdHookMockEmbedder{vec: vec},
		ms,
		newCmdHookRecaller(),
		slog.Default(),
	)

	// No project specified — all memories are eligible.
	out, err := hook.Execute(ctx, hooks.PreTurnInput{
		Message:     "what are the coding standards?",
		TokenBudget: 2000,
	})
	require.NoError(t, err)
	require.NotNil(t, out)
	assert.GreaterOrEqual(t, out.MemoryCount, 1)
	assert.NotEmpty(t, out.Context)
	assert.Greater(t, out.TokensUsed, 0)
}

// TestHookCmd_Pre_DefaultTokenBudget verifies that a zero budget is normalised.
func TestHookCmd_Pre_DefaultTokenBudget(t *testing.T) {
	ctx := context.Background()
	ms := store.NewMockStore()

	hook := hooks.NewPreTurnHook(
		&cmdHookMockEmbedder{dim: 8},
		ms,
		newCmdHookRecaller(),
		slog.Default(),
	)

	out, err := hook.Execute(ctx, hooks.PreTurnInput{Message: "test", TokenBudget: 0})
	require.NoError(t, err)
	assert.NotNil(t, out)
}

// ── PostTurnHook graceful-degradation tests ───────────────────────────────────

// TestHookCmd_Post_GracefulDegradation_CaptureError verifies that when the
// capturer (Claude API) fails, the error is returned. The cmd layer catches
// this and writes {"stored":false} instead of exiting non-zero.
func TestHookCmd_Post_GracefulDegradation_CaptureError(t *testing.T) {
	ctx := context.Background()
	ms := store.NewMockStore()

	cap := &cmdHookMockCapturer{err: errors.New("anthropic API unavailable")}
	cls := &cmdHookMockClassifier{memType: models.MemoryTypeFact}
	emb := &cmdHookMockEmbedder{dim: 8}

	hook := hooks.NewPostTurnHook(cap, cls, emb, ms, slog.Default(), 0.95)

	err := hook.Execute(ctx, hooks.PostTurnInput{
		UserMessage:      "How do I deploy?",
		AssistantMessage: "Run kubectl apply.",
		SessionID:        "sess-cmd-1",
		Project:          "proj-cmd",
	})
	// The hook returns the error; the cmd layer is responsible for catching it.
	require.Error(t, err)
	assert.ErrorContains(t, err, "anthropic API unavailable")
}

// TestHookCmd_Post_GracefulDegradation_EmbedError verifies that embed errors
// during post-turn are silently skipped (the hook does NOT propagate them).
func TestHookCmd_Post_GracefulDegradation_EmbedError(t *testing.T) {
	ctx := context.Background()
	ms := store.NewMockStore()

	cap := &cmdHookMockCapturer{
		memories: []models.CapturedMemory{
			{Content: "Deploy with kubectl", Type: models.MemoryTypeProcedure, Confidence: 0.9},
		},
	}
	cls := &cmdHookMockClassifier{memType: models.MemoryTypeFact}
	emb := &cmdHookMockEmbedder{err: errors.New("ollama unavailable"), dim: 8}

	hook := hooks.NewPostTurnHook(cap, cls, emb, ms, slog.Default(), 0.95)

	// Embed errors for individual memories are skipped — hook returns nil.
	err := hook.Execute(ctx, hooks.PostTurnInput{
		UserMessage:      "How do I deploy?",
		AssistantMessage: "Run kubectl apply.",
		SessionID:        "sess-cmd-2",
	})
	require.NoError(t, err)

	// Nothing stored because embed failed.
	stats, statsErr := ms.Stats(ctx)
	require.NoError(t, statsErr)
	assert.Equal(t, int64(0), stats.TotalMemories)
}

// TestHookCmd_Post_HappyPath verifies that the post-turn hook stores memories
// when all services are available.
func TestHookCmd_Post_HappyPath(t *testing.T) {
	ctx := context.Background()
	ms := store.NewMockStore()

	cap := &cmdHookMockCapturer{
		memories: []models.CapturedMemory{
			{Content: "Always run tests before merging", Type: models.MemoryTypeRule, Confidence: 0.92},
			{Content: "Use go test -race for race detection", Type: models.MemoryTypeProcedure, Confidence: 0.88},
		},
	}
	cls := &cmdHookMockClassifier{memType: models.MemoryTypeFact}
	emb := &cmdHookMockEmbedder{dim: 8} // distinct vectors — no false dedup

	hook := hooks.NewPostTurnHook(cap, cls, emb, ms, slog.Default(), 0.95)

	err := hook.Execute(ctx, hooks.PostTurnInput{
		UserMessage:      "What tests should I run?",
		AssistantMessage: "Run go test -race -count=1 ./...",
		SessionID:        "sess-cmd-3",
		Project:          "proj-cortex",
	})
	require.NoError(t, err)

	stats, statsErr := ms.Stats(ctx)
	require.NoError(t, statsErr)
	assert.Equal(t, int64(2), stats.TotalMemories)
}

// TestHookCmd_Post_EmptyExtraction verifies that when Claude extracts no
// memories, the hook completes without error and stores nothing.
func TestHookCmd_Post_EmptyExtraction(t *testing.T) {
	ctx := context.Background()
	ms := store.NewMockStore()

	cap := &cmdHookMockCapturer{memories: nil}
	cls := &cmdHookMockClassifier{memType: models.MemoryTypeFact}
	emb := &cmdHookMockEmbedder{dim: 8}

	hook := hooks.NewPostTurnHook(cap, cls, emb, ms, slog.Default(), 0.95)

	err := hook.Execute(ctx, hooks.PostTurnInput{
		UserMessage:      "Hello",
		AssistantMessage: "Hi there!",
		SessionID:        "sess-cmd-4",
	})
	require.NoError(t, err)

	stats, statsErr := ms.Stats(ctx)
	require.NoError(t, statsErr)
	assert.Equal(t, int64(0), stats.TotalMemories)
}
