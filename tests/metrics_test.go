package tests

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/classifier"
	"github.com/ajitpratap0/openclaw-cortex/internal/hooks"
	"github.com/ajitpratap0/openclaw-cortex/internal/metrics"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

// newTestLogger returns a silent logger suitable for use in tests.
func newTestLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// metricsMockCapturer is a test double for capture.Capturer.
type metricsMockCapturer struct {
	memories []models.CapturedMemory
}

func (m *metricsMockCapturer) Extract(_ context.Context, _, _ string) ([]models.CapturedMemory, error) {
	return m.memories, nil
}

// metricsMockEmbedder returns a fixed vector for any input.
type metricsMockEmbedder struct{}

func (m *metricsMockEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return []float32{0.1, 0.2, 0.3}, nil
}

func (m *metricsMockEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	result := make([][]float32, len(texts))
	for i := range texts {
		result[i] = []float32{0.1, 0.2, 0.3}
	}
	return result, nil
}

func (m *metricsMockEmbedder) Dimension() int {
	return 3
}

func TestMetricsCaptureIncrement(t *testing.T) {
	before := metrics.CaptureTotal.Value()

	st := store.NewMockStore()
	logger := newTestLogger(t)
	cls := classifier.NewClassifier(logger)
	cap := &metricsMockCapturer{memories: []models.CapturedMemory{
		{Content: "test memory", Type: models.MemoryTypeFact, Confidence: 0.9},
	}}
	emb := &metricsMockEmbedder{}

	hook := hooks.NewPostTurnHook(cap, cls, emb, st, logger, 0.95)
	err := hook.Execute(context.Background(), hooks.PostTurnInput{
		UserMessage:      "test",
		AssistantMessage: "response",
		SessionID:        uuid.New().String(),
	})
	require.NoError(t, err)

	assert.Greater(t, metrics.CaptureTotal.Value(), before)
}

func TestMetricsDedupSkip(t *testing.T) {
	before := metrics.DedupSkipped.Value()

	st := store.NewMockStore()
	logger := newTestLogger(t)
	cls := classifier.NewClassifier(logger)
	// Same content twice — second call should be dedup'd because vector is identical
	// and the package-level dedupThreshold is 0.95 (cosine similarity of 1.0 exceeds it).
	cap := &metricsMockCapturer{memories: []models.CapturedMemory{
		{Content: "duplicate memory", Type: models.MemoryTypeFact, Confidence: 0.9},
	}}
	emb := &metricsMockEmbedder{}

	hook := hooks.NewPostTurnHook(cap, cls, emb, st, logger, 0.95)

	// Pre-populate with the same vector so FindDuplicates returns a match.
	_ = st.Upsert(context.Background(), models.Memory{
		ID: uuid.New().String(), Content: "duplicate memory",
		Type: models.MemoryTypeFact, Scope: models.ScopeSession,
		Visibility:   models.VisibilityPrivate,
		CreatedAt:    time.Now(), UpdatedAt: time.Now(), LastAccessed: time.Now(),
	}, []float32{0.1, 0.2, 0.3})

	err := hook.Execute(context.Background(), hooks.PostTurnInput{
		UserMessage:      "test",
		AssistantMessage: "response",
		SessionID:        uuid.New().String(),
	})
	require.NoError(t, err)

	assert.Greater(t, metrics.DedupSkipped.Value(), before)
}
