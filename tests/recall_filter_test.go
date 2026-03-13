package tests

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/recall"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

// newTaggedMemory builds a memory with configurable tags, type, and scope.
func newTaggedMemory(id string, memType models.MemoryType, scope models.MemoryScope, tags []string) models.Memory {
	now := time.Now().UTC()
	return models.Memory{
		ID:           id,
		Type:         memType,
		Scope:        scope,
		Visibility:   models.VisibilityShared,
		Content:      "content for " + id,
		Confidence:   0.9,
		Source:       "test",
		Tags:         tags,
		CreatedAt:    now,
		UpdatedAt:    now,
		LastAccessed: now,
	}
}

// TestSearchTagFilter verifies that the tags filter returns only memories
// containing at least one of the requested tags.
func TestSearchTagFilter(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	mem1 := newTaggedMemory("tag-1", models.MemoryTypeFact, models.ScopePermanent, []string{"go", "testing"})
	mem2 := newTaggedMemory("tag-2", models.MemoryTypeFact, models.ScopePermanent, []string{"python", "ml"})
	mem3 := newTaggedMemory("tag-3", models.MemoryTypeFact, models.ScopePermanent, []string{"go", "concurrency"})

	vec := testVector(0.7)
	require.NoError(t, s.Upsert(ctx, mem1, vec))
	require.NoError(t, s.Upsert(ctx, mem2, vec))
	require.NoError(t, s.Upsert(ctx, mem3, vec))

	// Filter for "go" tag — should return mem1 and mem3.
	filters := &store.SearchFilters{Tags: []string{"go"}}
	results, err := s.Search(ctx, vec, 20, filters)
	require.NoError(t, err)
	require.Len(t, results, 2)

	ids := make(map[string]bool)
	for i := range results {
		ids[results[i].Memory.ID] = true
	}
	assert.True(t, ids["tag-1"], "mem1 should match tag 'go'")
	assert.True(t, ids["tag-3"], "mem3 should match tag 'go'")
}

// TestSearchTagFilterMultipleTags verifies that filtering with multiple tags
// requires ALL tags to be present (AND semantics).
func TestSearchTagFilterMultipleTags(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	mem1 := newTaggedMemory("multi-1", models.MemoryTypeFact, models.ScopePermanent, []string{"go", "testing"})
	mem2 := newTaggedMemory("multi-2", models.MemoryTypeFact, models.ScopePermanent, []string{"go", "concurrency"})

	vec := testVector(0.7)
	require.NoError(t, s.Upsert(ctx, mem1, vec))
	require.NoError(t, s.Upsert(ctx, mem2, vec))

	// Filter for both "go" AND "testing" — only mem1 matches.
	filters := &store.SearchFilters{Tags: []string{"go", "testing"}}
	results, err := s.Search(ctx, vec, 20, filters)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "multi-1", results[0].Memory.ID)
}

// TestSearchTagFilterNoMatch verifies that filtering with a non-existent tag
// returns no results.
func TestSearchTagFilterNoMatch(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	mem := newTaggedMemory("nomatch-1", models.MemoryTypeFact, models.ScopePermanent, []string{"go"})
	vec := testVector(0.7)
	require.NoError(t, s.Upsert(ctx, mem, vec))

	filters := &store.SearchFilters{Tags: []string{"rust"}}
	results, err := s.Search(ctx, vec, 20, filters)
	require.NoError(t, err)
	assert.Empty(t, results)
}

// TestRecallWithTypeFilter verifies that type filtering works when passed
// through to the store search and then ranked by the Recaller.
func TestRecallWithTypeFilter(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	vec := testVector(0.6)
	mem1 := newTaggedMemory("recall-rule", models.MemoryTypeRule, models.ScopePermanent, nil)
	mem2 := newTaggedMemory("recall-fact", models.MemoryTypeFact, models.ScopePermanent, nil)
	mem3 := newTaggedMemory("recall-episode", models.MemoryTypeEpisode, models.ScopePermanent, nil)

	require.NoError(t, s.Upsert(ctx, mem1, vec))
	require.NoError(t, s.Upsert(ctx, mem2, vec))
	require.NoError(t, s.Upsert(ctx, mem3, vec))

	// Filter for "rule" only.
	ruleType := models.MemoryTypeRule
	filters := &store.SearchFilters{Type: &ruleType}
	results, err := s.Search(ctx, vec, 50, filters)
	require.NoError(t, err)
	require.Len(t, results, 1)

	// Rank the filtered results.
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	recaller := recall.NewRecaller(recall.DefaultWeights(), logger)
	ranked := recaller.Rank(results, "")
	require.Len(t, ranked, 1)
	assert.Equal(t, "recall-rule", ranked[0].Memory.ID)
	assert.Equal(t, models.MemoryTypeRule, ranked[0].Memory.Type)
}

