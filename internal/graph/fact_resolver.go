package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/pkg/xmlutil"
)

// factResolutionPromptTemplate is the prompt used to determine if a new fact
// duplicates or contradicts existing facts between the same entity pair.
// User content is XML-escaped before interpolation to prevent prompt injection.
const factResolutionPromptTemplate = `You are a fact resolution system. Determine if the NEW FACT duplicates or contradicts any existing facts.

EXISTING FACTS (same entity pair):
%s

NEW FACT: %s

A duplicate means semantically the same fact, possibly with updated details.
A contradiction means the new fact directly invalidates an old one.

Return JSON: {"duplicate_indices": [int], "contradicted_indices": [int]}`

// factResolutionResponse is the expected JSON response from Claude.
type factResolutionResponse struct {
	DuplicateIndices    []int `json:"duplicate_indices"`
	ContradictedIndices []int `json:"contradicted_indices"`
}

// FactResolver determines whether a new fact is a duplicate, contradiction, or new
// by comparing it against existing facts between the same entity pair.
type FactResolver struct {
	graph  Client
	client *anthropic.Client
	model  string
	logger *slog.Logger
}

// NewFactResolver creates a new FactResolver.
// If apiKey is empty, the resolver will treat all facts as new (graceful degradation).
func NewFactResolver(graph Client, apiKey, model string, logger *slog.Logger) *FactResolver {
	if logger == nil {
		logger = slog.Default()
	}
	var client *anthropic.Client
	if apiKey != "" {
		c := anthropic.NewClient(option.WithAPIKey(apiKey))
		client = &c
	}
	return &FactResolver{
		graph:  graph,
		client: client,
		model:  model,
		logger: logger,
	}
}

// Resolve determines if a new fact is a duplicate, contradiction, or new.
// Returns (action, affectedFactIDs, error).
func (r *FactResolver) Resolve(ctx context.Context, newFact models.Fact, conversationContext string) (FactAction, []string, error) {
	// Retrieve candidate facts between the same entity pair.
	candidates, err := r.graph.GetFactsBetween(ctx, newFact.SourceEntityID, newFact.TargetEntityID)
	if err != nil {
		return FactActionInsert, nil, fmt.Errorf("fact resolver: get facts between entities: %w", err)
	}

	// Fast path: no existing facts → insert.
	if len(candidates) == 0 {
		return FactActionInsert, nil, nil
	}

	// Fast path: exact text + endpoint match → skip (duplicate).
	for i := range candidates {
		if candidates[i].Fact == newFact.Fact &&
			candidates[i].SourceEntityID == newFact.SourceEntityID &&
			candidates[i].TargetEntityID == newFact.TargetEntityID {
			return FactActionSkip, []string{candidates[i].ID}, nil
		}
	}

	// Graceful degradation: no API client → treat as new.
	if r.client == nil {
		r.logger.Warn("fact resolver: no API key configured, treating fact as new")
		return FactActionInsert, nil, nil
	}

	// Build the numbered list of existing facts for the prompt.
	var sb strings.Builder
	for i := range candidates {
		fmt.Fprintf(&sb, "%d. %s\n", i, xmlutil.Escape(candidates[i].Fact))
	}

	prompt := fmt.Sprintf(factResolutionPromptTemplate, sb.String(), xmlutil.Escape(newFact.Fact))

	resp, err := r.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(r.model),
		MaxTokens: 512,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(
				anthropic.NewTextBlock(prompt),
			),
		},
		System: []anthropic.TextBlockParam{
			{Text: "You are a precise fact resolution system. Output only valid JSON."},
		},
	})
	if err != nil {
		// On API error, treat as new fact (safe default, graceful degradation).
		r.logger.Warn("fact resolver: Claude API error, treating as new fact", "error", err)
		return FactActionInsert, nil, nil
	}

	var responseText string
	for i := range resp.Content {
		if resp.Content[i].Type == "text" {
			responseText = resp.Content[i].Text
			break
		}
	}

	if responseText == "" {
		r.logger.Warn("fact resolver: empty response from Claude, treating as new fact")
		return FactActionInsert, nil, nil
	}

	r.logger.Debug("fact resolution response", "response", responseText)

	var result factResolutionResponse
	if jsonErr := json.Unmarshal([]byte(responseText), &result); jsonErr != nil {
		r.logger.Warn("fact resolver: failed to parse Claude response, treating as new fact",
			"error", jsonErr, "raw", responseText)
		return FactActionInsert, nil, nil
	}

	// Contradictions take priority over duplicates.
	if len(result.ContradictedIndices) > 0 {
		ids := make([]string, 0, len(result.ContradictedIndices))
		for i := range result.ContradictedIndices {
			idx := result.ContradictedIndices[i]
			if idx >= 0 && idx < len(candidates) {
				ids = append(ids, candidates[idx].ID)
			}
		}
		if len(ids) > 0 {
			return FactActionInvalidate, ids, nil
		}
	}

	if len(result.DuplicateIndices) > 0 {
		ids := make([]string, 0, len(result.DuplicateIndices))
		for i := range result.DuplicateIndices {
			idx := result.DuplicateIndices[i]
			if idx >= 0 && idx < len(candidates) {
				ids = append(ids, candidates[idx].ID)
			}
		}
		if len(ids) > 0 {
			return FactActionSkip, ids, nil
		}
	}

	return FactActionInsert, nil, nil
}
