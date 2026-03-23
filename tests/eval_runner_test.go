package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
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
// still exposes --context (backward-compat sentinel) and the new --format and
// --limit flags. A flag rename in cmd_recall.go would otherwise break either
// the legacy harness (--context) or the updated harness (--format, --limit).
func TestCortexClientRecallContextFlagPresent(t *testing.T) {
	if !binExists() {
		t.Skip("binary not built; run: go build -o bin/openclaw-cortex ./cmd/openclaw-cortex")
	}
	out, _ := runCLI("recall", "--help")
	for _, flag := range []string{"--context", "--format", "--limit"} {
		if !strings.Contains(out, flag) {
			t.Errorf("recall --help does not mention %s flag:\n%s", flag, out)
		}
	}
}

// TestCortexClientStoreFlagsPresent verifies that the store command still
// exposes --type and --scope, which Store() hardcodes. A flag rename in
// cmd_store.go would otherwise cause Store() to silently store facts under
// the wrong type/scope or exit non-zero with no compile-time signal.
func TestCortexClientStoreFlagsPresent(t *testing.T) {
	if !binExists() {
		t.Skip("binary not built; run: go build -o bin/openclaw-cortex ./cmd/openclaw-cortex")
	}
	out, _ := runCLI("store", "--help")
	for _, flag := range []string{"--type", "--scope"} {
		if !strings.Contains(out, flag) {
			t.Errorf("store --help does not mention %s flag; Store() hardcoded flag coupling may be broken:\n%s", flag, out)
		}
	}
}

// TestCortexClientRecallJSONOutputFormat verifies that both `recall --format json`
// (new preferred flag) and `recall --context _` (backward-compat sentinel)
// trigger JSON output mode and that the output is parseable JSON.
//
// This test catches two failure modes that TestCortexClientRecallContextFlagPresent
// cannot: (1) the flag is present in --help but JSON mode no longer fires, and
// (2) the JSON schema changed so the output is no longer valid JSON.
//
// When Memgraph is not running the binary exits non-zero; the test distinguishes
// a connectivity error (skip) from an "unknown flag" error (hard fail) so flag
// renames are always caught even without a live store.
func TestCortexClientRecallJSONOutputFormat(t *testing.T) {
	if !binExists() {
		t.Skip("binary not built; run: go build -o bin/openclaw-cortex ./cmd/openclaw-cortex")
	}

	cases := []struct {
		name string
		args []string
	}{
		{
			name: "format_json_flag",
			args: []string{"recall", "--format", "json", "--budget", "500", "--", "test-query"},
		},
		{
			name: "context_sentinel_backward_compat",
			args: []string{"recall", "--context", "_", "--budget", "500", "--", "test-query"},
		},
		// TODO: add a unit test for the --format text / --context precedence rule
		// (jsonMode := format == "json" || (ctxJSON != "" && !cmd.Flags().Changed("format")))
		// without requiring Memgraph. The logic is pure boolean and could be extracted
		// into a testable helper in cmd_recall.go. The binary-level test is omitted here
		// because the binary exits non-zero without a live store, so the wantJSON: false
		// branch would see an empty stdout and pass vacuously — providing no real coverage.
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Single invocation: capture stdout and stderr separately so we can both
			// detect "unknown flag" errors (stderr) and parse JSON output (stdout).
			cmd := exec.Command(cliBinPath, tc.args...)
			var stdoutBuf, stderrBuf bytes.Buffer
			cmd.Stdout = &stdoutBuf
			cmd.Stderr = &stderrBuf
			runErr := cmd.Run()
			if runErr != nil {
				stderr := stderrBuf.String()
				if strings.Contains(stderr, "unknown flag") || strings.Contains(stderr, "flag provided but not defined") {
					t.Fatalf("flag not recognized: %s", stderr)
				}
				t.Skipf("recall exited non-zero (Memgraph likely not running): %v — skipping JSON format check", runErr)
			}

			// Binary exited 0: stdout must be parseable JSON (at minimum a valid empty array).
			var results []any
			if jsonErr := json.Unmarshal([]byte(strings.TrimSpace(stdoutBuf.String())), &results); jsonErr != nil {
				t.Errorf("recall stdout is not valid JSON: %v\nstdout: %s", jsonErr, stdoutBuf.String())
			}
		})
	}
}

// TestCortexClientRecallLimitFlag verifies that recall --limit N is recognized
// as a valid flag (not rejected with "unknown flag"). When Memgraph is not
// running the binary exits non-zero for a connectivity reason, not a flag
// error — the test distinguishes the two cases so the flag presence check
// works even without a live store.
func TestCortexClientRecallLimitFlag(t *testing.T) {
	if !binExists() {
		t.Skip("binary not built; run: go build -o bin/openclaw-cortex ./cmd/openclaw-cortex")
	}

	cmd := exec.Command(cliBinPath, "recall", "--format", "json", "--limit", "3", "--budget", "1500", "--", "test-query")
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf
	runErr := cmd.Run()
	if runErr != nil {
		stderr := stderrBuf.String()
		if strings.Contains(stderr, "unknown flag") || strings.Contains(stderr, "flag provided but not defined") {
			t.Fatalf("recall --limit flag not recognized (may have been removed or renamed): %s", stderr)
		}
		// Any other non-zero exit (e.g. Memgraph not running) is acceptable —
		// we only care that the flag itself is accepted by the parser.
	}
}