// TestRecallWithScopeFilter verifies that scope filtering works correctly
// when combined with multi-factor ranking.
func TestRecallWithScopeFilter(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	vec := testVector(0.6)
	mem1 := newTaggedMemory("recall-perm", models.MemoryTypeFact, models.ScopePermanent, nil)
	mem2 := newTaggedMemory("recall-sess", models.MemoryTypeFact, models.ScopeSession, nil)

	require.NoError(t, s.Upsert(ctx, mem1, vec))
	require.NoError(t, s.Upsert(ctx, mem2, vec))

	sessionScope := models.ScopeSession
	filters := &store.SearchFilters{Scope: &sessionScope}
	results, err := s.Search(ctx, vec, 50, filters)
	require.NoError(t, err)
	require.Len(t, results, 1)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	recaller := recall.NewRecaller(recall.DefaultWeights(), logger)
	ranked := recaller.Rank(results, "")
	require.Len(t, ranked, 1)
	assert.Equal(t, "recall-sess", ranked[0].Memory.ID)
	assert.Equal(t, models.ScopeSession, ranked[0].Memory.Scope)
}

// TestRecallWithCombinedFilters verifies that type, scope, and tags filters
// can be combined to narrow recall results before ranking.
func TestRecallWithCombinedFilters(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	vec := testVector(0.6)
	// The one memory that should match all filters.
	match := newTaggedMemory("combo-match", models.MemoryTypeRule, models.ScopeProject, []string{"important"})
	match.Project = "myproject"
	require.NoError(t, s.Upsert(ctx, match, vec))

	// Misses on type.
	missType := newTaggedMemory("combo-wrong-type", models.MemoryTypeFact, models.ScopeProject, []string{"important"})
	missType.Project = "myproject"
	require.NoError(t, s.Upsert(ctx, missType, vec))

	// Misses on tag.
	missTag := newTaggedMemory("combo-wrong-tag", models.MemoryTypeRule, models.ScopeProject, []string{"other"})
	missTag.Project = "myproject"
	require.NoError(t, s.Upsert(ctx, missTag, vec))

	// Misses on scope.
	missScope := newTaggedMemory("combo-wrong-scope", models.MemoryTypeRule, models.ScopePermanent, []string{"important"})
	missScope.Project = "myproject"
	require.NoError(t, s.Upsert(ctx, missScope, vec))

	ruleType := models.MemoryTypeRule
	projScope := models.ScopeProject
	proj := "myproject"
	filters := &store.SearchFilters{
		Type:    &ruleType,
		Scope:   &projScope,
		Project: &proj,
		Tags:    []string{"important"},
	}

	results, err := s.Search(ctx, vec, 50, filters)
	require.NoError(t, err)
	require.Len(t, results, 1)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	recaller := recall.NewRecaller(recall.DefaultWeights(), logger)
	ranked := recaller.Rank(results, "myproject")
	require.Len(t, ranked, 1)
	assert.Equal(t, "combo-match", ranked[0].Memory.ID)
}

// TestTagsFlagParsing verifies that a comma-separated tags string is correctly
// split into individual tag values (mirrors the CLI flag parsing logic).
func TestTagsFlagParsing(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"go,testing", []string{"go", "testing"}},
		{"single", []string{"single"}},
		{"a,b,c", []string{"a", "b", "c"}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			tags := strings.Split(tt.input, ",")
			assert.Equal(t, tt.expected, tags)
		})
	}
}

// TestListWithTagFilter verifies that the List method also respects the tags
// filter (consistency with Search).
func TestListWithTagFilter(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	mem1 := newTaggedMemory("list-tag-1", models.MemoryTypeFact, models.ScopePermanent, []string{"api", "http"})
	mem2 := newTaggedMemory("list-tag-2", models.MemoryTypeFact, models.ScopePermanent, []string{"cli"})

	require.NoError(t, s.Upsert(ctx, mem1, testVector(0.5)))
	require.NoError(t, s.Upsert(ctx, mem2, testVector(0.5)))

	filters := &store.SearchFilters{Tags: []string{"api"}}
	results, _, err := s.List(ctx, filters, 20, "")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "list-tag-1", results[0].ID)
}
