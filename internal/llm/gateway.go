package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// GatewayClient sends completions through an OpenAI-compatible HTTP gateway.
// It implements LLMClient.
type GatewayClient struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewGatewayClient creates a GatewayClient that POSTs to baseURL/v1/chat/completions
// authenticated with token.
func NewGatewayClient(baseURL, token string) *GatewayClient {
	return &GatewayClient{
		baseURL: baseURL,
		token:   token,
		http:    &http.Client{},
	}
}

// gatewayRequest is the JSON body sent to the OpenAI-compatible endpoint.
type gatewayRequest struct {
	Model     string           `json:"model"`
	Messages  []gatewayMessage `json:"messages"`
	MaxTokens int              `json:"max_tokens"`
}

type gatewayMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// gatewayResponse is a minimal representation of the OpenAI chat completions response.
type gatewayResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Complete sends a single-turn request to the gateway and returns the model reply.
func (g *GatewayClient) Complete(ctx context.Context, model, systemPrompt, userMessage string, maxTokens int) (string, error) {
	reqBody := gatewayRequest{
		Model: model,
		Messages: []gatewayMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userMessage},
		},
		MaxTokens: maxTokens,
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("gateway complete: marshal request: %w", err)
	}

	url := g.baseURL + "/v1/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("gateway complete: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+g.token)

	resp, err := g.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("gateway complete: do request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("gateway complete: read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("gateway complete: unexpected status %d: %s", resp.StatusCode, body)
	}

	var gwResp gatewayResponse
	if err = json.Unmarshal(body, &gwResp); err != nil {
		return "", fmt.Errorf("gateway complete: unmarshal response: %w", err)
	}

	if gwResp.Error != nil {
		return "", fmt.Errorf("gateway complete: api error: %s", gwResp.Error.Message)
	}

	if len(gwResp.Choices) == 0 {
		return "", fmt.Errorf("gateway complete: no choices in response")
	}

	return gwResp.Choices[0].Message.Content, nil
}
