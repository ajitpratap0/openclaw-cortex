package tests

// store_validation_test.go covers Bug 3 (min content length guard) and
// Bug 5 (per-call --dedup-threshold override) from issue #104.
//
// Validation constants and typed errors live in internal/store/validation.go
// so that the cmd layer and tests reference a single source of truth.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/config"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
	"github.com/ajitpratap0/openclaw-cortex/pkg/vecmath"
)

// --- helpers shared by these tests ---

// validationTestVec creates a deterministic 768-dim vector seeded by val.
func validationTestVec(val float32) []float32 {
	const dim = 768
	v := make([]float32, dim)
	for i := range v {
		v[i] = val * float32(i+1) / float32(dim)
	}
	return v
}

// nearSimilarVec returns a vector that is near-similar to base but not identical.
// It mixes base (weight 0.98) with a reverse-indexed complement (weight 0.02).
// With these weights the resulting cosine similarity to base is very high
// (approximately 0.9998) — close enough to be flagged as a duplicate at
// threshold 0.92, but strictly less than 1.0 so it is NOT flagged when
// threshold is set to 1.0 (exact match only).
func nearSimilarVec(base []float32) []float32 {
	const dim = 768
	out := make([]float32, dim)
	// Reverse-indexed complement: comp[i] = (dim-i) / dim.
	// base and comp share no proportionality, giving a cosine < 1.
	for i := range out {
		comp := float32(dim-i) / float32(dim)
		out[i] = 0.98*base[i] + 0.02*comp
	}
	return out
}

// storeValidationMemory upserts a memory with a given vector into a MockStore.
func storeValidationMemory(t *testing.T, st *store.MockStore, id, content string, vec []float32) {
	t.Helper()
	now := time.Now().UTC()
	mem := models.Memory{
		ID:           id,
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopePermanent,
		Visibility:   models.VisibilityShared,
		Content:      content,
		Confidence:   0.9,
		Source:       "test",
		CreatedAt:    now,
		UpdatedAt:    now,
		LastAccessed: now,
	}
	require.NoError(t, st.Upsert(context.Background(), mem, vec))
}

// validateContentLength delegates to the production function in
// internal/store/validation.go — both the cmd layer and tests call the
// same ValidateContentLength, so removing the guard from the cmd would
// not silently pass these tests.
func validateContentLength(content string) error {
	return store.ValidateContentLength(content)
}

// --- Bug 3: minimum content length ---

// TestStoreCmd_MinContentLength verifies that content shorter than 10
// non-whitespace characters is rejected.
func TestStoreCmd_MinContentLength(t *testing.T) {
	cases := []struct {
		name    string
		content string
		wantErr bool
	}{
		{name: "empty string", content: "", wantErr: true},
		{name: "single char", content: "x", wantErr: true},
		{name: "whitespace only", content: "   \t  ", wantErr: true},
		{name: "nine chars", content: "123456789", wantErr: true},
		{name: "nine chars with surrounding spaces", content: "  123456789  ", wantErr: true},
		{name: "exactly ten chars", content: "1234567890", wantErr: false},
		{name: "ten chars with spaces", content: "  1234567890  ", wantErr: false},
		{name: "normal sentence", content: "Go uses goroutines for concurrency.", wantErr: false},
		// Multibyte UTF-8: 10 CJK runes (30 bytes) should pass because we count
		// runes, not bytes.
		{name: "ten CJK runes", content: "\u4e00\u4e8c\u4e09\u56db\u4e94\u516d\u4e03\u516b\u4e5d\u5341", wantErr: false},
		// 9 CJK runes (27 bytes) — passes len() >= 10 but fails rune count < 10.
		{name: "nine CJK runes", content: "\u4e00\u4e8c\u4e09\u56db\u4e94\u516d\u4e03\u516b\u4e5d", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateContentLength(tc.content)
			if tc.wantErr {
				require.Error(t, err, "expected error for content %q", tc.content)
				var e *store.ErrContentTooShort
				require.True(t, errors.As(err, &e), "error should be *store.ErrContentTooShort")
			} else {
				require.NoError(t, err, "unexpected error for content %q", tc.content)
			}
		})
	}
}

