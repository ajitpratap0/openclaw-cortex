// Package runner provides shared types, a CortexClient wrapper, and scoring
// functions used by all benchmark harnesses.
package runner

import (
	"context"
	"fmt"
	"math"
	"os/exec"
	"strings"
)

// BenchmarkResult holds the outcome of one QA pair evaluation.
type BenchmarkResult struct {
	QuestionID  string  `json:"question_id"`
	Question    string  `json:"question"`
	GroundTruth string  `json:"ground_truth"`
	Retrieved   string  `json:"retrieved"`    // best recalled memory content
	ExactMatch  bool    `json:"exact_match"`
	F1Score     float64 `json:"f1_score"`
	RecalledAtK bool    `json:"recalled_at_k"` // was ground truth in top-k results?
}

// BenchmarkSummary aggregates results from a single benchmark run.
type BenchmarkSummary struct {
	Name           string            `json:"name"`
	TotalQuestions int               `json:"total_questions"`
	ExactMatchAcc  float64           `json:"exact_match_accuracy"`
	AvgF1          float64           `json:"avg_f1"`
	RecallAtK      float64           `json:"recall_at_k"`
	K              int               `json:"k"`
	Results        []BenchmarkResult `json:"results"`
}

// CortexClient wraps the openclaw-cortex binary via execFile (no shell injection).
type CortexClient struct {
	// BinaryPath is the path to the openclaw-cortex binary. Defaults to "openclaw-cortex".
	BinaryPath string
	// ConfigPath optionally points to an openclaw-cortex config file.
	ConfigPath string
}

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

func (c *CortexClient) baseArgs() []string {
	if c.ConfigPath != "" {
		return []string{"--config", c.ConfigPath}
	}
	return nil
}

// Recall runs `openclaw-cortex recall <query>` and returns up to limit lines of
// output, each representing one recalled memory's content.
func (c *CortexClient) Recall(ctx context.Context, query string, limit int) ([]string, error) {
	args := append(c.baseArgs(), "recall", query, "--budget", fmt.Sprintf("%d", limit*200))
	//nolint:gosec // binaryPath is set by the caller, not user-supplied in a web context.
	out, err := exec.CommandContext(ctx, c.BinaryPath, args...).Output()
	if err != nil {
		return nil, fmt.Errorf("runner: recall binary error: %w", err)
	}
	lines := splitNonEmpty(string(out))
	if len(lines) > limit {
		lines = lines[:limit]
	}
	return lines, nil
}

// Capture runs `openclaw-cortex capture --user <u> --assistant <a>`.
func (c *CortexClient) Capture(ctx context.Context, userMsg, assistantMsg string) error {
	args := append(c.baseArgs(), "capture",
		"--user", userMsg,
		"--assistant", assistantMsg,
		"--scope", "permanent",
	)
	//nolint:gosec
	cmd := exec.CommandContext(ctx, c.BinaryPath, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("runner: capture binary error: %w (output: %s)", err, out)
	}
	return nil
}

// Store runs `openclaw-cortex store <content>` to persist a fact memory.
func (c *CortexClient) Store(ctx context.Context, content string) error {
	args := append(c.baseArgs(), "store", content, "--scope", "permanent", "--type", "fact")
	//nolint:gosec
	cmd := exec.CommandContext(ctx, c.BinaryPath, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("runner: store binary error: %w (output: %s)", err, out)
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

func isAlphaNum(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '\''
}

// TokenF1 computes token-level F1 between retrieved and ground truth.
func TokenF1(retrieved, groundTruth string) float64 {
	predTokens := tokenize(retrieved)
	goldTokens := tokenize(groundTruth)

	if len(predTokens) == 0 && len(goldTokens) == 0 {
		return 1.0
	}
	if len(predTokens) == 0 || len(goldTokens) == 0 {
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
		overlap += int(math.Min(float64(predCount), float64(goldCount)))
	}

	if overlap == 0 {
		return 0.0
	}

	precision := float64(overlap) / float64(len(predTokens))
	recallScore := float64(overlap) / float64(len(goldTokens))
	denom := precision + recallScore
	if denom == 0 {
		return 0.0
	}
	return 2 * precision * recallScore / denom
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
func Summarize(name string, results []BenchmarkResult, k int) *BenchmarkSummary {
	total := len(results)
	if total == 0 {
		return &BenchmarkSummary{Name: name, K: k}
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
		Results:        results,
	}
}

// splitNonEmpty splits text by newlines, skipping blank lines.
func splitNonEmpty(s string) []string {
	raw := strings.Split(s, "\n")
	out := make([]string, 0, len(raw))
	for _, line := range raw {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
