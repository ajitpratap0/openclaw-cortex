package longmemeval_test

import (
	"testing"

	"github.com/ajitpratap0/openclaw-cortex/eval/longmemeval"
	"github.com/ajitpratap0/openclaw-cortex/eval/runner"
)

func TestDatasetSize(t *testing.T) {
	pairs := longmemeval.Dataset()
	if len(pairs) != 10 {
		t.Errorf("Dataset() returned %d pairs, want 10", len(pairs))
	}
}

func TestDatasetIDs(t *testing.T) {
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

func TestDatasetCategories(t *testing.T) {
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

func TestDatasetHasAllThreeCategories(t *testing.T) {
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

func TestDatasetHasFacts(t *testing.T) {
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

func TestDatasetNonEmptyQA(t *testing.T) {
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

// TestKnowledgeUpdateFactsHaveValidTo verifies that knowledge-update pairs
// include at least one fact marked with a ValidTo (superseded fact).
func TestKnowledgeUpdateFactsHaveValidTo(t *testing.T) {
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

// TestGroundTruthInFacts checks that each pair's GroundTruth appears (as a
// substring) in at least one of its ingested facts.  This validates that the
// synthetic data is self-consistent: the answer can in principle be retrieved.
func TestGroundTruthInFacts(t *testing.T) {
	pairs := longmemeval.Dataset()
	for i := range pairs {
		qp := &pairs[i]
		gt := qp.GroundTruth
		found := false
		for j := range qp.Facts {
			if containsCI(qp.Facts[j].Content, gt) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("pair %s: ground truth %q not found in any fact", qp.ID, gt)
		}
	}
}

// TestScoringOnSyntheticData runs scoring functions against hand-crafted
// retrieved strings to assert correctness without any binary dependency.
func TestScoringOnSyntheticData(t *testing.T) {
	pairs := longmemeval.Dataset()
	// For each pair, simulate a perfect retrieval (return a fact containing GT).
	for i := range pairs {
		qp := &pairs[i]
		// Find the fact that contains the ground truth.
		perfectRetrieval := ""
		for j := range qp.Facts {
			if containsCI(qp.Facts[j].Content, qp.GroundTruth) {
				perfectRetrieval = qp.Facts[j].Content
				break
			}
		}
		if perfectRetrieval == "" {
			continue // already caught by TestGroundTruthInFacts
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

func containsCI(s, sub string) bool {
	sl := toLower(s)
	subl := toLower(sub)
	for i := 0; i+len(subl) <= len(sl); i++ {
		if sl[i:i+len(subl)] == subl {
			return true
		}
	}
	return false
}

func toLower(s string) string {
	b := make([]byte, len(s))
	for i := range s {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		b[i] = c
	}
	return string(b)
}
