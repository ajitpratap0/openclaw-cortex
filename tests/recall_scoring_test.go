package tests

import (
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/recall"
)

func newTestLoggerRecall() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestRecallRankEmpty(t *testing.T) {
	r := recall.NewRecaller(recall.DefaultWeights(), newTestLoggerRecall())
	results := r.Rank(nil, "", "")
	assert.Empty(t, results)
}

func TestRecallRankByTypeBoost(t *testing.T) {
	r := recall.NewRecaller(recall.DefaultWeights(), newTestLoggerRecall())

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

	ranked := r.Rank(results, "", "")
	require.Len(t, ranked, 2)
	// Rule has type boost 1.5, episode has 0.8 — rule should rank higher
	assert.Equal(t, "rule1", ranked[0].Memory.ID)
}

func TestRecallRecencyDecay(t *testing.T) {
	r := recall.NewRecaller(recall.DefaultWeights(), newTestLoggerRecall())

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

	ranked := r.Rank(results, "", "")
	require.Len(t, ranked, 2)
	// Both IDs should be present in results
	ids := []string{ranked[0].Memory.ID, ranked[1].Memory.ID}
	assert.Contains(t, ids, "old")
	assert.Contains(t, ids, "new")
	assert.Equal(t, "new", ranked[0].Memory.ID, "newer memory should rank first due to recency")
	assert.Equal(t, "old", ranked[1].Memory.ID, "older memory should rank second")
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
				Similarity:    0.35,
				Recency:       0.15,
				Frequency:     0.10,
				TypeBoost:     0.10,
				ScopeBoost:    0.08,
				Confidence:    0.10,
				Reinforcement: 0.07,
				TagAffinity:   0.05,
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
	r := recall.NewRecaller(badWeights, newTestLoggerRecall())
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
	ranked := r.Rank(results, "", "")
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
	r := recall.NewRecaller(recall.DefaultWeights(), newTestLoggerRecall())

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

	ranked := r.Rank(results, "", "")
	require.Len(t, ranked, 2)
	// Procedure (1.3) > Fact (1.0) in type priority, so procedure ranks first
	assert.Equal(t, "proc1", ranked[0].Memory.ID)
}

func TestRecallRankOrderingPreferenceVsEpisode(t *testing.T) {
	r := recall.NewRecaller(recall.DefaultWeights(), newTestLoggerRecall())

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

	ranked := r.Rank(results, "", "")
	require.Len(t, ranked, 2)
	// Episode (0.8) > Preference (0.7) in type priority, so episode ranks first
	assert.Equal(t, "ep1", ranked[0].Memory.ID)
}

func TestRecallRankScopeProjectBoost(t *testing.T) {
	r := recall.NewRecaller(recall.DefaultWeights(), newTestLoggerRecall())

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

	ranked := r.Rank(results, "mycortex", "")
	require.Len(t, ranked, 2)
	// Project-scoped memory should beat global when project matches
	assert.Equal(t, "proj", ranked[0].Memory.ID)
}

func TestRecallRankFrequencyBoost(t *testing.T) {
	r := recall.NewRecaller(recall.DefaultWeights(), newTestLoggerRecall())

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

	ranked := r.Rank(results, "", "")
	require.Len(t, ranked, 2)
	// Frequent memory should rank higher due to frequency score
	assert.Equal(t, "frequent", ranked[0].Memory.ID)
}

func TestRecallRankZeroAccessCount(t *testing.T) {
	r := recall.NewRecaller(recall.DefaultWeights(), newTestLoggerRecall())

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

	ranked := r.Rank(results, "", "")
	require.Len(t, ranked, 1)
	// Should not panic on zero access count, frequency score should be 0
	assert.Equal(t, "never-accessed", ranked[0].Memory.ID)
	assert.InDelta(t, 0.0, ranked[0].FrequencyScore, 1e-9)
}

