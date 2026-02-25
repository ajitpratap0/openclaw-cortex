package embedder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
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
		client:    &http.Client{},
		logger:    logger,
	}
}

func (o *OllamaEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	reqBody := ollamaEmbedRequest{
		Model:  o.model,
		Prompt: text,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshalling request: %w", err)
	}

	url := o.baseURL + "/api/embeddings"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling Ollama API: %w", err)
	}
	defer resp.Body.Close()

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

func (o *OllamaEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	results := make([][]float32, 0, len(texts))
	for _, text := range texts {
		vec, err := o.Embed(ctx, text)
		if err != nil {
			return nil, fmt.Errorf("embedding text: %w", err)
		}
		results = append(results, vec)
	}
	return results, nil
}

func (o *OllamaEmbedder) Dimension() int {
	return o.dimension
}
