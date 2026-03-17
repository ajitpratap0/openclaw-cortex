package tests

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ajitpratap0/openclaw-cortex/internal/config"
	"github.com/ajitpratap0/openclaw-cortex/internal/embedder"
)

// ---------------------------------------------------------------------------
// LMStudioEmbedder unit tests
// ---------------------------------------------------------------------------

// TestLMStudioEmbedder_Success verifies that Embed returns the correct float32
// slice when the mock server returns a well-formed response.
func TestLMStudioEmbedder_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("missing Content-Type header")
		}
		resp := map[string]any{
			"data": []map[string]any{
				{"embedding": []float64{0.1, 0.2, 0.3}},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	emb := embedder.NewLMStudioEmbedder(srv.URL, "nomic-embed-text")
	got, err := emb.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []float32{0.1, 0.2, 0.3}
	if len(got) != len(want) {
		t.Fatalf("got len %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

// TestLMStudioEmbedder_ServerError verifies that a 500 response is surfaced as
// a non-nil error whose message contains the status code.
func TestLMStudioEmbedder_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	emb := embedder.NewLMStudioEmbedder(srv.URL, "nomic-embed-text")
	_, err := emb.Embed(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
	// Error message must mention the status code.
	if !containsString(err.Error(), "500") {
		t.Errorf("error %q does not mention status 500", err.Error())
	}
}

// TestLMStudioEmbedder_EmptyResponse verifies that an empty data array is
// treated as an error rather than returning a nil slice silently.
func TestLMStudioEmbedder_EmptyResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
	}))
	defer srv.Close()

	emb := embedder.NewLMStudioEmbedder(srv.URL, "nomic-embed-text")
	_, err := emb.Embed(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error for empty data array, got nil")
	}
}

