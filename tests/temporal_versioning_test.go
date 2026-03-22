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

// TestMockStoreTemporalVersioning verifies the v0.8.0 temporal-versioning contract
// end-to-end: InvalidateMemory + AsOf point-in-time queries return the correct
// version at each instant, and a default query returns only the live replacement.
//
// This complements TestTemporalVersioning_UpsertInvalidatesPredecessor (which
// tests auto-invalidation via SupersedesID) by testing explicit invalidation +
// List-level temporal filtering.
func TestMockStoreTemporalVersioning(t *testing.T) {
	ctx := context.Background()
	ms := store.NewMockStore()

	t0 := time.Now().UTC().Add(-2 * time.Hour)
	t1 := t0.Add(1 * time.Hour) // invalidation time
	t2 := t0.Add(90 * time.Minute)

	// Store old fact with an explicit ValidFrom = t0.
	oldFact := models.Memory{
		ID:           "old",
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopePermanent,
		Visibility:   models.VisibilityShared,
		Content:      "Alice's title is Junior Engineer",
		Confidence:   0.9,
		Source:       "test",
		ValidFrom:    t0,
		CreatedAt:    t0,
		UpdatedAt:    t0,
		LastAccessed: t0,
	}
	if err := ms.Upsert(ctx, oldFact, testVector(0.1)); err != nil {
		t.Fatalf("Upsert old fact: %v", err)
	}

	// Invalidate the old fact at t1.
	if err := ms.InvalidateMemory(ctx, "old", t1); err != nil {
		t.Fatalf("InvalidateMemory: %v", err)
	}

	// Store replacement fact with ValidFrom = t2.
	newFact := models.Memory{
		ID:           "new",
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopePermanent,
		Visibility:   models.VisibilityShared,
		Content:      "Alice's title is Senior Engineer",
		Confidence:   0.9,
		Source:       "test",
		ValidFrom:    t2,
		CreatedAt:    t2,
		UpdatedAt:    t2,
		LastAccessed: t2,
	}
	if err := ms.Upsert(ctx, newFact, testVector(0.2)); err != nil {
		t.Fatalf("Upsert new fact: %v", err)
	}

	// Point-in-time query at t0+30min (between t0 and t1): only old fact valid.
	asOf := t0.Add(30 * time.Minute)
	pastMemories, _, err := ms.List(ctx, &store.SearchFilters{AsOf: &asOf}, 100, "")
	if err != nil {
		t.Fatalf("List AsOf t0+30min: %v", err)
	}
	if len(pastMemories) != 1 || pastMemories[0].ID != "old" {
		ids := make([]string, len(pastMemories))
		for i, m := range pastMemories {
			ids[i] = m.ID
		}
		t.Errorf("AsOf query: want [old], got %v", ids)
	}

	// Default query (no AsOf): only replacement visible; old is invalidated.
	currentMemories, _, err := ms.List(ctx, nil, 100, "")
	if err != nil {
		t.Fatalf("List default: %v", err)
	}
	if len(currentMemories) != 1 || currentMemories[0].ID != "new" {
		ids := make([]string, len(currentMemories))
		for i, m := range currentMemories {
			ids[i] = m.ID
		}
		t.Errorf("default query: want [new], got %v", ids)
	}
}
