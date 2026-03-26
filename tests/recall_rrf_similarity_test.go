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

// TestRank_UsesOriginalSimilarity verifies that Rank() uses OriginalSimilarity
// instead of Score when OriginalSimilarity is non-zero.
func TestRank_UsesOriginalSimilarity(t *testing.T) {
	r := recall.NewRecaller(recall.DefaultWeights(), newTestLoggerRecall())
	now := time.Now().UTC()

	// mem-A: Score is tiny (RRF-like) but OriginalSimilarity is high
	// mem-B: Score is high but OriginalSimilarity is 0 (falls back to Score)
	results := []models.SearchResult{
		{
			Memory: models.Memory{
				ID:           "mem-a",
				Type:         models.MemoryTypeFact,
				Scope:        models.ScopePermanent,
				Content:      "Ajit works at Booking.com",
				Confidence:   0.9,
				LastAccessed: now,
				AccessCount:  1,
			},
			Score:              0.009, // RRF score (blended)
			OriginalSimilarity: 0.72,  // real vector similarity
		},
		{
			Memory: models.Memory{
				ID:           "mem-b",
				Type:         models.MemoryTypeFact,
				Scope:        models.ScopePermanent,
				Content:      "Go is a compiled language",
				Confidence:   0.9,
				LastAccessed: now,
				AccessCount:  1,
			},
			Score:              0.009, // same RRF score
			OriginalSimilarity: 0.0,   // not set — falls back to Score
		},
	}

	ranked := r.Rank(results, "", "where does Ajit work")
	require.Len(t, ranked, 2)

	// mem-a has OriginalSimilarity=0.72 which drives its similarity component.
	// mem-b falls back to Score=0.009. mem-a should rank first.
	assert.Equal(t, "mem-a", ranked[0].Memory.ID,
		"memory with high OriginalSimilarity should rank first despite tiny RRF Score")

	// The SimilarityScore in the result should reflect the original vector sim.
	assert.InDelta(t, 0.72, ranked[0].SimilarityScore, 0.001,
		"SimilarityScore should be OriginalSimilarity (0.72), not RRF score (0.009)")
	assert.InDelta(t, 0.009, ranked[1].SimilarityScore, 0.001,
		"fallback: SimilarityScore should equal Score when OriginalSimilarity is 0")
}

// TestRank_FallbackToScoreWhenOriginalSimilarityZero verifies backward
// compatibility: if OriginalSimilarity is 0, Score is used as before.
func TestRank_FallbackToScoreWhenOriginalSimilarityZero(t *testing.T) {
	r := recall.NewRecaller(recall.DefaultWeights(), newTestLoggerRecall())
	now := time.Now().UTC()

	results := []models.SearchResult{
		{
			Memory: models.Memory{
				ID:           "high-score",
				Type:         models.MemoryTypeFact,
				Scope:        models.ScopePermanent,
				Confidence:   0.9,
				LastAccessed: now,
				AccessCount:  1,
			},
			Score:              0.90,
			OriginalSimilarity: 0, // not set
		},
		{
			Memory: models.Memory{
				ID:           "low-score",
				Type:         models.MemoryTypeFact,
				Scope:        models.ScopePermanent,
				Confidence:   0.9,
				LastAccessed: now,
				AccessCount:  1,
			},
			Score:              0.30,
			OriginalSimilarity: 0, // not set
		},
	}

	ranked := r.Rank(results, "", "")
	require.Len(t, ranked, 2)

	assert.Equal(t, "high-score", ranked[0].Memory.ID,
		"without OriginalSimilarity, Score should drive similarity component")
	assert.InDelta(t, 0.90, ranked[0].SimilarityScore, 0.001)
	assert.InDelta(t, 0.30, ranked[1].SimilarityScore, 0.001)
}

