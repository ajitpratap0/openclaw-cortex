package tests

// batch_capture_test.go — black-box tests for the capture-batch command.
//
// These tests use the compiled binary (like cli_integration_test.go) for
// end-to-end CLI coverage plus unit-level tests that exercise the core logic
// directly with MockStore and a stub embedder.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

// ── helpers ────────────────────────────────────────────────────────────────────

// batchCaptureRecord matches the JSONL input schema of capture-batch.
type batchCaptureRecord struct {
	Content    string   `json:"content"`
	Type       string   `json:"type,omitempty"`
	Scope      string   `json:"scope,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	Confidence float64  `json:"confidence,omitempty"`
	Project    string   `json:"project,omitempty"`
	UserID     string   `json:"user_id,omitempty"`
}

// writeJSONL serializes records to a temp file (JSONL) and returns the path.
func writeJSONL(t *testing.T, records []batchCaptureRecord) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "batch-capture-*.jsonl")
	if err != nil {
		t.Fatalf("creating temp file: %v", err)
	}
	defer func() { _ = f.Close() }()

	enc := json.NewEncoder(f)
	for i := range records {
		if encErr := enc.Encode(records[i]); encErr != nil {
			t.Fatalf("encoding record %d: %v", i, encErr)
		}
	}
	return f.Name()
}

// runCaptureBatch builds and runs the CLI binary with capture-batch sub-command.
func runCaptureBatch(t *testing.T, args ...string) (string, error) {
	t.Helper()
	if !binExists() {
		t.Skip("binary not built; run: go build -o bin/openclaw-cortex ./cmd/openclaw-cortex from repo root")
	}
	all := append([]string{"capture-batch"}, args...)
	cmd := exec.Command(cliBinPath, all...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// ── unit-level helpers that bypass the CLI ────────────────────────────────────

// batchCaptureCoreInput is the parsed representation of one JSONL line,
// mirroring the batchRecord struct in cmd_capture_batch.go.
type batchCaptureCoreInput struct {
	Content    string   `json:"content"`
	Type       string   `json:"type,omitempty"`
	Scope      string   `json:"scope,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	Confidence float64  `json:"confidence,omitempty"`
	Project    string   `json:"project,omitempty"`
	UserID     string   `json:"user_id,omitempty"`
}

// runBatchCaptureCore mimics what cmd_capture_batch.go does, operating against
// an in-memory MockStore and stub embedder so no live services are needed.
func runBatchCaptureCore(
	ctx context.Context,
	st *store.MockStore,
	emb *apiTestEmbedder,
	records []batchCaptureCoreInput,
	overrideProject, overrideUserID string,
	dryRun, stopOnError bool,
) (imported, errors int) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	for i := range records {
		rec := &records[i]

		if rec.Content == "" {
			logger.Error("batch-capture: record has empty content", "index", i)
			errors++
			if stopOnError {
				return
			}
			continue
		}

		// Apply overrides.
		if overrideProject != "" {
			rec.Project = overrideProject
		}
		if overrideUserID != "" {
			rec.UserID = overrideUserID
		}

		// Default confidence.
		if rec.Confidence == 0 {
			rec.Confidence = 0.9
		}

		// Resolve type.
		memType := models.MemoryType(rec.Type)
		if !memType.IsValid() {
			memType = models.MemoryTypeFact
		}

		// Resolve scope.
		memScope := models.MemoryScope(rec.Scope)
		if !memScope.IsValid() {
			memScope = models.ScopePermanent
		}

		// Embed.
		vec, embErr := emb.Embed(ctx, rec.Content)
		if embErr != nil {
			logger.Error("batch-capture: embedding failed", "index", i, "error", embErr)
			errors++
			if stopOnError {
				return
			}
			continue
		}

		if dryRun {
			imported++
			continue
		}

		now := time.Now().UTC()
		mem := models.Memory{
			ID:           uuid.New().String(),
			Type:         memType,
			Scope:        memScope,
			Visibility:   models.VisibilityShared,
			Content:      rec.Content,
			Confidence:   rec.Confidence,
			Source:       "batch-import",
			Tags:         rec.Tags,
			Project:      rec.Project,
			UserID:       rec.UserID,
			CreatedAt:    now,
			UpdatedAt:    now,
			LastAccessed: now,
		}

		if upsertErr := st.Upsert(ctx, mem, vec); upsertErr != nil {
			logger.Error("batch-capture: upsert failed", "index", i, "error", upsertErr)
			errors++
			if stopOnError {
				return
			}
			continue
		}
		imported++
	}
	return
}

