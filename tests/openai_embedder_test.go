package tests

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/embedder"
)

// newFakeOpenAIServer returns an httptest.Server that responds to /v1/embeddings
// with a deterministic embedding response. Each input text produces one embedding
// of length dim. The index field in the response matches the position of the input.
func newFakeOpenAIServer(t *testing.T, dim int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			http.NotFound(w, r)
			return
		}

		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, `{"error":{"message":"missing auth","type":"invalid_request_error"}}`, http.StatusUnauthorized)
			return
		}

		var req struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		type dataItem struct {
			Object    string    `json:"object"`
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		}

		data := make([]dataItem, len(req.Input))
		for i := range req.Input {
			emb := make([]float32, dim)
			for j := range emb {
				emb[j] = float32(j) * 0.01
			}
			data[i] = dataItem{
				Object:    "embedding",
				Embedding: emb,
				Index:     i,
			}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": data})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newFakeOpenAIEmbedder creates an OpenAIEmbedder pointed at the fake server.
// It replaces the real OpenAI endpoint by monkey-patching via the test server URL prefix.
// Because NewOpenAIEmbedder hard-codes the URL we use a thin wrapper approach:
// the fake server serves /v1/embeddings so we just need the embedder to point there.
func newFakeOpenAIEmbedder(t *testing.T, srv *httptest.Server, dim int) *embedder.OpenAIEmbedder {
	t.Helper()
	return embedder.NewOpenAIEmbedderWithURL(srv.URL+"/v1/embeddings", "fake-key", "text-embedding-3-small", dim, slog.Default())
}

// TestOpenAIEmbedder_Embed_HappyPath verifies that Embed returns a float32 slice
// of the expected dimension when the server responds correctly.
func TestOpenAIEmbedder_Embed_HappyPath(t *testing.T) {
	const dim = 768
	srv := newFakeOpenAIServer(t, dim)
	emb := newFakeOpenAIEmbedder(t, srv, dim)

	vec, err := emb.Embed(context.Background(), "hello world")
	require.NoError(t, err)
	assert.Len(t, vec, dim)
	assert.Equal(t, float32(0.0), vec[0])
	assert.InDelta(t, 0.01, vec[1], 0.001)
}

// TestOpenAIEmbedder_Dimension verifies that Dimension returns the configured value.
func TestOpenAIEmbedder_Dimension(t *testing.T) {
	emb := embedder.NewOpenAIEmbedder("fake-key", "", 768, slog.Default())
	assert.Equal(t, 768, emb.Dimension())
}

// TestOpenAIEmbedder_Dimension_Default verifies that a zero dimension is normalised to 768.
func TestOpenAIEmbedder_Dimension_Default(t *testing.T) {
	emb := embedder.NewOpenAIEmbedder("fake-key", "", 0, slog.Default())
	assert.Equal(t, 768, emb.Dimension())
}

// TestOpenAIEmbedder_EmbedBatch_HappyPath verifies that EmbedBatch returns one
// embedding per input and that each has the expected dimension.
func TestOpenAIEmbedder_EmbedBatch_HappyPath(t *testing.T) {
	const dim = 64
	srv := newFakeOpenAIServer(t, dim)
	emb := newFakeOpenAIEmbedder(t, srv, dim)

	texts := []string{"first", "second", "third"}
	vecs, err := emb.EmbedBatch(context.Background(), texts)
	require.NoError(t, err)
	assert.Len(t, vecs, len(texts))
	for _, v := range vecs {
		assert.Len(t, v, dim)
	}
}

// TestOpenAIEmbedder_EmbedBatch_Empty verifies that EmbedBatch with no inputs
// returns nil without calling the server.
func TestOpenAIEmbedder_EmbedBatch_Empty(t *testing.T) {
	emb := embedder.NewOpenAIEmbedder("fake-key", "", 768, slog.Default())
	vecs, err := emb.EmbedBatch(context.Background(), nil)
	require.NoError(t, err)
	assert.Nil(t, vecs)
}

// TestOpenAIEmbedder_Embed_4xxError verifies that a 4xx response is surfaced as an error.
func TestOpenAIEmbedder_Embed_4xxError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid API key","type":"invalid_request_error","code":"invalid_api_key"}}`))
	}))
	t.Cleanup(srv.Close)

	emb := embedder.NewOpenAIEmbedderWithURL(srv.URL+"/v1/embeddings", "bad-key", "", 768, slog.Default())
	_, err := emb.Embed(context.Background(), "test")
	require.Error(t, err)
	assert.ErrorContains(t, err, "401")
}

// TestOpenAIEmbedder_Embed_5xxError verifies that a 5xx response is returned as an error.
func TestOpenAIEmbedder_Embed_5xxError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	emb := embedder.NewOpenAIEmbedderWithURL(srv.URL+"/v1/embeddings", "key", "", 768, slog.Default())
	_, err := emb.Embed(context.Background(), "test")
	require.Error(t, err)
	assert.ErrorContains(t, err, "500")
}

// TestOpenAIEmbedder_Embed_InvalidJSON verifies that malformed JSON in the response
// is propagated as an error.
func TestOpenAIEmbedder_Embed_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not-valid-json"))
	}))
	t.Cleanup(srv.Close)

	emb := embedder.NewOpenAIEmbedderWithURL(srv.URL+"/v1/embeddings", "key", "", 768, slog.Default())
	_, err := emb.Embed(context.Background(), "test")
	require.Error(t, err)
}

// TestOpenAIEmbedder_EmbedBatch_ServerError verifies that EmbedBatch propagates
// HTTP errors returned by the server.
func TestOpenAIEmbedder_EmbedBatch_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad request","type":"invalid_request_error"}}`))
	}))
	t.Cleanup(srv.Close)

	emb := embedder.NewOpenAIEmbedderWithURL(srv.URL+"/v1/embeddings", "key", "", 768, slog.Default())
	_, err := emb.EmbedBatch(context.Background(), []string{"text1", "text2"})
	require.Error(t, err)
	assert.ErrorContains(t, err, "bad request")
}

// TestOpenAIEmbedder_EmbedBatch_OrderPreserved verifies that EmbedBatch returns
// embeddings in the same order as the input texts even when the server returns
// them in reverse index order.
func TestOpenAIEmbedder_EmbedBatch_OrderPreserved(t *testing.T) {
	const dim = 4
	// Server returns embeddings in reverse order.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		type dataItem struct {
			Object    string    `json:"object"`
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		}

		n := len(req.Input)
		data := make([]dataItem, n)
		for i := range req.Input {
			// Use the index value as the first element so we can verify order.
			emb := make([]float32, dim)
			emb[0] = float32(i)
			// Return in reverse order to test sorting.
			data[n-1-i] = dataItem{
				Object:    "embedding",
				Embedding: emb,
				Index:     i,
			}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": data})
	}))
	t.Cleanup(srv.Close)

	emb := embedder.NewOpenAIEmbedderWithURL(srv.URL+"/v1/embeddings", "key", "", dim, slog.Default())
	vecs, err := emb.EmbedBatch(context.Background(), []string{"a", "b", "c"})
	require.NoError(t, err)
	require.Len(t, vecs, 3)
	// First element of each embedding encodes the original index.
	assert.Equal(t, float32(0), vecs[0][0])
	assert.Equal(t, float32(1), vecs[1][0])
	assert.Equal(t, float32(2), vecs[2][0])
}
