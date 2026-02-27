package tests

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/recall"
)

func TestRecallRecencyScore_ZeroTime(t *testing.T) {
	r := recall.NewRecaller(recall.DefaultWeights(), newTestLogger())

	results := []models.SearchResult{
		{Memory: models.Memory{
			ID:          "zero-time",
			Type:        models.MemoryTypeFact,
			Scope:       models.ScopePermanent,
			AccessCount: 0,
			// LastAccessed is zero (time.Time{})
		}, Score: 0.8},
	}

	ranked := r.Rank(results, "")
	require.Len(t, ranked, 1)
	// Zero last accessed should use a low default recency score (0.1)
	assert.InDelta(t, 0.1, ranked[0].RecencyScore, 0.001)
}

func TestRecallRecencyScore_FutureTime(t *testing.T) {
	// LastAccessed in the future should clamp to 0 hours and give max score
	r := recall.NewRecaller(recall.DefaultWeights(), newTestLogger())

	results := []models.SearchResult{
		{Memory: models.Memory{
			ID:           "future",
			Type:         models.MemoryTypeFact,
			Scope:        models.ScopePermanent,
			LastAccessed: time.Now().Add(24 * time.Hour), // future
			AccessCount:  1,
		}, Score: 0.8},
	}

	ranked := r.Rank(results, "")
	require.Len(t, ranked, 1)
	// Future time = 0 hours ago = recency score near 1.0
	assert.InDelta(t, 1.0, ranked[0].RecencyScore, 0.01)
}

func TestRecallTypeBoostScore_UnknownType(t *testing.T) {
	// A memory with an unknown type should get default boost (1.0/1.5)
	r := recall.NewRecaller(recall.DefaultWeights(), newTestLogger())

	now := time.Now().UTC()
	results := []models.SearchResult{
		{Memory: models.Memory{
			ID:           "unknown-type",
			Type:         models.MemoryType("unknown"),
			Scope:        models.ScopePermanent,
			LastAccessed: now,
			AccessCount:  1,
		}, Score: 0.8},
	}

	ranked := r.Rank(results, "")
	require.Len(t, ranked, 1)
	// Unknown type should get default 1.0/1.5 boost
	assert.InDelta(t, 1.0/1.5, ranked[0].TypeBoost, 0.001)
}

func TestRecallScopeBoostScore_NoProject(t *testing.T) {
	// When project is empty, all memories get default scope boost
	r := recall.NewRecaller(recall.DefaultWeights(), newTestLogger())

	now := time.Now().UTC()
	results := []models.SearchResult{
		{Memory: models.Memory{
			ID:           "project-scoped",
			Type:         models.MemoryTypeFact,
			Scope:        models.ScopeProject,
			Project:      "some-project",
			LastAccessed: now,
			AccessCount:  1,
		}, Score: 0.8},
	}

	ranked := r.Rank(results, "") // empty project
	require.Len(t, ranked, 1)
	// When project is empty, raw = 1.0, normalized = 1.0/1.5
	assert.InDelta(t, 1.0/1.5, ranked[0].ScopeBoost, 0.001)
}

func TestRecallScopeBoostScore_WrongProject(t *testing.T) {
	// Project-scoped memory for a different project should get low scope boost
	r := recall.NewRecaller(recall.DefaultWeights(), newTestLogger())

	now := time.Now().UTC()
	results := []models.SearchResult{
		{Memory: models.Memory{
			ID:           "wrong-project",
			Type:         models.MemoryTypeFact,
			Scope:        models.ScopeProject,
			Project:      "project-a",
			LastAccessed: now,
			AccessCount:  1,
		}, Score: 0.8},
	}

	ranked := r.Rank(results, "project-b") // different project
	require.Len(t, ranked, 1)
	// Wrong project: raw = 0.8, normalized = 0.8/1.5
	assert.InDelta(t, 0.8/1.5, ranked[0].ScopeBoost, 0.001)
}

func TestRecallScopeBoostScore_PermanentWithProject(t *testing.T) {
	// Permanent scope should get 1.0 scope boost even when querying with a project
	r := recall.NewRecaller(recall.DefaultWeights(), newTestLogger())

	now := time.Now().UTC()
	results := []models.SearchResult{
		{Memory: models.Memory{
			ID:           "permanent",
			Type:         models.MemoryTypeFact,
			Scope:        models.ScopePermanent,
			LastAccessed: now,
			AccessCount:  1,
		}, Score: 0.8},
	}

	ranked := r.Rank(results, "any-project")
	require.Len(t, ranked, 1)
	// Permanent scope: raw = 1.0, normalized = 1.0/1.5
	assert.InDelta(t, 1.0/1.5, ranked[0].ScopeBoost, 0.001)
}

func TestRecallWeightsValidate_AllZero(t *testing.T) {
	w := recall.Weights{
		Similarity: 0,
		Recency:    0,
		Frequency:  0,
		TypeBoost:  0,
		ScopeBoost: 0,
	}
	err := w.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "1.0")
}

func TestRecallWeightsValidate_NegativeWeight(t *testing.T) {
	w := recall.Weights{
		Similarity: -0.5,
		Recency:    0.5,
		Frequency:  0.3,
		TypeBoost:  0.4,
		ScopeBoost: 0.3,
	}
	err := w.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "similarity")
}

func TestRecallRankSortOrder(t *testing.T) {
	// Items should be sorted descending by FinalScore
	r := recall.NewRecaller(recall.DefaultWeights(), newTestLogger())

	now := time.Now().UTC()
	results := []models.SearchResult{
		{Memory: models.Memory{
			ID: "low", Type: models.MemoryTypePreference, Scope: models.ScopePermanent, LastAccessed: now, AccessCount: 1,
		}, Score: 0.1},
		{Memory: models.Memory{
			ID: "high", Type: models.MemoryTypeRule, Scope: models.ScopePermanent, LastAccessed: now, AccessCount: 100,
		}, Score: 0.95},
		{Memory: models.Memory{
			ID: "mid", Type: models.MemoryTypeFact, Scope: models.ScopePermanent, LastAccessed: now, AccessCount: 10,
		}, Score: 0.6},
	}

	ranked := r.Rank(results, "")
	require.Len(t, ranked, 3)

	// Verify strict descending order
	for i := 1; i < len(ranked); i++ {
		assert.GreaterOrEqual(t, ranked[i-1].FinalScore, ranked[i].FinalScore,
			"results should be sorted descending by final score")
	}
}
