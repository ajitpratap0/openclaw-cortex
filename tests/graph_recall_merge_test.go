package tests

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/graph"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/recall"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

// --- minimal stub graph clients ---

// fixedGraphClient returns a pre-configured list of memory IDs.
type fixedGraphClient struct {
	graph.Client // embed interface to satisfy non-recalled methods
	ids          []string
}

func (f *fixedGraphClient) RecallByGraph(_ context.Context, _ string, _ []float32, _ int) ([]string, error) {
	return f.ids, nil
}

// slowGraphClient sleeps longer than any reasonable budget to simulate timeout.
type slowGraphClient struct {
	graph.Client
}

func (s *slowGraphClient) RecallByGraph(ctx context.Context, _ string, _ []float32, _ int) ([]string, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(10 * time.Second):
		return nil, nil
	}
}

// --- helpers ---

func newGraphRecallLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func populatedStore(t *testing.T, memories []models.Memory) store.Store {
	t.Helper()
	ms := store.NewMockStore()
	for i := range memories {
		require.NoError(t, ms.Upsert(context.Background(), memories[i], []float32{0.1, 0.2, 0.3}))
	}
	return ms
}

// --- tests ---

// TestRecallWithGraphMerge verifies that graph-only memories are merged into
// Qdrant results without duplicates.
func TestRecallWithGraphMerge(t *testing.T) {
	mem1 := models.Memory{
		ID:         "mem-1",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Content:    "Qdrant memory one",
		Confidence: 0.9,
	}
	mem2 := models.Memory{
		ID:         "mem-2",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Content:    "Qdrant memory two (also returned by graph)",
		Confidence: 0.8,
	}
	mem3 := models.Memory{
		ID:         "mem-3",
		Type:       models.MemoryTypeRule,
		Scope:      models.ScopePermanent,
		Content:    "Graph-only memory",
		Confidence: 0.85,
	}

	// Qdrant returns mem-1 and mem-2; graph returns mem-2 (overlap) and mem-3 (new).
	qdrantResults := []models.SearchResult{
		{Memory: mem1, Score: 0.9},
		{Memory: mem2, Score: 0.8},
	}

	gc := &fixedGraphClient{ids: []string{"mem-2", "mem-3"}}
	st := populatedStore(t, []models.Memory{mem1, mem2, mem3})

	r := recall.NewRecaller(recall.DefaultWeights(), newGraphRecallLogger())
	r.SetGraphClient(gc, st, 500)

	results := r.RecallWithGraph(context.Background(), "test query", []float32{0.1, 0.2, 0.3}, qdrantResults, "")

	// All 3 unique memories should be present.
	require.Len(t, results, 3)

	// Build ID set for verification.
	ids := make(map[string]bool, len(results))
	for i := range results {
		ids[results[i].Memory.ID] = true
	}
	assert.True(t, ids["mem-1"], "mem-1 (qdrant-only) should be present")
	assert.True(t, ids["mem-2"], "mem-2 (overlap) should be present exactly once")
	assert.True(t, ids["mem-3"], "mem-3 (graph-only) should be present")
}

// TestRecallWithoutGraph verifies that a nil GraphClient falls back to Rank()
// on the provided qdrantResults without error.
func TestRecallWithoutGraph(t *testing.T) {
	mem1 := models.Memory{
		ID:         "no-graph-1",
		Type:       models.MemoryTypeRule,
		Scope:      models.ScopePermanent,
		Content:    "Rule memory",
		Confidence: 0.9,
	}
	mem2 := models.Memory{
		ID:         "no-graph-2",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Content:    "Fact memory",
		Confidence: 0.7,
	}

	qdrantResults := []models.SearchResult{
		{Memory: mem1, Score: 0.75},
		{Memory: mem2, Score: 0.80},
	}

	// No SetGraphClient call — graphClient remains nil.
	r := recall.NewRecaller(recall.DefaultWeights(), newGraphRecallLogger())

	results := r.RecallWithGraph(context.Background(), "query", nil, qdrantResults, "")

	require.Len(t, results, 2)
	// Rule should rank higher due to type boost (1.5×) + higher confidence (0.9 vs 0.7)
	// even though similarity is slightly lower (0.75 vs 0.80).
	assert.Equal(t, "no-graph-1", results[0].Memory.ID, "rule should rank first")
}

// TestRecallGraphTimeout verifies that a slow graph client that exceeds the
// latency budget causes the recaller to fall back gracefully to Qdrant-only results.
func TestRecallGraphTimeout(t *testing.T) {
	mem1 := models.Memory{
		ID:         "timeout-1",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Content:    "Qdrant result under timeout",
		Confidence: 0.8,
	}

	qdrantResults := []models.SearchResult{
		{Memory: mem1, Score: 0.8},
	}

	gc := &slowGraphClient{}
	st := store.NewMockStore()

	r := recall.NewRecaller(recall.DefaultWeights(), newGraphRecallLogger())
	r.SetGraphClient(gc, st, 50) // 50ms budget — slowGraphClient takes 10s

	start := time.Now()
	results := r.RecallWithGraph(context.Background(), "query", nil, qdrantResults, "")
	elapsed := time.Since(start)

	// Should return before the slow client's 10-second sleep.
	assert.Less(t, elapsed, 2*time.Second, "should not wait for slow graph client")

	// Qdrant-only result should still be returned.
	require.Len(t, results, 1)
	assert.Equal(t, "timeout-1", results[0].Memory.ID)
}
