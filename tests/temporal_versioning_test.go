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

// TestInvalidateMemory verifies that InvalidateMemory sets valid_to without deleting.
func TestInvalidateMemory(t *testing.T) {
	ctx := context.Background()
	st := store.NewMockStore()

	mem := models.Memory{
		ID:        "mem-1",
		Content:   "Ajit works at Pixis",
		Type:      models.MemoryTypeFact,
		Scope:     models.ScopePermanent,
		ValidFrom: time.Now().Add(-24 * time.Hour),
	}
	require.NoError(t, st.Upsert(ctx, mem, make([]float32, 8)))

	// Verify it's initially valid (no valid_to)
	stored, err := st.Get(ctx, "mem-1")
	require.NoError(t, err)
	assert.Nil(t, stored.ValidTo, "should have no valid_to initially")
	assert.True(t, stored.IsCurrentVersion, "should be current version initially")

	// Invalidate it
	invalidationTime := time.Now().UTC()
	require.NoError(t, st.InvalidateMemory(ctx, "mem-1", invalidationTime))

	// Verify valid_to is set
	stored, err = st.Get(ctx, "mem-1")
	require.NoError(t, err)
	require.NotNil(t, stored.ValidTo, "valid_to should be set after invalidation")
	assert.WithinDuration(t, invalidationTime, *stored.ValidTo, time.Second)
	assert.False(t, stored.IsCurrentVersion, "should not be current version after invalidation")
}

// TestGetHistory verifies the version chain is returned correctly.
func TestGetHistory(t *testing.T) {
	ctx := context.Background()
	st := store.NewMockStore()

	// Store v1
	v1 := models.Memory{
		ID:        "mem-v1",
		Content:   "Ajit works at Pixis",
		Type:      models.MemoryTypeFact,
		Scope:     models.ScopePermanent,
		ValidFrom: time.Now().Add(-48 * time.Hour),
	}
	require.NoError(t, st.Upsert(ctx, v1, make([]float32, 8)))

	// Invalidate v1
	require.NoError(t, st.InvalidateMemory(ctx, "mem-v1", time.Now().Add(-1*time.Hour)))

	// Store v2 that supersedes v1
	v2 := models.Memory{
		ID:           "mem-v2",
		Content:      "Ajit works at Booking.com",
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopePermanent,
		SupersedesID: "mem-v1",
		ValidFrom:    time.Now(),
	}
	require.NoError(t, st.Upsert(ctx, v2, make([]float32, 8)))

	// GetHistory starting from v2 should return both (v2 then v1 via chain)
	history, err := st.GetHistory(ctx, "mem-v2")
	require.NoError(t, err)
	require.NotEmpty(t, history, "history should not be empty")
	assert.Equal(t, "mem-v2", history[0].ID, "newest version should be first")
}

// TestTemporalFilteringDefaultExcludesInvalidated verifies that by default,
// Search and List exclude memories with valid_to set.
func TestTemporalFilteringDefaultExcludesInvalidated(t *testing.T) {
	ctx := context.Background()
	st := store.NewMockStore()

	// Active memory
	active := models.Memory{
		ID:        "active-mem",
		Content:   "Active memory content",
		Type:      models.MemoryTypeFact,
		Scope:     models.ScopePermanent,
		ValidFrom: time.Now().Add(-1 * time.Hour),
	}
	require.NoError(t, st.Upsert(ctx, active, make([]float32, 8)))

	// Invalidated memory
	invalidated := models.Memory{
		ID:        "invalidated-mem",
		Content:   "Invalidated memory content",
		Type:      models.MemoryTypeFact,
		Scope:     models.ScopePermanent,
		ValidFrom: time.Now().Add(-2 * time.Hour),
	}
	require.NoError(t, st.Upsert(ctx, invalidated, make([]float32, 8)))
	require.NoError(t, st.InvalidateMemory(ctx, "invalidated-mem", time.Now().Add(-30*time.Minute)))

	// List without IncludeInvalidated should only return active
	memories, _, err := st.List(ctx, nil, 100, "")
	require.NoError(t, err)

	ids := make([]string, 0, len(memories))
	for _, m := range memories {
		ids = append(ids, m.ID)
	}
	assert.Contains(t, ids, "active-mem", "active memory should be in results")
	assert.NotContains(t, ids, "invalidated-mem", "invalidated memory should NOT be in default results")
}

