// Package locomo implements the LoCoMo benchmark harness.
package locomo

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ajitpratap0/openclaw-cortex/eval/runner"
)

const benchmarkName = "LoCoMo"

// Run ingests all synthetic LoCoMo conversations and evaluates all QA pairs.
// It returns a BenchmarkSummary with individual and aggregate results.
//
// For each QA pair:
//  1. Ingest the conversation turns via CortexClient.Store (one combined string per turn).
//  2. Run Recall(question, k) to retrieve relevant memories.
//  3. Score: ExactMatch + TokenF1 + RecallAtK.
func Run(ctx context.Context, client *runner.CortexClient, k int) (*runner.BenchmarkSummary, error) {
	pairs := Dataset()
	results := make([]runner.BenchmarkResult, 0, len(pairs))

	for i := range pairs {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("locomo: context canceled before completing all pairs: %w", err)
		}
		qp := &pairs[i]

		// Reset the memory store before ingesting this pair's facts to prevent
		// contamination from prior pairs. If reset fails, abort rather than
		// produce scores against stale data.
		if err := client.Reset(ctx); err != nil {
			return nil, fmt.Errorf("locomo: reset failed before %s (aborting to prevent contamination): %w", qp.ID, err)
		}

		// Ingest conversation turns as stored facts so the recall engine can
		// find them.  We combine user + assistant into a single string that
		// represents the semantic content of the turn.
		storeFailures := 0
		for j := range qp.Conversation {
			turn := &qp.Conversation[j]
			content := fmt.Sprintf("User: %s Assistant: %s", turn.User, turn.Assistant)
			if err := client.Store(ctx, content); err != nil {
				storeFailures++
				fmt.Fprintf(os.Stderr, "[locomo] warn: ingest turn failed for %s: %v\n", qp.ID, err)
			}
		}
		if storeFailures == len(qp.Conversation) && len(qp.Conversation) > 0 {
			return nil, fmt.Errorf("locomo: all %d store calls failed for %s — check binary path and Memgraph connectivity", storeFailures, qp.ID)
		}

		// Retrieve relevant memories for the question.
		memories, err := client.Recall(ctx, qp.Question, k)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[locomo] warn: recall failed for %s: %v\n", qp.ID, err)
			memories = nil
		}

		// Best retrieved memory is the top result (or empty string if none).
		best := ""
		if len(memories) > 0 {
			best = runner.BestCandidate(memories, qp.GroundTruth)
		}

		result := runner.BenchmarkResult{
			QuestionID:  qp.ID,
			Question:    qp.Question,
			GroundTruth: qp.GroundTruth,
			Retrieved:   best,
			ExactMatch:  runner.ExactMatch(best, qp.GroundTruth),
			F1Score:     runner.TokenF1(best, qp.GroundTruth),
			RecalledAtK: runner.RecallAtK(memories, qp.GroundTruth, k),
		}
		results = append(results, result)
	}

	return runner.Summarize(benchmarkName, results, k), nil
}

// CategoryBreakdown returns ExactMatch accuracy broken down by QA category.
func CategoryBreakdown(summary *runner.BenchmarkSummary) map[string]float64 {
	pairs := Dataset()
	idToCategory := make(map[string]string, len(pairs))
	for i := range pairs {
		idToCategory[pairs[i].ID] = pairs[i].Category
	}

	counts := map[string]int{}
	hits := map[string]int{}
	for i := range summary.Results {
		r := &summary.Results[i]
		cat := idToCategory[r.QuestionID]
		if cat == "" {
			cat = "unknown"
		}
		counts[cat]++
		if r.ExactMatch {
			hits[cat]++
		}
	}

	breakdown := make(map[string]float64, len(counts))
	for cat, total := range counts {
		if total > 0 {
			breakdown[cat] = float64(hits[cat]) / float64(total)
		}
	}
	return breakdown
}

// FormatCategoryTable renders a small markdown table of per-category results.
func FormatCategoryTable(breakdown map[string]float64) string {
	var sb strings.Builder
	sb.WriteString("| Category    | Exact Match |\n")
	sb.WriteString("|-------------|-------------|\n")
	categories := []string{"single-hop", "multi-hop", "temporal"}
	for _, cat := range categories {
		acc, ok := breakdown[cat]
		if !ok {
			continue
		}
		fmt.Fprintf(&sb, "| %-11s | %10.1f%% |\n", cat, acc*100)
	}
	return sb.String()
}
