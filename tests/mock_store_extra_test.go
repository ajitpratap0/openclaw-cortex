package tests

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

func TestMockStore_EnsureCollection(t *testing.T) {
	s := store.NewMockStore()
	err := s.EnsureCollection(context.Background())
	assert.NoError(t, err)
}

func TestMockStore_Close(t *testing.T) {
	s := store.NewMockStore()
	err := s.Close()
	assert.NoError(t, err)
}

func TestMockStore_DeleteNotFound(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()
	err := s.Delete(ctx, "nonexistent-id")
	assert.Error(t, err)
}

func TestMockStore_GetNotFound(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()
	_, err := s.Get(ctx, "nonexistent-id")
	assert.Error(t, err)
}

func TestMockStore_UpdateAccessMetadata_NotFound(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()
	err := s.UpdateAccessMetadata(ctx, "no-such-id")
	assert.Error(t, err)
}

func TestMockStore_SearchWithScopeFilter(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	mem1 := newTestMemory("scope-perm", models.MemoryTypeFact, "permanent memory")
	mem1.Scope = models.ScopePermanent
	mem2 := newTestMemory("scope-sess", models.MemoryTypeFact, "session memory")
	mem2.Scope = models.ScopeSession

	_ = s.Upsert(ctx, mem1, testVector(0.5))
	_ = s.Upsert(ctx, mem2, testVector(0.5))

	scope := models.ScopePermanent
	results, err := s.Search(ctx, testVector(0.5), 10, &store.SearchFilters{Scope: &scope})
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "scope-perm", results[0].Memory.ID)
}

func TestMockStore_SearchWithVisibilityFilter(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	mem1 := newTestMemory("vis-shared", models.MemoryTypeFact, "shared memory")
	mem1.Visibility = models.VisibilityShared
	mem2 := newTestMemory("vis-priv", models.MemoryTypeFact, "private memory")
	mem2.Visibility = models.VisibilityPrivate

	_ = s.Upsert(ctx, mem1, testVector(0.5))
	_ = s.Upsert(ctx, mem2, testVector(0.5))

	vis := models.VisibilityPrivate
	results, err := s.Search(ctx, testVector(0.5), 10, &store.SearchFilters{Visibility: &vis})
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "vis-priv", results[0].Memory.ID)
}

func TestMockStore_SearchWithProjectFilter(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	mem1 := newTestMemory("proj-a", models.MemoryTypeFact, "project A memory")
	mem1.Project = "project-a"
	mem2 := newTestMemory("proj-b", models.MemoryTypeFact, "project B memory")
	mem2.Project = "project-b"

	_ = s.Upsert(ctx, mem1, testVector(0.5))
	_ = s.Upsert(ctx, mem2, testVector(0.5))

	proj := "project-a"
	results, err := s.Search(ctx, testVector(0.5), 10, &store.SearchFilters{Project: &proj})
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "proj-a", results[0].Memory.ID)
}

func TestMockStore_SearchWithSourceFilter(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	mem1 := newTestMemory("src-explicit", models.MemoryTypeFact, "explicit source memory")
	mem1.Source = "explicit"
	mem2 := newTestMemory("src-file", models.MemoryTypeFact, "file source memory")
	mem2.Source = "file"

	_ = s.Upsert(ctx, mem1, testVector(0.5))
	_ = s.Upsert(ctx, mem2, testVector(0.5))

	src := "file"
	results, err := s.Search(ctx, testVector(0.5), 10, &store.SearchFilters{Source: &src})
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "src-file", results[0].Memory.ID)
}

func TestMockStore_SearchWithTagsFilter(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	mem1 := newTestMemory("tag-go", models.MemoryTypeFact, "Go memory")
	mem1.Tags = []string{"go", "programming"}
	mem2 := newTestMemory("tag-rust", models.MemoryTypeFact, "Rust memory")
	mem2.Tags = []string{"rust", "programming"}

	_ = s.Upsert(ctx, mem1, testVector(0.5))
	_ = s.Upsert(ctx, mem2, testVector(0.5))

	results, err := s.Search(ctx, testVector(0.5), 10, &store.SearchFilters{Tags: []string{"go"}})
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "tag-go", results[0].Memory.ID)
}

