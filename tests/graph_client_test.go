package tests

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/graph"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
)

func TestMockGraphClient_EntityCRUD(t *testing.T) {
	c := graph.NewMockGraphClient()
	ctx := context.Background()

	entity := models.Entity{
		ID:   "e1",
		Name: "Alice",
		Type: models.EntityTypePerson,
	}

	if err := c.UpsertEntity(ctx, entity); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := c.GetEntity(ctx, "e1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "Alice" {
		t.Errorf("expected Alice, got %q", got.Name)
	}
}

func TestMockGraphClient_FactCRUD(t *testing.T) {
	c := graph.NewMockGraphClient()
	ctx := context.Background()

	fact := models.Fact{
		ID:             "f1",
		SourceEntityID: "e1",
		TargetEntityID: "e2",
		RelationType:   "WORKS_AT",
		Fact:           "Alice works at Acme",
		CreatedAt:      time.Now(),
		Confidence:     0.9,
	}

	if err := c.UpsertFact(ctx, fact); err != nil {
		t.Fatalf("upsert fact: %v", err)
	}

	facts, err := c.GetFactsBetween(ctx, "e1", "e2")
	if err != nil {
		t.Fatalf("get facts: %v", err)
	}
	if len(facts) != 1 {
		t.Fatalf("expected 1 fact, got %d", len(facts))
	}
	if facts[0].Fact != "Alice works at Acme" {
		t.Errorf("unexpected fact text: %q", facts[0].Fact)
	}
}

func TestMockGraphClient_InvalidateFact(t *testing.T) {
	c := graph.NewMockGraphClient()
	ctx := context.Background()

	now := time.Now()
	fact := models.Fact{
		ID:             "f1",
		SourceEntityID: "e1",
		TargetEntityID: "e2",
		Fact:           "Alice works at Acme",
		CreatedAt:      now,
	}
	if err := c.UpsertFact(ctx, fact); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	expiry := now.Add(time.Hour)
	if err := c.InvalidateFact(ctx, "f1", expiry, expiry); err != nil {
		t.Fatalf("invalidate: %v", err)
	}

	facts, getFErr := c.GetFactsBetween(ctx, "e1", "e2")
	if getFErr != nil {
		t.Fatalf("get facts: %v", getFErr)
	}
	if len(facts) != 0 {
		t.Errorf("expected 0 active facts after invalidation, got %d", len(facts))
	}
}

func TestMockGraphClient_Healthy(t *testing.T) {
	c := graph.NewMockGraphClient()
	if !c.Healthy(context.Background()) {
		t.Error("mock client should always be healthy")
	}
}

func TestMockGraphClient_GetFactsForEntity(t *testing.T) {
	c := graph.NewMockGraphClient()
	ctx := context.Background()

	c.UpsertFact(ctx, models.Fact{ID: "f1", SourceEntityID: "e1", TargetEntityID: "e2", Fact: "fact1", CreatedAt: time.Now()})
	c.UpsertFact(ctx, models.Fact{ID: "f2", SourceEntityID: "e3", TargetEntityID: "e1", Fact: "fact2", CreatedAt: time.Now()})
	c.UpsertFact(ctx, models.Fact{ID: "f3", SourceEntityID: "e2", TargetEntityID: "e3", Fact: "fact3", CreatedAt: time.Now()})

	facts, err := c.GetFactsForEntity(ctx, "e1")
	if err != nil {
		t.Fatalf("get facts: %v", err)
	}
	if len(facts) != 2 {
		t.Errorf("expected 2 facts involving e1, got %d", len(facts))
	}
}

func TestMockGraphClient_AppendEpisode(t *testing.T) {
	c := graph.NewMockGraphClient()
	ctx := context.Background()

	c.UpsertFact(ctx, models.Fact{ID: "f1", SourceEntityID: "e1", TargetEntityID: "e2", Fact: "fact1", CreatedAt: time.Now()})

	if err := c.AppendEpisode(ctx, "f1", "session-123"); err != nil {
		t.Fatalf("append episode: %v", err)
	}

	facts, _ := c.GetFactsBetween(ctx, "e1", "e2")
	if len(facts) != 1 {
		t.Fatalf("expected 1 fact, got %d", len(facts))
	}
	if len(facts[0].Episodes) != 1 || facts[0].Episodes[0] != "session-123" {
		t.Errorf("expected episodes [session-123], got %v", facts[0].Episodes)
	}
}

func TestMockGraphClient_RecallByGraph(t *testing.T) {
	c := graph.NewMockGraphClient()
	ctx := context.Background()

	c.UpsertFact(ctx, models.Fact{
		ID:              "f1",
		SourceEntityID:  "e1",
		TargetEntityID:  "e2",
		Fact:            "fact1",
		SourceMemoryIDs: []string{"mem-1", "mem-2"},
		CreatedAt:       time.Now(),
	})
	c.UpsertFact(ctx, models.Fact{
		ID:              "f2",
		SourceEntityID:  "e2",
		TargetEntityID:  "e3",
		Fact:            "fact2",
		SourceMemoryIDs: []string{"mem-2", "mem-3"},
		CreatedAt:       time.Now(),
	})

	memIDs, err := c.RecallByGraph(ctx, "test", nil, 10)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}

	// Should have 3 unique memory IDs: mem-1, mem-2, mem-3
	require.Len(t, memIDs, 3)
	seen := make(map[string]bool)
	for i := range memIDs {
		seen[memIDs[i]] = true
	}
	assert.True(t, seen["mem-1"], "expected mem-1 in results")
	assert.True(t, seen["mem-2"], "expected mem-2 in results")
	assert.True(t, seen["mem-3"], "expected mem-3 in results")
}

func TestMockGraphClient_SearchEntities(t *testing.T) {
	c := graph.NewMockGraphClient()
	ctx := context.Background()

	c.UpsertEntity(ctx, models.Entity{ID: "e1", Name: "Alice", Type: models.EntityTypePerson})
	c.UpsertEntity(ctx, models.Entity{ID: "e2", Name: "Bob", Type: models.EntityTypePerson})
	c.UpsertEntity(ctx, models.Entity{ID: "e3", Name: "Acme", Type: models.EntityTypeProject})

	results, err := c.SearchEntities(ctx, "alice", nil, "", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Name != "Alice" {
		t.Errorf("expected Alice, got %q", results[0].Name)
	}
}
