package tests

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/eval/dmr"
	"github.com/ajitpratap0/openclaw-cortex/eval/runner"
)

// ---------------------------------------------------------------------------
// DMR synthetic dataset tests
// ---------------------------------------------------------------------------

func TestDMRSyntheticDatasetSize(t *testing.T) {
	summary, err := dmr.Run(context.Background(), &stubHarnessClient{
		recallResp: []string{"answer"},
	}, 5, false)
	require.NoError(t, err)
	require.NotNil(t, summary)
	require.Greater(t, summary.TotalQuestions, 0, "synthetic dataset should have at least one QA pair")
}

func TestDMRSyntheticDatasetAllHopCategories(t *testing.T) {
	// Run with a stub that always succeeds to get a summary with all category fields.
	summary, err := dmr.Run(context.Background(), &stubHarnessClient{
		recallResp: []string{"answer"},
	}, 5, false)
	require.NoError(t, err)
	require.NotNil(t, summary)

	breakdown := dmr.CategoryBreakdown(summary)
	// The synthetic dataset covers all 5 hop depths.
	for _, cat := range []string{"1-hop", "2-hop", "3-hop", "4-hop", "5-hop"} {
		_, ok := breakdown[cat]
		require.True(t, ok, "synthetic dataset must include category %q", cat)
	}
}

// ---------------------------------------------------------------------------
// DMR harness control-flow tests (stub client, no binary required)
// ---------------------------------------------------------------------------

// TestDMRRunHappyPath verifies that Run() returns a non-error summary
// when all stubs succeed.
func TestDMRRunHappyPath(t *testing.T) {
	stub := &stubHarnessClient{recallResp: []string{"answer content"}}
	summary, err := dmr.Run(context.Background(), stub, 5, false)
	require.NoError(t, err)
	require.NotNil(t, summary)
	require.Equal(t, 0, summary.RecallFailures)
	require.Equal(t, "per-pair-reset", summary.Mode)
}

// TestDMRRunResetFailure verifies that Run() propagates a Reset error.
func TestDMRRunResetFailure(t *testing.T) {
	stub := &stubHarnessClient{resetErr: errors.New("stub: reset failed")}
	summary, err := dmr.Run(context.Background(), stub, 5, false)
	require.Error(t, err)
	require.Nil(t, summary)
	require.ErrorContains(t, err, "reset failed")
}

// TestDMRRunAllRecallFail verifies that Run() returns an error when every
// Recall call fails (all-fail guard).
func TestDMRRunAllRecallFail(t *testing.T) {
	// We need enough errors for the entire synthetic dataset.
	// Over-allocate to ensure all pairs fail regardless of dataset size.
	stub := &stubHarnessClient{recallErrs: recallErrors(100)}
	_, err := dmr.Run(context.Background(), stub, 5, false)
	require.Error(t, err)
	require.ErrorContains(t, err, "recall calls failed")
}

// TestDMRRunStoreFailure verifies that Run() propagates a Store error.
func TestDMRRunStoreFailure(t *testing.T) {
	stub := &stubHarnessClient{storeErr: errors.New("stub: store failed")}
	summary, err := dmr.Run(context.Background(), stub, 5, false)
	require.Error(t, err)
	require.Nil(t, summary)
	require.ErrorContains(t, err, "ingest turn failed")
}

