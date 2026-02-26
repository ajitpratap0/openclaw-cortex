package capture

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
)

// Capturer extracts structured memories from conversation text.
type Capturer interface {
	Extract(ctx context.Context, userMsg, assistantMsg string) ([]models.CapturedMemory, error)
}

// ClaudeCapturer uses Claude Haiku to extract memories.
type ClaudeCapturer struct {
	client *anthropic.Client
	model  string
	logger *slog.Logger
}

// NewCapturer creates a new Claude-based memory capturer.
func NewCapturer(apiKey, model string, logger *slog.Logger) *ClaudeCapturer {
	client := anthropic.NewClient(option.WithAPIKey(apiKey))
	return &ClaudeCapturer{
		client: &client,
		model:  model,
		logger: logger,
	}
}

// extractionPromptTemplate is the base prompt; user/assistant content is injected via XML tags
// to prevent prompt injection attacks.
const extractionPromptTemplate = `You are a memory extraction system. Analyze the conversation and extract discrete, reusable memories.

For each memory, provide:
- content: The memory text (concise, standalone, factual)
- type: One of "rule", "fact", "episode", "procedure", "preference"
  - rule: Operating principles, hard constraints, invariants
  - fact: Declarative knowledge, definitions, relationships
  - episode: Specific events with temporal context
  - procedure: How-to steps, processes, workflows
  - preference: User preferences, style choices, opinions
- confidence: 0.0-1.0 how confident you are this is a real memory
- tags: Relevant keywords for categorization

Return JSON array. If no memories worth extracting, return empty array [].

<user_message>%s</user_message>

<assistant_message>%s</assistant_message>

Extract memories as JSON array:`

type extractionResponse struct {
	Memories []models.CapturedMemory `json:"memories"`
}

func (c *ClaudeCapturer) Extract(ctx context.Context, userMsg, assistantMsg string) ([]models.CapturedMemory, error) {
	// Use XML delimiters to prevent prompt injection from user/assistant content.
	prompt := fmt.Sprintf(extractionPromptTemplate, userMsg, assistantMsg)

	resp, err := c.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(c.model),
		MaxTokens: 2048,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(
				anthropic.NewTextBlock(prompt),
			),
		},
		System: []anthropic.TextBlockParam{
			{Text: "You are a precise memory extraction system. Output only valid JSON."},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("calling Claude API: %w", err)
	}

	// Extract text from response
	var responseText string
	for _, block := range resp.Content {
		if block.Type == "text" {
			responseText = block.Text
			break
		}
	}

	if responseText == "" {
		return nil, fmt.Errorf("empty response from Claude")
	}

	c.logger.Debug("claude extraction response", "response", responseText)

	// Try to parse as array directly
	var memories []models.CapturedMemory
	if err := json.Unmarshal([]byte(responseText), &memories); err != nil {
		// Try wrapped format
		var wrapped extractionResponse
		if err2 := json.Unmarshal([]byte(responseText), &wrapped); err2 != nil {
			return nil, fmt.Errorf("parsing extraction response: %w (raw: %s)", err, responseText)
		}
		memories = wrapped.Memories
	}

	// Filter out low-confidence extractions
	var filtered []models.CapturedMemory
	for _, m := range memories {
		if m.Confidence >= 0.5 {
			filtered = append(filtered, m)
		}
	}

	c.logger.Info("extracted memories", "total", len(memories), "filtered", len(filtered))
	return filtered, nil
}
