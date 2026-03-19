package tests

import (
	"strings"
	"testing"

	"github.com/ajitpratap0/openclaw-cortex/eval/longmemeval"
	"github.com/ajitpratap0/openclaw-cortex/eval/runner"
)

func TestLongMemEvalDatasetSize(t *testing.T) {
	pairs := longmemeval.Dataset()
	if len(pairs) != 10 {
		t.Errorf("Dataset() returned %d pairs, want 10", len(pairs))
	}
}

func TestLongMemEvalDatasetIDs(t *testing.T) {
	pairs := longmemeval.Dataset()
	seen := make(map[string]bool, len(pairs))
	for i := range pairs {
		id := pairs[i].ID
		if seen[id] {
			t.Errorf("duplicate QA pair ID: %s", id)
		}
		seen[id] = true
		if id == "" {
			t.Errorf("pair at index %d has empty ID", i)
		}
	}
}

func TestLongMemEvalDatasetCategories(t *testing.T) {
	valid := map[string]bool{
		"temporal":         true,
		"multi-hop":        true,
		"knowledge-update": true,
	}
	pairs := longmemeval.Dataset()
	for i := range pairs {
		cat := pairs[i].Category
		if !valid[cat] {
			t.Errorf("pair %s has unexpected category %q", pairs[i].ID, cat)
		}
	}
}

func TestLongMemEvalDatasetHasAllThreeCategories(t *testing.T) {
	counts := map[string]int{}
	pairs := longmemeval.Dataset()
	for i := range pairs {
		counts[pairs[i].Category]++
	}
	for _, cat := range []string{"temporal", "multi-hop", "knowledge-update"} {
		if counts[cat] == 0 {
			t.Errorf("no QA pairs found for category %q", cat)
		}
	}
}

func TestLongMemEvalDatasetHasFacts(t *testing.T) {
	pairs := longmemeval.Dataset()
	for i := range pairs {
		qp := &pairs[i]
		if len(qp.Facts) == 0 {
			t.Errorf("pair %s has no facts", qp.ID)
		}
		for j := range qp.Facts {
			if qp.Facts[j].Content == "" {
				t.Errorf("pair %s fact %d has empty Content", qp.ID, j)
			}
		}
	}
}

func TestLongMemEvalDatasetNonEmptyQA(t *testing.T) {
	pairs := longmemeval.Dataset()
	for i := range pairs {
		qp := &pairs[i]
		if qp.Question == "" {
			t.Errorf("pair %s has empty Question", qp.ID)
		}
		if qp.GroundTruth == "" {
			t.Errorf("pair %s has empty GroundTruth", qp.ID)
		}
	}
}

// TestLongMemEvalKnowledgeUpdateFactsHaveValidTo verifies that knowledge-update pairs
// include at least one fact marked with a ValidTo (superseded fact).
func TestLongMemEvalKnowledgeUpdateFactsHaveValidTo(t *testing.T) {
	pairs := longmemeval.Dataset()
	for i := range pairs {
		qp := &pairs[i]
		if qp.Category != "knowledge-update" {
			continue
		}
		hasSuperseded := false
		for j := range qp.Facts {
			if qp.Facts[j].ValidTo != "" {
				hasSuperseded = true
				break
			}
		}
		if !hasSuperseded {
			t.Errorf("knowledge-update pair %s has no fact with ValidTo set", qp.ID)
		}
	}
}

// TestLongMemEvalGroundTruthInFacts checks that each pair's GroundTruth appears (as a
// substring) in at least one of its ingested facts.
func TestLongMemEvalGroundTruthInFacts(t *testing.T) {
	pairs := longmemeval.Dataset()
	for i := range pairs {
		qp := &pairs[i]
		gt := qp.GroundTruth
		found := false
		for j := range qp.Facts {
			if strings.Contains(strings.ToLower(qp.Facts[j].Content), strings.ToLower(gt)) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("pair %s: ground truth %q not found in any fact", qp.ID, gt)
		}
	}
}

// TestLongMemEvalScoringOnSyntheticData runs scoring functions against hand-crafted
// retrieved strings to assert correctness without any binary dependency.
func TestLongMemEvalScoringOnSyntheticData(t *testing.T) {
	pairs := longmemeval.Dataset()
	for i := range pairs {
		qp := &pairs[i]
		perfectRetrieval := ""
		for j := range qp.Facts {
			if strings.Contains(strings.ToLower(qp.Facts[j].Content), strings.ToLower(qp.GroundTruth)) {
				perfectRetrieval = qp.Facts[j].Content
				break
			}
		}
		if perfectRetrieval == "" {
			continue // already caught by TestLongMemEvalGroundTruthInFacts
		}

		if !runner.ExactMatch(perfectRetrieval, qp.GroundTruth) {
			t.Errorf("pair %s: ExactMatch failed with perfect retrieval", qp.ID)
		}
		f1 := runner.TokenF1(perfectRetrieval, qp.GroundTruth)
		if f1 <= 0 {
			t.Errorf("pair %s: TokenF1 = %.4f with perfect retrieval, want > 0", qp.ID, f1)
		}
		if !runner.RecallAtK([]string{perfectRetrieval}, qp.GroundTruth, 1) {
			t.Errorf("pair %s: RecallAtK=1 failed with perfect retrieval", qp.ID)
		}
	}
}