// TestDMRRunContextCancel verifies that Run() returns a context error when
// the context is canceled before any work is done.
func TestDMRRunContextCancel(t *testing.T) {
	stub := &stubHarnessClient{recallResp: []string{"answer content"}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	summary, err := dmr.Run(ctx, stub, 5, false)
	require.Error(t, err)
	require.Nil(t, summary)
	require.ErrorContains(t, err, "context canceled")
}

// TestDMRRunAccumulateMode verifies the accumulate=true protocol:
//   - Reset is called exactly once.
//   - Store is called for all conversation turns across all pairs (pass 1).
//   - Recall is called once per pair (pass 2).
//   - summary.Mode is "accumulate".
func TestDMRRunAccumulateMode(t *testing.T) {
	stub := &stubHarnessClient{recallResp: []string{"answer content"}}
	summary, err := dmr.Run(context.Background(), stub, 5, true)
	require.NoError(t, err)
	require.NotNil(t, summary)
	require.Equal(t, 1, stub.resetCount, "accumulate mode must call Reset exactly once")
	require.Equal(t, "accumulate", summary.Mode)
	require.Greater(t, stub.storeCount, 0, "accumulate mode must store turns in pass 1")
	require.Equal(t, summary.TotalQuestions, stub.recallCount,
		"accumulate mode must recall once per pair in pass 2")
}

// ---------------------------------------------------------------------------
// CategoryBreakdown tests
// ---------------------------------------------------------------------------

// TestDMRCategoryBreakdown verifies that CategoryBreakdown correctly aggregates
// results by hop-depth category.
func TestDMRCategoryBreakdown(t *testing.T) {
	results := []runner.BenchmarkResult{
		{QuestionID: "q1", Category: "1-hop", ExactMatch: true, F1Score: 1.0, RecalledAtK: true},
		{QuestionID: "q2", Category: "1-hop", ExactMatch: false, F1Score: 0.5, RecalledAtK: false},
		{QuestionID: "q3", Category: "2-hop", ExactMatch: true, F1Score: 1.0, RecalledAtK: true},
		{QuestionID: "q4", Category: "3-hop", ExactMatch: false, F1Score: 0.0, RecalledAtK: false},
	}
	summary := runner.Summarize("DMR", results, 5, 0)

	breakdown := dmr.CategoryBreakdown(summary)

	require.Contains(t, breakdown, "1-hop")
	require.Contains(t, breakdown, "2-hop")
	require.Contains(t, breakdown, "3-hop")

	hop1 := breakdown["1-hop"]
	require.Equal(t, 2, hop1.TotalQuestions)
	require.InDelta(t, 0.5, hop1.ExactMatchAcc, 0.001)
	require.InDelta(t, 0.75, hop1.AvgF1, 0.001)
	require.InDelta(t, 0.5, hop1.RecallAtK, 0.001)

	hop2 := breakdown["2-hop"]
	require.Equal(t, 1, hop2.TotalQuestions)
	require.InDelta(t, 1.0, hop2.ExactMatchAcc, 0.001)

	hop3 := breakdown["3-hop"]
	require.Equal(t, 1, hop3.TotalQuestions)
	require.InDelta(t, 0.0, hop3.ExactMatchAcc, 0.001)
}

// TestDMRCategoryBreakdown_Empty verifies that an empty summary returns an
// empty breakdown (no panics).
func TestDMRCategoryBreakdown_Empty(t *testing.T) {
	summary := runner.Summarize("DMR", nil, 5, 0)
	breakdown := dmr.CategoryBreakdown(summary)
	require.Empty(t, breakdown)
}

// ---------------------------------------------------------------------------
// FormatCategoryTable tests
// ---------------------------------------------------------------------------

// TestDMRFormatCategoryTable verifies that FormatCategoryTable renders a
// non-empty markdown table containing the expected hop-depth categories.
func TestDMRFormatCategoryTable(t *testing.T) {
	breakdowns := map[string]*runner.CategorySummary{
		"1-hop": {TotalQuestions: 2, ExactMatchAcc: 0.5, AvgF1: 0.75, RecallAtK: 0.5},
		"2-hop": {TotalQuestions: 1, ExactMatchAcc: 1.0, AvgF1: 1.0, RecallAtK: 1.0},
		"3-hop": {TotalQuestions: 1, ExactMatchAcc: 0.0, AvgF1: 0.0, RecallAtK: 0.0},
	}

	table := dmr.FormatCategoryTable(breakdowns)
	require.NotEmpty(t, table, "FormatCategoryTable should return a non-empty string")

	for _, cat := range []string{"1-hop", "2-hop", "3-hop"} {
		require.True(t, strings.Contains(table, cat), "table should contain category %q", cat)
	}
}

// TestDMRFormatCategoryTable_Empty verifies that FormatCategoryTable with an
// empty map returns only the header row (no panics, no category rows).
func TestDMRFormatCategoryTable_Empty(t *testing.T) {
	table := dmr.FormatCategoryTable(map[string]*runner.CategorySummary{})
	require.NotEmpty(t, table, "header should still be rendered for empty breakdowns")
	for _, cat := range []string{"1-hop", "2-hop", "3-hop", "4-hop", "5-hop"} {
		require.False(t, strings.Contains(table, cat), "empty table should not contain category rows")
	}
}

// ---------------------------------------------------------------------------
// LoadDataset tests
// ---------------------------------------------------------------------------

// TestDMRLoadDataset_FileNotFound verifies that LoadDataset returns an error
// for a missing file.
func TestDMRLoadDataset_FileNotFound(t *testing.T) {
	_, err := dmr.LoadDataset("/nonexistent/path/dmr_dataset.json")
	require.Error(t, err)
}

// TestDMRLoadDataset_MinimalValid writes a tiny valid DMR JSON fixture and
// verifies that LoadDataset parses it correctly.
func TestDMRLoadDataset_MinimalValid(t *testing.T) {
	fixture := []map[string]interface{}{
		{
			"id": "dmr-test-1",
			"conversation": []map[string]interface{}{
				{"speaker": "human", "content": "Bob works at TechCorp."},
				{"speaker": "ai", "content": "Interesting!"},
				{"speaker": "human", "content": "TechCorp uses Go."},
				{"speaker": "ai", "content": "Go is great."},
			},
			"question":  "What language does Bob's company use?",
			"answer":    "Go",
			"hop_count": 2,
		},
		{
			"id": "dmr-test-2",
			"conversation": []map[string]interface{}{
				{"speaker": "human", "content": "I prefer Python."},
				{"speaker": "ai", "content": "Python is versatile."},
			},
			"question":  "What language does the user prefer?",
			"answer":    "Python",
			"hop_count": 1,
		},
	}

	data, err := json.Marshal(fixture)
	require.NoError(t, err)

	dir := t.TempDir()
	path := filepath.Join(dir, "dmr_test.json")
	require.NoError(t, os.WriteFile(path, data, 0o600))

	pairs, err := dmr.LoadDataset(path)
	require.NoError(t, err)
	require.Len(t, pairs, 2)

	p0 := pairs[0]
	require.Equal(t, "dmr-test-1", p0.ID)
	require.Equal(t, "What language does Bob's company use?", p0.Question)
	require.Equal(t, "Go", p0.Answer)
	require.Equal(t, "2-hop", p0.Category)
	require.Len(t, p0.Conversation, 4)

	p1 := pairs[1]
	require.Equal(t, "dmr-test-2", p1.ID)
	require.Equal(t, "1-hop", p1.Category)
}

// TestDMRLoadDataset_InvalidJSON verifies that LoadDataset returns an error
// for malformed JSON.
func TestDMRLoadDataset_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	require.NoError(t, os.WriteFile(path, []byte("[not valid json"), 0o600))

	_, err := dmr.LoadDataset(path)
	require.Error(t, err)
}