func TestRecallRankZeroLastAccessed(t *testing.T) {
	r := recall.NewRecaller(recall.DefaultWeights(), newTestLoggerRecall())

	results := []models.SearchResult{
		{Memory: models.Memory{
			ID:    "zero-time",
			Type:  models.MemoryTypeFact,
			Scope: models.ScopePermanent,
			// LastAccessed is zero value
		}, Score: 0.75},
	}

	ranked := r.Rank(results, "", "")
	require.Len(t, ranked, 1)
	// Zero last accessed should return a low but valid recency score (0.1 per the implementation)
	assert.Equal(t, "zero-time", ranked[0].Memory.ID)
	assert.InDelta(t, 0.1, ranked[0].RecencyScore, 0.001)
}

func TestRecaller_ShouldRerank(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	r := recall.NewRecaller(recall.DefaultWeights(), logger)
	tight := []models.RecallResult{
		{Memory: models.Memory{ID: "a"}, FinalScore: 0.80},
		{Memory: models.Memory{ID: "b"}, FinalScore: 0.75},
		{Memory: models.Memory{ID: "c"}, FinalScore: 0.72},
		{Memory: models.Memory{ID: "d"}, FinalScore: 0.68},
	}
	assert.True(t, r.ShouldRerank(tight, 0.15))
	clear := []models.RecallResult{
		{Memory: models.Memory{ID: "a"}, FinalScore: 0.90},
		{Memory: models.Memory{ID: "b"}, FinalScore: 0.60},
		{Memory: models.Memory{ID: "c"}, FinalScore: 0.55},
		{Memory: models.Memory{ID: "d"}, FinalScore: 0.50},
	}
	assert.False(t, r.ShouldRerank(clear, 0.15))
	// Fewer than 4 results: never rerank
	assert.False(t, r.ShouldRerank(tight[:2], 0.15))
	// Zero threshold: never rerank
	assert.False(t, r.ShouldRerank(tight, 0))
}

func TestRecallFinalScoreIsWeightedSum(t *testing.T) {
	weights := recall.DefaultWeights()
	r := recall.NewRecaller(weights, newTestLoggerRecall())

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

	ranked := r.Rank(results, "", "")
	require.Len(t, ranked, 1)

	rr := ranked[0]
	expected := weights.Similarity*rr.SimilarityScore +
		weights.Recency*rr.RecencyScore +
		weights.Frequency*rr.FrequencyScore +
		weights.TypeBoost*rr.TypeBoost +
		weights.ScopeBoost*rr.ScopeBoost +
		weights.Confidence*rr.ConfidenceScore +
		weights.Reinforcement*rr.ReinforcementScore +
		weights.TagAffinity*rr.TagAffinityScore
	expected *= rr.SupersessionPenalty * rr.ConflictPenalty

	assert.InDelta(t, expected, rr.FinalScore, 0.0001)
}

// baseMemory returns a Memory with standard fields set so tests only need to
// override the field under test. Confidence defaults to 0.9 so it does not
// trigger the legacy-zero substitution.
func baseMemory(id string, now time.Time) models.Memory {
	return models.Memory{
		ID:           id,
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopePermanent,
		Confidence:   0.9,
		LastAccessed: now,
		AccessCount:  5,
	}
}

func TestConfidenceScoreInRanking(t *testing.T) {
	r := recall.NewRecaller(recall.DefaultWeights(), newTestLoggerRecall())
	now := time.Now().UTC()

	highConf := baseMemory("high-conf", now)
	highConf.Confidence = 0.9

	lowConf := baseMemory("low-conf", now)
	lowConf.Confidence = 0.5

	results := []models.SearchResult{
		{Memory: lowConf, Score: 0.85},
		{Memory: highConf, Score: 0.85},
	}

	ranked := r.Rank(results, "", "")
	require.Len(t, ranked, 2)
	assert.Equal(t, "high-conf", ranked[0].Memory.ID, "higher confidence should rank first")
	assert.Greater(t, ranked[0].ConfidenceScore, ranked[1].ConfidenceScore)
}

