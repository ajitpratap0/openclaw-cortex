// Package runner provides shared types, a CortexClient wrapper, and scoring
// functions used by all benchmark harnesses.
package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// BenchmarkResult holds the outcome of one QA pair evaluation.
//
// ExactMatch and F1Score are both computed against the oracle-selected
// best candidate (the top-k result with the highest token-F1 vs. ground
// truth, chosen by BestCandidate). They measure "could the answer be found
// anywhere in the top-k?" — an upper-bound / recall-style metric, NOT
// Precision@1. RecalledAtK is the canonical recall metric; ExactMatch is
// a stricter variant of the same signal. See eval/README.md § Metrics.
type BenchmarkResult struct {
	QuestionID  string  `json:"question_id"`
	Question    string  `json:"question"`
	GroundTruth string  `json:"ground_truth"`
	Retrieved   string  `json:"retrieved"`     // oracle-selected best candidate (highest token-F1 vs. ground truth)
	ExactMatch  bool    `json:"exact_match"`   // Retrieved contains GroundTruth (case-insensitive); oracle-selected, not top-ranked
	F1Score     float64 `json:"f1_score"`      // token-F1 of Retrieved vs. GroundTruth; oracle-selected, not top-ranked
	RecalledAtK bool    `json:"recalled_at_k"` // was ground truth in any of the top-k results?
}

// BenchmarkSummary aggregates results from a single benchmark run.
type BenchmarkSummary struct {
	Name           string  `json:"name"`
	TotalQuestions int     `json:"total_questions"`
	ExactMatchAcc  float64 `json:"exact_match_accuracy"`
	AvgF1          float64 `json:"avg_f1"`
	RecallAtK      float64 `json:"recall_at_k"`
	K              int     `json:"k"`
	// RecallFailures is the number of QA pairs for which the recall call failed
	// (binary error, connectivity issue, etc.). Non-zero values indicate that
	// scores for those pairs are artificially zero and should not be compared
	// against baselines without qualification.
	RecallFailures int               `json:"recall_failures,omitempty"`
	Results        []BenchmarkResult `json:"results"`
}

// Client is the interface that benchmark harnesses use to interact with the
// openclaw-cortex binary. CortexClient implements it; tests can inject a stub.
type Client interface {
	Reset(ctx context.Context) error
	Store(ctx context.Context, content string) error
	Recall(ctx context.Context, query string, limit int) ([]string, error)
}

// defaultCallTimeout is the per-subprocess call deadline applied by CortexClient.
// It bounds each individual Reset/Store/Recall invocation so a hung binary cannot
// consume the entire benchmark budget. Override via CortexClient.CallTimeout.
const defaultCallTimeout = 30 * time.Second

// recallJSONModeSentinel is a non-empty value passed to --context to trigger
// JSON output mode in cmd_recall.go (checked as ctxJSON != ""). The value
// itself is unused by the binary; "_" is a readable no-op.
//
// Note: --context and --project are separate flags in cmd_recall.go. The
// runner does not pass --project, so project="" and scope-boosting is not
// applied during eval recalls. A dedicated --format json flag in cmd_recall.go
// would eliminate this sentinel coupling; TODO(#91).
const recallJSONModeSentinel = "_"

// Compile-time assertion: recallJSONModeSentinel must be non-empty.
// If it is ever changed to "", this line produces a compile error:
//
//	invalid string index 0 (out of bounds for 0-character string)
//
// TODO(#91): remove once --format json flag replaces the sentinel.
var _ = recallJSONModeSentinel[0]

// CortexClient wraps the openclaw-cortex binary via execFile (no shell injection).
// It implements Client.
type CortexClient struct {
	// BinaryPath is the path to the openclaw-cortex binary. Defaults to "openclaw-cortex".
	BinaryPath string
	// ConfigPath optionally points to an openclaw-cortex config file.
	ConfigPath string
	// CallTimeout is the per-subprocess deadline for each Reset/Store/Recall call.
	// Zero means use defaultCallTimeout (30 s).
	CallTimeout time.Duration
}

// Compile-time assertion: CortexClient must implement Client.
var _ Client = (*CortexClient)(nil)

