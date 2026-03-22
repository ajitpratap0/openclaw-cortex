// Package longmemeval implements the LongMemEval benchmark harness.
package longmemeval

import (
	"context"
	"fmt"
	"os"

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
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("longmemeval: context canceled before completing all pairs: %w", err)
		}
		qp := &pairs[i]

		// Reset the memory store before ingesting this pair's facts to prevent
		// contamination from prior pairs. If reset fails, abort rather than
		// produce scores against stale data.
		if err := client.Reset(ctx); err != nil {
			return nil, fmt.Errorf("longmemeval: reset failed before %s (aborting to prevent contamination): %w", qp.ID, err)
		}

		// Ingest the facts for this QA pair.
		// Any store failure aborts the pair: partial ingestion means the recall
		// results are based on incomplete data, producing silently deflated scores.
		for j := range qp.Facts {
			fact := &qp.Facts[j]
			if err := client.Store(ctx, fact.Content); err != nil {
				return nil, fmt.Errorf("longmemeval: ingest fact failed for %s (fact %d): %w", qp.ID, j, err)
			}
		}

		// Retrieve relevant memories.
		memories, err := client.Recall(ctx, qp.Question, k)
		if err != nil {
			if ctx.Err() != nil {
				return nil, fmt.Errorf("longmemeval: context canceled during recall for %s: %w", qp.ID, ctx.Err())
			}
			fmt.Fprintf(os.Stderr, "[longmemeval] warn: recall failed for %s: %v\n", qp.ID, err)
			memories = nil
		}

		// Select the best candidate.
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