// TestCortexClientRecallLimitCapBehavior asserts that --limit N actually caps
// the returned JSON result slice to ≤ N items when Memgraph is running. When
// Memgraph is unavailable the test is skipped (connectivity failure), not
// failed — matching the pattern used in TestCortexClientRecallJSONOutputFormat.
func TestCortexClientRecallLimitCapBehavior(t *testing.T) {
	if !binExists() {
		t.Skip("binary not built; run: go build -o bin/openclaw-cortex ./cmd/openclaw-cortex")
	}

	const limit = 1
	cmd := exec.Command(cliBinPath, "recall", "--format", "json", "--limit", strconv.Itoa(limit), "--budget", "500", "--", "test-query")
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	runErr := cmd.Run()
	if runErr != nil {
		stderr := stderrBuf.String()
		if strings.Contains(stderr, "unknown flag") || strings.Contains(stderr, "flag provided but not defined") {
			t.Fatalf("flag not recognized: %s", stderr)
		}
		t.Skipf("recall exited non-zero (Memgraph likely not running): %v — skipping cap behavior check", runErr)
	}

	var results []any
	if jsonErr := json.Unmarshal([]byte(strings.TrimSpace(stdoutBuf.String())), &results); jsonErr != nil {
		t.Fatalf("recall stdout is not valid JSON: %v\nstdout: %s", jsonErr, stdoutBuf.String())
	}
	if len(results) > limit {
		t.Errorf("--limit %d: expected ≤ %d results, got %d", limit, limit, len(results))
	}
}

// TestCortexClientRecallInvalidFormat verifies that passing an unrecognized
// --format value causes the binary to exit non-zero with a descriptive error.
func TestCortexClientRecallInvalidFormat(t *testing.T) {
	if !binExists() {
		t.Skip("binary not built; run: go build -o bin/openclaw-cortex ./cmd/openclaw-cortex")
	}
	cmd := exec.Command(cliBinPath, "recall", "--format", "csv", "--", "test-query")
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected non-zero exit for --format csv, got exit 0")
	}
	if !strings.Contains(stderrBuf.String(), "unknown --format") {
		t.Errorf("expected error mentioning unknown --format, got: %s", stderrBuf.String())
	}
}

// TestCortexClientRecallNegativeLimitError verifies that --limit -1 causes
// the binary to exit non-zero with an error message that mentions --limit.
//
// Note: pflag may reject "--limit -1" before RunE fires (interpreting -1 as
// shorthand flag), producing "invalid argument" instead of the custom --limit
// message. The assertion accepts either so the test is robust across pflag versions.
func TestCortexClientRecallNegativeLimitError(t *testing.T) {
	if !binExists() {
		t.Skip("binary not built; run: go build -o bin/openclaw-cortex ./cmd/openclaw-cortex")
	}
	cmd := exec.Command(cliBinPath, "recall", "--limit", "-1", "--", "test-query")
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf
	if err := cmd.Run(); err == nil {
		t.Fatal("expected non-zero exit for --limit -1, got exit 0")
	}
	stderr := stderrBuf.String()
	if !strings.Contains(stderr, "--limit") && !strings.Contains(stderr, "invalid argument") {
		t.Errorf("expected error mentioning --limit or invalid argument, got: %s", stderr)
	}
}

// TestCortexClientRecallExceedMaxLimitError verifies that --limit 10001 causes
// the binary to exit non-zero with an error message mentioning "exceeds maximum".
// Like the negative-limit test, validation fires before any Memgraph connection.
func TestCortexClientRecallExceedMaxLimitError(t *testing.T) {
	if !binExists() {
		t.Skip("binary not built; run: go build -o bin/openclaw-cortex ./cmd/openclaw-cortex")
	}
	cmd := exec.Command(cliBinPath, "recall", "--limit", "10001", "--", "test-query")
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf
	if err := cmd.Run(); err == nil {
		t.Fatal("expected non-zero exit for --limit 10001, got exit 0")
	}
	if !strings.Contains(stderrBuf.String(), "exceeds maximum") {
		t.Errorf("expected error mentioning exceeds maximum, got: %s", stderrBuf.String())
	}
}

