package tests

import (
	"context"
	"testing"
	"time"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

// dummyVec returns a deterministic unit-ish float32 slice of length n.
func dummyVec(n int, seed float32) []float32 {
	v := make([]float32, n)
	for i := range v {
		v[i] = seed
	}
	return v
}

func TestUserID_StoredAndRetrieved(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	mem := models.Memory{
		ID:         "mem-alice-1",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Visibility: models.VisibilityPrivate,
		Content:    "Alice prefers dark mode",
		Confidence: 0.9,
		UserID:     "user-alice",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}

	if err := s.Upsert(ctx, mem, dummyVec(768, 0.1)); err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}

	got, err := s.Get(ctx, "mem-alice-1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if got.UserID != "user-alice" {
		t.Errorf("UserID: got %q, want %q", got.UserID, "user-alice")
	}
}

func TestUserID_FilterIsolation(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	now := time.Now().UTC()
	// Same vector so cosine similarity is equal — isolation is purely by UserID filter.
	vec := dummyVec(768, 0.5)

	alice := models.Memory{
		ID:         "mem-alice-2",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Visibility: models.VisibilityPrivate,
		Content:    "Alice likes Go",
		Confidence: 0.8,
		UserID:     "user-alice",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	bob := models.Memory{
		ID:         "mem-bob-2",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Visibility: models.VisibilityPrivate,
		Content:    "Bob likes Rust",
		Confidence: 0.8,
		UserID:     "user-bob",
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	if err := s.Upsert(ctx, alice, vec); err != nil {
		t.Fatalf("Upsert alice: %v", err)
	}
	if err := s.Upsert(ctx, bob, vec); err != nil {
		t.Fatalf("Upsert bob: %v", err)
	}

	// Search with filters.UserID = "user-alice" — should return alice's memory only.
	aliceFilters := &store.SearchFilters{UserID: "user-alice"}
	aliceResults, err := s.Search(ctx, vec, 10, aliceFilters)
	if err != nil {
		t.Fatalf("Search alice: %v", err)
	}
	for _, r := range aliceResults {
		if r.Memory.UserID != "user-alice" {
			t.Errorf("alice search returned memory with UserID=%q (id=%s)", r.Memory.UserID, r.Memory.ID)
		}
	}
	found := false
	for _, r := range aliceResults {
		if r.Memory.ID == "mem-alice-2" {
			found = true
		}
	}
	if !found {
		t.Error("alice search did not return alice's memory")
	}
	for _, r := range aliceResults {
		if r.Memory.ID == "mem-bob-2" {
			t.Error("alice search returned bob's memory")
		}
	}

	// Search with filters.UserID = "user-bob" — should return bob's memory only.
	bobFilters := &store.SearchFilters{UserID: "user-bob"}
	bobResults, err := s.Search(ctx, vec, 10, bobFilters)
	if err != nil {
		t.Fatalf("Search bob: %v", err)
	}
	for _, r := range bobResults {
		if r.Memory.UserID != "user-bob" {
			t.Errorf("bob search returned memory with UserID=%q (id=%s)", r.Memory.UserID, r.Memory.ID)
		}
	}
	found = false
	for _, r := range bobResults {
		if r.Memory.ID == "mem-bob-2" {
			found = true
		}
	}
	if !found {
		t.Error("bob search did not return bob's memory")
	}
	for _, r := range bobResults {
		if r.Memory.ID == "mem-alice-2" {
			t.Error("bob search returned alice's memory")
		}
	}

	// Search with no UserID filter — should return both.
	allFilters := &store.SearchFilters{}
	allResults, err := s.Search(ctx, vec, 10, allFilters)
	if err != nil {
		t.Fatalf("Search all: %v", err)
	}
	ids := make(map[string]bool)
	for _, r := range allResults {
		ids[r.Memory.ID] = true
	}
	if !ids["mem-alice-2"] {
		t.Error("unfiltered search missing alice's memory")
	}
	if !ids["mem-bob-2"] {
		t.Error("unfiltered search missing bob's memory")
	}
}

func TestUserID_Empty_NoBreakingChange(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	now := time.Now().UTC()
	vec := dummyVec(768, 0.3)

	legacy := models.Memory{
		ID:         "mem-legacy-3",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Visibility: models.VisibilityPrivate,
		Content:    "Legacy memory with no user",
		Confidence: 0.7,
		UserID:     "", // empty = unscoped legacy
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	if err := s.Upsert(ctx, legacy, vec); err != nil {
		t.Fatalf("Upsert legacy: %v", err)
	}

	// Search with no filter — should still find it.
	results, err := s.Search(ctx, vec, 10, &store.SearchFilters{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	found := false
	for _, r := range results {
		if r.Memory.ID == "mem-legacy-3" {
			found = true
			if r.Memory.UserID != "" {
				t.Errorf("legacy memory UserID: got %q, want empty string", r.Memory.UserID)
			}
		}
	}
	if !found {
		t.Error("legacy memory not found in unfiltered search")
	}

	// Get by ID — UserID should be empty string.
	got, err := s.Get(ctx, "mem-legacy-3")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.UserID != "" {
		t.Errorf("legacy memory UserID after Get: got %q, want empty string", got.UserID)
	}
}
