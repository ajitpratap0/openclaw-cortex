package tests

import (
	"strings"
	"testing"

	"github.com/ajitpratap0/openclaw-cortex/eval/locomo"
	"github.com/ajitpratap0/openclaw-cortex/eval/runner"
)

func TestLoCoMoDatasetSize(t *testing.T) {
	pairs := locomo.Dataset()
	if len(pairs) != 10 {
		t.Errorf("Dataset() returned %d pairs, want 10", len(pairs))
	}
}

func TestLoCoMoDatasetIDs(t *testing.T) {
	pairs := locomo.Dataset()
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

func TestLoCoMoDatasetCategories(t *testing.T) {
	valid := map[string]bool{
		"single-hop": true,
		"multi-hop":  true,
		"temporal":   true,
	}
	pairs := locomo.Dataset()
	for i := range pairs {
		cat := pairs[i].Category
		if !valid[cat] {
			t.Errorf("pair %s has unexpected category %q", pairs[i].ID, cat)
		}
	}
}

func TestLoCoMoDatasetHasAllThreeCategories(t *testing.T) {
	counts := map[string]int{}
	pairs := locomo.Dataset()
	for i := range pairs {
		counts[pairs[i].Category]++
	}
	for _, cat := range []string{"single-hop", "multi-hop", "temporal"} {
		if counts[cat] == 0 {
			t.Errorf("no QA pairs found for category %q", cat)
		}
	}
}

func TestLoCoMoDatasetConversations(t *testing.T) {
	pairs := locomo.Dataset()
	for i := range pairs {
		qp := &pairs[i]
		if len(qp.Conversation) == 0 {
			t.Errorf("pair %s has no conversation turns", qp.ID)
		}
		for j := range qp.Conversation {
			turn := &qp.Conversation[j]
			if turn.User == "" {
				t.Errorf("pair %s turn %d has empty User field", qp.ID, j)
			}
			if turn.Assistant == "" {
				t.Errorf("pair %s turn %d has empty Assistant field", qp.ID, j)
			}
		}
	}
}

func TestLoCoMoDatasetNonEmptyQA(t *testing.T) {
	pairs := locomo.Dataset()
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

func TestLoCoMoCategoryBreakdown(t *testing.T) {
	// Build a synthetic summary matching the dataset's QA pair IDs.
	pairs := locomo.Dataset()
	results := make([]runner.BenchmarkResult, len(pairs))
	for i := range pairs {
		results[i] = runner.BenchmarkResult{
			QuestionID: pairs[i].ID,
			ExactMatch: true,
		}
	}
	summary := runner.Summarize("LoCoMo", results, 5)

	breakdown := locomo.CategoryBreakdown(summary)
	for _, cat := range []string{"single-hop", "multi-hop", "temporal"} {
		acc, ok := breakdown[cat]
		if !ok {
			t.Errorf("category %q missing from breakdown", cat)
			continue
		}
		if acc != 1.0 {
			t.Errorf("category %q accuracy = %.2f, want 1.0 (all ExactMatch=true)", cat, acc)
		}
	}
}

func TestLoCoMoFormatCategoryTable(t *testing.T) {
	breakdown := map[string]float64{
		"single-hop": 1.0,
		"multi-hop":  0.5,
		"temporal":   0.333,
	}
	table := locomo.FormatCategoryTable(breakdown)
	if table == "" {
		t.Error("FormatCategoryTable returned empty string")
	}
	for _, cat := range []string{"single-hop", "multi-hop", "temporal"} {
		if !strings.Contains(table, cat) {
			t.Errorf("table missing category %q", cat)
		}
	}
}
