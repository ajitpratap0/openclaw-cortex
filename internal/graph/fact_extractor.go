package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ajitpratap0/openclaw-cortex/internal/llm"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/pkg/xmlutil"
)

// factExtractionPromptTemplate is the prompt used to extract relationship facts.
// User content is injected via XML tags to prevent prompt injection attacks.
const factExtractionPromptTemplate = `You are a fact extractor. Extract relationship facts from the conversation text.

For each fact provide:
- source_entity_name: must match one of the KNOWN ENTITIES exactly
- target_entity_name: must match one of the KNOWN ENTITIES exactly
- relation_type: must be one of the CANONICAL RELATION TYPES listed below; use RELATES_TO if none fits
- fact: natural language description, paraphrased (not verbatim quotes)
- valid_at: ISO 8601 if the text states when the fact became true, null if ongoing or unknown
- invalid_at: ISO 8601 if the text states when the fact ended, null otherwise

<canonical_relation_types>
WORKS_AT, HAS_ROLE, LOCATED_IN, MARRIED_TO, REPORTS_TO, EMPLOYED_BY, LIVES_IN, BASED_IN,
CEO_OF, LEADS, USES, DEPENDS_ON, DECIDED_TO, KNOWS, HAS_SKILL, PART_OF, COLLABORATES_WITH,
IMPLEMENTS, MANAGES, RELATES_TO
</canonical_relation_types>

Direction matters: source_entity_name performs the action; target_entity_name is the recipient.
Example: Alice WORKS_AT Acme Corp → source=Alice, target=Acme Corp.

Rules:
- Only extract facts between two DISTINCT known entities
- Do not invent entities not in the provided list
- Set valid_at to null for ongoing facts with no known start date
- Only set invalid_at when the text explicitly states something has ended
- Do not hallucinate temporal bounds — leave null when uncertain

<known_entities>%s</known_entities>
<content>%s</content>
<reference_time>%s</reference_time>

Return JSON array of facts. Return [] if no relationship facts found.`

// rawFact is the JSON shape returned by Claude for fact extraction.
type rawFact struct {
	SourceEntityName string  `json:"source_entity_name"`
	TargetEntityName string  `json:"target_entity_name"`
	RelationType     string  `json:"relation_type"`
	Fact             string  `json:"fact"`
	ValidAt          *string `json:"valid_at"`
	InvalidAt        *string `json:"invalid_at"`
}

// FactExtractor extracts relationship facts from conversation text using Claude.
type FactExtractor struct {
	client llm.LLMClient
	model  string
	logger *slog.Logger
}

// NewFactExtractor creates a new fact extractor backed by the Claude API.
func NewFactExtractor(client llm.LLMClient, model string, logger *slog.Logger) *FactExtractor {
	if logger == nil {
		logger = slog.Default()
	}
	return &FactExtractor{
		client: client,
		model:  model,
		logger: logger,
	}
}

// Extract extracts relationship facts from conversation text.
// entityNames is the list of known entity names from the extraction step.
// On API error it logs a warning and returns (nil, nil) for graceful degradation.
//
// NOTE: The returned facts have SourceEntityID and TargetEntityID set to entity
// NAMES (not UUIDs). The caller is responsible for resolving names to IDs after
// extraction, since Claude returns names and the ID mapping is external.
func (e *FactExtractor) Extract(ctx context.Context, content string, entityNames []string) ([]models.Fact, error) {
	if len(entityNames) == 0 {
		e.logger.Debug("fact extraction: no known entities, skipping")
		return nil, nil
	}

	if e.client == nil {
		e.logger.Warn("fact extraction: no LLM client configured, skipping")
		return nil, nil
	}

	namesJoined := strings.Join(entityNames, ", ")
	prompt := fmt.Sprintf(factExtractionPromptTemplate,
		xmlutil.Escape(namesJoined),
		xmlutil.Escape(content),
		time.Now().UTC().Format(time.RFC3339),
	)

	responseText, err := e.client.Complete(ctx, e.model,
		"You are a precise fact extraction system. Output only valid JSON.",
		prompt,
		2048,
	)
	if err != nil {
		e.logger.Warn("fact extraction: Claude API error, skipping", "error", err)
		return nil, nil
	}

	if responseText == "" {
		e.logger.Warn("fact extraction: empty response from Claude")
		return nil, nil
	}

	e.logger.Debug("fact extraction response", "response", responseText)

	responseText = llm.StripCodeFences(responseText)
	var raw []rawFact
	if jsonErr := json.Unmarshal([]byte(responseText), &raw); jsonErr != nil {
		return nil, fmt.Errorf("fact extraction: parsing response: %w (raw: %s)", jsonErr, responseText)
	}

	// Build a set of known entity names for validation.
	knownSet := make(map[string]struct{}, len(entityNames))
	for i := range entityNames {
		knownSet[entityNames[i]] = struct{}{}
	}

	now := time.Now().UTC()
	facts := make([]models.Fact, 0, len(raw))
	for i := range raw {
		// Skip facts referencing unknown entities.
		if _, ok := knownSet[raw[i].SourceEntityName]; !ok {
			e.logger.Warn("fact extraction: unknown source entity, skipping",
				"source", raw[i].SourceEntityName)
			continue
		}
		if _, ok := knownSet[raw[i].TargetEntityName]; !ok {
			e.logger.Warn("fact extraction: unknown target entity, skipping",
				"target", raw[i].TargetEntityName)
			continue
		}
		// Skip self-referential facts.
		if raw[i].SourceEntityName == raw[i].TargetEntityName {
			e.logger.Warn("fact extraction: self-referential fact, skipping",
				"entity", raw[i].SourceEntityName)
			continue
		}

		fact := models.Fact{
			ID:             uuid.New().String(),
			SourceEntityID: raw[i].SourceEntityName, // Name, not UUID — caller resolves
			TargetEntityID: raw[i].TargetEntityName, // Name, not UUID — caller resolves
			RelationType:   raw[i].RelationType,
			Fact:           raw[i].Fact,
			CreatedAt:      now,
			Confidence:     0.8, // initial extraction confidence
		}

		if raw[i].ValidAt != nil {
			t, parseErr := time.Parse(time.RFC3339, *raw[i].ValidAt)
			if parseErr == nil {
				fact.ValidAt = &t
			} else {
				e.logger.Warn("fact extraction: invalid valid_at timestamp",
					"valid_at", *raw[i].ValidAt, "error", parseErr)
			}
		}
		if raw[i].InvalidAt != nil {
			t, parseErr := time.Parse(time.RFC3339, *raw[i].InvalidAt)
			if parseErr == nil {
				fact.InvalidAt = &t
			} else {
				e.logger.Warn("fact extraction: invalid invalid_at timestamp",
					"invalid_at", *raw[i].InvalidAt, "error", parseErr)
			}
		}

		facts = append(facts, fact)
	}

	e.logger.Info("extracted facts", "count", len(facts))
	return facts, nil
}
