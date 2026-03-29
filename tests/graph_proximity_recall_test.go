package tests

// graph_proximity_recall_test.go — Phase D: RankWithGraphProximity + community sweep tests.
//
// Tests:
//  1. TestRankWithGraphProximity_Boost — memory with proximity 1.0 ranks above one with 0.0.
//  2. TestRankWithGraphProximity_NilMap — nil proximityMap behaves identically to Rank().
//  3. TestRecallWithGraph_ProximityMapBuilt — proximityMap is correctly built from hop distances.

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/recall"
)

// hopsTrackingClient is a test-only graph.Client that implements
// depthRecallerWithHops, returning a pre-configured hop-distance map.
type hopsTrackingClient struct {
	// embed the base depthTrackingClient so we satisfy graph.Client.
	depthTrackingClient
	// hopMap maps memoryID → hop distance (1 or 2).
	hopMap map[string]int
}

// RecallByGraphWithHops satisfies the depthRecallerWithHops interface in recall.go.
func (h *hopsTrackingClient) RecallByGraphWithHops(
	_ context.Context, _ string, _ []float32, _ int, _ int,
) ([]string, map[string]int, error) {
	ids := make([]string, 0, len(h.hopMap))
	for id := range h.hopMap {
		ids = append(ids, id)
	}
	return ids, h.hopMap, nil
}

// TestRankWithGraphProximity_Boost verifies that a memory with proximity 1.0
// ranks above an otherwise identical memory with proximity 0.0.
func TestRankWithGraphProximity_Boost(t *testing.T) {
	r := recall.NewRecaller(recall.DefaultWeights(), newTestLoggerRecall())

	now := time.Now().UTC()
	// Both memories are identical in all dimensions except graph proximity.
	nearMem := models.Memory{
		ID:           "near",
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopePermanent,
		Confidence:   0.8,
		LastAccessed: now,
		AccessCount:  5,
	}
	farMem := models.Memory{
		ID:           "far",
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopePermanent,
		Confidence:   0.8,
		LastAccessed: now,
		AccessCount:  5,
	}

	results := []models.SearchResult{
		{Memory: nearMem, Score: 0.85},
		{Memory: farMem, Score: 0.85},
	}

	// nearMem is 1-hop (proximity=1.0); farMem has no graph connection (0.0).
	proximityMap := map[string]float64{
		"near": 1.0,
		"far":  0.0,
	}

	ranked := r.RankWithGraphProximity(results, "", "query", proximityMap)
	require.Len(t, ranked, 2)

	assert.Equal(t, "near", ranked[0].Memory.ID,
		"memory with proximity 1.0 should rank above memory with proximity 0.0")
	assert.Greater(t, ranked[0].GraphProximityScore, ranked[1].GraphProximityScore,
		"graph proximity score should be higher for the 1-hop memory")
	assert.InDelta(t, 1.0, ranked[0].GraphProximityScore, 0.001)
	assert.InDelta(t, 0.0, ranked[1].GraphProximityScore, 0.001)
}

// TestRankWithGraphProximity_NilMap verifies that passing a nil proximityMap
// produces the same ranking as calling Rank() directly.
func TestRankWithGraphProximity_NilMap(t *testing.T) {
	r := recall.NewRecaller(recall.DefaultWeights(), newTestLoggerRecall())

	now := time.Now().UTC()
	memA := models.Memory{
		ID:           "rule-a",
		Type:         models.MemoryTypeRule,
		Scope:        models.ScopePermanent,
		Confidence:   0.9,
		LastAccessed: now,
		AccessCount:  3,
	}
	memB := models.Memory{
		ID:           "fact-b",
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopePermanent,
		Confidence:   0.7,
		LastAccessed: now,
		AccessCount:  3,
	}

	results := []models.SearchResult{
		{Memory: memB, Score: 0.85},
		{Memory: memA, Score: 0.85},
	}

	rankResult := r.Rank(results, "", "test query")
	proximityResult := r.RankWithGraphProximity(results, "", "test query", nil)

	require.Len(t, rankResult, 2)
	require.Len(t, proximityResult, 2)

	// Order must be identical.
	for i := range rankResult {
		assert.Equal(t, rankResult[i].Memory.ID, proximityResult[i].Memory.ID,
			"nil proximityMap should produce same ordering as Rank()")
		assert.InDelta(t, rankResult[i].FinalScore, proximityResult[i].FinalScore, 0.0001,
			"nil proximityMap should produce same final scores as Rank()")
		assert.InDelta(t, 0.0, proximityResult[i].GraphProximityScore, 0.001,
			"nil proximityMap should yield 0 graph proximity score")
	}
}

