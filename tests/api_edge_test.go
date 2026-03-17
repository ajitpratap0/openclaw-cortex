package tests

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/api"
	"github.com/ajitpratap0/openclaw-cortex/internal/hooks"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/recall"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

// --- api.Shutdown ---

// TestServer_Shutdown verifies that api.Shutdown on a not-yet-started http.Server
// (already idle) returns nil — the graceful-shutdown path must not error.
func TestServer_Shutdown(t *testing.T) {
	srv := &http.Server{Addr: "127.0.0.1:0"}
	err := api.Shutdown(srv, 5*time.Second)
	assert.NoError(t, err)
}

// --- GET /v1/stats ---

// TestHandleStats_EmptyStore verifies that the /v1/stats endpoint returns 200
// with zero TotalMemories when the store is empty.
func TestHandleStats_EmptyStore(t *testing.T) {
	ts, _ := newTestServer(t, "")

	resp := doRequest(t, http.MethodGet, ts.URL+"/v1/stats", nil, "")
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var stats models.CollectionStats
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&stats))
	assert.Equal(t, int64(0), stats.TotalMemories)
}

// TestHandleStats_WithMemories verifies that /v1/stats reflects seeded memories.
func TestHandleStats_WithMemories(t *testing.T) {
	ts, st := newTestServer(t, "")

	now := time.Now().UTC()
	for i := 0; i < 4; i++ {
		mem := models.Memory{
			ID:           fmt.Sprintf("edge-stats-%03d", i),
			Type:         models.MemoryTypeFact,
			Scope:        models.ScopePermanent,
			Visibility:   models.VisibilityPrivate,
			Content:      fmt.Sprintf("edge stats content %d", i),
			Confidence:   0.9,
			Source:       "test",
			CreatedAt:    now,
			UpdatedAt:    now,
			LastAccessed: now,
		}
		vec := make([]float32, 768)
		require.NoError(t, st.Upsert(context.Background(), mem, vec))
	}

	resp := doRequest(t, http.MethodGet, ts.URL+"/v1/stats", nil, "")
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var stats models.CollectionStats
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&stats))
	assert.Equal(t, int64(4), stats.TotalMemories)
}

// --- recall.Reasoner degradation via mockLLMClient ---

func newEdgeReasoner(llmResp string, llmErr error) *recall.Reasoner {
	logger := slog.Default()
	mock := &mockLLMClient{Resp: llmResp, Err: llmErr}
	return recall.NewReasoner(mock, "test-model", logger)
}

// TestReasoner_ReRank_LLMError verifies that an LLM error causes graceful
// degradation: the original order is returned and no error is propagated.
func TestReasoner_ReRank_LLMError(t *testing.T) {
	r := newEdgeReasoner("", errors.New("upstream API unavailable"))

	input := []models.RecallResult{
		newTestRecallResult("a", "first", 0.9),
		newTestRecallResult("b", "second", 0.8),
		newTestRecallResult("c", "third", 0.7),
	}

	results, err := r.ReRank(context.Background(), "query", input, 10)
	require.NoError(t, err)
	require.Len(t, results, 3)
	assert.Equal(t, "a", results[0].Memory.ID)
	assert.Equal(t, "b", results[1].Memory.ID)
	assert.Equal(t, "c", results[2].Memory.ID)
}

// TestReasoner_ReRank_EmptyLLMResponse verifies that a blank LLM response
// causes graceful degradation (original order returned, no error).
func TestReasoner_ReRank_EmptyLLMResponse(t *testing.T) {
	r := newEdgeReasoner("   ", nil) // whitespace-only trims to empty

	input := []models.RecallResult{
		newTestRecallResult("x", "content x", 0.9),
		newTestRecallResult("y", "content y", 0.8),
	}

	results, err := r.ReRank(context.Background(), "query", input, 10)
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Equal(t, "x", results[0].Memory.ID)
	assert.Equal(t, "y", results[1].Memory.ID)
}

// TestReasoner_ReRank_InvalidJSONResponse verifies that unparseable LLM output
// causes graceful degradation (original order returned, no error).
func TestReasoner_ReRank_InvalidJSONResponse(t *testing.T) {
	r := newEdgeReasoner("not valid json", nil)

	input := []models.RecallResult{
		newTestRecallResult("p", "content p", 0.9),
		newTestRecallResult("q", "content q", 0.8),
	}

	results, err := r.ReRank(context.Background(), "query", input, 10)
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Equal(t, "p", results[0].Memory.ID)
	assert.Equal(t, "q", results[1].Memory.ID)
}

// TestReasoner_ReRank_SuccessReorders verifies that a valid JSON index array
// from the LLM causes results to be reordered accordingly.
func TestReasoner_ReRank_SuccessReorders(t *testing.T) {
	// LLM says: put index 2 first, then 0, then 1.
	r := newEdgeReasoner("[2, 0, 1]", nil)

	input := []models.RecallResult{
		newTestRecallResult("id0", "content zero", 0.9),
		newTestRecallResult("id1", "content one", 0.8),
		newTestRecallResult("id2", "content two", 0.7),
	}

	results, err := r.ReRank(context.Background(), "query", input, 10)
	require.NoError(t, err)
	require.Len(t, results, 3)
	assert.Equal(t, "id2", results[0].Memory.ID)
	assert.Equal(t, "id0", results[1].Memory.ID)
	assert.Equal(t, "id1", results[2].Memory.ID)
}