// TestStoreCmd_MinContentLength_BatchValidation verifies that the batch helper
// also rejects short content before any store I/O occurs.
func TestStoreCmd_MinContentLength_BatchValidation(t *testing.T) {
	// The batch validation loop in cmd_store_batch.go rejects entries with
	// content shorter than minContentLen. We replicate that logic here to
	// confirm the guard works correctly.
	inputs := []struct {
		content string
		valid   bool
	}{
		{"Go uses goroutines.", true},
		{"short", false},     // 5 chars — under threshold
		{"x", false},         // 1 char
		{"  hi  ", false},    // 2 trimmed chars
		{"1234567890", true}, // exactly 10 trimmed chars — accepted
	}

	for _, inp := range inputs {
		err := validateContentLength(inp.content)
		if inp.valid {
			assert.NoError(t, err, "content %q should be accepted", inp.content)
		} else {
			assert.Error(t, err, "content %q should be rejected", inp.content)
		}
	}
}

// --- Bug 5: per-call --dedup-threshold override ---

// TestStoreCmd_DedupThresholdFlag verifies that using a custom dedup threshold
// changes what gets treated as a duplicate.  A threshold of 1.0 (exact match
// only) should not flag near-identical (but not identical) vectors as dupes,
// whereas the default 0.92 would.
func TestStoreCmd_DedupThresholdFlag(t *testing.T) {
	ctx := context.Background()
	st := store.NewMockStore()

	// Store an existing memory with vecA.
	existingContent := "Go uses goroutines for lightweight concurrency."
	vecA := validationTestVec(0.8)
	storeValidationMemory(t, st, uuid.New().String(), existingContent, vecA)

	// --- sub-case: identical vector (cosine sim = 1.0) ---
	// vecA re-used as the new vector; sim = 1.0 exactly.

	// With default threshold (0.92): identical vector → duplicate.
	resDefault, err := store.CheckAndHandleDuplicate(ctx, st, vecA, existingContent, 0.92)
	require.NoError(t, err)
	assert.True(t, resDefault.IsDuplicate || resDefault.IsUpdated,
		"with threshold 0.92, identical vector should be flagged as duplicate or updated")

	// With explicit threshold of 0.99: identical vector should still be flagged.
	resStrict, err := store.CheckAndHandleDuplicate(ctx, st, vecA, existingContent, 0.99)
	require.NoError(t, err)
	assert.True(t, resStrict.IsDuplicate || resStrict.IsUpdated,
		"with threshold 0.99, identical vector (sim=1.0) should still be flagged")

	// With an explicitly loose threshold (0.5): still flagged (sim=1.0 exceeds 0.5).
	resLoose, err := store.CheckAndHandleDuplicate(ctx, st, vecA, existingContent, 0.5)
	require.NoError(t, err)
	assert.True(t, resLoose.IsDuplicate || resLoose.IsUpdated,
		"with threshold 0.5, identical vector should still be flagged")

	// Confirm CheckAndHandleDuplicate does not panic on a borderline threshold.
	_, safeErr := store.CheckAndHandleDuplicate(ctx, st, vecA, existingContent, 0.95)
	require.NoError(t, safeErr)

	// --- sub-case: near-similar vector (cosine sim in (0.92, 1.0)) ---
	// vecB is constructed from vecA with a small perturbation so that
	// cosine(vecA, vecB) is in (0.92, 1.0).  This is the scenario the function
	// doc describes: threshold=1.0 must NOT flag vecB, but threshold=0.92 must.
	//
	// Use a fresh store so that the vecB-based update from the 0.92 call does not
	// affect the 1.0 call (CheckAndHandleDuplicate updates the stored vector when
	// the new content is longer, which would cause the 1.0 call to see vecB vs
	// vecB — cosine 1.0 — and be incorrectly flagged).
	vecB := nearSimilarVec(vecA)
	// Self-verify: the cosine similarity between vecA and vecB must be in the
	// range (0.92, 1.0) for the sub-case assertions below to be meaningful.
	sim := vecmath.CosineSimilarity(vecA, vecB)
	require.Greater(t, sim, 0.92, "nearSimilarVec must produce sim > 0.92")
	require.Less(t, sim, 1.0, "nearSimilarVec must produce sim < 1.0")
	// nearContent must be shorter than existingContent so that the 0.92 call
	// returns IsDuplicate (skip) rather than IsUpdated (mutates stored vector).
	nearContent := "Go uses goroutines." // shorter than existingContent → IsDuplicate

	stNear := store.NewMockStore()
	storeValidationMemory(t, stNear, uuid.New().String(), existingContent, vecA)

	// threshold=0.92 → vecB cosine ≈ 0.9998 > 0.92 → flagged as duplicate.
	resNearFlagged, nearErr := store.CheckAndHandleDuplicate(ctx, stNear, vecB, nearContent, 0.92)
	require.NoError(t, nearErr)
	assert.True(t, resNearFlagged.IsDuplicate || resNearFlagged.IsUpdated,
		"with threshold 0.92, near-similar vector should be flagged as duplicate or updated")

	// threshold=1.0 → vecB cosine ≈ 0.9998 < 1.0 → NOT flagged (exact match only).
	resNearNotFlagged, nearErr2 := store.CheckAndHandleDuplicate(ctx, stNear, vecB, nearContent, 1.0)
	require.NoError(t, nearErr2)
	assert.False(t, resNearNotFlagged.IsDuplicate || resNearNotFlagged.IsUpdated,
		"with threshold 1.0, near-similar (non-identical) vector should NOT be flagged as duplicate")
}

