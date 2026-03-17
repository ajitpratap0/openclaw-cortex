package embedder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// LMStudioEmbedder implements Embedder using the LM Studio local server's
// OpenAI-compatible /v1/embeddings endpoint.
type LMStudioEmbedder struct {
	baseURL string
	model   string
	client  *http.Client
}

// NewLMStudioEmbedder creates a new LM Studio embedder pointed at baseURL
// using the given model name.
func NewLMStudioEmbedder(baseURL, model string) *LMStudioEmbedder {
	return &LMStudioEmbedder{
		baseURL: baseURL,
		model:   model,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// lmStudioRequest is the JSON body sent to /v1/embeddings.
type lmStudioRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

// lmStudioResponse is the JSON body returned by /v1/embeddings.
type lmStudioResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
}

// Embed returns a vector embedding for the given text by calling the LM Studio
// /v1/embeddings endpoint.
func (e *LMStudioEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	reqBody, err := json.Marshal(lmStudioRequest{Model: e.model, Input: text})
	if err != nil {
		return nil, fmt.Errorf("lmstudio embed: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/v1/embeddings", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("lmstudio embed: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("lmstudio embed: do request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("lmstudio embed: unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var result lmStudioResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("lmstudio embed: decode response: %w", err)
	}

	if len(result.Data) == 0 {
		return nil, fmt.Errorf("lmstudio embed: empty data array in response")
	}

	raw := result.Data[0].Embedding
	if len(raw) == 0 {
		return nil, fmt.Errorf("lmstudio embed: empty embedding vector in response")
	}

	out := make([]float32, len(raw))
	for i, v := range raw {
		out[i] = float32(v)
	}
	return out, nil
}

// EmbedBatch returns embeddings for multiple texts by calling Embed in a loop.
func (e *LMStudioEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	results := make([][]float32, len(texts))
	for i, text := range texts {
		vec, err := e.Embed(ctx, text)
		if err != nil {
			return nil, fmt.Errorf("lmstudio embed batch[%d]: %w", i, err)
		}
		results[i] = vec
	}
	return results, nil
}

// Dimension returns 0 — LM Studio does not report dimension at construction
// time; callers should use the length of the returned slice from Embed.
func (e *LMStudioEmbedder) Dimension() int {
	return 0
}
