package tests

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

// TestResetRequiresYesFlag verifies that `reset` without --yes exits non-zero
// and prints a message that explains how to confirm. This is the safety-critical
// guard that prevents accidental data loss.
func TestResetRequiresYesFlag(t *testing.T) {
	if !binExists() {
		t.Skip("binary not built; run: go build -o bin/openclaw-cortex ./cmd/openclaw-cortex")
	}

	// runCLI uses CombinedOutput, so out contains both stdout and stderr.
	// Cobra writes RunE errors to stderr, which is captured here.
	out, err := runCLI("reset")
	if err == nil {
		t.Fatal("reset without --yes should exit non-zero, but got nil error")
	}
	// The error message should guide the user toward the --yes flag.
	if !strings.Contains(out, "--yes") {
		t.Errorf("output does not mention --yes:\n%s", out)
	}
}

// TestResetMockStoreDeleteAllMemories verifies the ResettableStore contract:
// DeleteAllMemories removes all previously upserted memories. This validates
// the store behavior that cmd_reset.go depends on without requiring a live
// Memgraph instance or a running binary.
func TestResetMockStoreDeleteAllMemories(t *testing.T) {
	ctx := context.Background()
	ms := store.NewMockStore()

	// Seed a few memories.
	for _, content := range []string{"fact A", "fact B", "fact C"} {
		m := models.Memory{
			ID:           content, // reuse content as ID for simplicity
			Type:         models.MemoryTypeFact,
			Scope:        models.ScopePermanent,
			Visibility:   models.VisibilityShared,
			Content:      content,
			Confidence:   0.9,
			Source:       "test",
			CreatedAt:    time.Now().UTC(),
			UpdatedAt:    time.Now().UTC(),
			LastAccessed: time.Now().UTC(),
		}
		vec := testVector(0.1)
		if err := ms.Upsert(ctx, m, vec); err != nil {
			t.Fatalf("seed Upsert(%q): %v", content, err)
		}
	}

	// Verify seeding worked.
	memories, _, err := ms.List(ctx, nil, 100, "")
	if err != nil {
		t.Fatalf("List before delete: %v", err)
	}
	if len(memories) != 3 {
		t.Fatalf("want 3 memories before delete, got %d", len(memories))
	}

	// Exercise DeleteAllMemories via the ResettableStore interface, exactly
	// as cmd_reset.go does it.
	var rs store.ResettableStore = ms
	deleteErr := rs.DeleteAllMemories(ctx)
	if deleteErr != nil {
		t.Fatalf("DeleteAllMemories: %v", deleteErr)
	}

	// Store must be empty.
	memories, _, err = ms.List(ctx, nil, 100, "")
	if err != nil {
		t.Fatalf("List after delete: %v", err)
	}
	if len(memories) != 0 {
		t.Errorf("want 0 memories after DeleteAllMemories, got %d", len(memories))
	}
}

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
