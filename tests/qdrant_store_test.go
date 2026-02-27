//go:build integration

package tests

// Integration tests for QdrantStore — require a running Qdrant instance.
//
// Run with:
//
//	go test -tags=integration -run TestQdrantStore ./tests/...
//
// Override the host via QDRANT_HOST env var (default: localhost).

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

func qdrantHost() string {
	if h := os.Getenv("QDRANT_HOST"); h != "" {
		return h
	}
	return "localhost"
}

func newIntegrationStore(t *testing.T) store.Store {
	t.Helper()
	const (
		collection = "test_cortex_integ"
		dim        = 768
	)
	st, err := store.NewQdrantStore(qdrantHost(), 6334, collection, dim, false, slog.Default())
	require.NoError(t, err, "connecting to Qdrant at %s:6334", qdrantHost())
	t.Cleanup(func() { _ = st.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, st.EnsureCollection(ctx))
	return st
}

func TestQdrantStore_UpsertAndGet(t *testing.T) {
	st := newIntegrationStore(t)
	ctx := context.Background()

	now := time.Now().UTC()
	mem := models.Memory{
		ID:           "integ-upsert-1",
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopePermanent,
		Visibility:   models.VisibilityShared,
		Content:      "Qdrant integration test memory",
		Confidence:   0.9,
		Source:       "test",
		Tags:         []string{"integration"},
		CreatedAt:    now,
		UpdatedAt:    now,
		LastAccessed: now,
	}
	vec := testVector(0.5)

	require.NoError(t, st.Upsert(ctx, mem, vec))
	t.Cleanup(func() { _ = st.Delete(ctx, mem.ID) })

	got, err := st.Get(ctx, mem.ID)
	require.NoError(t, err)
	assert.Equal(t, mem.ID, got.ID)
	assert.Equal(t, mem.Content, got.Content)
	assert.Equal(t, mem.Type, got.Type)
	assert.Equal(t, mem.Confidence, got.Confidence)
}

func TestQdrantStore_Get_ErrNotFound(t *testing.T) {
	st := newIntegrationStore(t)
	ctx := context.Background()

	_, err := st.Get(ctx, "00000000-0000-0000-0000-000000000000")
	require.Error(t, err)
	assert.True(t, errors.Is(err, store.ErrNotFound), "expected ErrNotFound, got: %v", err)
}

func TestQdrantStore_Search(t *testing.T) {
	st := newIntegrationStore(t)
	ctx := context.Background()

	now := time.Now().UTC()
	mem := models.Memory{
		ID: "integ-search-1", Type: models.MemoryTypeFact,
		Scope: models.ScopePermanent, Visibility: models.VisibilityShared,
		Content: "Search integration test", Confidence: 0.9, Source: "test",
		CreatedAt: now, UpdatedAt: now, LastAccessed: now,
	}
	vec := testVector(0.8)
	require.NoError(t, st.Upsert(ctx, mem, vec))
	t.Cleanup(func() { _ = st.Delete(ctx, mem.ID) })

	results, err := st.Search(ctx, vec, 5, nil)
	require.NoError(t, err)
	require.NotEmpty(t, results)
	assert.Equal(t, mem.ID, results[0].Memory.ID)
}

func TestQdrantStore_UpdateAccessMetadata(t *testing.T) {
	st := newIntegrationStore(t)
	ctx := context.Background()

	now := time.Now().UTC()
	mem := models.Memory{
		ID: "integ-access-1", Type: models.MemoryTypeFact,
		Scope: models.ScopePermanent, Visibility: models.VisibilityShared,
		Content: "Access metadata test", Confidence: 0.8, Source: "test",
		CreatedAt: now, UpdatedAt: now, LastAccessed: now,
		AccessCount: 0,
	}
	require.NoError(t, st.Upsert(ctx, mem, testVector(0.3)))
	t.Cleanup(func() { _ = st.Delete(ctx, mem.ID) })

	require.NoError(t, st.UpdateAccessMetadata(ctx, mem.ID))

	got, err := st.Get(ctx, mem.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), got.AccessCount)
	assert.True(t, got.LastAccessed.After(now.Add(-time.Second)))
}

func TestQdrantStore_Delete(t *testing.T) {
	st := newIntegrationStore(t)
	ctx := context.Background()

	now := time.Now().UTC()
	mem := models.Memory{
		ID: "integ-delete-1", Type: models.MemoryTypeFact,
		Scope: models.ScopePermanent, Visibility: models.VisibilityShared,
		Content: "To be deleted", Confidence: 0.9, Source: "test",
		CreatedAt: now, UpdatedAt: now, LastAccessed: now,
	}
	require.NoError(t, st.Upsert(ctx, mem, testVector(0.1)))
	require.NoError(t, st.Delete(ctx, mem.ID))

	_, err := st.Get(ctx, mem.ID)
	require.Error(t, err)
	assert.True(t, errors.Is(err, store.ErrNotFound))
}

func TestQdrantStore_Stats(t *testing.T) {
	st := newIntegrationStore(t)
	ctx := context.Background()

	stats, err := st.Stats(ctx)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, stats.TotalMemories, int64(0))
}
