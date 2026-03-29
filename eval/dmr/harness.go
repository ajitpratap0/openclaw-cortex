package dmr

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ajitpratap0/openclaw-cortex/eval/runner"
)

const benchmarkName = "DMR"

// Run ingests DMR conversation turns and evaluates all QA pairs.
// It returns a BenchmarkSummary with individual and aggregate results.
//
// When accumulate is false (default), the store is reset before every QA pair
// so each pair is evaluated against only its own conversation turns (isolation mode).
//
// When accumulate is true, the store is reset once at the start.  The run then
// proceeds in two passes:
//
//   - Pass 1: for every pair, call client.Store for each conversation turn WITHOUT
//     evaluating (ingest all turns into a single growing store).
//   - Pass 2: for every pair, call client.Recall and score the result.
//
// Accumulate mode measures recall against a fully-populated shared store and sets
// summary.Mode = "accumulate".  Per-pair-reset mode sets summary.Mode = "per-pair-reset".
func Run(ctx context.Context, client runner.Client, k int, accumulate bool) (*runner.BenchmarkSummary, error) {
	pairs := syntheticDataset()
	return run(ctx, client, pairs, k, accumulate)
}

// RunFromFile loads a DMR dataset from disk and runs the benchmark.
// It is equivalent to Run but uses the real dataset instead of the synthetic one.
func RunFromFile(ctx context.Context, client runner.Client, path string, k int, accumulate bool) (*runner.BenchmarkSummary, error) {
	pairs, err := LoadDataset(path)
	if err != nil {
		return nil, fmt.Errorf("dmr: load dataset: %w", err)
	}
	return run(ctx, client, pairs, k, accumulate)
}

// run is the shared implementation used by both Run and RunFromFile.
func run(ctx context.Context, client runner.Client, pairs []QAPair, k int, accumulate bool) (*runner.BenchmarkSummary, error) {
	results := make([]runner.BenchmarkResult, 0, len(pairs))
	recallFailures := 0

	if accumulate {
		if err := client.Reset(ctx); err != nil {
			return nil, fmt.Errorf("dmr: reset failed at start of accumulate run (aborting): %w", err)
		}

		// Pass 1: ingest all conversation turns.
		for i := range pairs {
			if err := ctx.Err(); err != nil {
				return nil, fmt.Errorf("dmr: context canceled during accumulate pass-1 store: %w", err)
			}
			qp := &pairs[i]
			for j := range qp.Conversation {
				turn := &qp.Conversation[j]
				content := fmt.Sprintf("%s: %s", turn.Speaker, turn.Content)
				if err := client.Store(ctx, content); err != nil {
					return nil, fmt.Errorf("dmr: ingest turn failed for %s (turn %d) during accumulate pass-1: %w", qp.ID, j, err)
				}
			}
		}

		// Pass 2: recall and score each pair.
		for i := range pairs {
			if err := ctx.Err(); err != nil {
				return nil, fmt.Errorf("dmr: context canceled before completing all pairs: %w", err)
			}
			qp := &pairs[i]

			memories, err := client.Recall(ctx, qp.Question, k)
			if err != nil {
				if ctx.Err() != nil {
					return nil, fmt.Errorf("dmr: context canceled during recall for %s (recall error: %v): %w", qp.ID, err, ctx.Err())
				}
				recallFailures++
				fmt.Fprintf(os.Stderr, "[dmr] warn: recall failed for %s: %v\n", qp.ID, err)
				memories = nil
			}

			best := ""
			if len(memories) > 0 {
				best = runner.BestCandidate(memories, qp.Answer)
			}

			result := runner.BenchmarkResult{
				QuestionID:  qp.ID,
				Question:    qp.Question,
				GroundTruth: qp.Answer,
				Retrieved:   best,
				ExactMatch:  runner.ExactMatch(best, qp.Answer),
				F1Score:     runner.TokenF1(best, qp.Answer),
				RecalledAtK: runner.RecallAtK(memories, qp.Answer, k),
				Category:    qp.Category,
			}
			results = append(results, result)
		}

		if recallFailures == len(pairs) && len(pairs) > 0 {
			return nil, fmt.Errorf("dmr: all %d recall calls failed — check binary path and Memgraph/Ollama connectivity", len(pairs))
		}
		summary := runner.Summarize(benchmarkName, results, k, recallFailures)
		summary.Mode = "accumulate"
		return summary, nil
	}

	// Per-pair-reset mode (default / backward-compatible).
	for i := range pairs {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("dmr: context canceled before completing all pairs: %w", err)
		}
		qp := &pairs[i]

		if err := client.Reset(ctx); err != nil {
			return nil, fmt.Errorf("dmr: reset failed before %s (aborting to prevent contamination): %w", qp.ID, err)
		}

		for j := range qp.Conversation {
			turn := &qp.Conversation[j]
			content := fmt.Sprintf("%s: %s", turn.Speaker, turn.Content)
			if err := client.Store(ctx, content); err != nil {
				return nil, fmt.Errorf("dmr: ingest turn failed for %s (turn %d): %w", qp.ID, j, err)
			}
		}

		memories, err := client.Recall(ctx, qp.Question, k)
		if err != nil {
			if ctx.Err() != nil {
				return nil, fmt.Errorf("dmr: context canceled during recall for %s (recall error: %v): %w", qp.ID, err, ctx.Err())
			}
			recallFailures++
			fmt.Fprintf(os.Stderr, "[dmr] warn: recall failed for %s: %v\n", qp.ID, err)
			memories = nil
		}

		best := ""
		if len(memories) > 0 {
			best = runner.BestCandidate(memories, qp.Answer)
		}

		result := runner.BenchmarkResult{
			QuestionID:  qp.ID,
			Question:    qp.Question,
			GroundTruth: qp.Answer,
			Retrieved:   best,
			ExactMatch:  runner.ExactMatch(best, qp.Answer),
			F1Score:     runner.TokenF1(best, qp.Answer),
			RecalledAtK: runner.RecallAtK(memories, qp.Answer, k),
			Category:    qp.Category,
		}
		results = append(results, result)
	}

	if recallFailures == len(pairs) && len(pairs) > 0 {
		return nil, fmt.Errorf("dmr: all %d recall calls failed — check binary path and Memgraph/Ollama connectivity", len(pairs))
	}
	summary := runner.Summarize(benchmarkName, results, k, recallFailures)
	summary.Mode = "per-pair-reset"
	return summary, nil
}

