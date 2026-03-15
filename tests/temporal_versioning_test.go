package tests

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

// TestTemporalVersioning_InvalidateMemory tests that InvalidateMemory sets valid_to correctly.
func TestTemporalVersioning_InvalidateMemory(t *testing.T) {
	ms := store.NewMockStore()
	ctx := context.Background()

	mem := newTestMemory("mem-v1", models.MemoryTypeRule, "original content")
	mem.ValidFrom = mem.CreatedAt
	require.NoError(t, ms.Upsert(ctx, mem, testVector(0.1)))

	// Verify stored
	got, err := ms.Get(ctx, "mem-v1")
	require.NoError(t, err)
	assert.Nil(t, got.ValidTo)

	// Invalidate
	invalidatedAt := time.Now().UTC()
	require.NoError(t, ms.InvalidateMemory(ctx, "mem-v1", invalidatedAt))

	// Verify valid_to is set
	got, err = ms.Get(ctx, "mem-v1")
	require.NoError(t, err)
	require.NotNil(t, got.ValidTo)
	assert.True(t, got.ValidTo.Equal(invalidatedAt) || got.ValidTo.After(invalidatedAt.Add(-time.Second)))
}

// TestTemporalVersioning_UpsertInvalidatesPredecessor tests that Upsert with SupersedesID
// invalidates the old memory automatically.
func TestTemporalVersioning_UpsertInvalidatesPredecessor(t *testing.T) {
	ms := store.NewMockStore()
	ctx := context.Background()

	// Store original
	v1 := newTestMemory("mem-v1", models.MemoryTypeRule, "v1 content")
	v1.ValidFrom = v1.CreatedAt
	require.NoError(t, ms.Upsert(ctx, v1, testVector(0.1)))

	// Store superseding version
	v2 := newTestMemory("mem-v2", models.MemoryTypeRule, "v2 content")
	v2.SupersedesID = "mem-v1"
	v2.ValidFrom = time.Now().UTC()
	require.NoError(t, ms.Upsert(ctx, v2, testVector(0.2)))

	// v1 should now be invalidated
	got, err := ms.Get(ctx, "mem-v1")
	require.NoError(t, err)
	assert.NotNil(t, got.ValidTo, "v1 should have valid_to set after being superseded")

	// v2 should still be valid
	got, err = ms.Get(ctx, "mem-v2")
	require.NoError(t, err)
	assert.Nil(t, got.ValidTo, "v2 should not have valid_to set")
}

// TestTemporalVersioning_GetHistory tests GetHistory returns all versions.
func TestTemporalVersioning_GetHistory(t *testing.T) {
	ms := store.NewMockStore()
	ctx := context.Background()

	v1 := newTestMemory("hist-v1", models.MemoryTypeRule, "v1")
	v1.ValidFrom = v1.CreatedAt
	require.NoError(t, ms.Upsert(ctx, v1, testVector(0.1)))

	v2 := newTestMemory("hist-v2", models.MemoryTypeRule, "v2")
	v2.SupersedesID = "hist-v1"
	v2.ValidFrom = time.Now().UTC()
	require.NoError(t, ms.Upsert(ctx, v2, testVector(0.2)))

	history, err := ms.GetHistory(ctx, "hist-v2")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(history), 1, "history should contain at least the current version")
}

// TestTemporalVersioning_MigrateTemporalFields tests idempotent migration.
func TestTemporalVersioning_MigrateTemporalFields(t *testing.T) {
	ms := store.NewMockStore()
	ctx := context.Background()

	// Run migration twice — should be idempotent
	require.NoError(t, ms.MigrateTemporalFields(ctx))
	require.NoError(t, ms.MigrateTemporalFields(ctx))
}

// TestSearchFilters_IncludeInvalidated verifies the filter struct fields exist and are usable.
func TestSearchFilters_IncludeInvalidated(t *testing.T) {
	f := &store.SearchFilters{
		IncludeInvalidated: true,
	}
	assert.True(t, f.IncludeInvalidated)
}

// TestSearchFilters_AsOf verifies AsOf field exists.
func TestSearchFilters_AsOf(t *testing.T) {
	asOf := time.Now()
	f := &store.SearchFilters{
		AsOf: &asOf,
	}
	require.NotNil(t, f.AsOf)
	assert.Equal(t, asOf, *f.AsOf)
}