// TestCortexClientRecallMaxLimitBoundaryAccepted verifies that --limit 10000
// (the runner's own cap, matching the binary's rejection threshold) is accepted
// by the binary without a "exceeds maximum" error. This creates a coupling signal:
// if someone lowers the binary's cap below 10000, this test fails and forces
// maxLimit in runner.go to be updated in sync.
func TestCortexClientRecallMaxLimitBoundaryAccepted(t *testing.T) {
	if !binExists() {
		t.Skip("binary not built; run: go build -o bin/openclaw-cortex ./cmd/openclaw-cortex")
	}
	cmd := exec.Command(cliBinPath, "recall", "--format", "json", "--limit", "10000", "--", "test-query")
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf
	_ = cmd.Run()
	stderr := stderrBuf.String()
	if strings.Contains(stderr, "exceeds maximum") || strings.Contains(stderr, "unknown flag") {
		t.Errorf("--limit 10000 should be accepted by the binary (no exceeds-maximum or unknown-flag error), got: %s", stderr)
	}
}

// TestFormatMarkdownTable verifies that FormatMarkdownTable produces a valid
// GitHub-flavored markdown table: header line, separator line, and one data
// row per summary, with column separators aligned for k=5 and k=100.
func TestFormatMarkdownTable(t *testing.T) {
	summaries := []*runner.BenchmarkSummary{
		{Name: "LoCoMo", TotalQuestions: 10, ExactMatchAcc: 0.8, AvgF1: 0.75, RecallAtK: 0.9, K: 5},
		{Name: "LongMemEval", TotalQuestions: 10, ExactMatchAcc: 0.6, AvgF1: 0.55, RecallAtK: 0.7, K: 5},
	}

	for _, k := range []int{5, 10, 100} {
		t.Run(fmt.Sprintf("k=%d", k), func(t *testing.T) {
			table := runner.FormatMarkdownTable(summaries, k)
			if table == "" {
				t.Fatal("FormatMarkdownTable returned empty string")
			}
			lines := strings.Split(strings.TrimRight(table, "\n"), "\n")
			// Must have at least header + separator + one row per summary.
			if len(lines) < 2+len(summaries) {
				t.Fatalf("expected >= %d lines, got %d:\n%s", 2+len(summaries), len(lines), table)
			}
			// Header must contain the exact Recall@k label (e.g. "Recall@5").
			if !strings.Contains(lines[0], fmt.Sprintf("Recall@%d", k)) {
				t.Errorf("header missing Recall@%d: %q", k, lines[0])
			}
			// Separator line must start with '|' and contain only '-', '|', spaces.
			sep := lines[1]
			for _, ch := range sep {
				if ch != '|' && ch != '-' && ch != ' ' {
					t.Errorf("unexpected char %q in separator: %q", ch, sep)
				}
			}
			// Each data row must contain the benchmark name.
			for i, s := range summaries {
				row := lines[2+i]
				if !strings.Contains(row, s.Name) {
					t.Errorf("row %d missing benchmark name %q: %q", i, s.Name, row)
				}
			}
			// Column counts must match across all lines.
			colCount := strings.Count(lines[0], "|")
			for i, line := range lines[1:] {
				if strings.Count(line, "|") != colCount {
					t.Errorf("line %d has %d '|' chars, header has %d: %q",
						i+1, strings.Count(line, "|"), colCount, line)
				}
			}
		})
	}
}

// TestRecallJSONResultSchema asserts that runner.RecallJSONResult correctly
// parses the JSON shape emitted by `openclaw-cortex recall --context _`.
// This test makes the schema coupling between runner.go and cmd_recall.go
// explicit and catches regressions without requiring a live binary.
//
// If this test fails, update RecallJSONResult and its doc comment to match
// the new schema emitted by cmd_recall.go.
func TestRecallJSONResultSchema(t *testing.T) {
	// Minimal JSON matching the shape cmd_recall.go produces:
	// []models.RecallResult serialized as an array of objects with a "memory"
	// key whose nested object has a "content" key.
	const input = `[{"memory":{"content":"the cat sat on the mat"}},{"memory":{"content":"Paris is the capital of France"}}]`

	var results []runner.RecallJSONResult
	if err := json.Unmarshal([]byte(input), &results); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if got := results[0].Memory.Content; got != "the cat sat on the mat" {
		t.Errorf("results[0].Memory.Content = %q, want %q", got, "the cat sat on the mat")
	}
	if got := results[1].Memory.Content; got != "Paris is the capital of France" {
		t.Errorf("results[1].Memory.Content = %q, want %q", got, "Paris is the capital of France")
	}
}

// TestRecallJSONResultEmptyArray asserts that an empty JSON array produces
// zero results without error — the "no memories found" case.
func TestRecallJSONResultEmptyArray(t *testing.T) {
	var results []runner.RecallJSONResult
	if err := json.Unmarshal([]byte(`[]`), &results); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

// TestRecallJSONResultMissingContentField asserts that a missing "content"
// key results in an empty string (not an error) — Go's zero-value default.
// This matches the "all content fields empty" guard in CortexClient.Recall.
func TestRecallJSONResultMissingContentField(t *testing.T) {
	const input = `[{"memory":{}}]`
	var results []runner.RecallJSONResult
	if err := json.Unmarshal([]byte(input), &results); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if got := results[0].Memory.Content; got != "" {
		t.Errorf("expected empty string for missing content, got %q", got)
	}
}
