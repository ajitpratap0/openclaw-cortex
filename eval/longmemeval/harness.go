// Package longmemeval implements the LongMemEval benchmark harness.
package longmemeval

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ajitpratap0/openclaw-cortex/eval/runner"
)

const benchmarkName = "LongMemEval"

// Run ingests all synthetic LongMemEval facts and evaluates all QA pairs.
// It returns a BenchmarkSummary with individual and aggregate results.
//
// When accumulate is false (default), the store is reset before every QA pair
// so each pair is evaluated against only its own facts (isolation mode).
//
// When accumulate is true, the store is reset once at the start.  The run then
// proceeds in two passes:
//
//   - Pass 1: for every pair, call client.Store for each fact WITHOUT evaluating
//     (ingest all facts into a single growing store).
//   - Pass 2: for every pair, call client.Recall and score the result.
//
// Accumulate mode measures recall against a fully-populated shared store and sets
// summary.Mode = "accumulate".  Per-pair-reset mode sets summary.Mode = "per-pair-reset".
func Run(ctx context.Context, client runner.Client, k int, accumulate bool) (*runner.BenchmarkSummary, error) {
	pairs := Dataset()
	results := make([]runner.BenchmarkResult, 0, len(pairs))
	recallFailures := 0

	if accumulate {
		// Single reset at the start.
		if err := client.Reset(ctx); err != nil {
			return nil, fmt.Errorf("longmemeval: reset failed at start of accumulate run (aborting): %w", err)
		}

		// Pass 1: ingest all facts.
		// Any store failure aborts the run: partial ingestion produces silently
		// deflated scores against an incomplete shared store.
		for i := range pairs {
			if err := ctx.Err(); err != nil {
				return nil, fmt.Errorf("longmemeval: context canceled during accumulate pass-1 store: %w", err)
			}
			qp := &pairs[i]
			for j := range qp.Facts {
				fact := &qp.Facts[j]
				if err := client.Store(ctx, fact.Content); err != nil {
					return nil, fmt.Errorf("longmemeval: ingest fact failed for %s (fact %d) during accumulate pass-1: %w", qp.ID, j, err)
				}
			}
		}

		// Pass 2: recall and score each pair.
		for i := range pairs {
			if err := ctx.Err(); err != nil {
				return nil, fmt.Errorf("longmemeval: context canceled before completing all pairs: %w", err)
			}
			qp := &pairs[i]

			memories, err := client.Recall(ctx, qp.Question, k)
			if err != nil {
				if ctx.Err() != nil {
					return nil, fmt.Errorf("longmemeval: context canceled during recall for %s (recall error: %v): %w", qp.ID, err, ctx.Err())
				}
				recallFailures++
				fmt.Fprintf(os.Stderr, "[longmemeval] warn: recall failed for %s: %v\n", qp.ID, err)
				memories = nil
			}

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

		if recallFailures == len(pairs) && len(pairs) > 0 {
			return nil, fmt.Errorf("longmemeval: all %d recall calls failed — check binary path and Memgraph/Ollama connectivity", len(pairs))
		}
		summary := runner.Summarize(benchmarkName, results, k, recallFailures)
		summary.Mode = "accumulate"
		return summary, nil
	}

	// Per-pair-reset mode (default / backward-compatible).
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
		// Any store failure aborts the entire benchmark run: partial ingestion means
		// the recall results are based on incomplete data, producing silently deflated scores.
		//
		// Note: knowledge-update pairs (lme-K*) have facts with a ValidTo field
		// marking the superseded fact, but this harness only stores fact.Content and
		// does not pass --supersedes to the binary. Both the old and new fact land in
		// the store with no valid_to set on the old one. The retrieval system's
		// temporal-versioning path (valid_from/valid_to, SearchFilters.AsOf) is
		// therefore not exercised here. BestCandidate selects the correct (newer) fact
		// by token-F1, not by graph-level invalidation. This harness measures semantic
		// retrieval only — temporal versioning is out of scope.
		for j := range qp.Facts {
			fact := &qp.Facts[j]
			if err := client.Store(ctx, fact.Content); err != nil {
				return nil, fmt.Errorf("longmemeval: ingest fact failed for %s (fact %d): %w", qp.ID, j, err)
			}
		}

		// Retrieve relevant memories.
		memories, err := client.Recall(ctx, qp.Question, k)
		if err != nil {
			// ctx.Err() distinguishes two cases:
			//   - parent context canceled: abort (global benchmark timeout fired)
			//   - per-call timeout inside CortexClient.Recall: count as recallFailure and continue
			// Narrow race: if the parent context is canceled between Recall returning
			// and this check, the cancellation is counted as a recallFailure instead of
			// aborting. Acceptable for benchmark purposes — scores show one extra failure.
			if ctx.Err() != nil {
				return nil, fmt.Errorf("longmemeval: context canceled during recall for %s (recall error: %v): %w", qp.ID, err, ctx.Err())
			}
			recallFailures++
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
			ExactMatch:  runner.ExactMatch(best, qp.GroundTruth), // oracle substring containment, not strict equality — see BenchmarkResult doc
			F1Score:     runner.TokenF1(best, qp.GroundTruth),
			RecalledAtK: runner.RecallAtK(memories, qp.GroundTruth, k),
		}
		results = append(results, result)
	}

	if recallFailures == len(pairs) && len(pairs) > 0 {
		return nil, fmt.Errorf("longmemeval: all %d recall calls failed — check binary path and Memgraph/Ollama connectivity", len(pairs))
	}
	summary := runner.Summarize(benchmarkName, results, k, recallFailures)
	summary.Mode = "per-pair-reset"
	return summary, nil
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
	sb.WriteString("| Category         | Exact Match |\n")
	sb.WriteString("|------------------|-------------|\n")
	categories := []string{"temporal", "multi-hop", "knowledge-update"}
	for _, cat := range categories {
		acc, ok := breakdown[cat]
		if !ok {
			continue
		}
		fmt.Fprintf(&sb, "| %-16s | %10.1f%% |\n", cat, acc*100)
	}
	return sb.String()
}
