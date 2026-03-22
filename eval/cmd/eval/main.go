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
//	--timeout    int      Total timeout in seconds (default: 300)
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

	if flag.NArg() > 0 {
		return fmt.Errorf("unexpected positional arguments: %v (use --benchmark to select a benchmark)", flag.Args())
	}

	if *timeout <= 0 {
		return fmt.Errorf("--timeout must be > 0, got %d", *timeout)
	}
	if *k <= 0 {
		return fmt.Errorf("--k must be > 0, got %d", *k)
	}

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

		// Reset between benchmarks so any residual LoCoMo state doesn't bleed into
		// LongMemEval. This is a defensive belt-and-suspenders step: longmemeval.Run
		// already calls Reset as its first operation, so the store would be wiped
		// anyway. The explicit reset here makes the isolation intent visible at the
		// call-site and ensures abort-on-failure semantics if the reset itself errors.
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
		if writeErr := os.WriteFile(*output, enc, 0o644); writeErr != nil {
			return fmt.Errorf("writing output file: %w", writeErr)
		}
		fmt.Fprintf(os.Stderr, "Results written to %s\n\n", *output)
	} else {
		fmt.Println(string(enc))
		fmt.Println()
	}

	// Warn on stderr if any benchmark had partial recall failures so the
	// degraded run is visible even when only reading the markdown table.
	for _, s := range summaries {
		if s.RecallFailures > 0 {
			fmt.Fprintf(os.Stderr, "WARNING: %s had %d/%d recall failures — scores are deflated\n",
				s.Name, s.RecallFailures, s.TotalQuestions)
		}
	}

	// Print markdown summary table to stderr so stdout stays clean JSON
	// (allows: go run ./eval/cmd/eval | jq '.').
	fmt.Fprintln(os.Stderr, runner.FormatMarkdownTable(summaries, *k))
	return nil
}

func runLocomo(ctx context.Context, client runner.Client, k int) (*runner.BenchmarkSummary, error) {
	fmt.Fprintln(os.Stderr, "Running LoCoMo benchmark...")
	return locomo.Run(ctx, client, k)
}

func runLongMemEval(ctx context.Context, client runner.Client, k int) (*runner.BenchmarkSummary, error) {
	fmt.Fprintln(os.Stderr, "Running LongMemEval benchmark...")
	return longmemeval.Run(ctx, client, k)
}
