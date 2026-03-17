package llm

import (
	"context"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/ajitpratap0/openclaw-cortex/internal/metrics"
	"github.com/ajitpratap0/openclaw-cortex/internal/sentry"
)

// LLMClient is the interface for sending a single-turn completion to a language model.
// All implementations must be safe for concurrent use.
type LLMClient interface {
	Complete(ctx context.Context, model, systemPrompt, userMessage string, maxTokens int) (string, error)
}

// AnthropicClient wraps the Anthropic Go SDK and implements LLMClient.
type AnthropicClient struct {
	client *anthropic.Client
}

// NewAnthropicClient creates an AnthropicClient authenticated with apiKey.
func NewAnthropicClient(apiKey string) *AnthropicClient {
	c := anthropic.NewClient(option.WithAPIKey(apiKey))
	return &AnthropicClient{client: &c}
}

// Complete sends a single-turn request to the Anthropic Messages API and returns the
// first text block from the response.
func (a *AnthropicClient) Complete(ctx context.Context, model, systemPrompt, userMessage string, maxTokens int) (string, error) {
	finish := sentry.StartSpan(ctx, "llm.complete", "AnthropicClient.Complete")
	defer finish()
	metrics.LLMCallsTotal.WithLabelValues(model).Inc()
	resp, err := a.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: int64(maxTokens),
		System: []anthropic.TextBlockParam{
			{Text: systemPrompt},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(
				anthropic.NewTextBlock(userMessage),
			),
		},
	})
	if err != nil {
		metrics.LLMErrorsTotal.WithLabelValues(model).Inc()
		return "", fmt.Errorf("anthropic complete: %w", err)
	}

	for i := range resp.Content {
		if resp.Content[i].Type == "text" {
			return resp.Content[i].Text, nil
		}
	}
	return "", fmt.Errorf("anthropic complete: no text in response")
}