func TestConfidenceZeroTreatedAsUnknown(t *testing.T) {
	r := recall.NewRecaller(recall.DefaultWeights(), newTestLoggerRecall())
	now := time.Now().UTC()

	// Confidence == 0 (legacy) should be treated as 0.7
	legacy := baseMemory("legacy", now)
	legacy.Confidence = 0

	explicit := baseMemory("explicit", now)
	explicit.Confidence = 0.5

	results := []models.SearchResult{
		{Memory: explicit, Score: 0.85},
		{Memory: legacy, Score: 0.85},
	}

	ranked := r.Rank(results, "", "")
	require.Len(t, ranked, 2)

	// Find each result by ID
	var legacyResult, explicitResult models.RecallResult
	for i := range ranked {
		switch ranked[i].Memory.ID {
		case "legacy":
			legacyResult = ranked[i]
		case "explicit":
			explicitResult = ranked[i]
		}
	}

	// Legacy (0 -> 0.7) should score higher than explicit 0.5
	assert.InDelta(t, 0.7, legacyResult.ConfidenceScore, 0.001)
	assert.InDelta(t, 0.5, explicitResult.ConfidenceScore, 0.001)
	assert.Greater(t, legacyResult.FinalScore, explicitResult.FinalScore,
		"legacy (0->0.7) should rank higher than explicit 0.5")
}

func TestReinforcementScoreInRanking(t *testing.T) {
	r := recall.NewRecaller(recall.DefaultWeights(), newTestLoggerRecall())
	now := time.Now().UTC()

	reinforced := baseMemory("reinforced", now)
	reinforced.ReinforcedCount = 10

	fresh := baseMemory("fresh", now)
	fresh.ReinforcedCount = 0

	results := []models.SearchResult{
		{Memory: fresh, Score: 0.85},
		{Memory: reinforced, Score: 0.85},
	}

	ranked := r.Rank(results, "", "")
	require.Len(t, ranked, 2)
	assert.Equal(t, "reinforced", ranked[0].Memory.ID, "reinforced memory should rank first")
	assert.Greater(t, ranked[0].ReinforcementScore, ranked[1].ReinforcementScore)
	assert.InDelta(t, 0.0, ranked[1].ReinforcementScore, 0.001, "zero reinforcement -> score 0")
}

func TestReinforcementScoreSaturation(t *testing.T) {
	r := recall.NewRecaller(recall.DefaultWeights(), newTestLoggerRecall())
	now := time.Now().UTC()

	mem31 := baseMemory("count-31", now)
	mem31.ReinforcedCount = 31

	mem100 := baseMemory("count-100", now)
	mem100.ReinforcedCount = 100

	results := []models.SearchResult{
		{Memory: mem31, Score: 0.85},
		{Memory: mem100, Score: 0.85},
	}

	ranked := r.Rank(results, "", "")
	require.Len(t, ranked, 2)

	var score31, score100 float64
	for i := range ranked {
		switch ranked[i].Memory.ID {
		case "count-31":
			score31 = ranked[i].ReinforcementScore
		case "count-100":
			score100 = ranked[i].ReinforcementScore
		}
	}

	// Both should be at or near 1.0 (saturated)
	assert.InDelta(t, 1.0, score31, 0.01, "31 reinforcements should saturate near 1.0")
	assert.InDelta(t, 1.0, score100, 0.01, "100 reinforcements should saturate at 1.0")
	// They should be essentially equal (both capped)
	assert.InDelta(t, score31, score100, 0.01, "saturation means both score the same")
}

func TestTagAffinityInRanking(t *testing.T) {
	r := recall.NewRecaller(recall.DefaultWeights(), newTestLoggerRecall())
	now := time.Now().UTC()

	memA := baseMemory("tag-match", now)
	memA.Tags = []string{"golang", "testing"}

	memB := baseMemory("no-tag-match", now)
	memB.Tags = []string{"python", "ml"}

	results := []models.SearchResult{
		{Memory: memB, Score: 0.85},
		{Memory: memA, Score: 0.85},
	}

	ranked := r.Rank(results, "", "golang testing best practices")
	require.Len(t, ranked, 2)
	assert.Equal(t, "tag-match", ranked[0].Memory.ID, "tag-matched memory should rank first")
	assert.Greater(t, ranked[0].TagAffinityScore, ranked[1].TagAffinityScore)
}

