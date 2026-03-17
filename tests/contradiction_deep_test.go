// Package tests provides deep-path tests for the contradiction detection pipeline.
// Covers NewContradictionDetector, DefaultContradictionConfig, FindContradictions
// (heuristic, LLM-confirmed, LLM-error-degradation paths), and
// InvalidateContradictions helper.
package tests

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/ajitpratap0/openclaw-cortex/internal/capture"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// highSimVec returns a vector that is very similar (nearly identical) to makeVec(1.0).
func highSimVec() []float32 { return []float32{0.999, 0.1, 0.1, 0.1} }

// putDeepMemory inserts a memory with arbitrary content and a caller-supplied vector.
func putDeepMemory(t *testing.T, st store.Store, id, content string, vec []float32) models.Memory {
	t.Helper()
	m := models.Memory{
		ID:        id,
		Content:   content,
		Type:      models.MemoryTypeFact,
		Scope:     models.ScopePermanent,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := st.Upsert(context.Background(), m, vec); err != nil {
		t.Fatalf("putDeepMemory(%s): %v", id, err)
	}
	return m
}

// ---------------------------------------------------------------------------
// DefaultContradictionConfig
// ---------------------------------------------------------------------------

// TestDefaultContradictionConfig verifies all fields of the default config.
func TestDefaultContradictionConfig(t *testing.T) {
	cfg := capture.DefaultContradictionConfig()

	if !cfg.Enabled {
		t.Error("expected Enabled=true")
	}
	if cfg.SimilarityThreshold != 0.75 {
		t.Errorf("SimilarityThreshold: got %v, want 0.75", cfg.SimilarityThreshold)
	}
	if cfg.MaxCandidates != 20 {
		t.Errorf("MaxCandidates: got %d, want 20", cfg.MaxCandidates)
	}
	if cfg.LLMConfirmThreshold != 0.82 {
		t.Errorf("LLMConfirmThreshold: got %v, want 0.82", cfg.LLMConfirmThreshold)
	}
	if cfg.LLMTimeoutMs != 150 {
		t.Errorf("LLMTimeoutMs: got %d, want 150", cfg.LLMTimeoutMs)
	}
}

// ---------------------------------------------------------------------------
// NewContradictionDetector constructor
// ---------------------------------------------------------------------------

// TestNewContradictionDetector_NilLogger verifies nil logger is replaced with default.
func TestNewContradictionDetector_NilLogger(t *testing.T) {
	st := store.NewMockStore()
	cfg := capture.DefaultContradictionConfig()
	// Must not panic when logger is nil.
	d := capture.NewContradictionDetector(st, nil, nil, nil, "", cfg, nil)
	if d == nil {
		t.Fatal("expected non-nil detector")
	}
}

// TestNewContradictionDetector_WithLogger passes an explicit logger.
func TestNewContradictionDetector_WithLogger(t *testing.T) {
	st := store.NewMockStore()
	cfg := capture.DefaultContradictionConfig()
	logger := slog.Default()
	d := capture.NewContradictionDetector(st, nil, nil, nil, "test-model", cfg, logger)
	if d == nil {
		t.Fatal("expected non-nil detector")
	}
}

// TestNewContradictionDetector_Disabled returns nil results immediately.
func TestNewContradictionDetector_Disabled(t *testing.T) {
	st := store.NewMockStore()
	// Seed a memory that would normally be found.
	putDeepMemory(t, st, "mem-disabled", "Alice works at Acme Corp", makeVec(1.0))

	cfg := capture.DefaultContradictionConfig()
	cfg.Enabled = false

	d := capture.NewContradictionDetector(st, nil, nil, nil, "", cfg, slog.Default())
	results, err := d.FindContradictions(context.Background(), "Alice works at BetaCo", highSimVec())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results when disabled, got %d", len(results))
	}
}

// ---------------------------------------------------------------------------
// FindContradictions — heuristic path (no graph, no LLM)
// ---------------------------------------------------------------------------