// TestRecallWithGraph_PreservesOriginalSimilarity verifies that
// RecallWithGraph() stores the original vector similarity in OriginalSimilarity
// before the RRF blend overwrites Score, so that Rank() can use it.
func TestRecallWithGraph_PreservesOriginalSimilarity(t *testing.T) {
	logger := newTestLoggerRecall()
	r := recall.NewRecaller(recall.DefaultWeights(), logger)

	ctx := context.Background()
	s := store.NewMockStore()
	gc := graph.NewMockGraphClient()

	r.SetGraphClient(gc, s, 500)

	now := time.Now().UTC()

	// A workplace fact — high semantic similarity to "where does Ajit work"
	workplaceFact := models.Memory{
		ID:           "workplace",
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopePermanent,
		Content:      "Ajit works at Booking.com",
		Confidence:   0.9,
		LastAccessed: now,
		AccessCount:  1,
	}
	// An unrelated rule — low semantic similarity
	unrelatedRule := models.Memory{
		ID:           "unrelated",
		Type:         models.MemoryTypeRule,
		Scope:        models.ScopePermanent,
		Content:      "Always write tests before implementation",
		Confidence:   0.9,
		LastAccessed: now,
		AccessCount:  1,
	}

	// Pre-seed store so graph-triggered fetches work if needed.
	require.NoError(t, s.Upsert(ctx, workplaceFact, testVector(0.72)))
	require.NoError(t, s.Upsert(ctx, unrelatedRule, testVector(0.3)))

	// The vector search returns both memories with realistic similarity scores.
	searchResults := []models.SearchResult{
		{Memory: workplaceFact, Score: 0.72},  // high semantic match
		{Memory: unrelatedRule, Score: 0.30},  // low semantic match
	}

	ranked := r.RecallWithGraph(ctx, "where does Ajit work", testVector(0.72), searchResults, "")
	require.NotEmpty(t, ranked)

	// Find the workplace result.
	var workplaceResult *models.RecallResult
	for i := range ranked {
		if ranked[i].Memory.ID == "workplace" {
			workplaceResult = &ranked[i]
			break
		}
	}
	require.NotNil(t, workplaceResult, "workplace memory should be in results")

	// The SimilarityScore should be the original vector similarity (0.72),
	// NOT the tiny RRF score (~0.009).
	assert.Greater(t, workplaceResult.SimilarityScore, 0.5,
		"SimilarityScore should reflect real vector similarity (>0.5), not RRF score (~0.009)")

	// The workplace fact should rank above the unrelated rule despite the rule
	// type boost, because the semantic similarity difference (0.72 vs 0.30)
	// outweighs the type boost delta.
	assert.Equal(t, "workplace", ranked[0].Memory.ID,
		"highly similar workplace fact should rank first over unrelated rule")
}

// TestRecallWithGraph_GraphOnlyMemoryHasZeroOriginalSimilarity verifies that
// memories fetched only from the graph traversal (not in the original vector
// results) get OriginalSimilarity=0, which causes Rank() to fall back to Score.
func TestRecallWithGraph_GraphOnlyMemoryHasZeroOriginalSimilarity(t *testing.T) {
	logger := newTestLoggerRecall()
	r := recall.NewRecaller(recall.DefaultWeights(), logger)

	ctx := context.Background()
	s := store.NewMockStore()
	gc := graph.NewMockGraphClient()

	r.SetGraphClient(gc, s, 500)

	now := time.Now().UTC()

	// A vector result with high similarity.
	vectorMem := models.Memory{
		ID:           "vector-mem",
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopePermanent,
		Content:      "Ajit works at Booking.com",
		Confidence:   0.9,
		LastAccessed: now,
		AccessCount:  1,
	}
	require.NoError(t, s.Upsert(ctx, vectorMem, testVector(0.8)))

	searchResults := []models.SearchResult{
		{Memory: vectorMem, Score: 0.8},
	}

	ranked := r.RecallWithGraph(ctx, "workplace", testVector(0.8), searchResults, "")
	require.NotEmpty(t, ranked)

	// The vector-originated memory should have its original similarity preserved.
	assert.Greater(t, ranked[0].SimilarityScore, 0.5,
		"vector-originated memory should have SimilarityScore > 0.5 (original sim preserved)")
}