func TestTagAffinityCaseInsensitive(t *testing.T) {
	r := recall.NewRecaller(recall.DefaultWeights(), newTestLoggerRecall())
	now := time.Now().UTC()

	mem := baseMemory("case-tag", now)
	mem.Tags = []string{"golang", "testing"}

	results := []models.SearchResult{
		{Memory: mem, Score: 0.85},
	}

	ranked := r.Rank(results, "", "GoLang TESTING")
	require.Len(t, ranked, 1)
	// Both tags match (case-insensitive) -> 2/2 = 1.0
	assert.InDelta(t, 1.0, ranked[0].TagAffinityScore, 0.001)
}

func TestTagAffinityMultiWordTag(t *testing.T) {
	r := recall.NewRecaller(recall.DefaultWeights(), newTestLoggerRecall())
	now := time.Now().UTC()

	mem := baseMemory("multiword-tag", now)
	mem.Tags = []string{"ci pipeline"}

	results := []models.SearchResult{
		{Memory: mem, Score: 0.85},
	}

	ranked := r.Rank(results, "", "ci pipeline deployment")
	require.Len(t, ranked, 1)
	// Multi-word tag "ci pipeline" — both words found in query -> 1/1 = 1.0
	assert.InDelta(t, 1.0, ranked[0].TagAffinityScore, 0.001)
}

func TestTagAffinityEmptyQuery(t *testing.T) {
	r := recall.NewRecaller(recall.DefaultWeights(), slog.Default())
	now := time.Now().UTC()

	results := []models.SearchResult{
		{
			Memory: models.Memory{
				ID:           "tagged",
				Type:         models.MemoryTypeRule,
				Scope:        models.ScopePermanent,
				Content:      "some rule",
				Confidence:   0.9,
				Tags:         []string{"golang", "testing"},
				CreatedAt:    now,
				UpdatedAt:    now,
				LastAccessed: now,
			},
			Score: 0.85,
		},
	}

	// Empty query should yield 0 tag affinity, not panic
	ranked := r.Rank(results, "", "")
	require.Len(t, ranked, 1)
	assert.InDelta(t, 0.0, ranked[0].TagAffinityScore, 1e-9, "empty query should yield 0 tag affinity")
}

func TestTagAffinityNoTags(t *testing.T) {
	r := recall.NewRecaller(recall.DefaultWeights(), newTestLoggerRecall())
	now := time.Now().UTC()

	mem := baseMemory("no-tags", now)
	mem.Tags = nil

	results := []models.SearchResult{
		{Memory: mem, Score: 0.85},
	}

	ranked := r.Rank(results, "", "golang testing")
	require.Len(t, ranked, 1)
	assert.InDelta(t, 0.0, ranked[0].TagAffinityScore, 0.001, "no tags should give 0 tag affinity")
}

func TestSupersessionPenalty(t *testing.T) {
	r := recall.NewRecaller(recall.DefaultWeights(), newTestLoggerRecall())
	now := time.Now().UTC()

	memA := baseMemory("A", now)
	memA.SupersedesID = "B" // A supersedes B

	memB := baseMemory("B", now)
	memB.SupersedesID = "C" // B supersedes C

	memC := baseMemory("C", now)
	// C has no SupersedesID

	results := []models.SearchResult{
		{Memory: memA, Score: 0.85},
		{Memory: memB, Score: 0.85},
		{Memory: memC, Score: 0.85},
	}

	ranked := r.Rank(results, "", "")
	require.Len(t, ranked, 3)

	penaltyByID := make(map[string]float64)
	for i := range ranked {
		penaltyByID[ranked[i].Memory.ID] = ranked[i].SupersessionPenalty
	}

	// A is not superseded by anyone in the result set
	assert.InDelta(t, 1.0, penaltyByID["A"], 0.001, "A (newest) should have no penalty")
	// B is superseded by A
	assert.InDelta(t, recall.SupersessionPenaltyFactor, penaltyByID["B"], 0.001, "B should be penalized")
	// C is superseded by B
	assert.InDelta(t, recall.SupersessionPenaltyFactor, penaltyByID["C"], 0.001, "C should be penalized")

	// A should rank highest
	assert.Equal(t, "A", ranked[0].Memory.ID, "A should rank first (no penalty)")
}