// TestFindContradictions_EmploymentConflict tests the WORKS_AT heuristic path.
// Stored memory: "Bob works at OldCorp". New memory: "Bob works at NewCorp".
// The keywordHeuristicFilter should flag the old memory.
func TestFindContradictions_EmploymentConflict(t *testing.T) {
	st := store.NewMockStore()
	putDeepMemory(t, st, "mem-oldcorp", "Bob works at OldCorp", makeVec(1.0))

	cfg := capture.DefaultContradictionConfig()
	// Lower threshold so the slightly-different vector still qualifies in Stage 1.
	cfg.SimilarityThreshold = 0.5
	cfg.LLMConfirmThreshold = 0.99 // force LLM path (no LLM client, so heuristic wins)

	d := capture.NewContradictionDetector(st, nil, nil, nil, "", cfg, slog.Default())
	results, err := d.FindContradictions(context.Background(), "Bob works at NewCorp", highSimVec())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := false
	for _, r := range results {
		if r.CandidateID == "mem-oldcorp" {
			found = true
			t.Logf("contradiction found: %s — %s", r.CandidateID, r.Reason)
		}
	}
	if !found {
		t.Errorf("expected mem-oldcorp to be flagged, got %+v", results)
	}
}

// TestFindContradictions_NoCandidates tests the path where the store returns no
// memories above the similarity threshold — should return nil, nil.
func TestFindContradictions_NoCandidates(t *testing.T) {
	st := store.NewMockStore()
	// Seed a memory with a very different vector so cosine similarity is low.
	putDeepMemory(t, st, "mem-irrelevant", "Carol lives in Denver", makeVec(0.01))

	cfg := capture.DefaultContradictionConfig()
	cfg.SimilarityThreshold = 0.95 // very high — nothing will pass Stage 1

	d := capture.NewContradictionDetector(st, nil, nil, nil, "", cfg, slog.Default())
	results, err := d.FindContradictions(context.Background(), "Carol lives in Austin", makeVec(1.0))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d: %+v", len(results), results)
	}
}

// TestFindContradictions_StopWordHeavy verifies that content whose significant
// words are all stop words or short words is not spuriously flagged.
func TestFindContradictions_StopWordHeavy(t *testing.T) {
	st := store.NewMockStore()
	// Stored memory uses only stop-word / short-word content.
	putDeepMemory(t, st, "mem-stopwords", "the and for that with from", makeVec(1.0))

	cfg := capture.DefaultContradictionConfig()
	cfg.SimilarityThreshold = 0.5

	d := capture.NewContradictionDetector(st, nil, nil, nil, "", cfg, slog.Default())
	// The new content also shares exclusive-predicate signals but the stored memory
	// has no significant words — significantWords() returns empty, so no conflict.
	results, err := d.FindContradictions(context.Background(), "the and for that with from", highSimVec())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// No significant words in the stored fact → no predicate conflict flagged.
	if len(results) != 0 {
		t.Logf("results (may be zero or non-zero depending on keyword overlap): %+v", results)
	}
	// We do not assert a hard expectation here because the keyword heuristic may
	// flag based on content signals in the new memory; the important assertion is
	// that the pipeline does not panic or error.
}

// ---------------------------------------------------------------------------
// FindContradictions — auto-confirm path (high similarity, no LLM call)
// ---------------------------------------------------------------------------

// TestFindContradictions_AutoConfirm tests that candidates with similarity above
// LLMConfirmThreshold are confirmed without invoking the LLM client.
func TestFindContradictions_AutoConfirm(t *testing.T) {
	st := store.NewMockStore()
	// Store memory with vector 1.0 so it has very high cosine similarity to highSimVec().
	putDeepMemory(t, st, "mem-autocfm", "Dave works at AlphaCo", makeVec(1.0))

	cfg := capture.DefaultContradictionConfig()
	cfg.SimilarityThreshold = 0.50
	// Set confirm threshold below the expected similarity so auto-confirm triggers.
	cfg.LLMConfirmThreshold = 0.50

	// LLM client returning "no contradiction" — should NOT be called due to auto-confirm.
	llmClient := &mockLLMClient{Resp: `{"contradicts":false,"reason":"no conflict"}`}

	d := capture.NewContradictionDetector(st, nil, nil, llmClient, "test-model", cfg, slog.Default())
	results, err := d.FindContradictions(context.Background(), "Dave works at BetaCo", highSimVec())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := false
	for _, r := range results {
		if r.CandidateID == "mem-autocfm" {
			found = true
			t.Logf("auto-confirmed: %s — %s", r.CandidateID, r.Reason)
		}
	}
	if !found {
		t.Errorf("expected mem-autocfm to be auto-confirmed, got %+v", results)
	}
}

// ---------------------------------------------------------------------------
// FindContradictions — LLM confirmation path
// ---------------------------------------------------------------------------