// CategoryBreakdown returns per-hop-depth aggregate metrics for a DMR summary.
// The returned map keys are hop-depth categories ("1-hop" through "5-hop").
// Each value is a *runner.CategorySummary with ExactMatchAcc, AvgF1, and RecallAtK.
func CategoryBreakdown(summary *runner.BenchmarkSummary) map[string]*runner.CategorySummary {
	// Build per-category accumulators.
	type acc struct {
		total      int
		exactHits  int
		f1Sum      float64
		recallHits int
	}
	buckets := map[string]*acc{}

	for i := range summary.Results {
		r := &summary.Results[i]
		cat := r.Category
		if cat == "" {
			cat = "unknown"
		}
		if _, ok := buckets[cat]; !ok {
			buckets[cat] = &acc{}
		}
		b := buckets[cat]
		b.total++
		if r.ExactMatch {
			b.exactHits++
		}
		b.f1Sum += r.F1Score
		if r.RecalledAtK {
			b.recallHits++
		}
	}

	out := make(map[string]*runner.CategorySummary, len(buckets))
	for cat, b := range buckets {
		cs := &runner.CategorySummary{TotalQuestions: b.total}
		if b.total > 0 {
			cs.ExactMatchAcc = float64(b.exactHits) / float64(b.total)
			cs.AvgF1 = b.f1Sum / float64(b.total)
			cs.RecallAtK = float64(b.recallHits) / float64(b.total)
		}
		out[cat] = cs
	}
	return out
}

// FormatCategoryTable renders a markdown table of per-hop-depth results.
func FormatCategoryTable(breakdowns map[string]*runner.CategorySummary) string {
	var sb strings.Builder
	sb.WriteString("| Category | Questions | Exact Match | Avg F1  | Recall@K |\n")
	sb.WriteString("|----------|-----------|-------------|---------|----------|\n")
	categories := []string{"1-hop", "2-hop", "3-hop", "4-hop", "5-hop"}
	for _, cat := range categories {
		cs, ok := breakdowns[cat]
		if !ok {
			continue
		}
		fmt.Fprintf(&sb, "| %-8s | %-9d | %10.1f%% | %.4f  | %8.1f%% |\n",
			cat,
			cs.TotalQuestions,
			cs.ExactMatchAcc*100,
			cs.AvgF1,
			cs.RecallAtK*100,
		)
	}
	return sb.String()
}
