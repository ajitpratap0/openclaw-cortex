package tests

import (
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/cortex/internal/models"
	"github.com/ajitpratap0/cortex/internal/recall"
)

func TestRecaller_Rank(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	r := recall.NewRecaller(recall.DefaultWeights(), logger)

	now := time.Now().UTC()

	results := []models.SearchResult{
		{
			Memory: models.Memory{
				ID:           "r-1",
				Type:         models.MemoryTypeFact,
				Content:      "A plain fact",
				LastAccessed: now.Add(-48 * time.Hour),
				AccessCount:  2,
			},
			Score: 0.85,
		},
		{
			Memory: models.Memory{
				ID:           "r-2",
				Type:         models.MemoryTypeRule,
				Content:      "An important rule",
				LastAccessed: now.Add(-1 * time.Hour),
				AccessCount:  10,
			},
			Score: 0.80,
		},
		{
			Memory: models.Memory{
				ID:           "r-3",
				Type:         models.MemoryTypeEpisode,
				Content:      "Something that happened",
				LastAccessed: now.Add(-168 * time.Hour),
				AccessCount:  1,
			},
			Score: 0.90,
		},
	}

	ranked := r.Rank(results, "")
	require.Len(t, ranked, 3)

	// Rule should be boosted due to type priority (1.5) and recency (1h ago)
	// Even though episode has highest similarity, rule should rank higher
	assert.Equal(t, "r-2", ranked[0].Memory.ID, "rule should rank first due to type boost + recency")
}

func TestRecaller_RankWithProjectBoost(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	r := recall.NewRecaller(recall.DefaultWeights(), logger)

	now := time.Now().UTC()

	results := []models.SearchResult{
		{
			Memory: models.Memory{
				ID:           "p-1",
				Type:         models.MemoryTypeFact,
				Scope:        models.ScopeProject,
				Project:      "cortex",
				Content:      "Cortex-specific fact",
				LastAccessed: now,
				AccessCount:  1,
			},
			Score: 0.80,
		},
		{
			Memory: models.Memory{
				ID:           "p-2",
				Type:         models.MemoryTypeFact,
				Scope:        models.ScopePermanent,
				Content:      "General fact",
				LastAccessed: now,
				AccessCount:  1,
			},
			Score: 0.82,
		},
	}

	ranked := r.Rank(results, "cortex")
	require.Len(t, ranked, 2)

	// Project-scoped memory should be boosted when project matches
	assert.Equal(t, "p-1", ranked[0].Memory.ID, "project-scoped memory should rank first with matching project")
}

func TestRecaller_RecencyDecay(t *testing.T) {
	tests := []struct {
		name        string
		hoursAgo    float64
		expectAbove float64
		expectBelow float64
	}{
		{"just now", 0, 0.99, 1.01},
		{"1 hour ago", 1, 0.99, 1.0},
		{"1 day ago", 24, 0.85, 0.95},
		{"1 week ago", 168, 0.45, 0.55},
		{"1 month ago", 720, 0.01, 0.15},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	r := recall.NewRecaller(recall.DefaultWeights(), logger)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Now().UTC()
			results := []models.SearchResult{
				{
					Memory: models.Memory{
						ID:           "decay-test",
						Type:         models.MemoryTypeFact,
						LastAccessed: now.Add(-time.Duration(tt.hoursAgo) * time.Hour),
					},
					Score: 1.0,
				},
			}

			ranked := r.Rank(results, "")
			require.Len(t, ranked, 1)
			assert.Greater(t, ranked[0].RecencyScore, tt.expectAbove)
			assert.Less(t, ranked[0].RecencyScore, tt.expectBelow)
		})
	}
}

func TestRecaller_TypePriority(t *testing.T) {
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