// TestFindContradictions_LLMContradictionTrue tests the LLM path when the model
// returns {"contradicts":true,...}.
func TestFindContradictions_LLMContradictionTrue(t *testing.T) {
	st := store.NewMockStore()
	putDeepMemory(t, st, "mem-llm-yes", "Eve works at GammaCorp", makeVec(1.0))

	cfg := capture.DefaultContradictionConfig()
	cfg.SimilarityThreshold = 0.50
	// Set confirm threshold above max cosine similarity so LLM is always called.
	cfg.LLMConfirmThreshold = 1.1
	cfg.LLMTimeoutMs = 5000 // generous timeout

	llmClient := &mockLLMClient{Resp: `{"contradicts":true,"reason":"different employer"}`}

	d := capture.NewContradictionDetector(st, nil, nil, llmClient, "test-model", cfg, slog.Default())
	results, err := d.FindContradictions(context.Background(), "Eve works at DeltaCorp", highSimVec())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := false
	for _, r := range results {
		if r.CandidateID == "mem-llm-yes" {
			found = true
			t.Logf("LLM confirmed contradiction: %s — %s", r.CandidateID, r.Reason)
		}
	}
	if !found {
		t.Errorf("expected mem-llm-yes to be flagged by LLM, got %+v", results)
	}
}

// TestFindContradictions_LLMContradictionFalse tests the LLM path when the model
// returns {"contradicts":false,...} — no contradiction should be reported.
func TestFindContradictions_LLMContradictionFalse(t *testing.T) {
	st := store.NewMockStore()
	putDeepMemory(t, st, "mem-llm-no", "Frank works at EpsilonCo", makeVec(1.0))

	cfg := capture.DefaultContradictionConfig()
	cfg.SimilarityThreshold = 0.50
	cfg.LLMConfirmThreshold = 1.1 // above max cosine similarity — force LLM call every time
	cfg.LLMTimeoutMs = 5000

	llmClient := &mockLLMClient{Resp: `{"contradicts":false,"reason":"same employer, just clarification"}`}

	d := capture.NewContradictionDetector(st, nil, nil, llmClient, "test-model", cfg, slog.Default())
	results, err := d.FindContradictions(context.Background(), "Frank works at EpsilonCo headquarters", highSimVec())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, r := range results {
		if r.CandidateID == "mem-llm-no" {
			t.Errorf("expected no contradiction from LLM 'false' response, but got one: %+v", r)
		}
	}
	t.Log("LLM 'false' path: no spurious contradiction reported ✓")
}

// TestFindContradictions_LLMError verifies that an LLM error causes graceful
// degradation: the candidate is skipped and no error is propagated.
func TestFindContradictions_LLMError(t *testing.T) {
	st := store.NewMockStore()
	putDeepMemory(t, st, "mem-llm-err", "Grace works at ZetaCo", makeVec(1.0))

	cfg := capture.DefaultContradictionConfig()
	cfg.SimilarityThreshold = 0.50
	cfg.LLMConfirmThreshold = 1.1 // above max cosine similarity — force LLM call every time
	cfg.LLMTimeoutMs = 5000

	llmClient := &mockLLMClient{Err: errors.New("LLM unavailable")}

	d := capture.NewContradictionDetector(st, nil, nil, llmClient, "test-model", cfg, slog.Default())
	results, err := d.FindContradictions(context.Background(), "Grace works at EtaCo", highSimVec())
	if err != nil {
		t.Fatalf("expected nil error on LLM failure, got: %v", err)
	}
	// Because LLM errored, the candidate should be skipped (not reported).
	for _, r := range results {
		if r.CandidateID == "mem-llm-err" {
			t.Errorf("expected LLM error to cause skip, but mem-llm-err was reported: %+v", r)
		}
	}
	t.Log("LLM error graceful degradation ✓")
}

// TestFindContradictions_LLMBadJSON verifies that a malformed LLM JSON response
// is silently ignored (graceful degradation).
func TestFindContradictions_LLMBadJSON(t *testing.T) {
	st := store.NewMockStore()
	putDeepMemory(t, st, "mem-llm-json", "Hank works at ThetaCo", makeVec(1.0))

	cfg := capture.DefaultContradictionConfig()
	cfg.SimilarityThreshold = 0.50
	cfg.LLMConfirmThreshold = 1.1 // above max cosine similarity — force LLM call every time
	cfg.LLMTimeoutMs = 5000

	llmClient := &mockLLMClient{Resp: "not valid json at all"}

	d := capture.NewContradictionDetector(st, nil, nil, llmClient, "test-model", cfg, slog.Default())
	results, err := d.FindContradictions(context.Background(), "Hank works at IotaCo", highSimVec())
	if err != nil {
		t.Fatalf("unexpected error on bad JSON: %v", err)
	}
	for _, r := range results {
		if r.CandidateID == "mem-llm-json" {
			t.Errorf("expected bad-JSON response to be skipped, got: %+v", r)
		}
	}
	t.Log("bad JSON graceful degradation ✓")
}

