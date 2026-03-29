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
	Retrieved   string  `json:"retrieved"`               // oracle-selected best candidate (highest token-F1 vs. ground truth)
	ExactMatch  bool    `json:"exact_match"`             // Retrieved contains GroundTruth (case-insensitive); oracle-selected, not top-ranked
	F1Score     float64 `json:"f1_score"`                // token-F1 of Retrieved vs. GroundTruth; oracle-selected, not top-ranked
	RecalledAtK bool    `json:"recalled_at_k"`           // was ground truth in any of the top-k results?
	Category    string  `json:"category,omitempty"`      // optional question category (e.g. "single-hop", "temporal")
}

// CategorySummary holds per-category aggregate metrics within a benchmark run.
// It is populated by callers that classify each BenchmarkResult with a Category
// and then call ComputeCategoryBreakdowns (or build the map manually).
type CategorySummary struct {
	TotalQuestions int     `json:"total_questions"`
	ExactMatchAcc  float64 `json:"exact_match_accuracy"`
	AvgF1          float64 `json:"avg_f1"`
	RecallAtK      float64 `json:"recall_at_k"`
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
	// Mode describes the ingestion strategy used during the run.
	// "accumulate" means all facts are loaded once and never reset between QA
	// pairs (measures recall against a growing store). "per-pair-reset" means
	// the store is wiped and re-populated for each QA pair (the current default
	// harness behavior).
	Mode string `json:"mode,omitempty"`
	// RecallAtK2 is an optional second recall@k metric computed at a different k
	// (K2) alongside the primary RecallAtK. Zero means it was not computed.
	RecallAtK2 float64 `json:"recall_at_k2,omitempty"`
	// CategoryBreakdowns holds per-category aggregate metrics when results carry
	// a non-empty Category field. Nil when no categories are present.
	CategoryBreakdowns map[string]*CategorySummary `json:"category_breakdowns,omitempty"`
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
// `openclaw-cortex recall --format json`.
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

// Recall runs `openclaw-cortex recall --format json --limit <limit> <query>`
// and returns up to limit memory content strings parsed from the JSON output.
//
// --limit N is a hard result count cap applied by the binary before token
// trimming. --budget is still passed as a generous ceiling (min(limit,2000)*500 tokens)
// so the token budget never silently truncates results for the synthetic
// benchmark datasets (each fact/turn ≤ 30 tokens).
//
// Note: Memgraph itself may return fewer than limit items if fewer memories
// match the query. In that case len(contents) < limit with no error —
// RecallAtK is evaluated against the actual candidates returned, not a
// padded set. For the synthetic datasets this is expected and correct.
func (c *CortexClient) Recall(ctx context.Context, query string, limit int) ([]string, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("runner: limit must be > 0, got %d", limit)
	}
	if query == "" {
		return nil, fmt.Errorf("runner: query must not be empty")
	}
	// maxLimit must match the --limit upper bound enforced by cmd_recall.go RunE
	// (currently 10000). Values above this will be rejected by the binary with a
	// non-zero exit, producing an opaque error rather than the friendly runner message.
	const maxLimit = 10000
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
	args = append(args, "recall",
		"--format", "json",
		"--limit", strconv.Itoa(limit),
		"--budget", strconv.Itoa(min(limit, 2000)*500),
		"--", query,
	)
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
			hint = " — JSON mode may not have activated; check --format json / --context sentinel coupling"
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
// Pass k2=0 (via SummarizeWithK2) to skip RecallAtK2 computation; use
// Summarize when you do not need the second recall threshold.
func Summarize(name string, results []BenchmarkResult, k, recallFailures int) *BenchmarkSummary {
	return summarize(name, results, k, 0, recallFailures)
}

// SummarizeWithK2 is identical to Summarize but also computes RecallAtK2 at
// the supplied k2. When k2 <= 0 it is ignored and RecallAtK2 is left at 0.
//
// Because BenchmarkResult stores only a boolean RecalledAtK (evaluated at the
// primary k), the K2 recall rate is approximated: when k2 >= k it equals
// RecallAtK (every result recalled at k is also recalled at any k2 >= k).
// When k2 < k the rate cannot be reliably derived from stored booleans and is
// set to 0. Harnesses that need an exact independent k2 metric should compute
// it from the raw retrieved list and store it separately before calling this.
func SummarizeWithK2(name string, results []BenchmarkResult, k, k2, recallFailures int) *BenchmarkSummary {
	return summarize(name, results, k, k2, recallFailures)
}

// summarize is the internal implementation shared by Summarize and SummarizeWithK2.
func summarize(name string, results []BenchmarkResult, k, k2, recallFailures int) *BenchmarkSummary {
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

	summary := &BenchmarkSummary{
		Name:           name,
		TotalQuestions: total,
		ExactMatchAcc:  float64(exactMatches) / float64(total),
		AvgF1:          f1Sum / float64(total),
		RecallAtK:      float64(recallHits) / float64(total),
		K:              k,
		RecallFailures: recallFailures,
		Results:        results,
	}

	// Compute RecallAtK2 when k2 > 0.
	// The approximation: if k2 >= k, RecallAtK2 == RecallAtK (monotonically
	// non-decreasing recall). If k2 < k we cannot determine which of the primary-k
	// recalled results would still be recalled at k2, so we leave RecallAtK2 at 0.
	if k2 > 0 && k2 >= k {
		summary.RecallAtK2 = summary.RecallAtK
	}

	return summary
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
// When any summary has RecallAtK2 > 0, an additional Recall@K2 column is
// appended. K2 is inferred from the first summary that has RecallAtK2 > 0;
// all summaries must use the same K2 for the column header to be meaningful.
func FormatMarkdownTable(summaries []*BenchmarkSummary, k int) string {
	var sb strings.Builder

	// Detect whether any summary carries a RecallAtK2 value so we know whether
	// to render the extra column. Also capture the K2 header label from the
	// first summary that has it set.
	hasK2 := false
	k2Label := ""
	for _, s := range summaries {
		if s.RecallAtK2 > 0 {
			hasK2 = true
			// Use the summary's K field as the k2 label if available; fall back
			// to a generic "K2" label. The caller is responsible for ensuring all
			// summaries use the same K2 threshold.
			k2Label = "K2"
			_ = s // used above
			break
		}
	}

	recallColW := len(fmt.Sprintf("Recall@%d", k)) + 2
	var header, sep string
	if hasK2 {
		recall2Header := fmt.Sprintf("Recall@%s", k2Label)
		recall2ColW := len(recall2Header) + 2
		header = fmt.Sprintf("| %-14s | Questions | Exact Match | Avg F1  | Recall@%d | %s |\n", "Benchmark", k, recall2Header)
		sep = fmt.Sprintf("|%s|-----------|-------------|---------|%s|%s|\n",
			strings.Repeat("-", 16),
			strings.Repeat("-", recallColW),
			strings.Repeat("-", recall2ColW),
		)
	} else {
		header = fmt.Sprintf("| %-14s | Questions | Exact Match | Avg F1  | Recall@%d |\n", "Benchmark", k)
		sep = fmt.Sprintf("|%s|-----------|-------------|---------|%s|\n",
			strings.Repeat("-", 16), strings.Repeat("-", recallColW))
	}

	sb.WriteString(header)
	sb.WriteString(sep)

	for _, s := range summaries {
		// recallColW-3: -2 for the surrounding spaces in "| %s |", -1 for the
		// literal "%" appended by "%%". This gives the numeric width of the
		// float part; "%%" then appends the "%" sign.
		recallCell := fmt.Sprintf("%*.1f%%", recallColW-3, s.RecallAtK*100)
		if hasK2 {
			recall2Header := fmt.Sprintf("Recall@%s", k2Label)
			recall2ColW := len(recall2Header) + 2
			recall2Cell := fmt.Sprintf("%*.1f%%", recall2ColW-3, s.RecallAtK2*100)
			fmt.Fprintf(&sb, "| %-14s | %-9d | %10.1f%% | %.4f  | %s | %s |\n",
				s.Name,
				s.TotalQuestions,
				s.ExactMatchAcc*100,
				s.AvgF1,
				recallCell,
				recall2Cell,
			)
		} else {
			fmt.Fprintf(&sb, "| %-14s | %-9d | %10.1f%% | %.4f  | %s |\n",
				s.Name,
				s.TotalQuestions,
				s.ExactMatchAcc*100,
				s.AvgF1,
				recallCell,
			)
		}
	}

	return sb.String()
}
