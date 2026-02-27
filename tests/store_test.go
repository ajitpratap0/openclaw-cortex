package tests

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

func newTestMemory(id string, memType models.MemoryType, content string) models.Memory {
	now := time.Now().UTC()
	return models.Memory{
		ID:           id,
		Type:         memType,
		Scope:        models.ScopePermanent,
		Visibility:   models.VisibilityShared,
		Content:      content,
		Confidence:   0.9,
		Source:       "explicit",
		Tags:         []string{"test"},
		CreatedAt:    now,
		UpdatedAt:    now,
		LastAccessed: now,
	}
}

func testVector(val float32) []float32 {
	const dim = 768
	v := make([]float32, dim)
	for i := range v {
		v[i] = val * float32(i+1) / float32(dim)
	}
	return v
}

// testVectorAlt creates a vector with a different pattern to ensure low cosine similarity.
func testVectorAlt(dim int) []float32 {
	v := make([]float32, dim)
	for i := range v {
		if i%2 == 0 {
			v[i] = 1.0
		} else {
			v[i] = -1.0
		}
	}
	return v
}

func TestMockStore_UpsertAndGet(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	mem := newTestMemory("test-1", models.MemoryTypeFact, "Go is a statically typed language")
	vec := testVector(0.5)

	err := s.Upsert(ctx, mem, vec)
	require.NoError(t, err)

	got, err := s.Get(ctx, "test-1")
	require.NoError(t, err)
	assert.Equal(t, "test-1", got.ID)
	assert.Equal(t, models.MemoryTypeFact, got.Type)
	assert.Equal(t, "Go is a statically typed language", got.Content)
}

func TestMockStore_Delete(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	mem := newTestMemory("test-del", models.MemoryTypeFact, "temporary")
	err := s.Upsert(ctx, mem, testVector(0.1))
	require.NoError(t, err)

	err = s.Delete(ctx, "test-del")
	require.NoError(t, err)

	_, err = s.Get(ctx, "test-del")
	assert.Error(t, err)
}

func TestMockStore_Search(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	// Insert memories with different vectors — use same base for s-1 as query
	queryVec := testVector(0.9)
	mem1 := newTestMemory("s-1", models.MemoryTypeFact, "Go is great")
	mem2 := newTestMemory("s-2", models.MemoryTypeRule, "Always write tests")
	mem3 := newTestMemory("s-3", models.MemoryTypeProcedure, "How to deploy")

	_ = s.Upsert(ctx, mem1, queryVec)               // exact match to query
	_ = s.Upsert(ctx, mem2, testVector(-0.5))   // different direction
	_ = s.Upsert(ctx, mem3, testVectorAlt(768))       // very different

	results, err := s.Search(ctx, queryVec, 10, nil)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 1)
	// Exact match should be first
	assert.Equal(t, "s-1", results[0].Memory.ID)
}

func TestMockStore_SearchWithFilters(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	mem1 := newTestMemory("f-1", models.MemoryTypeFact, "A fact")
	mem2 := newTestMemory("f-2", models.MemoryTypeRule, "A rule")

	_ = s.Upsert(ctx, mem1, testVector(0.5))
	_ = s.Upsert(ctx, mem2, testVector(0.5))

	ruleType := models.MemoryTypeRule
	results, err := s.Search(ctx, testVector(0.5), 10, &store.SearchFilters{Type: &ruleType})
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "f-2", results[0].Memory.ID)
}

func TestMockStore_FindDuplicates(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	mem := newTestMemory("dup-1", models.MemoryTypeFact, "Original memory")
	vec := testVector(0.8)
	_ = s.Upsert(ctx, mem, vec)

	// Same vector should be duplicate
	dupes, err := s.FindDuplicates(ctx, vec, 0.95)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(dupes), 1)

	// Different vector should not be duplicate
	dupes, err = s.FindDuplicates(ctx, testVectorAlt(768), 0.95)
	require.NoError(t, err)
	assert.Empty(t, dupes)
}

