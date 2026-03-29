// Command compare reads a published baselines JSON file and one or more
// BenchmarkSummary result files, then emits a GitHub-flavored markdown
// comparison table to stdout and a machine-readable comparison.json.
//
// Usage:
//
//	go run ./eval/cmd/compare \
//	  --baseline eval/baselines/published.json \
//	  --current  results_v1.2.3.json \
//	  [--output  comparison_v1.2.3.md]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ajitpratap0/openclaw-cortex/eval/runner"
)

// baselineSystem is one row from published.json "systems" array.
// Pointer fields are used so null (unknown) values can be distinguished
// from 0.0 (genuinely zero score).
type baselineSystem struct {
	Name          string   `json:"name"`
	LoCoMoEM      *float64 `json:"locomo_em"`
	LongMemEvalEM *float64 `json:"longmemeval_em"`
	DMREM         *float64 `json:"dmr_em"`
}

// baselines is the top-level published.json structure.
type baselines struct {
	Systems []baselineSystem  `json:"systems"`
	Sources map[string]string `json:"sources"`
}

// comparisonRow is one row in the machine-readable comparison output.
type comparisonRow struct {
	System        string   `json:"system"`
	LoCoMoEM      *float64 `json:"locomo_em"`
	LongMemEvalEM *float64 `json:"longmemeval_em"`
	DMREM         *float64 `json:"dmr_em"`
}