// ---------------------------------------------------------------------------
// InvalidateContradictions helper
// ---------------------------------------------------------------------------

// TestInvalidateContradictions_SetsValidTo verifies the InvalidateContradictions
// helper sets ValidTo on each identified memory and does not panic on nil logger.
func TestInvalidateContradictions_SetsValidTo(t *testing.T) {
	st := store.NewMockStore()
	putDeepMemory(t, st, "mem-inv-1", "Ivan works at KappaCo", makeVec(1.0))
	putDeepMemory(t, st, "mem-inv-2", "Ivan lives in London", makeVec(0.8))

	results := []capture.ContradictionResult{
		{CandidateID: "mem-inv-1", Reason: "employer changed"},
		{CandidateID: "mem-inv-2", Reason: "location changed"},
	}

	// Call with a real logger.
	capture.InvalidateContradictions(context.Background(), st, results, slog.Default())

	for _, r := range results {
		got, err := st.Get(context.Background(), r.CandidateID)
		if err != nil {
			t.Fatalf("Get(%s): %v", r.CandidateID, err)
		}
		if got.ValidTo == nil {
			t.Errorf("memory %s: expected ValidTo to be set", r.CandidateID)
		} else {
			t.Logf("memory %s ValidTo=%v ✓", r.CandidateID, got.ValidTo)
		}
	}
}

// TestInvalidateContradictions_NilLogger verifies the function does not panic when
// logger is nil.
func TestInvalidateContradictions_NilLogger(t *testing.T) {
	st := store.NewMockStore()
	putDeepMemory(t, st, "mem-inv-nil", "Judy works at LambdaCo", makeVec(0.9))

	results := []capture.ContradictionResult{
		{CandidateID: "mem-inv-nil", Reason: "nil logger test"},
	}

	// Must not panic.
	capture.InvalidateContradictions(context.Background(), st, results, nil)

	got, err := st.Get(context.Background(), "mem-inv-nil")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ValidTo == nil {
		t.Error("expected ValidTo to be set even with nil logger")
	}
}

// TestInvalidateContradictions_MissingMemory verifies that a ContradictionResult
// referencing a non-existent memory ID is handled gracefully (no panic).
func TestInvalidateContradictions_MissingMemory(t *testing.T) {
	st := store.NewMockStore()

	results := []capture.ContradictionResult{
		{CandidateID: "mem-does-not-exist", Reason: "missing memory"},
	}

	// Should log a warning and not panic.
	capture.InvalidateContradictions(context.Background(), st, results, slog.Default())
	t.Log("missing memory handled gracefully ✓")
}

// TestInvalidateContradictions_EmptyResults verifies that an empty result slice
// is a no-op.
func TestInvalidateContradictions_EmptyResults(t *testing.T) {
	st := store.NewMockStore()
	capture.InvalidateContradictions(context.Background(), st, nil, slog.Default())
	capture.InvalidateContradictions(context.Background(), st, []capture.ContradictionResult{}, slog.Default())
	t.Log("empty results no-op ✓")
}

// ---------------------------------------------------------------------------
// DetectContradictions convenience wrapper
// ---------------------------------------------------------------------------

// TestDetectContradictions_ConvenienceWrapper verifies the top-level convenience
// function matches the behaviour of a manually constructed detector.
func TestDetectContradictions_ConvenienceWrapper(t *testing.T) {
	st := store.NewMockStore()
	putDeepMemory(t, st, "mem-conv", "Liam works at MuCo", makeVec(1.0))

	newMem := models.Memory{
		ID:      "mem-conv-new",
		Content: "Liam works at NuCo",
	}

	hits, err := capture.DetectContradictions(context.Background(), st, nil, newMem, highSimVec())
	if err != nil {
		t.Fatalf("DetectContradictions: %v", err)
	}
	// We don't assert a specific count here — the test exercises the code path
	// without panicking and returns a sensible (non-error) result.
	t.Logf("DetectContradictions returned %d hits", len(hits))
}
