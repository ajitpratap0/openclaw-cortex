package tests

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/graph"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
)

func TestFactResolver_ExactDuplicate(t *testing.T) {
	mock := graph.NewMockGraphClient()
	ctx := context.Background()

	// Pre-populate with an existing fact.
	existingFact := models.Fact{
		ID:             "fact-1",
		SourceEntityID: "entity-a",
		TargetEntityID: "entity-b",
		RelationType:   "works_with",
		Fact:           "Alice works with Bob on Project X",
		Confidence:     0.9,
	}
	if err := mock.UpsertFact(ctx, existingFact); err != nil {
		t.Fatalf("upsert existing fact: %v", err)
	}

	// Create resolver with no API key (won't matter — fast path should trigger).
	resolver := graph.NewFactResolver(mock, "", "claude-3-haiku-20240307", slog.Default())

	newFact := models.Fact{
		SourceEntityID: "entity-a",
		TargetEntityID: "entity-b",
		Fact:           "Alice works with Bob on Project X",
	}

	action, ids, err := resolver.Resolve(ctx, newFact, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != graph.FactActionSkip {
		t.Errorf("expected FactActionSkip, got %s", action)
	}
	if len(ids) != 1 || ids[0] != "fact-1" {
		t.Errorf("expected affected ID [fact-1], got %v", ids)
	}
}

func TestFactResolver_NewFact(t *testing.T) {
	mock := graph.NewMockGraphClient()
	ctx := context.Background()

	// Pre-populate with an existing fact between different entities.
	existingFact := models.Fact{
		ID:             "fact-1",
		SourceEntityID: "entity-a",
		TargetEntityID: "entity-b",
		Fact:           "Alice works with Bob",
		Confidence:     0.9,
	}
	if err := mock.UpsertFact(ctx, existingFact); err != nil {
		t.Fatalf("upsert existing fact: %v", err)
	}

	resolver := graph.NewFactResolver(mock, "", "claude-3-haiku-20240307", slog.Default())

	// New fact between completely different entities → no candidates.
	newFact := models.Fact{
		SourceEntityID: "entity-c",
		TargetEntityID: "entity-d",
		Fact:           "Carol manages Dave",
	}

	action, ids, err := resolver.Resolve(ctx, newFact, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != graph.FactActionInsert {
		t.Errorf("expected FactActionInsert, got %s", action)
	}
	if len(ids) != 0 {
		t.Errorf("expected no affected IDs, got %v", ids)
	}
}

func TestFactResolver_NoCandidates(t *testing.T) {
	mock := graph.NewMockGraphClient()
	ctx := context.Background()

	// No facts at all in the graph.
	resolver := graph.NewFactResolver(mock, "", "claude-3-haiku-20240307", slog.Default())

	newFact := models.Fact{
		SourceEntityID: "entity-a",
		TargetEntityID: "entity-b",
		Fact:           "Alice mentors Bob",
	}

	action, ids, err := resolver.Resolve(ctx, newFact, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != graph.FactActionInsert {
		t.Errorf("expected FactActionInsert, got %s", action)
	}
	if len(ids) != 0 {
		t.Errorf("expected no affected IDs, got %v", ids)
	}
}

func TestFactResolver_GracefulDegradation(t *testing.T) {
	mock := graph.NewMockGraphClient()
	ctx := context.Background()

	// Pre-populate with an existing fact between the same entities
	// but with different text (so fast path won't trigger).
	existingFact := models.Fact{
		ID:             "fact-1",
		SourceEntityID: "entity-a",
		TargetEntityID: "entity-b",
		Fact:           "Alice likes working with Bob",
		Confidence:     0.9,
	}
	if err := mock.UpsertFact(ctx, existingFact); err != nil {
		t.Fatalf("upsert existing fact: %v", err)
	}

	// Empty API key → no Claude client → should treat as new fact.
	resolver := graph.NewFactResolver(mock, "", "claude-3-haiku-20240307", slog.Default())

	newFact := models.Fact{
		SourceEntityID: "entity-a",
		TargetEntityID: "entity-b",
		Fact:           "Alice enjoys collaborating with Bob",
	}

	action, ids, err := resolver.Resolve(ctx, newFact, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != graph.FactActionInsert {
		t.Errorf("expected FactActionInsert (graceful degradation), got %s", action)
	}
	if len(ids) != 0 {
		t.Errorf("expected no affected IDs, got %v", ids)
	}
}

func TestFactInvalidation_MockGraphClient(t *testing.T) {
	gc := graph.NewMockGraphClient()
	ctx := context.Background()

	now := time.Now().UTC()
	fact := models.Fact{
		ID:             "fact-1",
		SourceEntityID: "entity-a",
		TargetEntityID: "entity-b",
		RelationType:   "WORKS_AT",
		Fact:           "Alice works at Acme",
		CreatedAt:      now,
		Confidence:     0.9,
	}
	require.NoError(t, gc.UpsertFact(ctx, fact))

	// Verify fact is searchable before invalidation
	results, err := gc.SearchFacts(ctx, "", nil, 10)
	require.NoError(t, err)
	require.Len(t, results, 1)

	// Invalidate the fact (simulating contradiction detection)
	expiredAt := now.Add(time.Hour)
	invalidAt := now.Add(time.Hour)
	require.NoError(t, gc.InvalidateFact(ctx, fact.ID, expiredAt, invalidAt))

	// Verify fact is no longer returned by SearchFacts (which filters expired)
	results, err = gc.SearchFacts(ctx, "", nil, 10)
	require.NoError(t, err)
	require.Len(t, results, 0)

	// Verify fact is also excluded from GetFactsForEntity
	entityFacts, err := gc.GetFactsForEntity(ctx, "entity-a")
	require.NoError(t, err)
	require.Len(t, entityFacts, 0)

	// Verify RecallByGraph also excludes invalidated facts
	memIDs, err := gc.RecallByGraph(ctx, "", nil, 10)
	require.NoError(t, err)
	require.Len(t, memIDs, 0)
}

func TestFactActionConstants(t *testing.T) {
	assert.Equal(t, graph.FactAction("insert"), graph.FactActionInsert)
	assert.Equal(t, graph.FactAction("skip"), graph.FactActionSkip)
	assert.Equal(t, graph.FactAction("invalidate"), graph.FactActionInvalidate)
}
