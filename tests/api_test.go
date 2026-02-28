package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/api"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/recall"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

// apiTestEmbedder is a trivial embedder for API tests that returns a fixed vector.
type apiTestEmbedder struct{}

func (m *apiTestEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	v := make([]float32, 768)
	for i := range v {
		v[i] = 0.1
	}
	return v, nil
}

func (m *apiTestEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		v, err := m.Embed(context.Background(), texts[i])
		if err != nil {
			return nil, fmt.Errorf("embed batch: %w", err)
		}
		out[i] = v
	}
	return out, nil
}

func (m *apiTestEmbedder) Dimension() int { return 768 }

// newTestServer creates a test HTTP server with a MockStore and a fixed-vector embedder.
func newTestServer(t *testing.T, authToken string) (*httptest.Server, *store.MockStore) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	st := store.NewMockStore()
	rec := recall.NewRecaller(recall.DefaultWeights(), logger)
	emb := &apiTestEmbedder{}
	srv := api.NewServer(st, rec, emb, logger, authToken)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, st
}

func jsonBody(t *testing.T, v any) *bytes.Buffer {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return bytes.NewBuffer(b)
}

func doRequest(t *testing.T, method, url string, body *bytes.Buffer, token string) *http.Response {
	t.Helper()
	var req *http.Request
	var err error
	if body != nil {
		req, err = http.NewRequestWithContext(context.Background(), method, url, body)
	} else {
		req, err = http.NewRequestWithContext(context.Background(), method, url, http.NoBody)
	}
	require.NoError(t, err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

// TestAPI_Healthz verifies that GET /healthz returns 200 {"status":"ok"}.
func TestAPI_Healthz(t *testing.T) {
	ts, _ := newTestServer(t, "")

	resp := doRequest(t, http.MethodGet, ts.URL+"/healthz", nil, "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Equal(t, "ok", result["status"])
}

// TestAPI_Remember stores a memory and verifies the response.
func TestAPI_Remember(t *testing.T) {
	ts, st := newTestServer(t, "")

	body := jsonBody(t, map[string]any{
		"content":    "Go is a statically typed language",
		"type":       "fact",
		"scope":      "permanent",
		"confidence": 0.95,
	})
	resp := doRequest(t, http.MethodPost, ts.URL+"/v1/remember", body, "")
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.True(t, result["stored"].(bool))
	id, ok := result["id"].(string)
	require.True(t, ok)
	assert.NotEmpty(t, id)

	// Verify the memory is actually in the store.
	ctx := context.Background()
	mem, err := st.Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, "Go is a statically typed language", mem.Content)
}

// TestAPI_GetMemory retrieves a stored memory by ID.
func TestAPI_GetMemory(t *testing.T) {
	ts, st := newTestServer(t, "")

	// Pre-load a memory into the mock store.
	now := time.Now().UTC()
	mem := models.Memory{
		ID:           "get-test-001",
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopePermanent,
		Visibility:   models.VisibilityPrivate,
		Content:      "Test content for get",
		Confidence:   0.9,
		Source:       "test",
		CreatedAt:    now,
		UpdatedAt:    now,
		LastAccessed: now,
	}
	vec := make([]float32, 768)
	require.NoError(t, st.Upsert(context.Background(), mem, vec))

	resp := doRequest(t, http.MethodGet, ts.URL+"/v1/memories/get-test-001", nil, "")
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var got models.Memory
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	assert.Equal(t, "get-test-001", got.ID)
	assert.Equal(t, "Test content for get", got.Content)
}

// TestAPI_GetMemory_NotFound verifies 404 for a non-existent ID.
func TestAPI_GetMemory_NotFound(t *testing.T) {
	ts, _ := newTestServer(t, "")

	resp := doRequest(t, http.MethodGet, ts.URL+"/v1/memories/does-not-exist", nil, "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// TestAPI_DeleteMemory removes a memory and verifies it is gone.
func TestAPI_DeleteMemory(t *testing.T) {
	ts, st := newTestServer(t, "")

	now := time.Now().UTC()
	mem := models.Memory{
		ID:           "del-test-001",
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopeSession,
		Visibility:   models.VisibilityPrivate,
		Content:      "Delete me",
		Confidence:   0.9,
		Source:       "test",
		CreatedAt:    now,
		UpdatedAt:    now,
		LastAccessed: now,
	}
	vec := make([]float32, 768)
	require.NoError(t, st.Upsert(context.Background(), mem, vec))

	resp := doRequest(t, http.MethodDelete, ts.URL+"/v1/memories/del-test-001", nil, "")
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]bool
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.True(t, result["deleted"])

	// Verify it is gone from the store.
	_, err := st.Get(context.Background(), "del-test-001")
	assert.Error(t, err)
}

// TestAPI_DeleteMemory_NotFound verifies 404 for a non-existent ID.
func TestAPI_DeleteMemory_NotFound(t *testing.T) {
	ts, _ := newTestServer(t, "")

	resp := doRequest(t, http.MethodDelete, ts.URL+"/v1/memories/does-not-exist", nil, "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// TestAPI_Recall returns context from stored memories.
func TestAPI_Recall(t *testing.T) {
	ts, st := newTestServer(t, "")

	// Store a memory so search returns something.
	now := time.Now().UTC()
	mem := models.Memory{
		ID:           "recall-test-001",
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopePermanent,
		Visibility:   models.VisibilityPrivate,
		Content:      "Go was created at Google",
		Confidence:   0.9,
		Source:       "test",
		CreatedAt:    now,
		UpdatedAt:    now,
		LastAccessed: now,
	}
	vec := make([]float32, 768)
	for i := range vec {
		vec[i] = 0.1
	}
	require.NoError(t, st.Upsert(context.Background(), mem, vec))

	body := jsonBody(t, map[string]any{
		"message": "Who created Go?",
		"budget":  2000,
	})
	resp := doRequest(t, http.MethodPost, ts.URL+"/v1/recall", body, "")
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.GreaterOrEqual(t, result["memory_count"].(float64), float64(1))
	assert.NotEmpty(t, result["context"])
}

// TestAPI_Search returns search results.
func TestAPI_Search(t *testing.T) {
	ts, st := newTestServer(t, "")

	now := time.Now().UTC()
	mem := models.Memory{
		ID:           "search-test-001",
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopePermanent,
		Visibility:   models.VisibilityPrivate,
		Content:      "Rust is a systems language",
		Confidence:   0.9,
		Source:       "test",
		CreatedAt:    now,
		UpdatedAt:    now,
		LastAccessed: now,
	}
	vec := make([]float32, 768)
	for i := range vec {
		vec[i] = 0.1
	}
	require.NoError(t, st.Upsert(context.Background(), mem, vec))

	body := jsonBody(t, map[string]any{
		"message": "What is Rust?",
		"limit":   5,
	})
	resp := doRequest(t, http.MethodPost, ts.URL+"/v1/search", body, "")
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	results, ok := result["results"].([]any)
	require.True(t, ok)
	assert.GreaterOrEqual(t, len(results), 1)
}

// TestAPI_Stats returns collection statistics.
func TestAPI_Stats(t *testing.T) {
	ts, st := newTestServer(t, "")

	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		mem := models.Memory{
			ID:           fmt.Sprintf("stats-test-%03d", i),
			Type:         models.MemoryTypeFact,
			Scope:        models.ScopePermanent,
			Visibility:   models.VisibilityPrivate,
			Content:      fmt.Sprintf("Stats content %d", i),
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
	assert.Equal(t, int64(3), stats.TotalMemories)
}

// TestAPI_Auth_Required verifies that protected endpoints reject bad tokens.
func TestAPI_Auth_Required(t *testing.T) {
	ts, _ := newTestServer(t, "secret-token")

	endpoints := []struct {
		method string
		path   string
		body   *bytes.Buffer
	}{
		{http.MethodPost, "/v1/remember", jsonBody(t, map[string]any{"content": "x"})},
		{http.MethodPost, "/v1/recall", jsonBody(t, map[string]any{"message": "x"})},
		{http.MethodGet, "/v1/memories/some-id", nil},
		{http.MethodDelete, "/v1/memories/some-id", nil},
		{http.MethodPost, "/v1/search", jsonBody(t, map[string]any{"message": "x"})},
		{http.MethodGet, "/v1/stats", nil},
	}

	for _, ep := range endpoints {
		t.Run(ep.method+" "+ep.path, func(t *testing.T) {
			// No token — should get 401.
			resp := doRequest(t, ep.method, ts.URL+ep.path, ep.body, "")
			defer resp.Body.Close()
			assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
		})
	}
}

// TestAPI_Auth_WrongToken verifies 401 for an incorrect token.
func TestAPI_Auth_WrongToken(t *testing.T) {
	ts, _ := newTestServer(t, "correct-token")

	body := jsonBody(t, map[string]any{"content": "x"})
	resp := doRequest(t, http.MethodPost, ts.URL+"/v1/remember", body, "wrong-token")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

// TestAPI_Auth_ValidToken verifies that correct token passes auth.
func TestAPI_Auth_ValidToken(t *testing.T) {
	ts, _ := newTestServer(t, "my-token")

	body := jsonBody(t, map[string]any{
		"content": "Authenticated memory",
		"type":    "fact",
		"scope":   "session",
	})
	resp := doRequest(t, http.MethodPost, ts.URL+"/v1/remember", body, "my-token")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestAPI_Healthz_NoAuth verifies that /healthz works even with auth enabled.
func TestAPI_Healthz_NoAuth(t *testing.T) {
	ts, _ := newTestServer(t, "some-token")

	resp := doRequest(t, http.MethodGet, ts.URL+"/healthz", nil, "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestAPI_Remember_MissingContent verifies 400 for missing content.
func TestAPI_Remember_MissingContent(t *testing.T) {
	ts, _ := newTestServer(t, "")

	body := jsonBody(t, map[string]any{"type": "fact"})
	resp := doRequest(t, http.MethodPost, ts.URL+"/v1/remember", body, "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	var errResp map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&errResp))
	assert.NotEmpty(t, errResp["error"])
}

// TestAPI_Recall_MissingMessage verifies 400 for missing message.
func TestAPI_Recall_MissingMessage(t *testing.T) {
	ts, _ := newTestServer(t, "")

	body := jsonBody(t, map[string]any{"budget": 2000})
	resp := doRequest(t, http.MethodPost, ts.URL+"/v1/recall", body, "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// TestAPI_Search_MissingMessage verifies 400 for missing message in search.
func TestAPI_Search_MissingMessage(t *testing.T) {
	ts, _ := newTestServer(t, "")

	body := jsonBody(t, map[string]any{"limit": 10})
	resp := doRequest(t, http.MethodPost, ts.URL+"/v1/search", body, "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// TestAPI_Remember_InvalidJSON verifies 400 for malformed JSON.
func TestAPI_Remember_InvalidJSON(t *testing.T) {
	ts, _ := newTestServer(t, "")

	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		ts.URL+"/v1/remember",
		strings.NewReader("not-json"),
	)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}
