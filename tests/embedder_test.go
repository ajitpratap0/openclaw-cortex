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

// newFakeOllamaServer returns an httptest.Server that responds to both
// /api/embeddings (single) and /api/embed (batch) with deterministic embeddings.
func newFakeOllamaServer(t *testing.T, dim int) *httptest.Server {
	t.Helper()
	makeEmbedding := func() []float64 {
		emb := make([]float64, dim)
		for i := range emb {
			emb[i] = float64(i) * 0.01
		}
		return emb
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/embeddings":
			_ = json.NewEncoder(w).Encode(map[string]any{"embedding": makeEmbedding()})
		case "/api/embed":
			var req struct {
				Input []string `json:"input"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			embeddings := make([][]float64, len(req.Input))
			for i := range embeddings {
				embeddings[i] = makeEmbedding()
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": embeddings})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestOllamaEmbedder_Embed_HappyPath(t *testing.T) {
	const dim = 768
	srv := newFakeOllamaServer(t, dim)

	emb := embedder.NewOllamaEmbedder(srv.URL, "nomic-embed-text", dim, slog.Default())
	vec, err := emb.Embed(context.Background(), "hello world")
	require.NoError(t, err)
	assert.Len(t, vec, dim)
	assert.Equal(t, float32(0.0), vec[0])
	assert.InDelta(t, 0.01, vec[1], 0.001)
}

func TestOllamaEmbedder_Dimension(t *testing.T) {
	emb := embedder.NewOllamaEmbedder("http://localhost:11434", "model", 512, slog.Default())
	assert.Equal(t, 512, emb.Dimension())
}

func TestOllamaEmbedder_EmbedBatch_HappyPath(t *testing.T) {
	const dim = 64
	srv := newFakeOllamaServer(t, dim)

	emb := embedder.NewOllamaEmbedder(srv.URL, "nomic-embed-text", dim, slog.Default())
	texts := []string{"first", "second", "third"}

	vecs, err := emb.EmbedBatch(context.Background(), texts)
	require.NoError(t, err)
	assert.Len(t, vecs, len(texts))
	for _, v := range vecs {
		assert.Len(t, v, dim)
	}
}

func TestOllamaEmbedder_EmbedBatch_Empty(t *testing.T) {
	emb := embedder.NewOllamaEmbedder("http://localhost:11434", "model", 768, slog.Default())

	vecs, err := emb.EmbedBatch(context.Background(), nil)
	require.NoError(t, err)
	assert.Empty(t, vecs)
}

func TestOllamaEmbedder_Embed_ServerError(t *testing.T) {
	// Use 400 (not 500) so the client does not retry, keeping the test fast.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)

	emb := embedder.NewOllamaEmbedder(srv.URL, "model", 768, slog.Default())
	_, err := emb.Embed(context.Background(), "test")
	require.Error(t, err)
}

func TestOllamaEmbedder_Embed_EmptyEmbedding(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"embedding": []float64{}})
	}))
	t.Cleanup(srv.Close)

	emb := embedder.NewOllamaEmbedder(srv.URL, "model", 768, slog.Default())
	_, err := emb.Embed(context.Background(), "test")
	require.Error(t, err)
	assert.ErrorContains(t, err, "empty embedding")
}

func TestOllamaEmbedder_Embed_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not-json"))
	}))
	t.Cleanup(srv.Close)

	emb := embedder.NewOllamaEmbedder(srv.URL, "model", 768, slog.Default())
	_, err := emb.Embed(context.Background(), "test")
	require.Error(t, err)
}