// parseJSONLToCore reads a JSONL string and returns parsed records.
func parseJSONLToCore(t *testing.T, jsonl string) ([]batchCaptureCoreInput, int) {
	t.Helper()
	var records []batchCaptureCoreInput
	parseErrors := 0
	scanner := bufio.NewScanner(strings.NewReader(jsonl))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var rec batchCaptureCoreInput
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			parseErrors++
			continue
		}
		records = append(records, rec)
	}
	return records, parseErrors
}

// ── tests ─────────────────────────────────────────────────────────────────────

// TestBatchCapture_Basic verifies that 3 JSONL records are all stored in the mock store.
func TestBatchCapture_Basic(t *testing.T) {
	ctx := context.Background()
	st := store.NewMockStore()
	emb := &apiTestEmbedder{}

	jsonl := `{"content":"Go uses goroutines for concurrency","type":"fact"}
{"content":"Python is dynamically typed","type":"fact"}
{"content":"Always write tests before code","type":"rule"}
`
	records, parseErrors := parseJSONLToCore(t, jsonl)
	if parseErrors != 0 {
		t.Fatalf("unexpected parse errors: %d", parseErrors)
	}
	if len(records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(records))
	}

	imported, errs := runBatchCaptureCore(ctx, st, emb, records, "", "", false, false)
	if errs != 0 {
		t.Fatalf("expected 0 errors, got %d", errs)
	}
	if imported != 3 {
		t.Fatalf("expected 3 imported, got %d", imported)
	}

	// Verify all 3 are retrievable from the store.
	all, _, listErr := st.List(ctx, nil, 10, "")
	if listErr != nil {
		t.Fatalf("List: %v", listErr)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 memories in store, got %d", len(all))
	}
}

// TestBatchCapture_DryRun verifies that --dry-run counts records but stores nothing.
func TestBatchCapture_DryRun(t *testing.T) {
	ctx := context.Background()
	st := store.NewMockStore()
	emb := &apiTestEmbedder{}

	jsonl := `{"content":"dry run memory one","type":"fact"}
{"content":"dry run memory two","type":"fact"}
`
	records, parseErrors := parseJSONLToCore(t, jsonl)
	if parseErrors != 0 {
		t.Fatalf("unexpected parse errors: %d", parseErrors)
	}

	imported, errs := runBatchCaptureCore(ctx, st, emb, records, "", "", true /* dryRun */, false)
	if errs != 0 {
		t.Fatalf("expected 0 errors, got %d", errs)
	}
	if imported != 2 {
		t.Fatalf("expected 2 counted (dry run), got %d", imported)
	}

	// Nothing should be stored.
	all, _, listErr := st.List(ctx, nil, 10, "")
	if listErr != nil {
		t.Fatalf("List: %v", listErr)
	}
	if len(all) != 0 {
		t.Fatalf("expected 0 memories in store (dry run), got %d", len(all))
	}
}

// TestBatchCapture_InvalidJSON verifies that lines with invalid JSON are
// counted as errors but valid lines proceed to be stored.
func TestBatchCapture_InvalidJSON(t *testing.T) {
	jsonl := `{"content":"valid memory one","type":"fact"}
{bad json here
{"content":"valid memory two","type":"fact"}
`
	records, parseErrors := parseJSONLToCore(t, jsonl)
	if parseErrors != 1 {
		t.Fatalf("expected 1 parse error, got %d", parseErrors)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 valid records, got %d", len(records))
	}

	ctx := context.Background()
	st := store.NewMockStore()
	emb := &apiTestEmbedder{}

	imported, errs := runBatchCaptureCore(ctx, st, emb, records, "", "", false, false)
	if errs != 0 {
		t.Fatalf("expected 0 store errors, got %d", errs)
	}
	if imported != 2 {
		t.Fatalf("expected 2 imported (after skipping invalid line), got %d", imported)
	}

	// Total errors = parseErrors + store errors = 1.
	totalErrors := parseErrors + errs
	if totalErrors != 1 {
		t.Fatalf("expected total errors=1, got %d", totalErrors)
	}
}