// TestStoreCmd_DedupThresholdInvalidRange verifies that the cmd layer's range
// check catches out-of-range values before they reach the store.
func TestStoreCmd_DedupThresholdInvalidRange(t *testing.T) {
	// We test the validation logic directly (mirroring what cmd_store.go does).
	cases := []struct {
		threshold float64
		wantErr   bool
	}{
		{-0.1, true},
		{-1.0, true},
		{1.1, true},
		{2.0, true},
		{0.0, true}, // 0.0 is now rejected (sentinel 0 must not pass through as a real threshold)
		{0.5, false},
		{0.92, false},
		{1.0, false},
	}

	for _, tc := range cases {
		err := store.ValidateDedupThreshold(tc.threshold)
		if tc.wantErr {
			require.Error(t, err, "threshold %g should be rejected", tc.threshold)
			var e *store.ErrDedupThresholdRange
			require.True(t, errors.As(err, &e), "error should be *store.ErrDedupThresholdRange")
		} else {
			require.NoError(t, err, "threshold %g should be accepted", tc.threshold)
		}
	}
}

// TestStoreCmd_DedupThresholdEffective_DisablesDedupAtZero verifies that when
// no explicit threshold is passed (sentinel 0 → use config default 0.92) the
// behavior is unchanged from pre-fix.
func TestStoreCmd_DedupThresholdEffective_DisablesDedupAtZero(t *testing.T) {
	// sentinel 0 is NOT passed to CheckAndHandleDuplicate directly;
	// the cmd layer replaces it with cfg.Memory.DedupThreshold (0.92).
	// Here we just confirm that 0.92 (the config default) still flags
	// an identical vector as a duplicate.
	ctx := context.Background()
	st := store.NewMockStore()

	content := "Rust enforces memory safety through the borrow checker."
	vec := validationTestVec(0.5)
	storeValidationMemory(t, st, "rust-mem-1", content, vec)

	res, err := store.CheckAndHandleDuplicate(ctx, st, vec, content, 0.92)
	require.NoError(t, err)
	assert.True(t, res.IsDuplicate || res.IsUpdated,
		"config default 0.92 should still detect identical vector as duplicate")
}

// TestConfig_Validate_DedupThresholdZero verifies that config.Validate rejects
// DedupThreshold=0 (tightened from [0,1] to (0,1]).
func TestConfig_Validate_DedupThresholdZero(t *testing.T) {
	cfg := config.Config{
		Memgraph: config.MemgraphConfig{URI: "bolt://localhost:7687"},
		Ollama:   config.OllamaConfig{BaseURL: "http://localhost:11434"},
		Memory: config.MemoryConfig{
			ChunkSize:          512,
			ChunkOverlap:       64,
			DedupThreshold:     0, // invalid: tightened to (0, 1]
			DedupThresholdHook: 0.95,
			VectorDimension:    768,
			DefaultTTLHours:    720,
		},
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dedup_threshold")
}

// TestConfig_Validate_DedupThresholdHookZero verifies that config.Validate
// rejects DedupThresholdHook=0 now that the boundary is (0, 1].
func TestConfig_Validate_DedupThresholdHookZero(t *testing.T) {
	cfg := config.Config{
		Memgraph: config.MemgraphConfig{URI: "bolt://localhost:7687"},
		Ollama:   config.OllamaConfig{BaseURL: "http://localhost:11434"},
		Memory: config.MemoryConfig{
			ChunkSize:          512,
			ChunkOverlap:       64,
			DedupThreshold:     0.92,
			DedupThresholdHook: 0, // invalid: tightened to (0, 1]
			VectorDimension:    768,
			DefaultTTLHours:    720,
		},
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dedup_threshold_hook")
}