// TestLMStudioEmbedder_EmptyEmbeddingArray verifies that an embedding field
// present but empty in the first data element is also treated as an error.
func TestLMStudioEmbedder_EmptyEmbeddingArray(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]any{
			"data": []map[string]any{
				{"embedding": []float64{}},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	emb := embedder.NewLMStudioEmbedder(srv.URL, "nomic-embed-text")
	_, err := emb.Embed(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error for empty embedding vector, got nil")
	}
}

// TestLMStudioEmbedder_InvalidJSON verifies that malformed JSON in the response
// is surfaced as a decode error.
func TestLMStudioEmbedder_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{not valid json`))
	}))
	defer srv.Close()

	emb := embedder.NewLMStudioEmbedder(srv.URL, "model")
	_, err := emb.Embed(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected decode error for invalid JSON, got nil")
	}
}

// TestLMStudioEmbedder_ContextCancellation verifies that a canceled context
// causes Embed to return promptly with a context error.
func TestLMStudioEmbedder_ContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until context is canceled — should not be reached because the
		// context is already canceled before the request is sent.
		<-r.Context().Done()
		http.Error(w, "canceled", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	emb := embedder.NewLMStudioEmbedder(srv.URL, "model")
	_, err := emb.Embed(ctx, "hello")
	if err == nil {
		t.Fatal("expected error for canceled context, got nil")
	}
}

// ---------------------------------------------------------------------------
// embedder.New factory tests
// ---------------------------------------------------------------------------

// TestEmbedderFactory_OllamaDefault verifies that provider="" returns an
// OllamaEmbedder without error.
func TestEmbedderFactory_OllamaDefault(t *testing.T) {
	ollaCfg := config.OllamaConfig{
		BaseURL: "http://localhost:11434",
		Model:   "nomic-embed-text",
	}
	embCfg := config.EmbedderConfig{Provider: ""}
	emb, err := embedder.New(ollaCfg, embCfg, 768, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error for provider='': %v", err)
	}
	if emb == nil {
		t.Fatal("expected non-nil embedder")
	}
}

// TestEmbedderFactory_OllamaExplicit verifies that provider="ollama" also
// returns without error.
func TestEmbedderFactory_OllamaExplicit(t *testing.T) {
	ollaCfg := config.OllamaConfig{
		BaseURL: "http://localhost:11434",
		Model:   "nomic-embed-text",
	}
	embCfg := config.EmbedderConfig{Provider: "ollama"}
	emb, err := embedder.New(ollaCfg, embCfg, 768, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error for provider='ollama': %v", err)
	}
	if emb == nil {
		t.Fatal("expected non-nil embedder")
	}
}

// TestEmbedderFactory_LMStudioMissingModel verifies that provider="lmstudio"
// with an empty model returns an error whose message contains "lmstudio.model".
func TestEmbedderFactory_LMStudioMissingModel(t *testing.T) {
	ollaCfg := config.OllamaConfig{}
	embCfg := config.EmbedderConfig{
		Provider: "lmstudio",
		LMStudio: config.LMStudioConfig{Model: ""},
	}
	_, err := embedder.New(ollaCfg, embCfg, 768, slog.Default())
	if err == nil {
		t.Fatal("expected error for missing lmstudio model, got nil")
	}
	if !containsString(err.Error(), "lmstudio.model") {
		t.Errorf("error %q does not mention 'lmstudio.model'", err.Error())
	}
}

// TestEmbedderFactory_LMStudio verifies that provider="lmstudio" with
// a model set returns a non-nil embedder.
func TestEmbedderFactory_LMStudio(t *testing.T) {
	ollaCfg := config.OllamaConfig{}
	embCfg := config.EmbedderConfig{
		Provider: "lmstudio",
		LMStudio: config.LMStudioConfig{
			URL:   "http://localhost:1234",
			Model: "nomic-embed-text-v1.5",
		},
	}
	emb, err := embedder.New(ollaCfg, embCfg, 768, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if emb == nil {
		t.Fatal("expected non-nil embedder")
	}
}

// TestEmbedderFactory_LMStudioDefaultURL verifies that when LMStudio.URL is
// empty the factory substitutes the default "http://localhost:1234".
// (Behavioral test: relies on the embedder being constructible, not on making
// a real HTTP call.)
func TestEmbedderFactory_LMStudioDefaultURL(t *testing.T) {
	ollaCfg := config.OllamaConfig{}
	embCfg := config.EmbedderConfig{
		Provider: "lmstudio",
		LMStudio: config.LMStudioConfig{
			URL:   "", // empty — factory must fill in default
			Model: "model",
		},
	}
	emb, err := embedder.New(ollaCfg, embCfg, 768, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if emb == nil {
		t.Fatal("expected non-nil embedder")
	}
}

// TestEmbedderFactory_UnknownProvider verifies that an unsupported provider
// string returns an error.
func TestEmbedderFactory_UnknownProvider(t *testing.T) {
	ollaCfg := config.OllamaConfig{}
	embCfg := config.EmbedderConfig{Provider: "openai"} // no longer supported
	_, err := embedder.New(ollaCfg, embCfg, 768, slog.Default())
	if err == nil {
		t.Fatal("expected error for unknown provider 'openai', got nil")
	}
}

// TestLMStudioEmbedder_Dimension verifies that Dimension returns 0 (unknown
// at construction time).
func TestLMStudioEmbedder_Dimension(t *testing.T) {
	emb := embedder.NewLMStudioEmbedder("http://localhost:1234", "model")
	if got := emb.Dimension(); got != 0 {
		t.Errorf("Dimension() = %d, want 0", got)
	}
}

// TestLMStudioEmbedder_EmbedBatch_Success verifies that EmbedBatch returns
// one embedding per input text.
func TestLMStudioEmbedder_EmbedBatch_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]any{
			"data": []map[string]any{
				{"embedding": []float64{0.1, 0.2, 0.3}},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	emb := embedder.NewLMStudioEmbedder(srv.URL, "model")
	results, err := emb.EmbedBatch(context.Background(), []string{"hello", "world"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// containsString is a local helper to avoid importing strings in test bodies.
func containsString(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