// TestIncludeInvalidatedFlag verifies that IncludeInvalidated=true returns all memories.
func TestIncludeInvalidatedFlag(t *testing.T) {
	ctx := context.Background()
	st := store.NewMockStore()

	active := models.Memory{
		ID:        "active-2",
		Content:   "Active",
		Type:      models.MemoryTypeFact,
		Scope:     models.ScopePermanent,
		ValidFrom: time.Now().Add(-1 * time.Hour),
	}
	require.NoError(t, st.Upsert(ctx, active, make([]float32, 8)))

	invalidated := models.Memory{
		ID:        "invalidated-2",
		Content:   "Invalidated",
		Type:      models.MemoryTypeFact,
		Scope:     models.ScopePermanent,
		ValidFrom: time.Now().Add(-2 * time.Hour),
	}
	require.NoError(t, st.Upsert(ctx, invalidated, make([]float32, 8)))
	require.NoError(t, st.InvalidateMemory(ctx, "invalidated-2", time.Now()))

	filters := &store.SearchFilters{IncludeInvalidated: true}
	memories, _, err := st.List(ctx, filters, 100, "")
	require.NoError(t, err)

	ids := make([]string, 0, len(memories))
	for _, m := range memories {
		ids = append(ids, m.ID)
	}
	assert.Contains(t, ids, "active-2")
	assert.Contains(t, ids, "invalidated-2", "should be included when IncludeInvalidated=true")
}

// TestUpsertSupersedes verifies that Upsert auto-invalidates superseded memory.
func TestUpsertSupersedes(t *testing.T) {
	ctx := context.Background()
	st := store.NewMockStore()

	// Store original
	original := models.Memory{
		ID:        "orig-mem",
		Content:   "Old fact",
		Type:      models.MemoryTypeFact,
		Scope:     models.ScopePermanent,
		ValidFrom: time.Now().Add(-1 * time.Hour),
	}
	require.NoError(t, st.Upsert(ctx, original, make([]float32, 8)))

	// Store superseding memory — this should auto-invalidate original
	newMem := models.Memory{
		ID:           "new-mem",
		Content:      "Updated fact",
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopePermanent,
		SupersedesID: "orig-mem",
	}
	require.NoError(t, st.Upsert(ctx, newMem, make([]float32, 8)))

	// Original should now be invalidated
	orig, err := st.Get(ctx, "orig-mem")
	require.NoError(t, err)
	assert.NotNil(t, orig.ValidTo, "superseded memory should have valid_to set")

	// New memory should be active
	updated, err := st.Get(ctx, "new-mem")
	require.NoError(t, err)
	assert.Nil(t, updated.ValidTo, "new superseding memory should be active")
}

// TestAsOfFilter verifies point-in-time recall.
func TestAsOfFilter(t *testing.T) {
	ctx := context.Background()
	st := store.NewMockStore()

	past := time.Now().Add(-10 * time.Hour)
	middle := time.Now().Add(-5 * time.Hour)
	recent := time.Now().Add(-1 * time.Hour)

	// Memory valid in the past, now invalidated
	oldMem := models.Memory{
		ID:        "old-fact",
		Content:   "Ajit works at Pixis",
		Type:      models.MemoryTypeFact,
		Scope:     models.ScopePermanent,
		ValidFrom: past,
	}
	require.NoError(t, st.Upsert(ctx, oldMem, make([]float32, 8)))
	require.NoError(t, st.InvalidateMemory(ctx, "old-fact", middle))

	// Memory valid now
	newMem := models.Memory{
		ID:        "new-fact",
		Content:   "Ajit works at Booking.com",
		Type:      models.MemoryTypeFact,
		Scope:     models.ScopePermanent,
		ValidFrom: middle,
	}
	require.NoError(t, st.Upsert(ctx, newMem, make([]float32, 8)))

	// As-of query in the past should return old-fact but NOT new-fact
	asOf := time.Now().Add(-7 * time.Hour) // between past and middle
	filters := &store.SearchFilters{AsOf: &asOf}
	memories, _, err := st.List(ctx, filters, 100, "")
	require.NoError(t, err)

	ids := make([]string, 0, len(memories))
	for _, m := range memories {
		ids = append(ids, m.ID)
	}
	assert.Contains(t, ids, "old-fact", "old fact was valid at as-of time")
	assert.NotContains(t, ids, "new-fact", "new fact did not exist at as-of time")

	// As-of query recent should return new-fact but NOT old-fact
	filters2 := &store.SearchFilters{AsOf: &recent}
	memories2, _, err := st.List(ctx, filters2, 100, "")
	require.NoError(t, err)

	ids2 := make([]string, 0, len(memories2))
	for _, m := range memories2 {
		ids2 = append(ids2, m.ID)
	}
	assert.Contains(t, ids2, "new-fact", "new fact is valid at recent time")
	assert.NotContains(t, ids2, "old-fact", "old fact was invalidated before recent time")
}
