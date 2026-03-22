package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"

	"github.com/ajitpratap0/openclaw-cortex/eval/runner"
)

func TestRunnerExactMatch(t *testing.T) {
	tests := []struct {
		name        string
		retrieved   string
		groundTruth string
		want        bool
	}{
		{"exact", "Alice uses Go for her projects", "Go", true},
		{"case insensitive", "Alice uses go for her projects", "Go", true},
		{"substring", "She prefers Python over Java", "Python", true},
		{"no match", "Alice likes hiking", "Go", false},
		{"empty ground truth", "Alice likes hiking", "", false},
		{"empty retrieved", "", "Go", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runner.ExactMatch(tt.retrieved, tt.groundTruth)
			if got != tt.want {
				t.Errorf("ExactMatch(%q, %q) = %v, want %v", tt.retrieved, tt.groundTruth, got, tt.want)
			}
		})
	}
}

func TestRunnerTokenF1(t *testing.T) {
	tests := []struct {
		name        string
		retrieved   string
		groundTruth string
		wantMin     float64
		wantMax     float64
	}{
		{
			name:        "perfect match",
			retrieved:   "Alice uses Go",
			groundTruth: "Alice uses Go",
			wantMin:     1.0,
			wantMax:     1.0,
		},
		{
			name:        "no overlap",
			retrieved:   "Bob likes hiking",
			groundTruth: "Alice uses Go",
			wantMin:     0.0,
			wantMax:     0.0,
		},
		{
			name:        "partial overlap",
			retrieved:   "Alice uses Python",
			groundTruth: "Alice uses Go",
			wantMin:     0.5,
			wantMax:     0.75,
		},
		{
			name:        "both empty",
			retrieved:   "",
			groundTruth: "",
			wantMin:     0.0,
			wantMax:     0.0,
		},
		{
			name:        "retrieved empty",
			retrieved:   "",
			groundTruth: "Alice",
			wantMin:     0.0,
			wantMax:     0.0,
		},
		{
			name:        "ground truth empty",
			retrieved:   "Alice",
			groundTruth: "",
			wantMin:     0.0,
			wantMax:     0.0,
		},
		{
			// All-punctuation ground truth tokenizes to zero tokens — guard prevents
			// division by zero (NaN) at the recallScore line.
			name:        "all-punctuation ground truth",
			retrieved:   "Alice uses Go",
			groundTruth: "---",
			wantMin:     0.0,
			wantMax:     0.0,
		},
		{
			name:        "all-punctuation retrieved",
			retrieved:   "---",
			groundTruth: "Go",
			wantMin:     0.0,
			wantMax:     0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runner.TokenF1(tt.retrieved, tt.groundTruth)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("TokenF1(%q, %q) = %.4f, want [%.4f, %.4f]",
					tt.retrieved, tt.groundTruth, got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestRunnerRecallAtK(t *testing.T) {
	memories := []string{
		"Alice's favorite language is Go",
		"Bob worked at Google for 5 years",
		"Carol prefers Python for data science",
		"Dave uses Rust for systems programming",
		"Eve enjoys functional programming in Haskell",
	}

	tests := []struct {
		name        string
		groundTruth string
		k           int
		want        bool
	}{
		{"found at k=1", "Go", 1, true},
		{"found at k=3", "Python", 3, true},
		{"not found at k=2", "Python", 2, false},
		{"found at k=5", "Haskell", 5, true},
		{"not found at all", "JavaScript", 5, false},
		{"k=0 never found", "Go", 0, false},
		{"k larger than slice", "Haskell", 100, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runner.RecallAtK(memories, tt.groundTruth, tt.k)
			if got != tt.want {
				t.Errorf("RecallAtK(memories, %q, %d) = %v, want %v", tt.groundTruth, tt.k, got, tt.want)
			}
		})
	}
}

func TestRunnerSummarize(t *testing.T) {
	results := []runner.BenchmarkResult{
		{QuestionID: "q1", ExactMatch: true, F1Score: 1.0, RecalledAtK: true},
		{QuestionID: "q2", ExactMatch: false, F1Score: 0.5, RecalledAtK: true},
		{QuestionID: "q3", ExactMatch: false, F1Score: 0.0, RecalledAtK: false},
		{QuestionID: "q4", ExactMatch: true, F1Score: 0.8, RecalledAtK: true},
	}

	summary := runner.Summarize("test", results, 5, 0)

	if summary.Name != "test" {
		t.Errorf("Name = %q, want %q", summary.Name, "test")
	}
	if summary.TotalQuestions != 4 {
		t.Errorf("TotalQuestions = %d, want 4", summary.TotalQuestions)
	}
	if summary.K != 5 {
		t.Errorf("K = %d, want 5", summary.K)
	}

	wantEM := 0.5 // 2/4
	if summary.ExactMatchAcc != wantEM {
		t.Errorf("ExactMatchAcc = %.4f, want %.4f", summary.ExactMatchAcc, wantEM)
	}

	wantF1 := (1.0 + 0.5 + 0.0 + 0.8) / 4.0
	if summary.AvgF1 != wantF1 {
		t.Errorf("AvgF1 = %.4f, want %.4f", summary.AvgF1, wantF1)
	}

	wantRecall := 0.75 // 3/4
	if summary.RecallAtK != wantRecall {
		t.Errorf("RecallAtK = %.4f, want %.4f", summary.RecallAtK, wantRecall)
	}
}

func TestRunnerSummarizeEmpty(t *testing.T) {
	summary := runner.Summarize("empty", nil, 5, 0)
	if summary.TotalQuestions != 0 {
		t.Errorf("TotalQuestions = %d, want 0", summary.TotalQuestions)
	}
	if summary.ExactMatchAcc != 0.0 {
		t.Errorf("ExactMatchAcc = %.4f, want 0.0", summary.ExactMatchAcc)
	}
}

func TestRunnerBestCandidate(t *testing.T) {
	tests := []struct {
		name        string
		memories    []string
		groundTruth string
		want        string
	}{
		{
			name:        "empty memories",
			memories:    []string{},
			groundTruth: "Go",
			want:        "",
		},
		{
			name:        "single memory",
			memories:    []string{"Alice uses Go"},
			groundTruth: "Go",
			want:        "Alice uses Go",
		},
		{
			name:        "picks highest F1",
			memories:    []string{"Bob likes hiking", "Alice uses Go for projects"},
			groundTruth: "Go",
			want:        "Alice uses Go for projects",
		},
		{
			name:        "all zero F1 falls back to first",
			memories:    []string{"Bob likes hiking", "Carol enjoys running"},
			groundTruth: "Go",
			want:        "Bob likes hiking",
		},
		{
			// "Alice uses Go" (3 tokens, 2 overlap with "Go Alice") → F1≈0.80
			// "Go is used by Alice" (5 tokens, 2 overlap) → F1≈0.57
			// First wins by higher F1, not by tie-break.
			name:        "higher F1 wins over longer candidate",
			memories:    []string{"Alice uses Go", "Go is used by Alice"},
			groundTruth: "Go Alice",
			want:        "Alice uses Go",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runner.BestCandidate(tt.memories, tt.groundTruth)
			if got != tt.want {
				t.Errorf("BestCandidate(%v, %q) = %q, want %q", tt.memories, tt.groundTruth, got, tt.want)
			}
		})
	}
}

func TestCortexClientStoreEmptyContent(t *testing.T) {
	c := runner.NewCortexClient("", "")
	err := c.Store(context.Background(), "")
	if err == nil {
		t.Fatal("Store(\"\") should return an error, got nil")
	}
}

func TestCortexClientRecallZeroLimit(t *testing.T) {
	c := runner.NewCortexClient("", "")
	_, err := c.Recall(context.Background(), "query", 0)
	if err == nil {
		t.Fatal("Recall with limit=0 should return an error, got nil")
	}
}

func TestCortexClientRecallEmptyQuery(t *testing.T) {
	c := runner.NewCortexClient("", "")
	_, err := c.Recall(context.Background(), "", 5)
	if err == nil {
		t.Fatal("Recall with empty query should return an error, got nil")
	}
}

// TestCortexClientRecallContextFlagPresent verifies that the recall command
// still exposes --context, which recallJSONModeSentinel depends on. A flag
// rename in cmd_recall.go would otherwise break the harness silently.
func TestCortexClientRecallContextFlagPresent(t *testing.T) {
	if !binExists() {
		t.Skip("binary not built; run: go build -o bin/openclaw-cortex ./cmd/openclaw-cortex")
	}
	out, _ := runCLI("recall", "--help")
	if !strings.Contains(out, "--context") {
		t.Errorf("recall --help does not mention --context flag; recallJSONModeSentinel coupling may be broken:\n%s", out)
	}
}

// TestCortexClientRecallJSONOutputFormat verifies that `recall --context _`
// triggers JSON output mode and that the output is parseable JSON.
//
// This test catches two failure modes that TestCortexClientRecallContextFlagPresent
// cannot: (1) --context is present in --help but the empty-check (`ctxJSON != ""`)
// was changed so JSON mode no longer fires, and (2) the JSON schema changed so
// the output is no longer valid JSON.
//
// When Memgraph is not running the binary exits non-zero; the test distinguishes
// a connectivity error (skip) from an "unknown flag" error (hard fail) so flag
// renames are always caught even without a live store.
func TestCortexClientRecallJSONOutputFormat(t *testing.T) {
	if !binExists() {
		t.Skip("binary not built; run: go build -o bin/openclaw-cortex ./cmd/openclaw-cortex")
	}

	// Single invocation: capture stdout and stderr separately so we can both
	// detect "unknown flag" errors (stderr) and parse JSON output (stdout).
	cmd := exec.Command(cliBinPath, "recall", "--context", "_", "--budget", "500", "--", "test-query")
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	runErr := cmd.Run()
	if runErr != nil {
		stderr := stderrBuf.String()
		if strings.Contains(stderr, "unknown flag") || strings.Contains(stderr, "flag provided but not defined") {
			t.Fatalf("recall --context flag not recognized (may have been renamed): %s", stderr)
		}
		t.Skipf("recall exited non-zero (Memgraph likely not running): %v — skipping JSON format check", runErr)
	}

	// Binary exited 0: stdout must be parseable JSON (at minimum a valid empty array).
	var results []any
	if jsonErr := json.Unmarshal([]byte(strings.TrimSpace(stdoutBuf.String())), &results); jsonErr != nil {
		t.Errorf("recall --context _ stdout is not valid JSON: %v\nstdout: %s", jsonErr, stdoutBuf.String())
	}
}