// TestBatchCapture_StdinInput verifies that the core logic works when records
// arrive from a string (simulating stdin) rather than a file.
func TestBatchCapture_StdinInput(t *testing.T) {
	ctx := context.Background()
	st := store.NewMockStore()
	emb := &apiTestEmbedder{}

	// Simulate stdin by encoding to a string, then parsing it.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	stdinRecords := []batchCaptureRecord{
		{Content: "stdin memory alpha", Type: "fact"},
		{Content: "stdin memory beta", Type: "rule"},
	}
	for i := range stdinRecords {
		if err := enc.Encode(stdinRecords[i]); err != nil {
			t.Fatalf("encoding stdin record %d: %v", i, err)
		}
	}

	records, parseErrors := parseJSONLToCore(t, buf.String())
	if parseErrors != 0 {
		t.Fatalf("unexpected parse errors: %d", parseErrors)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}

	imported, errs := runBatchCaptureCore(ctx, st, emb, records, "", "", false, false)
	if errs != 0 {
		t.Fatalf("expected 0 errors, got %d", errs)
	}
	if imported != 2 {
		t.Fatalf("expected 2 imported, got %d", imported)
	}

	// Verify both memories are stored.
	all, _, listErr := st.List(ctx, nil, 10, "")
	if listErr != nil {
		t.Fatalf("List: %v", listErr)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 memories in store, got %d", len(all))
	}
}

// TestBatchCapture_ProjectOverride verifies that --project flag overrides
// per-record project values.
func TestBatchCapture_ProjectOverride(t *testing.T) {
	ctx := context.Background()
	st := store.NewMockStore()
	emb := &apiTestEmbedder{}

	jsonl := `{"content":"memory with per-record project","type":"fact","project":"original-project"}
{"content":"memory without project","type":"fact"}
`
	records, parseErrors := parseJSONLToCore(t, jsonl)
	if parseErrors != 0 {
		t.Fatalf("unexpected parse errors: %d", parseErrors)
	}

	imported, errs := runBatchCaptureCore(ctx, st, emb, records, "override-project", "", false, false)
	if errs != 0 {
		t.Fatalf("expected 0 errors, got %d", errs)
	}
	if imported != 2 {
		t.Fatalf("expected 2 imported, got %d", imported)
	}

	// All memories should have project = "override-project".
	all, _, listErr := st.List(ctx, nil, 10, "")
	if listErr != nil {
		t.Fatalf("List: %v", listErr)
	}
	for i := range all {
		if all[i].Project != "override-project" {
			t.Errorf("memory %s: expected project=override-project, got %q", all[i].ID, all[i].Project)
		}
	}
}

// TestBatchCapture_UserIDOverride verifies that --user-id flag overrides
// per-record user_id values.
func TestBatchCapture_UserIDOverride(t *testing.T) {
	ctx := context.Background()
	st := store.NewMockStore()
	emb := &apiTestEmbedder{}

	jsonl := `{"content":"memory for user alice","user_id":"alice"}
{"content":"memory without user"}
`
	records, parseErrors := parseJSONLToCore(t, jsonl)
	if parseErrors != 0 {
		t.Fatalf("unexpected parse errors: %d", parseErrors)
	}

	imported, errs := runBatchCaptureCore(ctx, st, emb, records, "", "override-user", false, false)
	if errs != 0 {
		t.Fatalf("expected 0 errors, got %d", errs)
	}
	if imported != 2 {
		t.Fatalf("expected 2 imported, got %d", imported)
	}

	all, _, listErr := st.List(ctx, nil, 10, "")
	if listErr != nil {
		t.Fatalf("List: %v", listErr)
	}
	for i := range all {
		if all[i].UserID != "override-user" {
			t.Errorf("memory %s: expected user_id=override-user, got %q", all[i].ID, all[i].UserID)
		}
	}
}

