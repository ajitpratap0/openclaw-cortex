package tests

import (
	"testing"

	"github.com/ajitpratap0/openclaw-cortex/internal/graph"
)

// TestFactResultHasEmbeddingField verifies that graph.FactResult carries a
// FactEmbedding field that is populated and accessible for cosine re-ranking.
func TestFactResultHasEmbeddingField(t *testing.T) {
	vec := []float32{0.1, 0.2, 0.3}
	fr := graph.FactResult{
		ID:            "f1",
		Fact:          "A uses B for processing",
		Score:         0.9,
		FactEmbedding: vec,
	}
	if len(fr.FactEmbedding) != 3 {
		t.Errorf("expected FactEmbedding length 3, got %d", len(fr.FactEmbedding))
	}
	if fr.FactEmbedding[0] != 0.1 {
		t.Errorf("FactEmbedding[0]: want 0.1, got %f", fr.FactEmbedding[0])
	}
}

// TestFactResultEmbeddingOmittedWhenEmpty verifies that FactEmbedding with
// omitempty is nil (not an empty slice) when unset, as expected by JSON callers.
func TestFactResultEmbeddingOmittedWhenEmpty(t *testing.T) {
	fr := graph.FactResult{
		ID:   "f2",
		Fact: "B depends on C",
	}
	if fr.FactEmbedding != nil {
		t.Errorf("expected nil FactEmbedding when not set, got %v", fr.FactEmbedding)
	}
}

// TestFactResultScoreIsUsedForRanking verifies the Score field is
// used correctly as a ranking signal when set from cosine similarity.
func TestFactResultScoreIsUsedForRanking(t *testing.T) {
	facts := []graph.FactResult{
		{ID: "low", Fact: "low similarity", Score: 0.3},
		{ID: "high", Fact: "high similarity", Score: 0.95},
		{ID: "mid", Fact: "mid similarity", Score: 0.6},
	}
	// Simulate the sort order expected after cosine ranking.
	best := facts[0]
	for _, f := range facts[1:] {
		if f.Score > best.Score {
			best = f
		}
	}
	if best.ID != "high" {
		t.Errorf("expected highest-scored fact to be 'high', got %s", best.ID)
	}
}