// NewCortexClient returns a CortexClient with sensible defaults.
func NewCortexClient(binaryPath, configPath string) *CortexClient {
	if binaryPath == "" {
		binaryPath = "openclaw-cortex"
	}
	return &CortexClient{
		BinaryPath: binaryPath,
		ConfigPath: configPath,
	}
}

// callTimeout returns the effective per-call deadline.
func (c *CortexClient) callTimeout() time.Duration {
	if c.CallTimeout > 0 {
		return c.CallTimeout
	}
	return defaultCallTimeout
}

// RecallJSONResult is a minimal struct for parsing JSON output from
// `openclaw-cortex recall --context _`.
//
// Schema: matches cmd_recall.go output as of commit e38b3d5f.
// The binary serializes []models.RecallResult (internal/models/memory.go):
//
//	[{"memory":{"content":"..."},...}, ...]
//
// The outer key is "memory" (json:"memory") and the content key is "content"
// (json:"content"). If the recall command's output schema changes — e.g. the
// outer wrapper is flattened or the field is renamed — update this struct and
// TestRecallJSONResultSchema in tests/eval_runner_test.go.
//
// Exported so that tests/eval_runner_test.go can test JSON schema parsing
// without requiring a live binary (CLAUDE.md: tests live in tests/).
type RecallJSONResult struct {
	Memory struct {
		Content string `json:"content"`
	} `json:"memory"`
}

// Recall runs `openclaw-cortex recall --context _ <query>` and returns up to
// limit memory content strings parsed from the JSON output.
//
// --budget limit*500 is a token-based heuristic, not a hard result count.
// The binary trims output to that many tokens; if memories are verbose the
// binary may return fewer than limit results, and the trailing contents[:limit]
// slice becomes a no-op. For the synthetic benchmark datasets (each fact/turn
// ≤ 30 tokens) 500 tokens per expected result is intentionally generous, making
// under-counting in practice very unlikely.
func (c *CortexClient) Recall(ctx context.Context, query string, limit int) ([]string, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("runner: limit must be > 0, got %d", limit)
	}
	if query == "" {
		return nil, fmt.Errorf("runner: query must not be empty")
	}
	// maxLimit guards against int overflow on 32-bit targets: limit*500 uses
	// int arithmetic, which is 32-bit on GOARCH=386/arm. On a 32-bit target
	// the overflow threshold is 2^31/500 ≈ 4,294,967; maxLimit (1<<20 = 1,048,576)
	// keeps limit*500 well below that ceiling.
	const maxLimit = 1 << 20 // 1 048 576 — far beyond any reasonable k
	if limit > maxLimit {
		return nil, fmt.Errorf("runner: limit %d exceeds maximum %d", limit, maxLimit)
	}
	callCtx, callCancel := context.WithTimeout(ctx, c.callTimeout())
	defer callCancel()
	// Build args without a shared helper to keep the non-aliasing
	// property local to this call site.
	var args []string
	if c.ConfigPath != "" {
		args = append(args, "--config", c.ConfigPath)
	}
	args = append(args, "recall", "--budget", strconv.Itoa(limit*500), "--context", recallJSONModeSentinel, "--", query)
	//nolint:gosec // binaryPath is set by the caller, not user-supplied in a web context.
	cmd := exec.CommandContext(callCtx, c.BinaryPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("runner: recall binary error: %w (stderr: %s)", err, stderr.String())
	}
	// The binary must emit at least `[]` (2 bytes) when there are zero results
	// in JSON mode — empty stdout is not a valid "no results" response and
	// indicates a broken binary or unexpected output format. cmd_recall.go must
	// preserve this invariant if its empty-result behavior ever changes.
	if stdout.Len() == 0 {
		return nil, fmt.Errorf("runner: recall binary produced no output (exit 0 but empty stdout)")
	}
	trimmed := bytes.TrimSpace(stdout.Bytes())
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("runner: recall binary produced only whitespace (stdout: %q)", stdout.String())
	}
	// First-byte sanity check: JSON arrays start with '['. Any other first byte
	// means JSON mode did not activate — the sentinel coupling (issue #91) may be
	// broken. Surface an actionable error rather than a confusing JSON parse error.
	if trimmed[0] != '[' {
		hint := ""
		if trimmed[0] == '{' {
			hint = " (got JSON object — binary may be returning a single result instead of an array; check recall output schema)"
		} else {
			hint = " — JSON mode may not have activated; check --context sentinel coupling (issue #91)"
		}
		return nil, fmt.Errorf("runner: recall output is not a JSON array (first byte %q)%s\noutput: %s", trimmed[0], hint, stdout.String())
	}
	var results []RecallJSONResult
	if err := json.Unmarshal(trimmed, &results); err != nil {
		return nil, fmt.Errorf("runner: recall JSON parse error: %w (output: %s)", err, stdout.String())
	}
	// Guard against silent JSON shape mismatch: if the binary returned items
	// but every content field is empty, the schema likely doesn't match.
	// hasNonEmpty is set inline while building contents to avoid a second pass.
	hasNonEmpty := false
	contents := make([]string, 0, len(results))
	for i := range results {
		s := results[i].Memory.Content
		contents = append(contents, s)
		if s != "" {
			hasNonEmpty = true
		}
	}
	if len(results) > 0 && !hasNonEmpty {
		return nil, fmt.Errorf("runner: recall returned %d results but all content fields are empty — possible JSON schema mismatch (output: %s)", len(results), stdout.String())
	}
	if len(contents) > limit {
		contents = contents[:limit]
	}
	return contents, nil
}

