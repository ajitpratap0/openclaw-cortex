package tests

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/recall"
)

// newTestRecallResult builds a minimal RecallResult for reasoner tests.
func newTestRecallResult(id, content string, score float64) models.RecallResult {
	now := time.Now().UTC()
	return models.RecallResult{
		Memory: models.Memory{
			ID:           id,
			Type:         models.MemoryTypeFact,
			Scope:        models.ScopePermanent,
			Content:      content,
			Confidence:   0.9,
			CreatedAt:    now,
			UpdatedAt:    now,
			LastAccessed: now,
		},
		SimilarityScore: score,
		FinalScore:      score,
	}
}

func TestReasoner_ReRank_EmptyResults(t *testing.T) {
	r := recall.NewReasoner("fake-key", "claude-haiku-4-5-20251001", slog.Default())
	results, err := r.ReRank(context.Background(), "test query", nil, 10)
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestReasoner_ReRank_EmptySlice(t *testing.T) {
	r := recall.NewReasoner("fake-key", "claude-haiku-4-5-20251001", slog.Default())
	results, err := r.ReRank(context.Background(), "test query", []models.RecallResult{}, 10)
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestReasoner_ReRank_InvalidAPIKey_DegradeGracefully(t *testing.T) {
	// With an invalid API key, the Reasoner must return the original order without error.
	r := recall.NewReasoner("invalid-key-xxx", "claude-haiku-4-5-20251001", slog.Default())

	input := []models.RecallResult{
		newTestRecallResult("id-1", "content one", 0.9),
		newTestRecallResult("id-2", "content two", 0.8),
		newTestRecallResult("id-3", "content three", 0.7),
	}

	results, err := r.ReRank(context.Background(), "test query", input, 10)
	// Must not return an error — graceful degradation is the contract.
	require.NoError(t, err)
	require.Len(t, results, 3)
	// Original order preserved on failure.
	assert.Equal(t, "id-1", results[0].Memory.ID)
	assert.Equal(t, "id-2", results[1].Memory.ID)
	assert.Equal(t, "id-3", results[2].Memory.ID)
}

func TestReasoner_ReRank_ZeroCandidates_UsesDefault(t *testing.T) {
	// maxCandidates=0 should use the internal default, not panic.
	r := recall.NewReasoner("invalid-key", "claude-haiku-4-5-20251001", slog.Default())
	input := []models.RecallResult{
		newTestRecallResult("id-1", "content", 0.9),
	}
	results, err := r.ReRank(context.Background(), "query", input, 0)
	require.NoError(t, err)
	assert.Len(t, results, 1)
}

func TestReasoner_ReRank_TailPreserved(t *testing.T) {
	// Results beyond maxCandidates should appear unchanged after the re-ranked set.
	r := recall.NewReasoner("invalid-key", "claude-haiku-4-5-20251001", slog.Default())

	input := make([]models.RecallResult, 5)
	for i := range input {
		input[i] = newTestRecallResult(
			"id-"+string(rune('A'+i)),
			"content",
			float64(5-i)*0.1,
		)
	}

	// With maxCandidates=3, the last 2 should appear at the end.
	results, err := r.ReRank(context.Background(), "query", input, 3)
	require.NoError(t, err)
	require.Len(t, results, 5)
	// The tail (indices 3,4) must be the last two results.
	assert.Equal(t, input[3].Memory.ID, results[3].Memory.ID)
	assert.Equal(t, input[4].Memory.ID, results[4].Memory.ID)
}

func TestReasoner_ReRank_Integration(t *testing.T) {
	// Integration test — only runs when ANTHROPIC_API_KEY is set.
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set — skipping integration test")
	}

	r := recall.NewReasoner(apiKey, "claude-haiku-4-5-20251001", slog.Default())
	input := []models.RecallResult{
		newTestRecallResult("id-1", "Python is a dynamically typed language", 0.7),
		newTestRecallResult("id-2", "Always deploy with --dry-run first in production", 0.8),
		newTestRecallResult("id-3", "Kubernetes uses RBAC for access control", 0.75),
	}

	results, err := r.ReRank(context.Background(), "how do I safely deploy to production?", input, 3)
	require.NoError(t, err)
	require.Len(t, results, 3)
	// The deployment-safety memory should be ranked first by Claude.
	assert.Equal(t, "id-2", results[0].Memory.ID)
}
