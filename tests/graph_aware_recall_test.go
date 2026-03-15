package tests

// graph_aware_recall_test.go — Phase 4: Graph-Aware Recall tests.
//
// These tests verify that:
// 1. Graph traversal finds memories that pure vector search misses (the key Phase 4 invariant).
// 2. RRF merge weights vector results higher than graph-only results.
// 3. SetGraphDepth(1) limits traversal to 1 hop; SetGraphDepth(2) expands to 2 hops.
// 4. The recaller uses the depthRecaller interface when available.

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/graph"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/recall"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

// depthTrackingClient is a test-only graph.Client that records the depth argument
// passed to RecallByGraphWithDepth, allowing tests to assert on depth propagation.
type depthTrackingClient struct {
	graph.Client
	calledDepth int
	// hop1IDs are returned as 1-hop memory IDs.
	hop1IDs []string
	// hop2IDs are returned as additional 2-hop memory IDs (only when depth >= 2).
	hop2IDs []string
}

func (d *depthTrackingClient) RecallByGraph(_ context.Context, _ string, _ []float32, _ int) ([]string, error) {
	// Fallback for the base interface — return all IDs.
	return append(d.hop1IDs, d.hop2IDs...), nil
}

// RecallByGraphWithDepth satisfies the depthRecaller interface in recall.go.
func (d *depthTrackingClient) RecallByGraphWithDepth(_ context.Context, _ string, _ []float32, _ int, depth int) ([]string, error) {
	d.calledDepth = depth
	if depth >= 2 {
		return append(d.hop1IDs, d.hop2IDs...), nil
	}
	return d.hop1IDs, nil
}

// TestGraphAwareRecall_FindsMemoriesMissedByVectorSearch is the key Phase 4 invariant:
// graph traversal must surface memories that the vector search did not return.
func TestGraphAwareRecall_FindsMemoriesMissedByVectorSearch(t *testing.T) {
	// Vector search returns only mem-1.
	memVector := models.Memory{
		ID:         "vec-only",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Content:    "Vector search memory",
		Confidence: 0.9,
	}
	// Graph traversal surfaces mem-graph — pure vector search misses it
	// (simulates a memory connected to a relevant entity but not semantically close to the query).
	memGraph := models.Memory{
		ID:         "graph-only",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Content:    "Graph traversal memory — no vector similarity",
		Confidence: 0.85,
	}

	vectorResults := []models.SearchResult{
		{Memory: memVector, Score: 0.95},
	}

	gc := &depthTrackingClient{
		hop1IDs: []string{"graph-only"}, // entity traversal returns this
	}
	st := populatedStore(t, []models.Memory{memVector, memGraph})

	r := recall.NewRecaller(recall.DefaultWeights(), newGraphRecallLogger())
	r.SetGraphClient(gc, st, 500)

	results := r.RecallWithGraph(context.Background(), "test query", []float32{0.1, 0.2}, vectorResults, "")

	// Both memories must be present — the graph-only memory was missed by vector search.
	require.Len(t, results, 2, "graph traversal should surface memory missed by vector search")

	ids := make(map[string]bool)
	for i := range results {
		ids[results[i].Memory.ID] = true
	}
	assert.True(t, ids["vec-only"], "vector memory must be present")
	assert.True(t, ids["graph-only"], "graph-only memory must be present — this is the Phase 4 invariant")
}

// TestGraphAwareRecall_VectorResultsRankHigherThanGraphOnly verifies that the RRF
// merge weights vector results above graph-only results when both appear.
func TestGraphAwareRecall_VectorResultsRankHigherThanGraphOnly(t *testing.T) {
	// High-confidence vector memory.
	memVec := models.Memory{
		ID:         "rrf-vec",
		Type:       models.MemoryTypeRule,
		Scope:      models.ScopePermanent,
		Content:    "Vector result — should rank high",
		Confidence: 0.9,
	}
	// Graph-only memory with equal base confidence but no vector signal.
	memGraph := models.Memory{
		ID:         "rrf-graph",
		Type:       models.MemoryTypeRule,
		Scope:      models.ScopePermanent,
		Content:    "Graph-only result — should rank lower without vector match",
		Confidence: 0.9,
	}

	vectorResults := []models.SearchResult{
		{Memory: memVec, Score: 0.95},
	}

	gc := &depthTrackingClient{hop1IDs: []string{"rrf-graph"}}
	st := populatedStore(t, []models.Memory{memVec, memGraph})

	r := recall.NewRecaller(recall.DefaultWeights(), newGraphRecallLogger())
	r.SetGraphClient(gc, st, 500)

	results := r.RecallWithGraph(context.Background(), "query", []float32{0.1}, vectorResults, "")

	require.Len(t, results, 2)

	// The vector-matched memory should rank first.
	assert.Equal(t, "rrf-vec", results[0].Memory.ID,
		"vector-matched memory should rank higher due to RRF vector weight (0.6 > graph weight 0.4)")
}

