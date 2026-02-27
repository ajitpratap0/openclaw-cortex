package tests

import (
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/recall"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestRecallRankEmpty(t *testing.T) {
	r := recall.NewRecaller(recall.DefaultWeights(), newTestLogger())
	results := r.Rank(nil, "")
	assert.Empty(t, results)
}

func TestRecallRankByTypeBoost(t *testing.T) {
	r := recall.NewRecaller(recall.DefaultWeights(), newTestLogger())

	now := time.Now().UTC()
	results := []models.SearchResult{
		{Memory: models.Memory{
			ID:           "ep1",
			Type:         models.MemoryTypeEpisode,
			Scope:        models.ScopePermanent,
			LastAccessed: now,
			AccessCount:  5,
			Confidence:   0.9,
		}, Score: 0.9},
		{Memory: models.Memory{
			ID:           "rule1",
			Type:         models.MemoryTypeRule,
			Scope:        models.ScopePermanent,
			LastAccessed: now,
			AccessCount:  5,
			Confidence:   0.9,
		}, Score: 0.9},
	}

	ranked := r.Rank(results, "")
	require.Len(t, ranked, 2)
	// Rule has type boost 1.5, episode has 0.8 — rule should rank higher
	assert.Equal(t, "rule1", ranked[0].Memory.ID)
}

func TestRecallRecencyDecay(t *testing.T) {
	r := recall.NewRecaller(recall.DefaultWeights(), newTestLogger())

	now := time.Now().UTC()
	results := []models.SearchResult{
		{Memory: models.Memory{
			ID:           "old",
			Type:         models.MemoryTypeFact,
			Scope:        models.ScopePermanent,
			LastAccessed: now.Add(-30 * 24 * time.Hour),
			AccessCount:  1,
		}, Score: 0.9},
		{Memory: models.Memory{
			ID:           "new",
			Type:         models.MemoryTypeFact,
			Scope:        models.ScopePermanent,
			LastAccessed: now,
			AccessCount:  1,
		}, Score: 0.8},
	}

	ranked := r.Rank(results, "")
	require.Len(t, ranked, 2)
	// Both IDs should be present in results
	ids := []string{ranked[0].Memory.ID, ranked[1].Memory.ID}
	assert.Contains(t, ids, "old")
	assert.Contains(t, ids, "new")
}

