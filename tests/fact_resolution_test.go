package tests

import (
	"context"
	"log/slog"
	"testing"

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
		t.Errorf("expected FactActionSkip, got %d", action)
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
		t.Errorf("expected FactActionInsert, got %d", action)
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
		t.Errorf("expected FactActionInsert, got %d", action)
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
		t.Errorf("expected FactActionInsert (graceful degradation), got %d", action)
	}
	if len(ids) != 0 {
		t.Errorf("expected no affected IDs, got %v", ids)
	}
}
