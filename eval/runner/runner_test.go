package runner

import (
	"encoding/json"
	"testing"
)

// TestRecallJSONResultSchema asserts that recallJSONResult correctly parses
// the JSON shape emitted by `openclaw-cortex recall --context _`.
// This test makes the schema coupling between runner.go and cmd_recall.go
// explicit and catches regressions without requiring a live binary.
//
// If this test fails, update recallJSONResult and its doc comment to match
// the new schema emitted by cmd_recall.go.
func TestRecallJSONResultSchema(t *testing.T) {
	// Minimal JSON matching the shape cmd_recall.go produces:
	// []models.RecallResult serialised as an array of objects with a "memory"
	// key whose nested object has a "content" key.
	const input = `[{"memory":{"content":"the cat sat on the mat"}},{"memory":{"content":"Paris is the capital of France"}}]`

	var results []recallJSONResult
	if err := json.Unmarshal([]byte(input), &results); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if got := results[0].Memory.Content; got != "the cat sat on the mat" {
		t.Errorf("results[0].Memory.Content = %q, want %q", got, "the cat sat on the mat")
	}
	if got := results[1].Memory.Content; got != "Paris is the capital of France" {
		t.Errorf("results[1].Memory.Content = %q, want %q", got, "Paris is the capital of France")
	}
}

// TestRecallJSONResultEmptyArray asserts that an empty JSON array produces
// zero results without error — the "no memories found" case.
func TestRecallJSONResultEmptyArray(t *testing.T) {
	var results []recallJSONResult
	if err := json.Unmarshal([]byte(`[]`), &results); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

// TestRecallJSONResultMissingContentField asserts that a missing "content"
// key results in an empty string (not an error) — Go's zero-value default.
// This matches the "all content fields empty" guard in CortexClient.Recall.
func TestRecallJSONResultMissingContentField(t *testing.T) {
	const input = `[{"memory":{}}]`
	var results []recallJSONResult
	if err := json.Unmarshal([]byte(input), &results); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if got := results[0].Memory.Content; got != "" {
		t.Errorf("expected empty string for missing content, got %q", got)
	}
}