// TestDMRLoadDataset_HopCategoryMapping verifies that hop_count integers map
// to the expected category strings.
func TestDMRLoadDataset_HopCategoryMapping(t *testing.T) {
	cases := []struct {
		hopCount int
		wantCat  string
	}{
		{1, "1-hop"},
		{2, "2-hop"},
		{3, "3-hop"},
		{4, "4-hop"},
		{5, "5-hop"},
		{0, "1-hop"}, // zero → 1-hop (floor)
		{6, "6-hop"}, // > 5 → formatted string
	}

	dir := t.TempDir()

	for _, tc := range cases {
		tc := tc
		t.Run(tc.wantCat, func(t *testing.T) {
			fixture := []map[string]interface{}{
				{
					"id":           "test",
					"conversation": []map[string]interface{}{},
					"question":     "q?",
					"answer":       "a",
					"hop_count":    tc.hopCount,
				},
			}
			data, err := json.Marshal(fixture)
			require.NoError(t, err)

			path := filepath.Join(dir, tc.wantCat+".json")
			require.NoError(t, os.WriteFile(path, data, 0o600))

			pairs, err := dmr.LoadDataset(path)
			require.NoError(t, err)
			require.Len(t, pairs, 1)
			require.Equal(t, tc.wantCat, pairs[0].Category)
		})
	}
}
