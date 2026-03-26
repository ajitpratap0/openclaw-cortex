package tests

import (
	"context"
	"testing"
	"time"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

// TestValidBeforeFilter verifies that ValidBefore filters out memories whose
// valid_from is after the cutoff, and includes those at or before it.
func TestValidBeforeFilter(t *testing.T) {
	ctx := context.Background()
	ms := store.NewMockStore()

	jan := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	feb := time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC)
	mar := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)

	temporalMustStore(t, ctx, ms, temporalMakeMemory("mem-jan", "January fact", jan))
	temporalMustStore(t, ctx, ms, temporalMakeMemory("mem-feb", "February fact", feb))
	temporalMustStore(t, ctx, ms, temporalMakeMemory("mem-mar", "March fact", mar))

	cutoff := time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC)
	filters := &store.SearchFilters{ValidBefore: &cutoff, IncludeInvalidated: true}

	results, err := ms.Search(ctx, temporalDummyVec(), 10, filters)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	ids := temporalResultIDs(results)
	if !temporalContains(ids, "mem-jan") {
		t.Errorf("expected mem-jan in results (valid_from Jan <= Feb cutoff), got %v", ids)
	}
	if !temporalContains(ids, "mem-feb") {
		t.Errorf("expected mem-feb in results (valid_from Feb <= Feb cutoff), got %v", ids)
	}
	if temporalContains(ids, "mem-mar") {
		t.Errorf("expected mem-mar excluded (valid_from Mar > Feb cutoff), got %v", ids)
	}
}

// TestValidAfterFilter verifies that ValidAfter filters out memories whose
// valid_from is before the cutoff, and includes those at or after it.
func TestValidAfterFilter(t *testing.T) {
	ctx := context.Background()
	ms := store.NewMockStore()

	jan := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	feb := time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC)
	mar := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)

	temporalMustStore(t, ctx, ms, temporalMakeMemory("mem-jan", "January fact", jan))
	temporalMustStore(t, ctx, ms, temporalMakeMemory("mem-feb", "February fact", feb))
	temporalMustStore(t, ctx, ms, temporalMakeMemory("mem-mar", "March fact", mar))

	cutoff := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	filters := &store.SearchFilters{ValidAfter: &cutoff, IncludeInvalidated: true}

	results, err := ms.Search(ctx, temporalDummyVec(), 10, filters)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	ids := temporalResultIDs(results)
	if temporalContains(ids, "mem-jan") {
		t.Errorf("expected mem-jan excluded (valid_from Jan < Feb cutoff), got %v", ids)
	}
	if !temporalContains(ids, "mem-feb") {
		t.Errorf("expected mem-feb in results (valid_from Feb >= Feb cutoff), got %v", ids)
	}
	if !temporalContains(ids, "mem-mar") {
		t.Errorf("expected mem-mar in results (valid_from Mar >= Feb cutoff), got %v", ids)
	}
}

// TestValidBeforeAndAfterCombined verifies that both filters can be applied
// together as a range.
func TestValidBeforeAndAfterCombined(t *testing.T) {
	ctx := context.Background()
	ms := store.NewMockStore()

	jan := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	feb := time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC)
	mar := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	apr := time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC)

	temporalMustStore(t, ctx, ms, temporalMakeMemory("mem-jan", "January", jan))
	temporalMustStore(t, ctx, ms, temporalMakeMemory("mem-feb", "February", feb))
	temporalMustStore(t, ctx, ms, temporalMakeMemory("mem-mar", "March", mar))
	temporalMustStore(t, ctx, ms, temporalMakeMemory("mem-apr", "April", apr))

	after := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	before := time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC)
	filters := &store.SearchFilters{ValidAfter: &after, ValidBefore: &before, IncludeInvalidated: true}

	results, err := ms.Search(ctx, temporalDummyVec(), 10, filters)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	ids := temporalResultIDs(results)
	if temporalContains(ids, "mem-jan") {
		t.Errorf("mem-jan should be excluded (before range)")
	}
	if !temporalContains(ids, "mem-feb") {
		t.Errorf("mem-feb should be included")
	}
	if !temporalContains(ids, "mem-mar") {
		t.Errorf("mem-mar should be included")
	}
	if temporalContains(ids, "mem-apr") {
		t.Errorf("mem-apr should be excluded (after range)")
	}
}

// TestValidAfterRelativeRange verifies filtering to the last 7 days using
// a ValidAfter cutoff relative to now.
func TestValidAfterRelativeRange(t *testing.T) {
	ctx := context.Background()
	ms := store.NewMockStore()

	recent := time.Now().UTC().Add(-24 * time.Hour)
	old := time.Now().UTC().Add(-30 * 24 * time.Hour)

	temporalMustStore(t, ctx, ms, temporalMakeMemory("mem-recent", "recent memory", recent))
	temporalMustStore(t, ctx, ms, temporalMakeMemory("mem-old", "old memory", old))

	// Filter to memories in the last 7 days.
	cutoff := time.Now().UTC().Add(-7 * 24 * time.Hour)
	filters := &store.SearchFilters{ValidAfter: &cutoff, IncludeInvalidated: true}
	filtered, err := ms.Search(ctx, temporalDummyVec(), 10, filters)
	if err != nil {
		t.Fatalf("Search with ValidAfter: %v", err)
	}
	ids := temporalResultIDs(filtered)
	if !temporalContains(ids, "mem-recent") {
		t.Errorf("expected mem-recent in results, got %v", ids)
	}
	if temporalContains(ids, "mem-old") {
		t.Errorf("expected mem-old excluded by ValidAfter, got %v", ids)
	}
}

// --- helpers local to this file (prefixed to avoid collisions) ---

func temporalMakeMemory(id, content string, validFrom time.Time) models.Memory {
	return models.Memory{
		ID:         id,
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Content:    content,
		Confidence: 0.9,
		Source:     "test",
		CreatedAt:  validFrom,
		UpdatedAt:  validFrom,
		ValidFrom:  validFrom,
	}
}

func temporalMustStore(t *testing.T, ctx context.Context, ms *store.MockStore, mem models.Memory) {
	t.Helper()
	if err := ms.Upsert(ctx, mem, temporalDummyVec()); err != nil {
		t.Fatalf("Upsert %s: %v", mem.ID, err)
	}
}

func temporalDummyVec() []float32 {
	v := make([]float32, 768)
	v[0] = 1.0
	return v
}

func temporalResultIDs(results []models.SearchResult) []string {
	ids := make([]string, len(results))
	for i := range results {
		ids[i] = results[i].Memory.ID
	}
	return ids
}

func temporalContains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