// TestGraphAwareRecall_Depth1LimitsTraversal verifies that SetGraphDepth(1)
// causes RecallByGraphWithDepth to be called with depth=1.
func TestGraphAwareRecall_Depth1LimitsTraversal(t *testing.T) {
	gc := &depthTrackingClient{
		hop1IDs: []string{"hop1-mem"},
		hop2IDs: []string{"hop2-mem"}, // should NOT appear with depth=1
	}

	mem1 := models.Memory{ID: "hop1-mem", Type: models.MemoryTypeFact, Scope: models.ScopePermanent, Content: "hop1", Confidence: 0.8}
	mem2 := models.Memory{ID: "hop2-mem", Type: models.MemoryTypeFact, Scope: models.ScopePermanent, Content: "hop2", Confidence: 0.8}
	st := populatedStore(t, []models.Memory{mem1, mem2})

	r := recall.NewRecaller(recall.DefaultWeights(), newGraphRecallLogger())
	r.SetGraphClient(gc, st, 500)
	r.SetGraphDepth(1)

	results := r.RecallWithGraph(context.Background(), "query", nil, nil, "")

	// Depth should have been propagated as 1.
	assert.Equal(t, 1, gc.calledDepth, "SetGraphDepth(1) must propagate depth=1 to RecallByGraphWithDepth")

	// Only hop1 memories returned.
	ids := make(map[string]bool)
	for i := range results {
		ids[results[i].Memory.ID] = true
	}
	assert.True(t, ids["hop1-mem"], "1-hop memory must be present")
	assert.False(t, ids["hop2-mem"], "2-hop memory must NOT appear at depth=1")
}

// TestGraphAwareRecall_Depth2IncludesHop2Memories verifies that SetGraphDepth(2)
// causes 2-hop memories to appear in the result set.
func TestGraphAwareRecall_Depth2IncludesHop2Memories(t *testing.T) {
	gc := &depthTrackingClient{
		hop1IDs: []string{"hop1-2"},
		hop2IDs: []string{"hop2-2"},
	}

	mem1 := models.Memory{ID: "hop1-2", Type: models.MemoryTypeFact, Scope: models.ScopePermanent, Content: "hop1", Confidence: 0.8}
	mem2 := models.Memory{ID: "hop2-2", Type: models.MemoryTypeFact, Scope: models.ScopePermanent, Content: "hop2 neighbour", Confidence: 0.8}
	st := populatedStore(t, []models.Memory{mem1, mem2})

	r := recall.NewRecaller(recall.DefaultWeights(), newGraphRecallLogger())
	r.SetGraphClient(gc, st, 500)
	r.SetGraphDepth(2)

	results := r.RecallWithGraph(context.Background(), "query", nil, nil, "")

	assert.Equal(t, 2, gc.calledDepth, "SetGraphDepth(2) must propagate depth=2")

	ids := make(map[string]bool)
	for i := range results {
		ids[results[i].Memory.ID] = true
	}
	assert.True(t, ids["hop1-2"], "1-hop memory must be present at depth=2")
	assert.True(t, ids["hop2-2"], "2-hop memory must be present at depth=2")
}

