package tests

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/cortex/internal/models"
	"github.com/ajitpratap0/cortex/internal/store"
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

func testVector(dim int, val float32) []float32 {
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
	vec := testVector(768, 0.5)

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
	err := s.Upsert(ctx, mem, testVector(768, 0.1))
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
	queryVec := testVector(768, 0.9)
	mem1 := newTestMemory("s-1", models.MemoryTypeFact, "Go is great")
	mem2 := newTestMemory("s-2", models.MemoryTypeRule, "Always write tests")
	mem3 := newTestMemory("s-3", models.MemoryTypeProcedure, "How to deploy")

	_ = s.Upsert(ctx, mem1, queryVec)               // exact match to query
	_ = s.Upsert(ctx, mem2, testVector(768, -0.5))   // different direction
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

	_ = s.Upsert(ctx, mem1, testVector(768, 0.5))
	_ = s.Upsert(ctx, mem2, testVector(768, 0.5))

	ruleType := models.MemoryTypeRule
	results, err := s.Search(ctx, testVector(768, 0.5), 10, &store.SearchFilters{Type: &ruleType})
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "f-2", results[0].Memory.ID)
}

func TestMockStore_FindDuplicates(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	mem := newTestMemory("dup-1", models.MemoryTypeFact, "Original memory")
	vec := testVector(768, 0.8)
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
			"list-"+string(rune('a'+i)),
			models.MemoryTypeFact,
			"memory content",
		)
		_ = s.Upsert(ctx, mem, testVector(768, float32(i)*0.1))
	}

	results, err := s.List(ctx, nil, 3, 0)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(results), 3)
}

func TestMockStore_UpdateAccessMetadata(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	mem := newTestMemory("access-1", models.MemoryTypeFact, "track access")
	_ = s.Upsert(ctx, mem, testVector(768, 0.5))

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

	_ = s.Upsert(ctx, newTestMemory("st-1", models.MemoryTypeFact, "f1"), testVector(768, 0.1))
	_ = s.Upsert(ctx, newTestMemory("st-2", models.MemoryTypeFact, "f2"), testVector(768, 0.2))
	_ = s.Upsert(ctx, newTestMemory("st-3", models.MemoryTypeRule, "r1"), testVector(768, 0.3))

	stats, err := s.Stats(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(3), stats.TotalMemories)
	assert.Equal(t, int64(2), stats.ByType["fact"])
	assert.Equal(t, int64(1), stats.ByType["rule"])
}