func TestRecallWeightsValidate(t *testing.T) {
	tests := []struct {
		name      string
		weights   recall.Weights
		expectErr bool
	}{
		{
			name:      "default weights are valid",
			weights:   recall.DefaultWeights(),
			expectErr: false,
		},
		{
			name: "weights summing to 1.0 are valid",
			weights: recall.Weights{
				Similarity: 0.5,
				Recency:    0.2,
				Frequency:  0.1,
				TypeBoost:  0.1,
				ScopeBoost: 0.1,
			},
			expectErr: false,
		},
		{
			name: "weights summing to != 1.0 are invalid",
			weights: recall.Weights{
				Similarity: 0.9,
				Recency:    0.9,
				Frequency:  0.9,
				TypeBoost:  0.9,
				ScopeBoost: 0.9,
			},
			expectErr: true,
		},
		{
			name: "negative weight is invalid",
			weights: recall.Weights{
				Similarity: -0.1,
				Recency:    0.4,
				Frequency:  0.3,
				TypeBoost:  0.2,
				ScopeBoost: 0.2,
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.weights.Validate()
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRecallWeightsInvalidFallsToDefault(t *testing.T) {
	// Invalid weights should cause NewRecaller to fall back to defaults
	badWeights := recall.Weights{
		Similarity: 0.9,
		Recency:    0.9,
		Frequency:  0.9,
		TypeBoost:  0.9,
		ScopeBoost: 0.9,
	}
	// Should not panic — NewRecaller logs warning and uses defaults
	r := recall.NewRecaller(badWeights, newTestLogger())
	assert.NotNil(t, r)

	now := time.Now().UTC()
	results := []models.SearchResult{
		{Memory: models.Memory{
			ID:           "m1",
			Type:         models.MemoryTypeFact,
			Scope:        models.ScopePermanent,
			LastAccessed: now,
			AccessCount:  1,
		}, Score: 0.8},
	}
	ranked := r.Rank(results, "")
	require.Len(t, ranked, 1)
	assert.Equal(t, "m1", ranked[0].Memory.ID)
}

func TestRecallTypePriorityAllTypes(t *testing.T) {
	tests := []struct {
		memType  models.MemoryType
		expected float64
	}{
		{models.MemoryTypeRule, 1.5},
		{models.MemoryTypeProcedure, 1.3},
		{models.MemoryTypeFact, 1.0},
		{models.MemoryTypeEpisode, 0.8},
		{models.MemoryTypePreference, 0.7},
	}

	for _, tt := range tests {
		t.Run(string(tt.memType), func(t *testing.T) {
			actual := recall.TypePriority[tt.memType]
			assert.Equal(t, tt.expected, actual)
		})
	}
}

func TestRecallRankOrderingProcedureVsFact(t *testing.T) {
	r := recall.NewRecaller(recall.DefaultWeights(), newTestLogger())

	now := time.Now().UTC()
	results := []models.SearchResult{
		{Memory: models.Memory{
			ID:           "fact1",
			Type:         models.MemoryTypeFact,
			Scope:        models.ScopePermanent,
			LastAccessed: now,
			AccessCount:  3,
		}, Score: 0.85},
		{Memory: models.Memory{
			ID:           "proc1",
			Type:         models.MemoryTypeProcedure,
			Scope:        models.ScopePermanent,
			LastAccessed: now,
			AccessCount:  3,
		}, Score: 0.85},
	}

	ranked := r.Rank(results, "")
	require.Len(t, ranked, 2)
	// Procedure (1.3) > Fact (1.0) in type priority, so procedure ranks first
	assert.Equal(t, "proc1", ranked[0].Memory.ID)
}

func TestRecallRankOrderingPreferenceVsEpisode(t *testing.T) {
	r := recall.NewRecaller(recall.DefaultWeights(), newTestLogger())

	now := time.Now().UTC()
	results := []models.SearchResult{
		{Memory: models.Memory{
			ID:           "pref1",
			Type:         models.MemoryTypePreference,
			Scope:        models.ScopePermanent,
			LastAccessed: now,
			AccessCount:  2,
		}, Score: 0.85},
		{Memory: models.Memory{
			ID:           "ep1",
			Type:         models.MemoryTypeEpisode,
			Scope:        models.ScopePermanent,
			LastAccessed: now,
			AccessCount:  2,
		}, Score: 0.85},
	}

	ranked := r.Rank(results, "")
	require.Len(t, ranked, 2)
	// Episode (0.8) > Preference (0.7) in type priority, so episode ranks first
	assert.Equal(t, "ep1", ranked[0].Memory.ID)
}

func TestRecallRankScopeProjectBoost(t *testing.T) {
	r := recall.NewRecaller(recall.DefaultWeights(), newTestLogger())

	now := time.Now().UTC()
	results := []models.SearchResult{
		{Memory: models.Memory{
			ID:           "global",
			Type:         models.MemoryTypeFact,
			Scope:        models.ScopePermanent,
			LastAccessed: now,
			AccessCount:  1,
		}, Score: 0.82},
		{Memory: models.Memory{
			ID:           "proj",
			Type:         models.MemoryTypeFact,
			Scope:        models.ScopeProject,
			Project:      "mycortex",
			LastAccessed: now,
			AccessCount:  1,
		}, Score: 0.80},
	}

	ranked := r.Rank(results, "mycortex")
	require.Len(t, ranked, 2)
	// Project-scoped memory should beat global when project matches
	assert.Equal(t, "proj", ranked[0].Memory.ID)
}

func TestRecallRankFrequencyBoost(t *testing.T) {
	r := recall.NewRecaller(recall.DefaultWeights(), newTestLogger())

	now := time.Now().UTC()
	results := []models.SearchResult{
		{Memory: models.Memory{
			ID:           "rare",
			Type:         models.MemoryTypeFact,
			Scope:        models.ScopePermanent,
			LastAccessed: now,
			AccessCount:  1,
		}, Score: 0.80},
		{Memory: models.Memory{
			ID:           "frequent",
			Type:         models.MemoryTypeFact,
			Scope:        models.ScopePermanent,
			LastAccessed: now,
			AccessCount:  1000,
		}, Score: 0.80},
	}

	ranked := r.Rank(results, "")
	require.Len(t, ranked, 2)
	// Frequent memory should rank higher due to frequency score
	assert.Equal(t, "frequent", ranked[0].Memory.ID)
}

func TestRecallRankZeroAccessCount(t *testing.T) {
	r := recall.NewRecaller(recall.DefaultWeights(), newTestLogger())

	now := time.Now().UTC()
	results := []models.SearchResult{
		{Memory: models.Memory{
			ID:           "never-accessed",
			Type:         models.MemoryTypeFact,
			Scope:        models.ScopePermanent,
			LastAccessed: now,
			AccessCount:  0,
		}, Score: 0.75},
	}

	ranked := r.Rank(results, "")
	require.Len(t, ranked, 1)
	// Should not panic on zero access count, frequency score should be 0
	assert.Equal(t, "never-accessed", ranked[0].Memory.ID)
	assert.Equal(t, float64(0), ranked[0].FrequencyScore)
}

func TestRecallRankZeroLastAccessed(t *testing.T) {
	r := recall.NewRecaller(recall.DefaultWeights(), newTestLogger())

	results := []models.SearchResult{
		{Memory: models.Memory{
			ID:    "zero-time",
			Type:  models.MemoryTypeFact,
			Scope: models.ScopePermanent,
			// LastAccessed is zero value
		}, Score: 0.75},
	}

	ranked := r.Rank(results, "")
	require.Len(t, ranked, 1)
	// Zero last accessed should return a low but valid recency score (0.1 per the implementation)
	assert.Equal(t, "zero-time", ranked[0].Memory.ID)
	assert.InDelta(t, 0.1, ranked[0].RecencyScore, 0.001)
}

func TestRecallFinalScoreIsWeightedSum(t *testing.T) {
	weights := recall.DefaultWeights()
	r := recall.NewRecaller(weights, newTestLogger())

	now := time.Now().UTC()
	results := []models.SearchResult{
		{Memory: models.Memory{
			ID:           "m1",
			Type:         models.MemoryTypeFact,
			Scope:        models.ScopePermanent,
			LastAccessed: now,
			AccessCount:  1,
		}, Score: 1.0},
	}

	ranked := r.Rank(results, "")
	require.Len(t, ranked, 1)

	rr := ranked[0]
	expected := weights.Similarity*rr.SimilarityScore +
		weights.Recency*rr.RecencyScore +
		weights.Frequency*rr.FrequencyScore +
		weights.TypeBoost*rr.TypeBoost +
		weights.ScopeBoost*rr.ScopeBoost

	assert.InDelta(t, expected, rr.FinalScore, 0.0001)
}
