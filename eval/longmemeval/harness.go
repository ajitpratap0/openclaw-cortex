// Package longmemeval implements the LongMemEval benchmark harness.
package longmemeval

import (
	"context"
	"fmt"

	"github.com/ajitpratap0/openclaw-cortex/eval/runner"
)

const benchmarkName = "LongMemEval"

// Run ingests all synthetic LongMemEval facts and evaluates all QA pairs.
// It returns a BenchmarkSummary with individual and aggregate results.
//
// For each QA pair:
//  1. Ingest all facts via CortexClient.Store.
//  2. Run Recall(question, k) to retrieve relevant memories.
//  3. Score: ExactMatch + TokenF1 + RecallAtK.
func Run(ctx context.Context, client *runner.CortexClient, k int) (*runner.BenchmarkSummary, error) {
	pairs := Dataset()
	results := make([]runner.BenchmarkResult, 0, len(pairs))

	for i := range pairs {
		qp := &pairs[i]

		// Ingest the facts for this QA pair.
		for j := range qp.Facts {
			fact := &qp.Facts[j]
			if err := client.Store(ctx, fact.Content); err != nil {
				// Non-fatal: log and continue.
				fmt.Printf("[longmemeval] warn: ingest fact failed for %s: %v\n", qp.ID, err)
			}
		}

		// Retrieve relevant memories.
		memories, err := client.Recall(ctx, qp.Question, k)
		if err != nil {
			fmt.Printf("[longmemeval] warn: recall failed for %s: %v\n", qp.ID, err)
			memories = nil
		}

		// Select the best candidate.
		best := ""
		if len(memories) > 0 {
			best = bestCandidate(memories, qp.GroundTruth)
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

// bestCandidate picks the memory from the retrieved list that has the highest
// token-F1 against the ground truth. Falls back to the first result if no
// candidate scores above zero.
func bestCandidate(memories []string, groundTruth string) string {
	best := memories[0]
	bestF1 := runner.TokenF1(memories[0], groundTruth)

	for i := 1; i < len(memories); i++ {
		f1 := runner.TokenF1(memories[i], groundTruth)
		if f1 > bestF1 {
			bestF1 = f1
			best = memories[i]
		}
	}
	return best
}