func TestSupersessionOnlyInResultSet(t *testing.T) {
	r := recall.NewRecaller(recall.DefaultWeights(), newTestLoggerRecall())
	now := time.Now().UTC()

	// A supersedes B, but B is not in the result set
	memA := baseMemory("A", now)
	memA.SupersedesID = "B"

	results := []models.SearchResult{
		{Memory: memA, Score: 0.85},
	}

	ranked := r.Rank(results, "", "")
	require.Len(t, ranked, 1)
	// A should NOT be penalized (it's the replacement, not the replaced)
	assert.InDelta(t, 1.0, ranked[0].SupersessionPenalty, 0.001,
		"superseding memory should not be penalized when superseded is absent")
}

func TestConflictPenalty(t *testing.T) {
	r := recall.NewRecaller(recall.DefaultWeights(), newTestLoggerRecall())
	now := time.Now().UTC()

	active := baseMemory("active-conflict", now)
	active.ConflictStatus = models.ConflictStatusActive

	noConflict := baseMemory("no-conflict", now)
	noConflict.ConflictStatus = models.ConflictStatusNone

	resolved := baseMemory("resolved-conflict", now)
	resolved.ConflictStatus = models.ConflictStatusResolved

	results := []models.SearchResult{
		{Memory: active, Score: 0.85},
		{Memory: noConflict, Score: 0.85},
		{Memory: resolved, Score: 0.85},
	}

	ranked := r.Rank(results, "", "")
	require.Len(t, ranked, 3)

	penaltyByID := make(map[string]float64)
	for i := range ranked {
		penaltyByID[ranked[i].Memory.ID] = ranked[i].ConflictPenalty
	}

	assert.InDelta(t, recall.ConflictPenaltyFactor, penaltyByID["active-conflict"], 0.001,
		"active conflict should have 0.8 penalty")
	assert.InDelta(t, 1.0, penaltyByID["no-conflict"], 0.001,
		"no conflict should have 1.0 penalty (no penalty)")
	assert.InDelta(t, 1.0, penaltyByID["resolved-conflict"], 0.001,
		"resolved conflict should have 1.0 penalty (no penalty)")
}

func TestPenaltiesStack(t *testing.T) {
	r := recall.NewRecaller(recall.DefaultWeights(), newTestLoggerRecall())
	now := time.Now().UTC()

	// Memory that is both superseded AND has active conflict
	superseded := baseMemory("superseded-conflict", now)
	superseded.ConflictStatus = models.ConflictStatusActive

	// The superseding memory
	superseder := baseMemory("superseder", now)
	superseder.SupersedesID = "superseded-conflict"

	results := []models.SearchResult{
		{Memory: superseded, Score: 0.85},
		{Memory: superseder, Score: 0.85},
	}

	ranked := r.Rank(results, "", "")
	require.Len(t, ranked, 2)

	var penalizedResult models.RecallResult
	for i := range ranked {
		if ranked[i].Memory.ID == "superseded-conflict" {
			penalizedResult = ranked[i]
		}
	}

	assert.InDelta(t, recall.SupersessionPenaltyFactor, penalizedResult.SupersessionPenalty, 0.001)
	assert.InDelta(t, recall.ConflictPenaltyFactor, penalizedResult.ConflictPenalty, 0.001)

	// Compute expected final score: weightedSum * 0.3 * 0.8
	weights := recall.DefaultWeights()
	weightedSum := weights.Similarity*penalizedResult.SimilarityScore +
		weights.Recency*penalizedResult.RecencyScore +
		weights.Frequency*penalizedResult.FrequencyScore +
		weights.TypeBoost*penalizedResult.TypeBoost +
		weights.ScopeBoost*penalizedResult.ScopeBoost +
		weights.Confidence*penalizedResult.ConfidenceScore +
		weights.Reinforcement*penalizedResult.ReinforcementScore +
		weights.TagAffinity*penalizedResult.TagAffinityScore
	expectedFinal := weightedSum * recall.SupersessionPenaltyFactor * recall.ConflictPenaltyFactor

	assert.InDelta(t, expectedFinal, penalizedResult.FinalScore, 0.0001,
		"final score should be weightedSum * 0.3 * 0.8")
}