// TestRecallWithGraph_ProximityMapBuilt verifies that when a graph client implements
// the depthRecallerWithHops interface, RecallWithGraph correctly builds a proximity
// map (1-hop → 1.0, 2-hop → 0.5) and applies it to the final ranking.
func TestRecallWithGraph_ProximityMapBuilt(t *testing.T) {
	// mem-hop1: 1-hop memory — should get proximity 1.0
	hop1Mem := models.Memory{
		ID:         "mem-hop1",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Content:    "1-hop memory",
		Confidence: 0.8,
	}
	// mem-hop2: 2-hop memory — should get proximity 0.5
	hop2Mem := models.Memory{
		ID:         "mem-hop2",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Content:    "2-hop memory",
		Confidence: 0.8,
	}
	// mem-vec: vector-only memory — no graph proximity (0.0)
	vecMem := models.Memory{
		ID:         "mem-vec",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Content:    "vector-only memory",
		Confidence: 0.8,
	}

	vectorResults := []models.SearchResult{
		{Memory: vecMem, Score: 0.85},
	}

	hopMap := map[string]int{
		"mem-hop1": 1,
		"mem-hop2": 2,
	}

	gc := &hopsTrackingClient{
		depthTrackingClient: depthTrackingClient{
			hop1IDs: []string{"mem-hop1"},
			hop2IDs: []string{"mem-hop2"},
		},
		hopMap: hopMap,
	}
	st := populatedStore(t, []models.Memory{hop1Mem, hop2Mem, vecMem})

	r := recall.NewRecaller(recall.DefaultWeights(), newGraphRecallLogger())
	r.SetGraphClient(gc, st, 500)
	r.SetGraphDepth(2)

	results := r.RecallWithGraph(context.Background(), "test query", []float32{0.1, 0.2}, vectorResults, "")

	require.Len(t, results, 3, "all 3 memories (1-hop, 2-hop, vector-only) must be present")

	// Find each result by ID and verify graph proximity scores.
	scoreByID := make(map[string]models.RecallResult, len(results))
	for i := range results {
		scoreByID[results[i].Memory.ID] = results[i]
	}

	hop1Result, ok := scoreByID["mem-hop1"]
	require.True(t, ok, "mem-hop1 must be in results")
	assert.InDelta(t, 1.0, hop1Result.GraphProximityScore, 0.001,
		"1-hop memory should have proximity score 1.0")

	hop2Result, ok := scoreByID["mem-hop2"]
	require.True(t, ok, "mem-hop2 must be in results")
	assert.InDelta(t, 0.5, hop2Result.GraphProximityScore, 0.001,
		"2-hop memory should have proximity score 0.5")

	vecResult, ok := scoreByID["mem-vec"]
	require.True(t, ok, "mem-vec must be in results")
	assert.InDelta(t, 0.0, vecResult.GraphProximityScore, 0.001,
		"vector-only memory (no graph hop) should have proximity score 0.0")

	// Verify that the 1-hop memory ranks above the 2-hop memory due to higher proximity.
	assert.Greater(t, hop1Result.GraphProximityScore, hop2Result.GraphProximityScore,
		"1-hop proximity (1.0) should exceed 2-hop proximity (0.5)")
}