// comparisonOutput is the machine-readable summary written to comparison.json.
type comparisonOutput struct {
	Rows    []comparisonRow   `json:"rows"`
	Sources map[string]string `json:"sources,omitempty"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "compare: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	baselinePath := flag.String("baseline", "", "Path to published baselines JSON (required)")
	currentPath := flag.String("current", "", "Path to current BenchmarkSummary JSON result file (required)")
	outputPath := flag.String("output", "", "Path to write markdown output (default: stdout)")
	flag.Parse()

	if *baselinePath == "" {
		return fmt.Errorf("--baseline is required")
	}
	if *currentPath == "" {
		return fmt.Errorf("--current is required")
	}

	// Load baselines.
	bl, err := loadBaselines(*baselinePath)
	if err != nil {
		return fmt.Errorf("loading baselines: %w", err)
	}

	// Load current benchmark results. The eval binary writes a JSON array of
	// BenchmarkSummary values (one per benchmark run in the file).
	summaries, err := loadSummaries(*currentPath)
	if err != nil {
		return fmt.Errorf("loading current results: %w", err)
	}

	// Index current results by benchmark name (case-insensitive).
	currentByName := make(map[string]*runner.BenchmarkSummary, len(summaries))
	for i := range summaries {
		currentByName[strings.ToLower(summaries[i].Name)] = summaries[i]
	}

	// Build the OpenClaw Cortex row from current results.
	cortexRow := comparisonRow{System: "OpenClaw Cortex (this run)"}
	if s, ok := currentByName["locomo"]; ok {
		v := s.ExactMatchAcc
		cortexRow.LoCoMoEM = &v
	}
	if s, ok := currentByName["longmemeval"]; ok {
		v := s.ExactMatchAcc
		cortexRow.LongMemEvalEM = &v
	}
	if s, ok := currentByName["dmr"]; ok {
		v := s.ExactMatchAcc
		cortexRow.DMREM = &v
	}

	// Build full rows list: published baselines first, then our result.
	rows := make([]comparisonRow, 0, len(bl.Systems)+1)
	for _, sys := range bl.Systems {
		rows = append(rows, comparisonRow{
			System:        sys.Name,
			LoCoMoEM:      sys.LoCoMoEM,
			LongMemEvalEM: sys.LongMemEvalEM,
			DMREM:         sys.DMREM,
		})
	}
	rows = append(rows, cortexRow)

	// Render GitHub-flavored markdown table.
	md := renderMarkdownTable(rows, bl.Sources)

	// Write markdown to file or stdout.
	if *outputPath != "" {
		if writeErr := os.WriteFile(*outputPath, []byte(md), 0o644); writeErr != nil {
			return fmt.Errorf("writing markdown output: %w", writeErr)
		}
		fmt.Fprintf(os.Stderr, "Markdown table written to %s\n", *outputPath)
	} else {
		fmt.Print(md)
	}

	// Write machine-readable JSON alongside the markdown (or next to the input).
	out := comparisonOutput{Rows: rows, Sources: bl.Sources}
	enc, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding comparison JSON: %w", err)
	}

	jsonPath := "comparison.json"
	if *outputPath != "" {
		// Place comparison.json next to the markdown file.
		dir := filepath.Dir(*outputPath)
		jsonPath = filepath.Join(dir, "comparison.json")
	}
	if writeErr := os.WriteFile(jsonPath, enc, 0o644); writeErr != nil {
		return fmt.Errorf("writing comparison JSON: %w", writeErr)
	}
	fmt.Fprintf(os.Stderr, "Machine-readable summary written to %s\n", jsonPath)

	return nil
}

// loadBaselines reads and parses published.json.
func loadBaselines(path string) (*baselines, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is a CLI argument, not user-supplied in a web context
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var bl baselines
	if err := json.Unmarshal(data, &bl); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &bl, nil
}

// loadSummaries reads a JSON file produced by eval/cmd/eval (array of
// BenchmarkSummary). Also accepts a single BenchmarkSummary object for
// convenience (wraps it in a slice).
func loadSummaries(path string) ([]*runner.BenchmarkSummary, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is a CLI argument, not user-supplied in a web context
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	trimmed := strings.TrimSpace(string(data))
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("%s is empty", path)
	}

	// Try array first (normal eval output).
	if trimmed[0] == '[' {
		var summaries []*runner.BenchmarkSummary
		if err := json.Unmarshal(data, &summaries); err != nil {
			return nil, fmt.Errorf("parse array in %s: %w", path, err)
		}
		return summaries, nil
	}

	// Fall back to single object.
	var single runner.BenchmarkSummary
	if err := json.Unmarshal(data, &single); err != nil {
		return nil, fmt.Errorf("parse object in %s: %w", path, err)
	}
	return []*runner.BenchmarkSummary{&single}, nil
}

// fmtEM formats an exact-match accuracy pointer as a percentage string,
// or returns "—" when the value is nil (not measured).
func fmtEM(v *float64) string {
	if v == nil {
		return "—"
	}
	return fmt.Sprintf("%.1f%%", *v*100)
}

// renderMarkdownTable builds a GFM table comparing all rows.
func renderMarkdownTable(rows []comparisonRow, sources map[string]string) string {
	var sb strings.Builder

	sb.WriteString("## Benchmark Comparison\n\n")
	sb.WriteString("| System | LoCoMo EM | LongMemEval EM | DMR EM |\n")
	sb.WriteString("|--------|-----------|----------------|--------|\n")

	for _, r := range rows {
		fmt.Fprintf(&sb, "| %-30s | %-9s | %-14s | %-6s |\n",
			r.System,
			fmtEM(r.LoCoMoEM),
			fmtEM(r.LongMemEvalEM),
			fmtEM(r.DMREM),
		)
	}

	if len(sources) > 0 {
		sb.WriteString("\n### Sources\n\n")
		// Emit sources in a stable order: locomo, longmemeval, dmr, then others.
		ordered := []string{"locomo", "longmemeval", "dmr"}
		emitted := make(map[string]bool)
		for _, k := range ordered {
			if v, ok := sources[k]; ok {
				fmt.Fprintf(&sb, "- **%s**: %s\n", k, v)
				emitted[k] = true
			}
		}
		for k, v := range sources {
			if !emitted[k] {
				fmt.Fprintf(&sb, "- **%s**: %s\n", k, v)
			}
		}
	}

	return sb.String()
}