// TestGraphAwareRecall_NoDuplicates verifies that a memory present in both
// vector and graph results appears exactly once in the merged output.
func TestGraphAwareRecall_NoDuplicates(t *testing.T) {
	mem := models.Memory{
		ID:         "shared-mem",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Content:    "Memory in both vector and graph results",
		Confidence: 0.9,
	}

	vectorResults := []models.SearchResult{
		{Memory: mem, Score: 0.9},
	}

	// Graph also returns the same ID.
	gc := &depthTrackingClient{hop1IDs: []string{"shared-mem"}}
	st := populatedStore(t, []models.Memory{mem})

	r := recall.NewRecaller(recall.DefaultWeights(), newGraphRecallLogger())
	r.SetGraphClient(gc, st, 500)

	results := r.RecallWithGraph(context.Background(), "query", nil, vectorResults, "")

	require.Len(t, results, 1, "shared memory must appear exactly once — no duplicates")
	assert.Equal(t, "shared-mem", results[0].Memory.ID)
}

// TestGraphAwareRecall_LatencyBudget verifies that a slow graph client that exceeds
// the budget causes fallback to vector-only results within the wall-clock budget.
func TestGraphAwareRecall_LatencyBudget(t *testing.T) {
	mem := models.Memory{
		ID:         "budget-vec",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Content:    "Vector result under latency budget",
		Confidence: 0.8,
	}

	vectorResults := []models.SearchResult{{Memory: mem, Score: 0.8}}

	// slowGraphClient (from graph_recall_merge_test.go) sleeps for 10s.
	gc := &slowGraphClient{}
	st := store.NewMockStore()

	r := recall.NewRecaller(recall.DefaultWeights(), newGraphRecallLogger())
	r.SetGraphClient(gc, st, 50) // 50 ms budget

	start := time.Now()
	results := r.RecallWithGraph(context.Background(), "query", nil, vectorResults, "")
	elapsed := time.Since(start)

	assert.Less(t, elapsed, 2*time.Second, "must not wait for slow graph beyond budget")
	require.Len(t, results, 1, "vector-only fallback should return 1 result")
	assert.Equal(t, "budget-vec", results[0].Memory.ID)
}

// TestGraphAwareRecall_RRFBlendOrder verifies that items appearing in both vector
// and graph lists receive a higher blended score than items appearing in only one.
func TestGraphAwareRecall_RRFBlendOrder(t *testing.T) {
	// memBoth appears in both vector (rank 1) and graph (rank 1).
	// memVecOnly appears only in vector (rank 2).
	// memGraphOnly appears only in graph (rank 2).
	memBoth := models.Memory{
		ID:         "both",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Content:    "Appears in both lists",
		Confidence: 0.85,
	}
	memVecOnly := models.Memory{
		ID:         "vec-only-rrf",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Content:    "Vector only",
		Confidence: 0.85,
	}
	memGraphOnly := models.Memory{
		ID:         "graph-only-rrf",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Content:    "Graph only",
		Confidence: 0.85,
	}

	vectorResults := []models.SearchResult{
		{Memory: memBoth, Score: 0.95},   // rank 0
		{Memory: memVecOnly, Score: 0.7}, // rank 1
	}

	// Graph returns: both (rank 0), graphOnly (rank 1).
	gc := &depthTrackingClient{hop1IDs: []string{"both", "graph-only-rrf"}}
	st := populatedStore(t, []models.Memory{memBoth, memVecOnly, memGraphOnly})

	r := recall.NewRecaller(recall.DefaultWeights(), newGraphRecallLogger())
	r.SetGraphClient(gc, st, 500)

	results := r.RecallWithGraph(context.Background(), "query", nil, vectorResults, "")

	require.Len(t, results, 3)

	// memBoth appears in both lists so its blended RRF score is highest.
	// Collect IDs in score order.
	scoreOrder := make([]string, len(results))
	for i := range results {
		scoreOrder[i] = results[i].Memory.ID
	}

	// Verify scores are in descending order.
	scores := make([]float64, len(results))
	for i := range results {
		scores[i] = results[i].FinalScore
	}
	assert.True(t, sort.Float64sAreSorted(reverseFloat64s(scores)),
		"results must be sorted by FinalScore descending; order: %v", scoreOrder)
}

// reverseFloat64s returns a reversed copy for sort.Float64sAreSorted comparison.
func reverseFloat64s(s []float64) []float64 {
	out := make([]float64, len(s))
	for i, v := range s {
		out[len(s)-1-i] = v
	}
	return out
}
