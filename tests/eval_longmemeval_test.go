package tests

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

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
			if qp.Facts[j].DatasetValidTo != "" {
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

// --- Run() control-flow tests (stub client, no binary required) ---

// TestLongMemEvalRunHappyPath verifies that Run() returns a non-error summary with
// TotalQuestions matching the dataset size when Reset, Store, and Recall all succeed.
func TestLongMemEvalRunHappyPath(t *testing.T) {
	pairs := longmemeval.Dataset()
	stub := &stubHarnessClient{
		recallResp: []string{"answer content"},
	}
	summary, err := longmemeval.Run(context.Background(), stub, 5)
	require.NoError(t, err)
	require.NotNil(t, summary)
	require.Equal(t, len(pairs), summary.TotalQuestions)
	require.Equal(t, 0, summary.RecallFailures)
}

// TestLongMemEvalRunResetFailure verifies that Run() propagates a Reset error
// and aborts immediately rather than producing a partial summary.
func TestLongMemEvalRunResetFailure(t *testing.T) {
	stub := &stubHarnessClient{
		resetErr: errors.New("stub: reset failed"),
	}
	summary, err := longmemeval.Run(context.Background(), stub, 5)
	require.Error(t, err)
	require.Nil(t, summary)
	require.ErrorContains(t, err, "reset failed")
}

// TestLongMemEvalRunAllRecallFail verifies that Run() returns an error (not a
// partial summary) when every Recall call fails — the all-fail guard.
func TestLongMemEvalRunAllRecallFail(t *testing.T) {
	pairs := longmemeval.Dataset()
	stub := &stubHarnessClient{
		recallErrs: recallErrors(len(pairs)),
	}
	summary, err := longmemeval.Run(context.Background(), stub, 5)
	require.Error(t, err)
	require.Nil(t, summary)
	require.ErrorContains(t, err, "recall calls failed")
}

// TestLongMemEvalRunPartialRecallFail verifies that Run() returns a summary
// (not an error) when only some Recall calls fail, and RecallFailures
// reflects the number of failed calls.
func TestLongMemEvalRunPartialRecallFail(t *testing.T) {
	pairs := longmemeval.Dataset()
	// First pair succeeds; the rest fail.
	errs := recallErrors(len(pairs))
	errs[0] = nil
	stub := &stubHarnessClient{
		recallErrs: errs,
		recallResp: []string{"answer content"},
	}
	summary, err := longmemeval.Run(context.Background(), stub, 5)
	require.NoError(t, err)
	require.NotNil(t, summary)
	require.Equal(t, len(pairs)-1, summary.RecallFailures)
}

// TestLongMemEvalRunContextCancel verifies that Run() returns a context error
// when the context is already canceled on entry.
func TestLongMemEvalRunContextCancel(t *testing.T) {
	stub := &stubHarnessClient{
		recallResp: []string{"answer content"},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before calling Run
	summary, err := longmemeval.Run(ctx, stub, 5)
	require.Error(t, err)
	require.Nil(t, summary)
	require.ErrorContains(t, err, "context canceled")
}
