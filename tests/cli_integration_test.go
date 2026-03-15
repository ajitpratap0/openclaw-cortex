package tests

// cli_integration_test.go exercises the compiled openclaw-cortex binary to
// verify that the search and recall commands expose the expected flags and
// produce correctly-shaped JSON output.
//
// Tests that only need the binary (help text) are safe to run in short mode.
// Tests that need live Qdrant / Ollama are skipped when testing.Short() is true
// or when the binary is not present.

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cliBinPath is relative to the tests/ package directory, which is the working
// directory used by `go test`.  The binary lives one level up at bin/.
const cliBinPath = "../bin/openclaw-cortex"

// binExists returns true when the compiled binary is present.
func binExists() bool {
	_, statErr := os.Stat(cliBinPath)
	return !os.IsNotExist(statErr)
}

// runCLI executes the binary with the supplied arguments and returns combined
// stdout+stderr output together with the exit error (nil on exit 0).
func runCLI(args ...string) (string, error) {
	cmd := exec.Command(cliBinPath, args...)
	out, runErr := cmd.CombinedOutput()
	return string(out), runErr
}

// ── help-text tests (safe in short mode) ──────────────────────────────────────

// TestCLI_Search_Help_ScopeFlag verifies that `search --help` documents the
// --scope flag.  This catches the regression where a plugin passes --scope but
// the binary doesn't declare it.
func TestCLI_Search_Help_ScopeFlag(t *testing.T) {
	if !binExists() {
		t.Skip("binary not built; run: go build -o bin/openclaw-cortex ./cmd/openclaw-cortex from the repo root")
	}

	out, err := runCLI("search", "--help")
	// --help exits with code 0; tolerate non-zero only to read output.
	_ = err

	assert.Contains(t, out, "--scope", "search --help must document the --scope flag")
}

// TestCLI_Search_Help_JSONFlag verifies that `search --help` documents the
// --json flag.
func TestCLI_Search_Help_JSONFlag(t *testing.T) {
	if !binExists() {
		t.Skip("binary not built; run: go build -o bin/openclaw-cortex ./cmd/openclaw-cortex from the repo root")
	}

	out, _ := runCLI("search", "--help")
	assert.Contains(t, out, "--json", "search --help must document the --json flag")
}

// TestCLI_Recall_Help_ContextFlag verifies that `recall --help` documents the
// --context flag.
func TestCLI_Recall_Help_ContextFlag(t *testing.T) {
	if !binExists() {
		t.Skip("binary not built; run: go build -o bin/openclaw-cortex ./cmd/openclaw-cortex from the repo root")
	}

	out, _ := runCLI("recall", "--help")
	assert.Contains(t, out, "--context", "recall --help must document the --context flag")
}

// TestCLI_Search_UnknownFlag_ReturnsError verifies that passing a genuinely
// unknown flag causes a non-zero exit and an error message.
func TestCLI_Search_UnknownFlag_ReturnsError(t *testing.T) {
	if !binExists() {
		t.Skip("binary not built; run: go build -o bin/openclaw-cortex ./cmd/openclaw-cortex from the repo root")
	}

	out, err := runCLI("search", "test-query", "--nonexistent")
	require.Error(t, err, "search with unknown flag should exit non-zero")
	assert.True(t,
		strings.Contains(out, "unknown flag") || strings.Contains(out, "unknown"),
		"output should mention unknown flag, got: %s", out,
	)
}

// ── live-service tests (skipped in short mode) ────────────────────────────────

// TestCLI_Search_JSON_ValidOutput verifies that `search --json` produces a
// valid JSON array when Qdrant is available.
func TestCLI_Search_JSON_ValidOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if !binExists() {
		t.Skip("binary not built")
	}

	out, err := runCLI("search", "test query", "--json")
	if err != nil {
		// Qdrant or Ollama unavailable — skip gracefully.
		t.Skipf("search command failed (live services unavailable): %v\noutput: %s", err, out)
	}

	var results []json.RawMessage
	jsonErr := json.Unmarshal([]byte(strings.TrimSpace(out)), &results)
	require.NoError(t, jsonErr, "search --json output must be a valid JSON array; got: %s", out)
}

// TestCLI_Search_ScopeFlag_Accepted verifies that --scope does not cause an
// "unknown flag" error (the core regression this flag check catches).
func TestCLI_Search_ScopeFlag_Accepted(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if !binExists() {
		t.Skip("binary not built")
	}

	out, err := runCLI("search", "test", "--scope", "permanent", "--json")
	if err != nil {
		// A connection error (Qdrant/Ollama down) is acceptable; an "unknown flag"
		// error is not.
		require.False(t,
			strings.Contains(out, "unknown flag"),
			"--scope must be a recognized flag; binary output: %s", out,
		)
		t.Skipf("search command failed (live services unavailable): %v", err)
	}
}

// TestCLI_Recall_JSON_ValidOutput verifies that `recall --context json`
// produces a valid JSON array when Qdrant and Ollama are available.
func TestCLI_Recall_JSON_ValidOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if !binExists() {
		t.Skip("binary not built")
	}

	out, err := runCLI("recall", "test query", "--context", "json")
	if err != nil {
		t.Skipf("recall command failed (live services unavailable): %v\noutput: %s", err, out)
	}

	var results []json.RawMessage
	jsonErr := json.Unmarshal([]byte(strings.TrimSpace(out)), &results)
	require.NoError(t, jsonErr, "recall --context json output must be a valid JSON array; got: %s", out)
}

// TestCLI_Search_JSON_OutputShape verifies that each element in the search
// JSON output has the expected SearchResult shape: { memory, score }.
func TestCLI_Search_JSON_OutputShape(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if !binExists() {
		t.Skip("binary not built")
	}

	out, err := runCLI("search", "test query", "--json")
	if err != nil {
		t.Skipf("search command failed (live services unavailable): %v\noutput: %s", err, out)
	}

	type searchResult struct {
		Memory json.RawMessage `json:"memory"`
		Score  *float64        `json:"score"`
	}

	var results []searchResult
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &results),
		"output must be a JSON array of SearchResult; got: %s", out)

	for i := range results {
		assert.NotNil(t, results[i].Score,
			"SearchResult[%d] must have a 'score' field", i)
		assert.NotEmpty(t, results[i].Memory,
			"SearchResult[%d] must have a 'memory' field", i)
	}
}

// TestCLI_Recall_JSON_OutputShape verifies that each element in the recall
// JSON output has the RecallResult shape: { memory, final_score } (NOT score).
func TestCLI_Recall_JSON_OutputShape(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if !binExists() {
		t.Skip("binary not built")
	}

	out, err := runCLI("recall", "test query", "--context", "json")
	if err != nil {
		t.Skipf("recall command failed (live services unavailable): %v\noutput: %s", err, out)
	}

	type recallResult struct {
		Memory     json.RawMessage `json:"memory"`
		FinalScore *float64        `json:"final_score"`
	}

	var results []recallResult
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &results),
		"output must be a JSON array of RecallResult; got: %s", out)

	for i := range results {
		assert.NotNil(t, results[i].FinalScore,
			"RecallResult[%d] must have a 'final_score' field (not 'score')", i)
		assert.NotEmpty(t, results[i].Memory,
			"RecallResult[%d] must have a 'memory' field", i)
	}
}