// TestReasoner_ReRank_MoreThanMaxCandidates verifies that results beyond
// maxCandidates are appended unchanged after the re-ranked candidates.
func TestReasoner_ReRank_MoreThanMaxCandidates(t *testing.T) {
	// maxCandidates=2; LLM reorders the first two; rest are tail.
	r := newEdgeReasoner("[1, 0]", nil)

	input := []models.RecallResult{
		newTestRecallResult("c0", "zero", 0.9),
		newTestRecallResult("c1", "one", 0.8),
		newTestRecallResult("c2", "two", 0.7),   // tail
		newTestRecallResult("c3", "three", 0.6), // tail
	}

	results, err := r.ReRank(context.Background(), "query", input, 2)
	require.NoError(t, err)
	require.Len(t, results, 4)
	// Reordered candidates.
	assert.Equal(t, "c1", results[0].Memory.ID)
	assert.Equal(t, "c0", results[1].Memory.ID)
	// Tail preserved in original order.
	assert.Equal(t, "c2", results[2].Memory.ID)
	assert.Equal(t, "c3", results[3].Memory.ID)
}

// TestReasoner_ReRank_OutOfRangeIndices verifies that out-of-range indices
// returned by the LLM are silently ignored without panic.
func TestReasoner_ReRank_OutOfRangeIndices(t *testing.T) {
	// LLM returns indices including 99 (out of range) and -1 (negative).
	r := newEdgeReasoner("[99, -1, 0, 1]", nil)

	input := []models.RecallResult{
		newTestRecallResult("r0", "zero", 0.9),
		newTestRecallResult("r1", "one", 0.8),
	}

	results, err := r.ReRank(context.Background(), "query", input, 10)
	require.NoError(t, err)
	require.Len(t, results, 2)
	// Valid indices (0 and 1) are included; out-of-range are dropped.
	assert.Equal(t, "r0", results[0].Memory.ID)
	assert.Equal(t, "r1", results[1].Memory.ID)
}

// --- recall.NewRecaller.SetGraphWeights ---

// TestRecaller_SetGraphWeights_Valid verifies that valid non-negative weights
// are accepted and override the defaults.
func TestRecaller_SetGraphWeights_Valid(t *testing.T) {
	logger := slog.Default()
	rec := recall.NewRecaller(recall.DefaultWeights(), logger)

	// SetGraphWeights should not panic and should accept valid values.
	rec.SetGraphWeights(0.7, 0.3)

	// Confirm RecallWithGraph still runs without graph client (falls back to Rank).
	results := rec.RecallWithGraph(
		context.Background(),
		"test query",
		make([]float32, 768),
		[]models.SearchResult{},
		"",
	)
	assert.Empty(t, results)
}

// TestRecaller_SetGraphWeights_NegativeIgnored verifies that negative weights
// are ignored and the recaller continues to use previous/default values.
func TestRecaller_SetGraphWeights_NegativeIgnored(t *testing.T) {
	logger := slog.Default()
	rec := recall.NewRecaller(recall.DefaultWeights(), logger)

	// Set valid weights first.
	rec.SetGraphWeights(0.6, 0.4)
	// Negative weights should be rejected silently.
	rec.SetGraphWeights(-1.0, 0.5)
	rec.SetGraphWeights(0.5, -0.1)

	// Recaller should still function — no panic, no error.
	results := rec.RecallWithGraph(
		context.Background(),
		"query",
		make([]float32, 768),
		[]models.SearchResult{
			{
				Memory: models.Memory{
					ID:           "wt-1",
					Type:         models.MemoryTypeFact,
					Scope:        models.ScopePermanent,
					Content:      "weight test memory",
					Confidence:   0.9,
					CreatedAt:    time.Now().UTC(),
					UpdatedAt:    time.Now().UTC(),
					LastAccessed: time.Now().UTC(),
				},
				Score: 0.8,
			},
		},
		"",
	)
	require.Len(t, results, 1)
	assert.Equal(t, "wt-1", results[0].Memory.ID)
}

// statsFailStore wraps MockStore and injects an error on Stats calls.
type statsFailStore struct {
	*store.MockStore
	statsErr error
}

func (s *statsFailStore) Stats(_ context.Context) (*models.CollectionStats, error) {
	return nil, s.statsErr
}

// TestHandleStats_StoreError verifies that when the store returns an error,
// the /v1/stats endpoint returns 500.
func TestHandleStats_StoreError(t *testing.T) {
	logger := slog.Default()
	st := &statsFailStore{
		MockStore: store.NewMockStore(),
		statsErr:  errors.New("db unreachable"),
	}
	rec := recall.NewRecaller(recall.DefaultWeights(), logger)
	emb := &apiTestEmbedder{}
	srv := api.NewServer(st, rec, emb, logger, "", "")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp := doRequest(t, http.MethodGet, ts.URL+"/v1/stats", nil, "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// --- hooks.PreTurnHook.WithReasoner (0% → covered) ---

func TestPreTurnHook_WithReasoner_ChainsAndReturnsHook(t *testing.T) {
	st := store.NewMockStore()
	emb := &apiTestEmbedder{}
	logger := slog.Default()
	rec := recall.NewRecaller(recall.DefaultWeights(), logger)
	reasoner := recall.NewReasoner(&mockLLMClient{Resp: "[]"}, "model", logger)

	hook := hooks.NewPreTurnHook(emb, st, rec, logger)
	chained := hook.WithReasoner(reasoner, hooks.RerankConfig{ScoreSpreadThreshold: 0.1})
	assert.NotNil(t, chained, "WithReasoner should return the hook for method chaining")
	assert.Equal(t, hook, chained, "WithReasoner should return the same hook (not a copy)")
}
