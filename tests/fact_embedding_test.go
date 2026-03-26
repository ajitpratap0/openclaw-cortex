package tests

import (
	"context"
	"errors"
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

// TestUpsertFact_AutoEmbedding verifies that when an embedder is configured via
// SetEmbedder, UpsertFact automatically computes and stores the embedding when
// the Fact.FactEmbedding field is empty, enabling cosine ranking in SearchFacts.
func TestUpsertFact_AutoEmbedding(t *testing.T) {
	ctx := context.Background()
	gc := graph.NewMockGraphClient()

	// Stub embedder: returns a fixed vector and counts calls.
	stubEmb := &stubEmbedder{vec: []float32{1, 0, 0}}
	gc.SetEmbedder(stubEmb)

	// Upsert a fact with NO pre-computed embedding.
	f := models.Fact{ID: "f1", Fact: "Alice works at ACME Corp", SourceEntityID: "e1", TargetEntityID: "e2"}
	if err := gc.UpsertFact(ctx, f); err != nil {
		t.Fatalf("UpsertFact: %v", err)
	}

	// Embedder must have been called exactly once.
	if stubEmb.calls != 1 {
		t.Errorf("expected embedder called 1 time, got %d", stubEmb.calls)
	}

	// SearchFacts with the same vector should find the fact (non-zero score).
	results, err := gc.SearchFacts(ctx, "", []float32{1, 0, 0}, 10)
	if err != nil {
		t.Fatalf("SearchFacts: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result after auto-embedding, got 0")
	}
	if results[0].ID != "f1" {
		t.Errorf("expected f1 as top result, got %q", results[0].ID)
	}
	if results[0].Score <= 0 {
		t.Errorf("expected positive cosine score, got %f", results[0].Score)
	}
}

// TestUpsertFact_SkipsEmbeddingWhenPresent verifies that SetEmbedder does not
// overwrite a fact embedding that was already set by the caller.
func TestUpsertFact_SkipsEmbeddingWhenPresent(t *testing.T) {
	ctx := context.Background()
	gc := graph.NewMockGraphClient()

	stubEmb := &stubEmbedder{vec: []float32{0, 1, 0}}
	gc.SetEmbedder(stubEmb)

	// Fact with a pre-computed embedding — embedder must NOT be called.
	f := models.Fact{
		ID: "f2", Fact: "Bob lives in New York",
		SourceEntityID: "e1", TargetEntityID: "e2",
		FactEmbedding: []float32{0, 0, 1},
	}
	if err := gc.UpsertFact(ctx, f); err != nil {
		t.Fatalf("UpsertFact: %v", err)
	}
	if stubEmb.calls != 0 {
		t.Errorf("embedder should not be called when FactEmbedding is already set, got %d calls", stubEmb.calls)
	}
}

// TestUpsertFact_EmbedderErrorIgnored verifies that an embedder error is non-fatal:
// the fact is stored without an embedding rather than returning an error.
func TestUpsertFact_EmbedderErrorIgnored(t *testing.T) {
	ctx := context.Background()
	gc := graph.NewMockGraphClient()

	gc.SetEmbedder(&failEmbedder{})

	f := models.Fact{ID: "f3", Fact: "some fact", SourceEntityID: "e1", TargetEntityID: "e2"}
	if err := gc.UpsertFact(ctx, f); err != nil {
		t.Errorf("UpsertFact should not fail when embedder errors, got: %v", err)
	}
	// Fact should still be retrievable via text search.
	results, err := gc.SearchFacts(ctx, "some fact", nil, 10)
	if err != nil {
		t.Fatalf("SearchFacts: %v", err)
	}
	if !containsFactID(factResultIDs(results), "f3") {
		t.Errorf("expected f3 in text search results after failed embedding, got %v", factResultIDs(results))
	}
}

// --- stub embedders ---

type stubEmbedder struct {
	vec   []float32
	calls int
}

func (s *stubEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	s.calls++
	return s.vec, nil
}
func (s *stubEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = s.vec
		s.calls++
	}
	return out, nil
}
func (s *stubEmbedder) Dimension() int { return len(s.vec) }

type failEmbedder struct{}

func (f *failEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, errors.New("embed: intentional test error")
}
func (f *failEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	return nil, errors.New("embed: intentional test error")
}
func (f *failEmbedder) Dimension() int { return 3 }

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
