package tests

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/embedder"
)

// TestOllamaEmbedder_Embed_RetryOn500 verifies that 500 errors trigger retries
// and eventually succeed when the server recovers.
func TestOllamaEmbedder_Embed_RetryOn500(t *testing.T) {
	const dim = 8
	var callCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := callCount.Add(1)
		if n < 3 {
			// First two calls fail with 500
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		// Third call succeeds
		embedding := make([]float64, dim)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"embedding": embedding})
	}))
	t.Cleanup(srv.Close)

	emb := embedder.NewOllamaEmbedder(srv.URL, "model", dim, slog.Default())
	vec, err := emb.Embed(context.Background(), "test")
	require.NoError(t, err)
	assert.Len(t, vec, dim)
	assert.GreaterOrEqual(t, int(callCount.Load()), 3)
}

// TestOllamaEmbedder_EmbedBatch_ServerError verifies that EmbedBatch propagates errors.
func TestOllamaEmbedder_EmbedBatch_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad request"))
	}))
	t.Cleanup(srv.Close)

	emb := embedder.NewOllamaEmbedder(srv.URL, "model", 768, slog.Default())
	_, err := emb.EmbedBatch(context.Background(), []string{"text1", "text2"})
	require.Error(t, err)
}

// TestOllamaEmbedder_EmbedBatch_ConcurrentEmbeds verifies that EmbedBatch handles
// multiple texts concurrently.
func TestOllamaEmbedder_EmbedBatch_ConcurrentEmbeds(t *testing.T) {
	const dim = 16
	srv := newFakeOllamaServer(t, dim)

	emb := embedder.NewOllamaEmbedder(srv.URL, "model", dim, slog.Default())
	texts := make([]string, 10)
	for i := range texts {
		texts[i] = "text sample"
	}

	vecs, err := emb.EmbedBatch(context.Background(), texts)
	require.NoError(t, err)
	assert.Len(t, vecs, 10)
	for _, v := range vecs {
		assert.Len(t, v, dim)
	}
}