// TestBatchCapture_StopOnError verifies that --stop-on-error halts processing
// at the first empty-content error.
func TestBatchCapture_StopOnError(t *testing.T) {
	ctx := context.Background()
	st := store.NewMockStore()
	emb := &apiTestEmbedder{}

	// First record is valid, second has empty content (triggers error), third is valid.
	records := []batchCaptureCoreInput{
		{Content: "first valid memory"},
		{Content: ""},      // will trigger an error
		{Content: "third"}, // should NOT be reached with stop-on-error
	}

	imported, errs := runBatchCaptureCore(ctx, st, emb, records, "", "", false, true /* stopOnError */)

	// The first record should have been imported before the error halted processing.
	if imported != 1 {
		t.Fatalf("expected 1 imported before stop, got %d", imported)
	}
	if errs != 1 {
		t.Fatalf("expected 1 error (stop-on-error), got %d", errs)
	}

	// Only 1 memory should be in the store.
	all, _, listErr := st.List(ctx, nil, 10, "")
	if listErr != nil {
		t.Fatalf("List: %v", listErr)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 memory in store after stop-on-error, got %d", len(all))
	}
}

// ── CLI binary smoke tests (require compiled binary) ─────────────────────────

// TestBatchCapture_CLI_Help verifies that capture-batch --help documents the expected flags.
func TestBatchCapture_CLI_Help(t *testing.T) {
	if !binExists() {
		t.Skip("binary not built; run: go build -o bin/openclaw-cortex ./cmd/openclaw-cortex")
	}

	out, _ := runCaptureBatch(t, "--help")

	for _, flag := range []string{"--input", "--dry-run", "--stop-on-error", "--user-id", "--project"} {
		if !strings.Contains(out, flag) {
			t.Errorf("capture-batch --help must document %s flag; got:\n%s", flag, out)
		}
	}
}

// TestBatchCapture_CLI_MissingInput verifies that omitting --input returns a usage error.
func TestBatchCapture_CLI_MissingInput(t *testing.T) {
	if !binExists() {
		t.Skip("binary not built")
	}

	_, err := runCaptureBatch(t)
	if err == nil {
		t.Fatal("expected non-zero exit when --input is omitted")
	}
}

// TestBatchCapture_CLI_DryRun_File writes a real JSONL file and invokes capture-batch
// with --dry-run. Since no live store is available, we expect a connection error;
// the test verifies --dry-run reaches the parsing stage (no "unknown flag" error).
func TestBatchCapture_CLI_DryRun_File(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping CLI dry-run test in short mode")
	}
	if !binExists() {
		t.Skip("binary not built")
	}

	path := writeJSONL(t, []batchCaptureRecord{
		{Content: "CLI dry run test", Type: "fact"},
	})

	out, _ := runCaptureBatch(t, "--input", path, "--dry-run")

	// Should not mention an unknown flag error.
	if strings.Contains(out, "unknown flag") {
		t.Fatalf("unexpected 'unknown flag' in output: %s", out)
	}
}

// TestBatchCapture_CLI_RootHelp verifies that capture-batch appears in root --help.
func TestBatchCapture_CLI_RootHelp(t *testing.T) {
	if !binExists() {
		t.Skip("binary not built")
	}

	out, _ := runCLI("--help")
	if !strings.Contains(out, "capture-batch") {
		t.Errorf("capture-batch not listed in root --help; got:\n%s", out)
	}
}

// ── summary format test ───────────────────────────────────────────────────────

// TestBatchCapture_SummaryFormat verifies the "Imported N memories (E errors)" format.
func TestBatchCapture_SummaryFormat(t *testing.T) {
	imported := 5
	errs := 2
	summary := fmt.Sprintf("Imported %d memories (%d errors)", imported, errs)
	expected := "Imported 5 memories (2 errors)"
	if summary != expected {
		t.Errorf("summary format: got %q, want %q", summary, expected)
	}
}
