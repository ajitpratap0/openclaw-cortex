// Package tests provides integration-level tests for contradiction detection.
// Uses MockStore (no real Memgraph connection) to verify end-to-end behavior.
package tests

import (
	"context"
	"testing"
	"time"

	"github.com/ajitpratap0/openclaw-cortex/internal/capture"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

// makeVec returns a float32 slice for testing.
// Component 0 = val; components 1-3 are small constants so vectors with
// different val have distinct cosine similarity.
func makeVec(val float32) []float32 {
	return []float32{val, 0.1, 0.1, 0.1}
}

// putMemory upserts a memory into the store and returns it.
func putMemory(t *testing.T, st store.Store, id, content string, vec []float32) models.Memory {
	t.Helper()
	m := models.Memory{
		ID:        id,
		Content:   content,
		Type:      models.MemoryTypeFact,
		Scope:     models.ScopePermanent,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := st.Upsert(context.Background(), m, vec); err != nil {
		t.Fatalf("putMemory(%s): %v", id, err)
	}
	return m
}

// TestContradiction_WorksAt stores "Ajit works at Pixis", then checks that
// detecting contradictions for "Ajit works at Booking.com" flags the Pixis memory.
// It then calls InvalidateMemory and verifies ValidTo is set.
func TestContradiction_WorksAt(t *testing.T) {
	st := store.NewMockStore()

	// Store first memory — Ajit works at Pixis.
	pixisVec := makeVec(1.0)
	pixisMem := putMemory(t, st, "mem-pixis", "Ajit works at Pixis", pixisVec)

	// Build the contradicting memory — Ajit works at Booking.com.
	// Similar vector (same entity) but different employer.
	bookingVec := makeVec(0.98)
	bookingMem := models.Memory{
		ID:        "mem-booking",
		Content:   "Ajit works at Booking.com",
		Type:      models.MemoryTypeFact,
		Scope:     models.ScopePermanent,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// Detect contradictions (store has only pixisMem at this point).
	hits, err := capture.DetectContradictions(context.Background(), st, nil, bookingMem, bookingVec)
	if err != nil {
		t.Fatalf("DetectContradictions: %v", err)
	}

	found := false
	for _, h := range hits {
		if h.MemoryID == pixisMem.ID {
			found = true
			t.Logf("flagged contradiction: %s — %s", h.MemoryID, h.Reason)
		}
	}
	if !found {
		t.Errorf("expected Pixis memory (%s) to be flagged as contradicting, hits=%+v", pixisMem.ID, hits)
	}

	// Simulate the store pipeline: invalidate contradicted memories.
	now := time.Now().UTC()
	for _, h := range hits {
		if err := st.InvalidateMemory(context.Background(), h.MemoryID, now); err != nil {
			t.Errorf("InvalidateMemory(%s): %v", h.MemoryID, err)
		}
	}

	// Verify Pixis memory now has ValidTo set.
	got, err := st.Get(context.Background(), pixisMem.ID)
	if err != nil {
		t.Fatalf("Get(pixis): %v", err)
	}
	if got == nil {
		t.Fatal("Pixis memory not found after invalidation")
	}
	if got.ValidTo == nil {
		t.Error("expected Pixis memory.ValidTo to be set, got nil")
	} else {
		t.Logf("Pixis memory.ValidTo = %v ✓", got.ValidTo)
	}
}

// TestContradiction_NoFalsePositive stores two unrelated memories and verifies
// DetectContradictions does not flag them as contradictions.
func TestContradiction_NoFalsePositive(t *testing.T) {
	st := store.NewMockStore()

	// Store an unrelated memory — no exclusive-predicate signal.
	putMemory(t, st, "mem-coffee", "Ajit likes coffee in the morning", makeVec(0.5))

	// New memory — different topic, low similarity vector.
	newMem := models.Memory{
		ID:        "mem-drone",
		Content:   "Ajit is a certified drone pilot",
		Type:      models.MemoryTypeFact,
		Scope:     models.ScopePermanent,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	hits, err := capture.DetectContradictions(context.Background(), st, nil, newMem, makeVec(0.3))
	if err != nil {
		t.Fatalf("DetectContradictions: %v", err)
	}
	if len(hits) > 0 {
		t.Errorf("expected no contradictions for unrelated memories, got %d: %+v", len(hits), hits)
	}
	t.Log("no false positives ✓")
}

// TestInvalidateMemory_SetsValidTo verifies the InvalidateMemory API directly.
func TestInvalidateMemory_SetsValidTo(t *testing.T) {
	st := store.NewMockStore()
	mem := putMemory(t, st, "mem-loc", "Ajit lives in Bangalore", makeVec(0.7))

	invalidateAt := time.Now().UTC()
	if err := st.InvalidateMemory(context.Background(), mem.ID, invalidateAt); err != nil {
		t.Fatalf("InvalidateMemory: %v", err)
	}

	got, err := st.Get(context.Background(), mem.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("memory not found after invalidation")
	}
	if got.ValidTo == nil {
		t.Fatal("expected ValidTo to be set, got nil")
	}
	t.Logf("ValidTo = %v ✓", got.ValidTo)
}
