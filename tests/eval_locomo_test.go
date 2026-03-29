package tests

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

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
	summary := runner.Summarize("LoCoMo", results, 5, 0)

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

// TestLoCoMoGroundTruthInConversations checks that each pair's GroundTruth
// appears (as a case-insensitive substring) in at least one of the pair's
// conversation turns. Without this, a dataset bug would silently produce
// deflated scores with no failing test.
func TestLoCoMoGroundTruthInConversations(t *testing.T) {
	pairs := locomo.Dataset()
	for i := range pairs {
		qp := &pairs[i]
		gt := strings.ToLower(qp.GroundTruth)
		found := false
		for _, turn := range qp.Conversation {
			if strings.Contains(strings.ToLower(turn.User), gt) ||
				strings.Contains(strings.ToLower(turn.Assistant), gt) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("pair %s: ground truth %q not found in any conversation turn", qp.ID, qp.GroundTruth)
		}
	}
}

// --- Run() control-flow tests (stub client, no binary required) ---

// TestLoCoMoRunHappyPath verifies that Run() returns a non-error summary with
// TotalQuestions matching the dataset size when Reset, Store, and Recall all succeed.
func TestLoCoMoRunHappyPath(t *testing.T) {
	pairs := locomo.Dataset()
	stub := &stubHarnessClient{
		recallResp: []string{"answer content"},
	}
	summary, err := locomo.Run(context.Background(), stub, 5, false)
	require.NoError(t, err)
	require.NotNil(t, summary)
	require.Equal(t, len(pairs), summary.TotalQuestions)
	require.Equal(t, 0, summary.RecallFailures)
}

// TestLoCoMoRunResetFailure verifies that Run() propagates a Reset error
// and aborts immediately rather than producing a partial summary.
func TestLoCoMoRunResetFailure(t *testing.T) {
	stub := &stubHarnessClient{
		resetErr: errors.New("stub: reset failed"),
	}
	summary, err := locomo.Run(context.Background(), stub, 5, false)
	require.Error(t, err)
	require.Nil(t, summary)
	require.ErrorContains(t, err, "reset failed")
}

// TestLoCoMoRunAllRecallFail verifies that Run() returns an error (not a
// partial summary) when every Recall call fails — the all-fail guard.
func TestLoCoMoRunAllRecallFail(t *testing.T) {
	pairs := locomo.Dataset()
	stub := &stubHarnessClient{
		recallErrs: recallErrors(len(pairs)),
	}
	summary, err := locomo.Run(context.Background(), stub, 5, false)
	require.Error(t, err)
	require.Nil(t, summary)
	require.ErrorContains(t, err, "recall calls failed")
}

// TestLoCoMoRunPartialRecallFail verifies that Run() returns a summary
// (not an error) when only some Recall calls fail, and RecallFailures
// reflects the number of failed calls.
func TestLoCoMoRunPartialRecallFail(t *testing.T) {
	pairs := locomo.Dataset()
	// First pair succeeds; the rest fail.
	errs := recallErrors(len(pairs))
	errs[0] = nil
	stub := &stubHarnessClient{
		recallErrs: errs,
		recallResp: []string{"answer content"},
	}
	summary, err := locomo.Run(context.Background(), stub, 5, false)
	require.NoError(t, err)
	require.NotNil(t, summary)
	require.Equal(t, len(pairs)-1, summary.RecallFailures)
}

// TestLoCoMoRunStoreFailure verifies that Run() propagates a Store error
// and aborts immediately rather than producing a partial summary.
func TestLoCoMoRunStoreFailure(t *testing.T) {
	stub := &stubHarnessClient{
		storeErr: errors.New("stub: store failed"),
	}
	summary, err := locomo.Run(context.Background(), stub, 5, false)
	require.Error(t, err)
	require.Nil(t, summary)
	require.ErrorContains(t, err, "ingest turn failed")
}

// TestLoCoMoRunContextCancel verifies that Run() returns a context error
// when the context is already canceled on entry.
func TestLoCoMoRunContextCancel(t *testing.T) {
	stub := &stubHarnessClient{
		recallResp: []string{"answer content"},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before calling Run
	summary, err := locomo.Run(ctx, stub, 5, false)
	require.Error(t, err)
	require.Nil(t, summary)
	require.ErrorContains(t, err, "context canceled")
}

// TestLocomo_AccumulateMode_TwoPass verifies the accumulate=true protocol:
//   - Reset is called exactly once (at the start).
//   - Store is called once per conversation turn across ALL pairs (pass 1) before
//     any Recall call is made.
//   - Recall is called once per pair (pass 2).
//   - summary.Mode is set to "accumulate".
func TestLocomo_AccumulateMode_TwoPass(t *testing.T) {
	pairs := locomo.Dataset()

	// Count total turns across all pairs (expected Store calls in pass 1).
	totalTurns := 0
	for i := range pairs {
		totalTurns += len(pairs[i].Conversation)
	}

	stub := &stubHarnessClient{
		recallResp: []string{"answer content"},
	}

	summary, err := locomo.Run(context.Background(), stub, 5, true)
	require.NoError(t, err)
	require.NotNil(t, summary)

	// Exactly one Reset at the start of the accumulate run.
	require.Equal(t, 1, stub.resetCount, "accumulate mode must call Reset exactly once")

	// Store called once per turn (pass 1), before any Recall.
	require.Equal(t, totalTurns, stub.storeCount,
		"accumulate mode must store all turns in pass 1")

	// Recall called once per pair (pass 2).
	require.Equal(t, len(pairs), stub.recallCount,
		"accumulate mode must recall once per pair in pass 2")

	// Mode field set correctly.
	require.Equal(t, "accumulate", summary.Mode)
	require.Equal(t, len(pairs), summary.TotalQuestions)
}
