package tests

import (
	"context"
	"sort"
	"testing"

	"github.com/ajitpratap0/openclaw-cortex/internal/graph"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
)

// TestSearchFacts_TextContainsFilter verifies that SearchFacts returns only
// facts whose text contains the query string (text-CONTAINS fallback path,
// used when no query embedding is provided).
func TestSearchFacts_TextContainsFilter(t *testing.T) {
	ctx := context.Background()
	gc := graph.NewMockGraphClient()

	facts := []models.Fact{
		{ID: "f1", Fact: "Alice works at ACME Corp", SourceEntityID: "e1", TargetEntityID: "e2"},
		{ID: "f2", Fact: "Bob lives in New York", SourceEntityID: "e3", TargetEntityID: "e4"},
		{ID: "f3", Fact: "Alice is friends with Bob", SourceEntityID: "e1", TargetEntityID: "e3"},
	}
	for i := range facts {
		if err := gc.UpsertFact(ctx, facts[i]); err != nil {
			t.Fatalf("UpsertFact %s: %v", facts[i].ID, err)
		}
	}

	// Query with no embedding — text-CONTAINS filter only.
	results, err := gc.SearchFacts(ctx, "Alice", nil, 10)
	if err != nil {
		t.Fatalf("SearchFacts: %v", err)
	}

	ids := factResultIDs(results)
	if !containsFactID(ids, "f1") {
		t.Errorf("expected f1 (contains 'Alice') in results, got %v", ids)
	}
	if !containsFactID(ids, "f3") {
		t.Errorf("expected f3 (contains 'Alice') in results, got %v", ids)
	}
	if containsFactID(ids, "f2") {
		t.Errorf("expected f2 (no 'Alice') excluded from results, got %v", ids)
	}
}

// TestSearchFacts_CosineRanking verifies that SearchFacts returns facts ordered
// by cosine similarity descending when a query embedding is provided.
func TestSearchFacts_CosineRanking(t *testing.T) {
	ctx := context.Background()
	gc := graph.NewMockGraphClient()

	// Three unit-ish embeddings with known relative similarities to the query.
	// query  = {1, 0, 0}
	// close  = {0.9, 0.1, 0} — high cosine with query
	// mid    = {0.5, 0.5, 0} — medium cosine with query
	// far    = {0, 0, 1}     — zero cosine with query
	facts := []models.Fact{
		{ID: "far", Fact: "unrelated fact", SourceEntityID: "e1", TargetEntityID: "e2",
			FactEmbedding: []float32{0, 0, 1}},
		{ID: "close", Fact: "very relevant fact", SourceEntityID: "e1", TargetEntityID: "e2",
			FactEmbedding: []float32{0.9, 0.1, 0}},
		{ID: "mid", Fact: "somewhat relevant fact", SourceEntityID: "e1", TargetEntityID: "e2",
			FactEmbedding: []float32{0.5, 0.5, 0}},
	}
	for i := range facts {
		if err := gc.UpsertFact(ctx, facts[i]); err != nil {
			t.Fatalf("UpsertFact %s: %v", facts[i].ID, err)
		}
	}

	query := []float32{1, 0, 0}
	results, err := gc.SearchFacts(ctx, "", query, 10)
	if err != nil {
		t.Fatalf("SearchFacts: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Results must be sorted by Score descending.
	if !sort.SliceIsSorted(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	}) {
		t.Errorf("results not sorted by Score descending: %v", results)
	}

	if results[0].ID != "close" {
		t.Errorf("expected 'close' as highest-ranked result, got %q (score %.4f)", results[0].ID, results[0].Score)
	}
	if results[2].ID != "far" {
		t.Errorf("expected 'far' as lowest-ranked result, got %q (score %.4f)", results[2].ID, results[2].Score)
	}
}

// TestSearchFacts_LimitRespected verifies that SearchFacts honors the limit
// parameter and returns at most that many results.
func TestSearchFacts_LimitRespected(t *testing.T) {
	ctx := context.Background()
	gc := graph.NewMockGraphClient()

	for i := range 5 {
		f := models.Fact{
			ID:   "f" + string(rune('0'+i)),
			Fact: "fact number",
		}
		if err := gc.UpsertFact(ctx, f); err != nil {
			t.Fatalf("UpsertFact: %v", err)
		}
	}

	results, err := gc.SearchFacts(ctx, "fact", nil, 3)
	if err != nil {
		t.Fatalf("SearchFacts: %v", err)
	}
	if len(results) > 3 {
		t.Errorf("expected at most 3 results, got %d", len(results))
	}
}

// --- helpers ---

func factResultIDs(results []graph.FactResult) []string {
	ids := make([]string, len(results))
	for i, r := range results {
		ids[i] = r.ID
	}
	return ids
}

func containsFactID(ids []string, id string) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}