// Store runs `openclaw-cortex store <content>` to persist a fact memory.
// --scope permanent is intentional: eval facts represent ground-truth
// knowledge that should outlive a session, and all eval facts receive the
// same scope so relative recall scoring is unaffected by scope-boost.
func (c *CortexClient) Store(ctx context.Context, content string) error {
	if content == "" {
		return fmt.Errorf("runner: content must not be empty")
	}
	callCtx, callCancel := context.WithTimeout(ctx, c.callTimeout())
	defer callCancel()
	var args []string
	if c.ConfigPath != "" {
		args = append(args, "--config", c.ConfigPath)
	}
	args = append(args, "store", "--scope", "permanent", "--type", "fact", "--", content)
	//nolint:gosec // BinaryPath is caller-controlled, not user-supplied in a web context.
	cmd := exec.CommandContext(callCtx, c.BinaryPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("runner: store binary error: %w (stderr: %s)", err, stderr.String())
	}
	return nil
}

// Reset calls `openclaw-cortex reset --yes` to wipe all memories from the store.
// Used by benchmark harnesses to isolate QA pairs from each other.
func (c *CortexClient) Reset(ctx context.Context) error {
	callCtx, callCancel := context.WithTimeout(ctx, c.callTimeout())
	defer callCancel()
	var args []string
	if c.ConfigPath != "" {
		args = append(args, "--config", c.ConfigPath)
	}
	args = append(args, "reset", "--yes")
	//nolint:gosec // BinaryPath is caller-controlled, not user-supplied in a web context.
	cmd := exec.CommandContext(callCtx, c.BinaryPath, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("runner: reset binary error: %w (output: %s)", err, out)
	}
	return nil
}

// --- Scoring functions ---

// ExactMatch returns true if retrieved contains the ground truth (case-insensitive).
func ExactMatch(retrieved, groundTruth string) bool {
	if groundTruth == "" {
		return false
	}
	return strings.Contains(
		strings.ToLower(retrieved),
		strings.ToLower(groundTruth),
	)
}