func TestMockStore_List(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	for i := 0; i < 5; i++ {
		mem := newTestMemory(
			fmt.Sprintf("list-%02d", i),
			models.MemoryTypeFact,
			"memory content",
		)
		_ = s.Upsert(ctx, mem, testVector(float32(i)*0.1))
	}

	results, _, err := s.List(ctx, nil, 3, "")
	require.NoError(t, err)
	assert.LessOrEqual(t, len(results), 3)
}

func TestMockStore_CursorPagination_MultiPage(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	// Store 10 items with sortable IDs (pad with leading zeros for deterministic order).
	ids := []string{"p-01", "p-02", "p-03", "p-04", "p-05", "p-06", "p-07", "p-08", "p-09", "p-10"}
	for i, id := range ids {
		mem := newTestMemory(id, models.MemoryTypeFact, "content-"+id)
		require.NoError(t, s.Upsert(ctx, mem, testVector(float32(i)*0.1)))
	}

	// Paginate with limit=3 and collect all results.
	var allIDs []string
	cursor := ""
	pages := 0
	for {
		results, next, err := s.List(ctx, nil, 3, cursor)
		require.NoError(t, err)
		for _, r := range results {
			allIDs = append(allIDs, r.ID)
		}
		pages++
		if next == "" {
			break
		}
		cursor = next
	}

	// All 10 items must be returned across pages.
	assert.Equal(t, len(ids), len(allIDs), "all items should be returned across pages")
	// IDs should be sorted (MockStore sorts by ID).
	for i := 1; i < len(allIDs); i++ {
		assert.True(t, allIDs[i-1] < allIDs[i], "IDs should be in sorted order")
	}
	assert.GreaterOrEqual(t, pages, 2, "should require multiple pages")
}

func TestMockStore_CursorPagination_LastElement(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	ids := []string{"le-1", "le-2", "le-3"}
	for i, id := range ids {
		mem := newTestMemory(id, models.MemoryTypeFact, "content")
		require.NoError(t, s.Upsert(ctx, mem, testVector(float32(i)*0.1)))
	}

	// Fetch all with a limit larger than total items.
	results, next, err := s.List(ctx, nil, 10, "")
	require.NoError(t, err)
	assert.Len(t, results, 3)
	assert.Empty(t, next, "cursor should be empty when all items fit in one page")

	// Using the last element ID as cursor should return empty next page.
	lastID := results[len(results)-1].ID
	results, next, err = s.List(ctx, nil, 10, lastID)
	require.NoError(t, err)
	assert.Empty(t, results, "no items should remain after the last element cursor")
	assert.Empty(t, next)
}

func TestMockStore_CursorPagination_EmptyCursor(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("ec-%02d", i)
		mem := newTestMemory(id, models.MemoryTypeFact, "content")
		require.NoError(t, s.Upsert(ctx, mem, testVector(float32(i)*0.1)))
	}

	// Empty string cursor should return the first page.
	results, _, err := s.List(ctx, nil, 3, "")
	require.NoError(t, err)
	assert.Len(t, results, 3)
	assert.Equal(t, "ec-00", results[0].ID)
}

func TestMockStore_UpdateAccessMetadata(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	mem := newTestMemory("access-1", models.MemoryTypeFact, "track access")
	_ = s.Upsert(ctx, mem, testVector(0.5))

	err := s.UpdateAccessMetadata(ctx, "access-1")
	require.NoError(t, err)

	got, _ := s.Get(ctx, "access-1")
	assert.Equal(t, int64(1), got.AccessCount)

	err = s.UpdateAccessMetadata(ctx, "access-1")
	require.NoError(t, err)

	got, _ = s.Get(ctx, "access-1")
	assert.Equal(t, int64(2), got.AccessCount)
}

func TestMockStore_Stats(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	_ = s.Upsert(ctx, newTestMemory("st-1", models.MemoryTypeFact, "f1"), testVector(0.1))
	_ = s.Upsert(ctx, newTestMemory("st-2", models.MemoryTypeFact, "f2"), testVector(0.2))
	_ = s.Upsert(ctx, newTestMemory("st-3", models.MemoryTypeRule, "r1"), testVector(0.3))

	stats, err := s.Stats(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(3), stats.TotalMemories)
	assert.Equal(t, int64(2), stats.ByType["fact"])
	assert.Equal(t, int64(1), stats.ByType["rule"])
}