func TestMockStore_SearchWithTagsFilterMultiRequired(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	mem1 := newTestMemory("tag-both", models.MemoryTypeFact, "both tags memory")
	mem1.Tags = []string{"go", "testing"}
	mem2 := newTestMemory("tag-one", models.MemoryTypeFact, "one tag memory")
	mem2.Tags = []string{"go"}

	_ = s.Upsert(ctx, mem1, testVector(0.5))
	_ = s.Upsert(ctx, mem2, testVector(0.5))

	// Require both "go" and "testing" tags
	results, err := s.Search(ctx, testVector(0.5), 10, &store.SearchFilters{Tags: []string{"go", "testing"}})
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "tag-both", results[0].Memory.ID)
}

func TestMockStore_SearchWithMetadata(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	// Store memory with metadata — verify metadata is preserved
	mem := newTestMemory("meta-1", models.MemoryTypeFact, "memory with metadata")
	mem.Metadata = map[string]any{
		"section_path":  "Intro / Overview",
		"section_depth": 2,
		"word_count":    42,
	}

	_ = s.Upsert(ctx, mem, testVector(0.5))

	got, err := s.Get(ctx, "meta-1")
	require.NoError(t, err)
	assert.Equal(t, "Intro / Overview", got.Metadata["section_path"])
	assert.Equal(t, 2, got.Metadata["section_depth"])
}

func TestMockStore_ListWithScopeFilter(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	for i, scope := range []models.MemoryScope{models.ScopePermanent, models.ScopeSession, models.ScopeProject} {
		mem := newTestMemory(
			[]string{"ls-perm", "ls-sess", "ls-proj"}[i],
			models.MemoryTypeFact,
			"memory",
		)
		mem.Scope = scope
		_ = s.Upsert(ctx, mem, testVector(float32(i)*0.1))
	}

	scope := models.ScopeSession
	results, _, err := s.List(ctx, &store.SearchFilters{Scope: &scope}, 10, "")
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "ls-sess", results[0].ID)
}

func TestMockStore_ListWithUnknownCursor(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	mem := newTestMemory("cursor-1", models.MemoryTypeFact, "content")
	_ = s.Upsert(ctx, mem, testVector(0.5))

	// Cursor that doesn't match any ID — should return empty
	results, next, err := s.List(ctx, nil, 10, "nonexistent-cursor-id")
	require.NoError(t, err)
	assert.Empty(t, results)
	assert.Empty(t, next)
}

func TestMockStore_UpsertWithTags(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	mem := newTestMemory("tagged", models.MemoryTypeFact, "tagged memory")
	mem.Tags = []string{"tag1", "tag2", "tag3"}
	_ = s.Upsert(ctx, mem, testVector(0.5))

	got, err := s.Get(ctx, "tagged")
	require.NoError(t, err)
	assert.Equal(t, []string{"tag1", "tag2", "tag3"}, got.Tags)

	// Mutating original tags should not affect stored copy
	mem.Tags[0] = "mutated"
	got2, err := s.Get(ctx, "tagged")
	require.NoError(t, err)
	assert.Equal(t, "tag1", got2.Tags[0], "stored tags should be immutable from outside")
}

func TestMockStore_SearchLimitRespected(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	for i := 0; i < 10; i++ {
		mem := newTestMemory(
			[]string{"lim-0", "lim-1", "lim-2", "lim-3", "lim-4", "lim-5", "lim-6", "lim-7", "lim-8", "lim-9"}[i],
			models.MemoryTypeFact,
			"content",
		)
		_ = s.Upsert(ctx, mem, testVector(float32(i)*0.1))
	}

	results, err := s.Search(ctx, testVector(0.5), 3, nil)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(results), 3)
}
