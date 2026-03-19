// Package runner provides shared types, a CortexClient wrapper, and scoring
// functions used by all benchmark harnesses.
package runner

import (
	"bytes"
	"context"
	"encoding/json"
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
	Retrieved   string  `json:"retrieved"` // best recalled memory content
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

// recallJSONResult is a minimal struct for parsing JSON output from
// `openclaw-cortex recall --context _`.
type recallJSONResult struct {
	Memory struct {
		Content string `json:"content"`
	} `json:"memory"`
}

// Recall runs `openclaw-cortex recall --context _ <query>` and returns up to
// limit memory content strings parsed from the JSON output.
func (c *CortexClient) Recall(ctx context.Context, query string, limit int) ([]string, error) {
	args := append(c.baseArgs(), "recall", "--budget", fmt.Sprintf("%d", limit*500), "--context", "_", "--", query)
	//nolint:gosec // binaryPath is set by the caller, not user-supplied in a web context.
	cmd := exec.CommandContext(ctx, c.BinaryPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("runner: recall binary error: %w (stderr: %s)", err, stderr.String())
	}
	var results []recallJSONResult
	if err := json.Unmarshal(stdout.Bytes(), &results); err != nil {
		return nil, fmt.Errorf("runner: recall JSON parse error: %w (output: %s)", err, stdout.String())
	}
	contents := make([]string, 0, len(results))
	for i := range results {
		contents = append(contents, results[i].Memory.Content)
	}
	if len(contents) > limit {
		contents = contents[:limit]
	}
	return contents, nil
}

// Store runs `openclaw-cortex store <content>` to persist a fact memory.
func (c *CortexClient) Store(ctx context.Context, content string) error {
	args := append(c.baseArgs(), "store", "--scope", "permanent", "--type", "fact", "--", content)
	//nolint:gosec
	cmd := exec.CommandContext(ctx, c.BinaryPath, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("runner: store binary error: %w (output: %s)", err, out)
	}
	return nil
}

// Reset calls `openclaw-cortex reset --yes` to wipe all memories from the store.
// Used by benchmark harnesses to isolate QA pairs from each other.
func (c *CortexClient) Reset(ctx context.Context) error {
	args := append(c.baseArgs(), "reset", "--yes")
	//nolint:gosec
	cmd := exec.CommandContext(ctx, c.BinaryPath, args...)
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

func isAlphaNum(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '\''
}

// TokenF1 computes token-level F1 between retrieved and ground truth.
// Returns 0 if groundTruth is empty (consistent with ExactMatch).
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
		overlap += int(math.Min(float64(predCount), float64(goldCount)))
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

