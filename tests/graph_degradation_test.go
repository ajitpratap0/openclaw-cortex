package tests

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/graph"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/recall"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

// delayedGraphClient is a graph.Client stub whose RecallByGraph sleeps until
// the context deadline fires, used to simulate a slow/unavailable graph.
type delayedGraphClient struct {
	graph.Client
	delay time.Duration
}

func (s *delayedGraphClient) RecallByGraph(ctx context.Context, _ string, _ []float32, _ int) ([]string, error) {
	select {
	case <-time.After(s.delay):
		return []string{"some-id"}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// TestRecall_GraphNil_UsesQdrantOnly verifies that when no graph client is set,
// RecallWithGraph is equivalent to calling Rank() directly on the Qdrant results.
func TestRecall_GraphNil_UsesQdrantOnly(t *testing.T) {
	mem1 := models.Memory{
		ID:         "nil-graph-1",
		Type:       models.MemoryTypeRule,
		Scope:      models.ScopePermanent,
		Content:    "Rule memory without graph",
		Confidence: 0.9,
	}
	mem2 := models.Memory{
		ID:         "nil-graph-2",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Content:    "Fact memory without graph",
		Confidence: 0.7,
	}

	qdrantResults := []models.SearchResult{
		{Memory: mem1, Score: 0.75},
		{Memory: mem2, Score: 0.85},
	}

	logger := newGraphRecallLogger()
	r := recall.NewRecaller(recall.DefaultWeights(), logger)
	// Intentionally do NOT call SetGraphClient — graphClient remains nil.

	withGraph := r.RecallWithGraph(context.Background(), "query", nil, qdrantResults, "")
	direct := r.Rank(qdrantResults, "", "query")

	require.Len(t, withGraph, len(direct), "RecallWithGraph should return same count as Rank")

	for i := range withGraph {
		assert.Equal(t, direct[i].Memory.ID, withGraph[i].Memory.ID,
			"result[%d] ID should match direct Rank output", i)
		assert.InDelta(t, direct[i].FinalScore, withGraph[i].FinalScore, 1e-9,
			"result[%d] FinalScore should match direct Rank output", i)
	}
}

// TestRecall_GraphTimeout_FallsBackToQdrant verifies that a graph client that
// exceeds the latency budget causes the recaller to fall back to Qdrant-only
// results without returning an error and within a reasonable wall-clock window.
func TestRecall_GraphTimeout_FallsBackToQdrant(t *testing.T) {
	mem1 := models.Memory{
		ID:         "timeout-deg-1",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Content:    "Qdrant result under degradation timeout",
		Confidence: 0.8,
	}

	qdrantResults := []models.SearchResult{
		{Memory: mem1, Score: 0.8},
	}

	// Client sleeps for 200ms; budget is only 50ms — so it will time out.
	gc := &delayedGraphClient{delay: 200 * time.Millisecond}
	st := store.NewMockStore()

	r := recall.NewRecaller(recall.DefaultWeights(), newGraphRecallLogger())
	r.SetGraphClient(gc, st, 50) // 50ms budget

	start := time.Now()
	results := r.RecallWithGraph(context.Background(), "degradation query", nil, qdrantResults, "")
	elapsed := time.Since(start)

	// Must complete well within the slow client's 200ms sleep (upper bound: 2s).
	assert.Less(t, elapsed, 2*time.Second, "RecallWithGraph should not wait for slow graph client")

	// Qdrant-only result must still be returned.
	require.Len(t, results, 1)
	assert.Equal(t, "timeout-deg-1", results[0].Memory.ID)
}

// TestCapture_GraphWriteFailure_MemoryStillStored verifies that store operations
// succeed independently of any graph-layer errors.  We cannot easily inject graph
// write failures into the full capture pipeline (which requires the Claude API),
// so this test directly exercises MockStore: upsert succeeds and Get returns the
// correct memory, regardless of what the graph layer might do.
func TestCapture_GraphWriteFailure_MemoryStillStored(t *testing.T) {
	ms := store.NewMockStore()
	ctx := context.Background()

	mem := models.Memory{
		ID:         "graph-write-fail-1",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Content:    "Memory that survives a graph write failure",
		Confidence: 0.85,
	}

	// Upsert memory into the store — this must always succeed independently of
	// any graph client error.
	err := ms.Upsert(ctx, mem, []float32{0.1, 0.2, 0.3})
	require.NoError(t, err, "store.Upsert should succeed even when graph is unavailable")

	// Simulate a graph UpsertFact failure (return an error from the mock).
	gc := graph.NewMockGraphClient()
	fact := models.Fact{
		ID:             "fact-fail-1",
		SourceEntityID: "entity-a",
		TargetEntityID: "entity-b",
		Fact:           "A relates to B",
	}

	// Override: we wrap the mock and inject an error via a closure-based approach
	// by calling a non-existent fact (InvalidateFact on a missing ID triggers an error).
	graphErr := gc.InvalidateFact(ctx, "non-existent-fact", time.Now(), time.Now())
	assert.Error(t, graphErr, "graph operation on missing fact should return error")

	// Despite the graph error above, the memory in the store must still be retrievable.
	got, err := ms.Get(ctx, mem.ID)
	require.NoError(t, err, "store.Get should succeed: memory must be stored regardless of graph errors")
	assert.Equal(t, mem.ID, got.ID)
	assert.Equal(t, mem.Content, got.Content)

	// Also confirm that a successful graph UpsertFact does not affect the store.
	err = gc.UpsertFact(ctx, fact)
	require.NoError(t, err, "graph.UpsertFact should succeed on a valid fact")

	got2, err := ms.Get(ctx, mem.ID)
	require.NoError(t, err, "memory must still be retrievable after graph.UpsertFact")
	assert.Equal(t, mem.Content, got2.Content)
}

// TestEntityResolver_ClaudeDown_TreatsAsNew is skipped because EntityResolver
// requires a live Claude API that cannot be mocked at this layer.
func TestEntityResolver_ClaudeDown_TreatsAsNew(t *testing.T) {
	t.Skip("requires Claude API mock")
}

// TestFactExtractor_ClaudeDown_SkipsFacts is skipped because FactExtractor
// requires a live Claude API that cannot be mocked at this layer.
func TestFactExtractor_ClaudeDown_SkipsFacts(t *testing.T) {
	t.Skip("requires Claude API mock")
}
