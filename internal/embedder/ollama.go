package embedder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

const (
	ollamaHTTPTimeout   = 30 * time.Second
	ollamaMaxRetries    = 3
	ollamaRetryBaseWait = 500 * time.Millisecond
	embedBatchWorkers   = 8
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

// EmbedBatch embeds multiple texts using a bounded worker pool (up to embedBatchWorkers
// goroutines, capped at len(texts)). The pool is canceled on the first error so that
// in-flight Ollama requests are aborted early rather than running to completion.
func (o *OllamaEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	numWorkers := min(embedBatchWorkers, len(texts))

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type job struct {
		idx  int
		text string
	}
	jobs := make(chan job, len(texts))
	for i, t := range texts {
		jobs <- job{i, t}
	}
	close(jobs)

	results := make([][]float32, len(texts))
	errs := make([]error, len(texts))
	var once sync.Once
	var wg sync.WaitGroup

	for range numWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				if ctx.Err() != nil {
					errs[j.idx] = ctx.Err()
					continue
				}
				vec, err := o.Embed(ctx, j.text)
				results[j.idx] = vec
				errs[j.idx] = err
				if err != nil {
					once.Do(func() { cancel() })
				}
			}
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			return nil, fmt.Errorf("embedding text at index %d: %w", i, err)
		}
	}
	return results, nil
}

// Dimension returns the embedding vector dimension.
func (o *OllamaEmbedder) Dimension() int {
	return o.dimension
}
