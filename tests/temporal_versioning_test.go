package tests

import (
	"context"
	"testing"
	"time"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

// TestMockStoreTemporalVersioning verifies the v0.8.0 temporal-versioning contract:
// InvalidateMemory + AsOf point-in-time queries work correctly in MockStore.
//
// The scenario mirrors what the longmemeval harness would exercise for
// knowledge-update pairs (lme-K*) if --supersedes were passed:
// 1. An old fact is stored and then invalidated (ValidTo set).
// 2. A replacement fact is stored with a later ValidFrom.
// 3. A point-in-time query (AsOf between the two) must return only the old fact.
// 4. A default query (no AsOf) must return only the replacement.
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