func TestNewDefaultWeightsSum(t *testing.T) {
	w := recall.DefaultWeights()

	sum := w.Similarity + w.Recency + w.Frequency + w.TypeBoost +
		w.ScopeBoost + w.Confidence + w.Reinforcement + w.TagAffinity
	assert.InDelta(t, 1.0, sum, 0.01, "default weights should sum to 1.0")

	// Verify all 8 weights are positive
	assert.Greater(t, w.Similarity, 0.0, "Similarity weight must be positive")
	assert.Greater(t, w.Recency, 0.0, "Recency weight must be positive")
	assert.Greater(t, w.Frequency, 0.0, "Frequency weight must be positive")
	assert.Greater(t, w.TypeBoost, 0.0, "TypeBoost weight must be positive")
	assert.Greater(t, w.ScopeBoost, 0.0, "ScopeBoost weight must be positive")
	assert.Greater(t, w.Confidence, 0.0, "Confidence weight must be positive")
	assert.Greater(t, w.Reinforcement, 0.0, "Reinforcement weight must be positive")
	assert.Greater(t, w.TagAffinity, 0.0, "TagAffinity weight must be positive")
}

func TestFullRankingWithAllFactors(t *testing.T) {
	r := recall.NewRecaller(recall.DefaultWeights(), newTestLoggerRecall())
	now := time.Now().UTC()

	// Best: high confidence, reinforced, tags match, no penalties
	best := baseMemory("best", now)
	best.Confidence = 0.95
	best.ReinforcedCount = 15
	best.Tags = []string{"golang", "testing"}
	best.Type = models.MemoryTypeRule

	// Good: decent confidence, some reinforcement, partial tag match
	good := baseMemory("good", now)
	good.Confidence = 0.8
	good.ReinforcedCount = 3
	good.Tags = []string{"golang", "docker"}
	good.Type = models.MemoryTypeFact

	// Mid: low confidence, no reinforcement, no tag match
	mid := baseMemory("mid", now)
	mid.Confidence = 0.5
	mid.ReinforcedCount = 0
	mid.Tags = []string{"python"}
	mid.Type = models.MemoryTypeEpisode

	// Worst: superseded AND active conflict
	worst := baseMemory("worst", now)
	worst.Confidence = 0.5
	worst.ReinforcedCount = 0
	worst.Tags = []string{"python"}
	worst.Type = models.MemoryTypeEpisode
	worst.ConflictStatus = models.ConflictStatusActive

	// The superseder that makes "worst" penalized
	superseder := baseMemory("superseder", now)
	superseder.SupersedesID = "worst"
	superseder.Confidence = 0.6
	superseder.Type = models.MemoryTypeEpisode

	results := []models.SearchResult{
		{Memory: worst, Score: 0.85},
		{Memory: mid, Score: 0.85},
		{Memory: good, Score: 0.85},
		{Memory: best, Score: 0.85},
		{Memory: superseder, Score: 0.85},
	}

	ranked := r.Rank(results, "", "golang testing best practices")
	require.Len(t, ranked, 5)

	// Best should be first (high confidence + reinforced + tag match + rule type)
	assert.Equal(t, "best", ranked[0].Memory.ID, "best should rank first")

	// Worst should be last (superseded + active conflict)
	assert.Equal(t, "worst", ranked[len(ranked)-1].Memory.ID, "worst should rank last")

	// Verify all scores are positive and non-NaN
	for i := range ranked {
		assert.Greater(t, ranked[i].FinalScore, 0.0, "final score should be positive for %s", ranked[i].Memory.ID)
		assert.False(t, ranked[i].FinalScore != ranked[i].FinalScore, "final score should not be NaN")
	}

	// Verify descending order
	for i := 1; i < len(ranked); i++ {
		assert.GreaterOrEqual(t, ranked[i-1].FinalScore, ranked[i].FinalScore,
			"results should be sorted descending by final score")
	}
}
