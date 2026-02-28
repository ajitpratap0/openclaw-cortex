package capture

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/google/uuid"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/pkg/xmlutil"
)

// entityExtractionPromptTemplate is the prompt used to identify entities in memory content.
// User content is injected via an XML tag to prevent prompt injection attacks.
const entityExtractionPromptTemplate = `You are an entity extraction system. Analyze the text and identify named entities.

For each entity provide:
- name: The canonical name of the entity
- type: One of "person", "project", "system", "decision", "concept"
  - person: A named individual
  - project: A named project, product, or initiative
  - system: A named technical system, service, or tool
  - decision: A named decision, choice, or resolved question
  - concept: A named domain concept, idea, or abstraction
- aliases: Alternative names or abbreviations (may be empty)

Return a JSON array of entities. If no notable entities are found, return [].

<content>%s</content>

Extract entities as JSON array:`

// capturedEntity is the raw JSON shape returned by Claude for entity extraction.
type capturedEntity struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Aliases []string `json:"aliases"`
}

// EntityExtractor identifies named entities in memory content using Claude.
type EntityExtractor struct {
	client *anthropic.Client
	model  string
	logger *slog.Logger
}

// NewEntityExtractor creates a new entity extractor backed by the Claude API.
func NewEntityExtractor(apiKey, model string, logger *slog.Logger) *EntityExtractor {
	if logger == nil {
		logger = slog.Default()
	}
	c := anthropic.NewClient(option.WithAPIKey(apiKey))
	return &EntityExtractor{
		client: &c,
		model:  model,
		logger: logger,
	}
}

// Extract identifies entities in the given content using Claude.
// On API error it logs a warning and returns (nil, nil) for graceful degradation.
func (e *EntityExtractor) Extract(ctx context.Context, content string) ([]models.Entity, error) {
	prompt := fmt.Sprintf(entityExtractionPromptTemplate, xmlutil.Escape(content))

	resp, err := e.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(e.model),
		MaxTokens: 1024,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(
				anthropic.NewTextBlock(prompt),
			),
		},
		System: []anthropic.TextBlockParam{
			{Text: "You are a precise entity extraction system. Output only valid JSON."},
		},
	})
	if err != nil {
		e.logger.Warn("entity extraction: Claude API error, skipping", "error", err)
		return nil, nil
	}

	var responseText string
	for i := range resp.Content {
		if resp.Content[i].Type == "text" {
			responseText = resp.Content[i].Text
			break
		}
	}

	if responseText == "" {
		e.logger.Warn("entity extraction: empty response from Claude")
		return nil, nil
	}

	e.logger.Debug("entity extraction response", "response", responseText)

	var raw []capturedEntity
	if jsonErr := json.Unmarshal([]byte(responseText), &raw); jsonErr != nil {
		return nil, fmt.Errorf("entity extraction: parsing response: %w (raw: %s)", jsonErr, responseText)
	}

	entities := make([]models.Entity, 0, len(raw))
	for i := range raw {
		et := models.EntityType(raw[i].Type)
		if !et.IsValid() {
			e.logger.Warn("entity extraction: unknown entity type, defaulting to concept",
				"type", raw[i].Type, "name", raw[i].Name)
			et = models.EntityTypeConcept
		}
		entities = append(entities, models.Entity{
			ID:      uuid.New().String(),
			Name:    raw[i].Name,
			Type:    et,
			Aliases: raw[i].Aliases,
		})
	}

	e.logger.Info("extracted entities", "count", len(entities))
	return entities, nil
}