// tokenize splits text into lowercase words, stripping punctuation.
func tokenize(text string) []string {
	text = strings.ToLower(text)
	var tokens []string
	current := strings.Builder{}
	for _, r := range text {
		if isAlphaNum(r) {
			current.WriteRune(r)
		} else if current.Len() > 0 {
			tokens = append(tokens, current.String())
			current.Reset()
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

// isAlphaNum returns true for lowercase letters, digits, and apostrophes.
// Apostrophes are kept so contractions like "don't" remain one token.
// Note: the standard SQuAD evaluation script strips all punctuation including
// apostrophes — our tokenizer is more lenient, but this is harmless for the
// synthetic datasets used here.
func isAlphaNum(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '\''
}

// TokenF1 computes token-level F1 between retrieved and ground truth.
// Returns 0 in all degenerate cases (empty groundTruth, all-punctuation inputs,
// or no token overlap) so it stays consistent with ExactMatch's false return for
// empty groundTruth.
func TokenF1(retrieved, groundTruth string) float64 {
	if groundTruth == "" {
		return 0.0
	}
	predTokens := tokenize(retrieved)
	goldTokens := tokenize(groundTruth)

	if len(goldTokens) == 0 {
		return 0.0
	}
	if len(predTokens) == 0 {
		return 0.0
	}

	// Build a frequency map for gold tokens.
	goldFreq := make(map[string]int, len(goldTokens))
	for _, t := range goldTokens {
		goldFreq[t]++
	}

	// Count overlapping tokens.
	predFreq := make(map[string]int, len(predTokens))
	for _, t := range predTokens {
		predFreq[t]++
	}

	overlap := 0
	for tok, predCount := range predFreq {
		goldCount := goldFreq[tok]
		overlap += min(predCount, goldCount)
	}

	if overlap == 0 {
		return 0.0
	}

	precision := float64(overlap) / float64(len(predTokens))
	recallScore := float64(overlap) / float64(len(goldTokens))
	return 2 * precision * recallScore / (precision + recallScore)
}

// RecallAtK checks if any of the top-k retrieved memories contains the ground truth.
func RecallAtK(memories []string, groundTruth string, k int) bool {
	for i := range min(k, len(memories)) {
		if ExactMatch(memories[i], groundTruth) {
			return true
		}
	}
	return false
}

// Summarize aggregates a slice of BenchmarkResult into a BenchmarkSummary.
// recallFailures is the number of QA pairs for which the recall step failed;
// it is recorded in the summary so callers can detect partially-degraded runs.
func Summarize(name string, results []BenchmarkResult, k, recallFailures int) *BenchmarkSummary {
	total := len(results)
	if total == 0 {
		return &BenchmarkSummary{Name: name, K: k, RecallFailures: recallFailures}
	}

	exactMatches := 0
	f1Sum := 0.0
	recallHits := 0

	for i := range results {
		if results[i].ExactMatch {
			exactMatches++
		}
		f1Sum += results[i].F1Score
		if results[i].RecalledAtK {
			recallHits++
		}
	}

	return &BenchmarkSummary{
		Name:           name,
		TotalQuestions: total,
		ExactMatchAcc:  float64(exactMatches) / float64(total),
		AvgF1:          f1Sum / float64(total),
		RecallAtK:      float64(recallHits) / float64(total),
		K:              k,
		RecallFailures: recallFailures,
		Results:        results,
	}
}

// BestCandidate picks the memory from the retrieved list that has the highest
// token-F1 against the ground truth. Falls back to the first result if no
// candidate scores above zero.
func BestCandidate(memories []string, groundTruth string) string {
	if len(memories) == 0 {
		return ""
	}
	best := memories[0]
	bestF1 := TokenF1(memories[0], groundTruth)

	for i := 1; i < len(memories); i++ {
		f1 := TokenF1(memories[i], groundTruth)
		if f1 > bestF1 {
			bestF1 = f1
			best = memories[i]
		}
	}
	return best
}

// FormatMarkdownTable renders a GitHub-flavored markdown results table for
// a slice of BenchmarkSummary values. k is the recall-at-k value used only
// for the column header label.
//
// Column widths are fixed except for Recall@k, which grows with k to avoid
// misalignment for k>=10 (e.g. "Recall@5"=8 chars, "Recall@100"=10 chars).
func FormatMarkdownTable(summaries []*BenchmarkSummary, k int) string {
	var sb strings.Builder

	header := fmt.Sprintf("| %-14s | Questions | Exact Match | Avg F1  | Recall@%d |\n", "Benchmark", k)
	recallColW := len(fmt.Sprintf("Recall@%d", k)) + 2
	sep := fmt.Sprintf("|%s|-----------|-------------|---------|%s|\n",
		strings.Repeat("-", 16), strings.Repeat("-", recallColW))

	sb.WriteString(header)
	sb.WriteString(sep)

	for _, s := range summaries {
		recallCell := fmt.Sprintf("%*.1f%%", recallColW-3, s.RecallAtK*100)
		fmt.Fprintf(&sb, "| %-14s | %-9d | %10.1f%% | %.4f  | %s |\n",
			s.Name,
			s.TotalQuestions,
			s.ExactMatchAcc*100,
			s.AvgF1,
			recallCell,
		)
	}

	return sb.String()
}
