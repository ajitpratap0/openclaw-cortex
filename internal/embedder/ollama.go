package embedder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/ajitpratap0/openclaw-cortex/internal/sentry"
)

const (
	ollamaHTTPTimeout   = 30 * time.Second
	ollamaMaxRetries    = 3
	ollamaRetryBaseWait = 500 * time.Millisecond
)

// OllamaEmbedder implements Embedder using the Ollama HTTP API.
type OllamaEmbedder struct {
	baseURL   string
	model     string
	dimension int
	client    *http.Client
	logger    *slog.Logger
}

type ollamaEmbedRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type ollamaEmbedResponse struct {
	Embedding []float64 `json:"embedding"`
}

// ollamaBatchEmbedRequest uses the /api/embed endpoint which accepts multiple inputs.
type ollamaBatchEmbedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

// ollamaBatchEmbedResponse returns multiple embeddings from /api/embed.
type ollamaBatchEmbedResponse struct {
	Embeddings [][]float64 `json:"embeddings"`
}

// NewOllamaEmbedder creates a new Ollama-based embedder.
func NewOllamaEmbedder(baseURL, model string, dimension int, logger *slog.Logger) *OllamaEmbedder {
	return &OllamaEmbedder{
		baseURL:   baseURL,
		model:     model,
		dimension: dimension,
		client:    &http.Client{Timeout: ollamaHTTPTimeout},
		logger:    logger,
	}
}

// Embed returns a vector embedding for the given text using the Ollama API.
func (o *OllamaEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	finish := sentry.StartSpan(ctx, "embed.ollama", "OllamaEmbedder.Embed")
	defer finish()
	reqBody := ollamaEmbedRequest{
		Model:  o.model,
		Prompt: text,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	endpoint := o.baseURL + "/api/embeddings"

	var resp *http.Response
	for attempt := 0; attempt < ollamaMaxRetries; attempt++ {
		if attempt > 0 {
			wait := ollamaRetryBaseWait * time.Duration(1<<(attempt-1))
			o.logger.Warn("retrying Ollama request", "attempt", attempt+1, "wait", wait)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait):
			}
		}

		req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
		if reqErr != nil {
			return nil, fmt.Errorf("creating request: %w", reqErr)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err = o.client.Do(req)
		if err != nil {
			if attempt < ollamaMaxRetries-1 {
				continue
			}
			return nil, fmt.Errorf("calling Ollama API: %w", err)
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			_ = resp.Body.Close()
			if attempt < ollamaMaxRetries-1 {
				continue
			}
			return nil, fmt.Errorf("ollama API returned %d after %d attempts", resp.StatusCode, ollamaMaxRetries)
		}
		break
	}

	if resp == nil {
		return nil, fmt.Errorf("ollama API: no response after %d attempts", ollamaMaxRetries)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama API returned %d: %s", resp.StatusCode, string(body))
	}

	var result ollamaEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	if len(result.Embedding) == 0 {
		return nil, fmt.Errorf("ollama returned empty embedding")
	}

	// Convert float64 to float32 for Qdrant compatibility
	vec := make([]float32, len(result.Embedding))
	for i, v := range result.Embedding {
		vec[i] = float32(v)
	}

	o.logger.Debug("generated embedding", "model", o.model, "dimension", len(vec))
	return vec, nil
}

// EmbedBatch embeds multiple texts in a single HTTP call to Ollama's /api/embed
// endpoint, which accepts an array of inputs and returns all embeddings at once.
// This is dramatically faster than per-text calls (1 round-trip vs N).
func (o *OllamaEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	// Single text — use the standard Embed path.
	if len(texts) == 1 {
		vec, err := o.Embed(ctx, texts[0])
		if err != nil {
			return nil, err
		}
		return [][]float32{vec}, nil
	}

	reqBody := ollamaBatchEmbedRequest{
		Model: o.model,
		Input: texts,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("embed batch: marshaling request: %w", err)
	}

	endpoint := o.baseURL + "/api/embed"

	var resp *http.Response
	for attempt := 0; attempt < ollamaMaxRetries; attempt++ {
		if attempt > 0 {
			wait := ollamaRetryBaseWait * time.Duration(1<<(attempt-1))
			o.logger.Warn("retrying Ollama batch request", "attempt", attempt+1, "wait", wait)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait):
			}
		}

		req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
		if reqErr != nil {
			return nil, fmt.Errorf("embed batch: creating request: %w", reqErr)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err = o.client.Do(req)
		if err != nil {
			if attempt < ollamaMaxRetries-1 {
				continue
			}
			return nil, fmt.Errorf("embed batch: calling Ollama API: %w", err)
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			_ = resp.Body.Close()
			if attempt < ollamaMaxRetries-1 {
				continue
			}
			return nil, fmt.Errorf("embed batch: ollama API returned %d after %d attempts", resp.StatusCode, ollamaMaxRetries)
		}
		break
	}

	if resp == nil {
		return nil, fmt.Errorf("embed batch: no response after %d attempts", ollamaMaxRetries)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("embed batch: ollama API returned %d: %s", resp.StatusCode, string(body))
	}

	var result ollamaBatchEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("embed batch: decoding response: %w", err)
	}

	if len(result.Embeddings) != len(texts) {
		return nil, fmt.Errorf("embed batch: expected %d embeddings, got %d", len(texts), len(result.Embeddings))
	}

	// Convert float64 to float32.
	vectors := make([][]float32, len(result.Embeddings))
	for i, emb := range result.Embeddings {
		if len(emb) == 0 {
			return nil, fmt.Errorf("embed batch: empty embedding at index %d", i)
		}
		vec := make([]float32, len(emb))
		for j, v := range emb {
			vec[j] = float32(v)
		}
		vectors[i] = vec
	}

	o.logger.Debug("generated batch embeddings", "model", o.model, "count", len(vectors), "dimension", len(vectors[0]))
	return vectors, nil
}

// Dimension returns the embedding vector dimension.
func (o *OllamaEmbedder) Dimension() int {
	return o.dimension
}
