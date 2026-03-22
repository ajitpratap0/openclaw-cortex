// Command eval runs LoCoMo and/or LongMemEval benchmarks against a live
// openclaw-cortex instance and reports results as JSON and a markdown table.
//
// Usage:
//
//	go run ./eval/cmd/eval [flags]
//
// Flags:
//
//	--benchmark  string   Which benchmark to run: locomo, longmemeval, all (default: all)
//	--binary     string   Path to openclaw-cortex binary (default: openclaw-cortex)
//	--k          int      k for recall@k metric (default: 5)
//	--output     string   Output file path for JSON results (default: stdout)
//	--config     string   Path to openclaw-cortex config file (optional)
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ajitpratap0/openclaw-cortex/eval/locomo"
	"github.com/ajitpratap0/openclaw-cortex/eval/longmemeval"
	"github.com/ajitpratap0/openclaw-cortex/eval/runner"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "eval: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	benchmark := flag.String("benchmark", "all", "Which benchmark to run: locomo, longmemeval, all")
	binary := flag.String("binary", "openclaw-cortex", "Path to openclaw-cortex binary")
	k := flag.Int("k", 5, "k for recall@k metric")
	output := flag.String("output", "", "Output file path for JSON results (default: stdout)")
	configPath := flag.String("config", "", "Path to openclaw-cortex config file")
	timeout := flag.Int("timeout", 300, "Total timeout in seconds (default: 300)")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*timeout)*time.Second)
	defer cancel()

	client := runner.NewCortexClient(*binary, *configPath)

	var summaries []*runner.BenchmarkSummary

	switch strings.ToLower(*benchmark) {
	case "locomo":
		s, err := runLocomo(ctx, client, *k)
		if err != nil {
			return fmt.Errorf("running LoCoMo: %w", err)
		}
		summaries = append(summaries, s)

	case "longmemeval":
		s, err := runLongMemEval(ctx, client, *k)
		if err != nil {
			return fmt.Errorf("running LongMemEval: %w", err)
		}
		summaries = append(summaries, s)

	case "all":
		s1, err := runLocomo(ctx, client, *k)
		if err != nil {
			return fmt.Errorf("running LoCoMo: %w", err)
		}
		summaries = append(summaries, s1)

		// Reset between benchmarks so LoCoMo facts don't contaminate LongMemEval.
		// If this fails, LongMemEval results would be meaningless — abort rather than
		// produce tainted scores.
		if resetErr := client.Reset(ctx); resetErr != nil {
			return fmt.Errorf("inter-benchmark reset failed (aborting to prevent contamination): %w", resetErr)
		}

		s2, err := runLongMemEval(ctx, client, *k)
		if err != nil {
			return fmt.Errorf("running LongMemEval: %w", err)
		}
		summaries = append(summaries, s2)

	default:
		return fmt.Errorf("unknown benchmark %q — choose locomo, longmemeval, or all", *benchmark)
	}

	// Encode results as JSON.
	enc, err := json.MarshalIndent(summaries, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding JSON: %w", err)
	}

	if *output != "" {
		if writeErr := os.WriteFile(*output, enc, 0o600); writeErr != nil {
			return fmt.Errorf("writing output file: %w", writeErr)
		}
		fmt.Printf("Results written to %s\n\n", *output)
	} else {
		fmt.Println(string(enc))
		fmt.Println()
	}

	// Print markdown summary table.
	fmt.Println(markdownTable(summaries, *k))
	return nil
}

func runLocomo(ctx context.Context, client *runner.CortexClient, k int) (*runner.BenchmarkSummary, error) {
	fmt.Fprintln(os.Stderr, "Running LoCoMo benchmark...")
	return locomo.Run(ctx, client, k)
}

func runLongMemEval(ctx context.Context, client *runner.CortexClient, k int) (*runner.BenchmarkSummary, error) {
	fmt.Fprintln(os.Stderr, "Running LongMemEval benchmark...")
	return longmemeval.Run(ctx, client, k)
}

// markdownTable renders a results table in GitHub-flavored markdown.
func markdownTable(summaries []*runner.BenchmarkSummary, k int) string {
	var sb strings.Builder

	header := fmt.Sprintf("| %-14s | Questions | Exact Match | Avg F1  | Recall@%d |\n", "Benchmark", k)
	sep := fmt.Sprintf("|%s|-----------|-------------|---------|----------|\n",
		strings.Repeat("-", 16))

	sb.WriteString(header)
	sb.WriteString(sep)

	for _, s := range summaries {
		fmt.Fprintf(&sb, "| %-14s | %-9d | %10.1f%% | %.4f  | %8.1f%% |\n",
			s.Name,
			s.TotalQuestions,
			s.ExactMatchAcc*100,
			s.AvgF1,
			s.RecallAtK*100,
		)
	}

	return sb.String()
}
