package embedder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"time"
)

const (
	openAIEmbedURL     = "https://api.openai.com/v1/embeddings"
	openAIHTTPTimeout  = 30 * time.Second
	openAIDefaultModel = "text-embedding-3-small"
	openAIDefaultDim   = 768

	openAIMaxRetries    = 3
	openAIMaxRetryAfter = 60 * time.Second
	maxResponseSize     = 10 * 1024 * 1024 // 10 MB
)

// OpenAIEmbedder implements Embedder using the OpenAI Embeddings API.
// It uses text-embedding-3-small with a configurable dimensions parameter
// to maintain compatibility with existing Qdrant collections.
type OpenAIEmbedder struct {
	apiKey     string
	model      string
	dimensions int
	endpointURL string
	client     *http.Client
	logger     *slog.Logger
}

// openAIEmbedRequest is the JSON body sent to the OpenAI embeddings endpoint.
type openAIEmbedRequest struct {
	Model      string   `json:"model"`
	Input      []string `json:"input"`
	Dimensions int      `json:"dimensions,omitempty"`
}

// openAIEmbedData is one item in the OpenAI embeddings response data array.
type openAIEmbedData struct {
	Embedding []float32 `json:"embedding"`
	Index     int       `json:"index"`
}

// openAIEmbedResponse is the full JSON response from the OpenAI embeddings endpoint.
type openAIEmbedResponse struct {
	Data []openAIEmbedData `json:"data"`
}

// openAIErrorResponse is the JSON error body returned by the OpenAI API on failure.
type openAIErrorResponse struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

// NewOpenAIEmbedder creates a new OpenAI-based embedder.
//
// apiKey is the OpenAI API key (required).
// model defaults to "text-embedding-3-small" when empty.
// dimensions defaults to 768 when 0, enabling compatibility with existing Qdrant collections.
func NewOpenAIEmbedder(apiKey, model string, dimensions int, logger *slog.Logger) *OpenAIEmbedder {
	return NewOpenAIEmbedderWithURL(openAIEmbedURL, apiKey, model, dimensions, logger)
}

// NewOpenAIEmbedderWithURL creates a new OpenAI-based embedder with a custom endpoint URL.
// This is intended for testing with a local httptest server; production code should use
// NewOpenAIEmbedder instead.
func NewOpenAIEmbedderWithURL(endpointURL, apiKey, model string, dimensions int, logger *slog.Logger) *OpenAIEmbedder {
	if model == "" {
		model = openAIDefaultModel
	}
	if dimensions <= 0 {
		dimensions = openAIDefaultDim
	}
	return &OpenAIEmbedder{
		apiKey:      apiKey,
		model:       model,
		dimensions:  dimensions,
		endpointURL: endpointURL,
		client:      &http.Client{Timeout: openAIHTTPTimeout},
		logger:      logger,
	}
}

// Embed returns a vector embedding for the given text using the OpenAI API.
func (o *OpenAIEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	vecs, err := o.embedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("openai embedder: no embedding returned")
	}
	return vecs[0], nil
}

// EmbedBatch returns vector embeddings for multiple texts in a single API call.
func (o *OpenAIEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	return o.embedBatch(ctx, texts)
}

// Dimension returns the configured embedding dimension.
func (o *OpenAIEmbedder) Dimension() int {
	return o.dimensions
}

// embedBatch calls the OpenAI embeddings API with a slice of input strings.
// The response items are sorted by index before being returned so the output
// order always matches the input order.
// It retries up to openAIMaxRetries times on 429 and 5xx responses.
func (o *OpenAIEmbedder) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	reqBody := openAIEmbedRequest{
		Model:      o.model,
		Input:      texts,
		Dimensions: o.dimensions,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("openai embedder: marshaling request: %w", err)
	}

	var (
		resp    *http.Response
		rawBody []byte
	)

	for attempt := 0; attempt < openAIMaxRetries; attempt++ {
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, o.endpointURL, bytes.NewReader(bodyBytes))
		if reqErr != nil {
			return nil, fmt.Errorf("openai embedder: creating request: %w", reqErr)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+o.apiKey)

		resp, err = o.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("openai embedder: calling API: %w", err)
		}

		rawBody, err = io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
		_ = resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("openai embedder: reading response body: %w", err)
		}

		// Retry on 429 (rate limit) and 5xx (server errors).
		if resp.StatusCode == http.StatusTooManyRequests {
			if attempt < openAIMaxRetries-1 {
				wait := parseRetryAfter(resp.Header.Get("Retry-After"), openAIMaxRetryAfter)
				o.logger.Warn("openai rate limited, retrying", "attempt", attempt+1, "wait", wait)
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(wait):
				}
				continue
			}
		} else if resp.StatusCode >= 500 {
			if attempt < openAIMaxRetries-1 {
				wait := time.Duration(1<<attempt) * time.Second // 1s, 2s, 4s
				o.logger.Warn("openai server error, retrying", "attempt", attempt+1, "status", resp.StatusCode, "wait", wait)
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(wait):
				}
				continue
			}
		}

		// Non-retryable response or final attempt — exit loop.
		break
	}

	if resp.StatusCode != http.StatusOK {
		var apiErr openAIErrorResponse
		if jsonErr := json.Unmarshal(rawBody, &apiErr); jsonErr == nil && apiErr.Error.Message != "" {
			return nil, fmt.Errorf("openai embedder: API error %d: %s", resp.StatusCode, apiErr.Error.Message)
		}
		bodyPreview := string(rawBody)
		if len(bodyPreview) > 512 {
			bodyPreview = bodyPreview[:512] + "..."
		}
		return nil, fmt.Errorf("openai embedder: API returned %d: %s", resp.StatusCode, bodyPreview)
	}

	var result openAIEmbedResponse
	if err = json.Unmarshal(rawBody, &result); err != nil {
		return nil, fmt.Errorf("openai embedder: decoding response: %w", err)
	}

	if len(result.Data) == 0 {
		return nil, fmt.Errorf("openai embedder: no embeddings in response")
	}

	// Sort by index to guarantee output matches input order.
	sort.Slice(result.Data, func(i, j int) bool {
		return result.Data[i].Index < result.Data[j].Index
	})

	vecs := make([][]float32, len(result.Data))
	for i := range result.Data {
		vecs[i] = result.Data[i].Embedding
	}

	o.logger.Debug("generated embeddings via OpenAI", "model", o.model, "count", len(vecs), "dimension", o.dimensions)
	return vecs, nil
}

// parseRetryAfter parses the Retry-After header value (seconds as integer) and
// returns a wait duration capped at maxWait. Falls back to 1 second if the
// header is absent or cannot be parsed.
func parseRetryAfter(header string, maxWait time.Duration) time.Duration {
	if header == "" {
		return time.Second
	}
	secs, err := strconv.Atoi(header)
	if err != nil || secs <= 0 {
		return time.Second
	}
	wait := time.Duration(secs) * time.Second
	if wait > maxWait {
		return maxWait
	}
	return wait
}
