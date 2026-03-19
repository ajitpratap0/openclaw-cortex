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

	// NOTE: facts ingested for one QA pair remain in the memory store when
	// subsequent pairs are evaluated. This is a known limitation of the eval
	// design — proper per-pair isolation requires flushing the DB between pairs,
	// which is not feasible without adding a truncate/reset command to the binary.
	// TODO(eval): add a --reset flag to openclaw-cortex to flush memories between pairs.
	for i := range pairs {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("longmemeval: context canceled before completing all pairs: %w", err)
		}
		qp := &pairs[i]

		// Ingest the facts for this QA pair.
		storeFailures := 0
		for j := range qp.Facts {
			fact := &qp.Facts[j]
			if err := client.Store(ctx, fact.Content); err != nil {
				storeFailures++
				fmt.Fprintf(os.Stderr, "[longmemeval] warn: ingest fact failed for %s: %v\n", qp.ID, err)
			}
		}
		if storeFailures == len(qp.Facts) && len(qp.Facts) > 0 {
			fmt.Fprintf(os.Stderr, "[longmemeval] error: all %d store calls failed for %s — recall will return nothing\n", storeFailures, qp.ID)
		}

		// Retrieve relevant memories.
		memories, err := client.Recall(ctx, qp.Question, k)
		if err != nil {
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
