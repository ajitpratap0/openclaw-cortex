package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ajitpratap0/openclaw-cortex/internal/llm"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/pkg/vecmath"
	"github.com/ajitpratap0/openclaw-cortex/pkg/xmlutil"
)

// entityResolutionPromptTemplate is the Claude prompt for stage-3 duplicate detection.
// User content is XML-escaped before interpolation to prevent prompt injection.
const entityResolutionPromptTemplate = `You are an entity resolution system. Determine if the NEW ENTITY is a duplicate
of any EXISTING ENTITY. Entities are duplicates only if they refer to the same
real-world object or concept. Semantic equivalence is allowed (e.g., "the CEO"
= "John Smith" if context makes it clear).

<new_entity>Name: %s, Type: %s</new_entity>
<existing_entities>%s</existing_entities>
<context>%s</context>

Return JSON: {"is_duplicate": bool, "existing_id": "id or empty"}`

// resolutionResponse is the JSON shape returned by Claude for entity resolution.
type resolutionResponse struct {
	IsDuplicate bool   `json:"is_duplicate"`
	ExistingID  string `json:"existing_id"`
}

// EntityResolver implements three-stage entity resolution:
// 1. Candidate retrieval via graph search
// 2. Deterministic fast-path (exact name, alias, embedding cosine)
// 3. Claude Haiku fallback for ambiguous cases
type EntityResolver struct {
	graph     Client
	client    llm.LLMClient
	model     string
	threshold float64
	maxCands  int
	logger    *slog.Logger
}

// NewEntityResolver creates a new EntityResolver.
// If client is nil, stage 3 (Claude fallback) is disabled and ambiguous entities
// are treated as new.
func NewEntityResolver(graph Client, client llm.LLMClient, model string, threshold float64, maxCands int, logger *slog.Logger) *EntityResolver {
	if logger == nil {
		logger = slog.Default()
	}
	return &EntityResolver{
		graph:     graph,
		client:    client,
		model:     model,
		threshold: threshold,
		maxCands:  maxCands,
		logger:    logger,
	}
}

// Resolve performs three-stage entity resolution and returns (resolvedID, isNew, err).
// If the entity matches an existing one, resolvedID is the existing entity's ID and
// isNew is false. Otherwise, resolvedID is the extracted entity's ID and isNew is true.
func (r *EntityResolver) Resolve(ctx context.Context, extracted models.Entity, embedding []float32, conversationContext string) (string, bool, error) {
	// Stage 1: Candidate retrieval
	candidates, err := r.graph.SearchEntities(ctx, extracted.Name, embedding, extracted.Project, r.maxCands)
	if err != nil {
		return "", false, fmt.Errorf("entity resolution: search candidates: %w", err)
	}

	if len(candidates) == 0 {
		r.logger.Debug("entity resolution: no candidates, treating as new", "name", extracted.Name)
		return extracted.ID, true, nil
	}

	// Stage 2: Deterministic fast-path
	resolvedID, matched := r.deterministicMatch(ctx, extracted, embedding, candidates)
	if matched {
		r.logger.Debug("entity resolution: deterministic match found", "name", extracted.Name, "resolved_id", resolvedID)
		return resolvedID, false, nil
	}

	// Stage 3: Claude Haiku fallback
	resolvedID, matched, claudeErr := r.claudeFallback(ctx, extracted, candidates, conversationContext)
	if claudeErr != nil {
		// On Claude error, treat as new entity (safe default)
		r.logger.Warn("entity resolution: Claude fallback error, treating as new",
			"name", extracted.Name, "error", claudeErr)
		return extracted.ID, true, nil
	}
	if matched {
		r.logger.Debug("entity resolution: Claude match found", "name", extracted.Name, "resolved_id", resolvedID)
		return resolvedID, false, nil
	}

	r.logger.Debug("entity resolution: no match found, treating as new", "name", extracted.Name)
	return extracted.ID, true, nil
}

// deterministicMatch checks candidates for exact name match, alias match, or
// high cosine similarity. Returns (resolvedID, matched).
func (r *EntityResolver) deterministicMatch(ctx context.Context, extracted models.Entity, embedding []float32, candidates []EntityResult) (string, bool) {
	for i := range candidates {
		// Exact name match (case-insensitive)
		if strings.EqualFold(candidates[i].Name, extracted.Name) {
			return candidates[i].ID, true
		}
	}

	// Alias match: fetch full entities to check aliases
	for i := range candidates {
		fullEntity, getErr := r.graph.GetEntity(ctx, candidates[i].ID)
		if getErr != nil {
			r.logger.Warn("entity resolution: failed to get entity for alias check",
				"id", candidates[i].ID, "error", getErr)
			continue
		}

		for j := range fullEntity.Aliases {
			if strings.EqualFold(fullEntity.Aliases[j], extracted.Name) {
				return candidates[i].ID, true
			}
		}

		// Also check if the new entity's aliases match the candidate name
		for j := range extracted.Aliases {
			if strings.EqualFold(extracted.Aliases[j], fullEntity.Name) {
				return candidates[i].ID, true
			}
		}

		// Embedding cosine similarity check
		if len(embedding) > 0 && len(fullEntity.NameEmbedding) > 0 {
			sim := vecmath.CosineSimilarity(embedding, fullEntity.NameEmbedding)
			if sim > r.threshold {
				return candidates[i].ID, true
			}
		}
	}

	return "", false
}

// claudeFallback asks Claude whether the extracted entity is a duplicate of any candidate.
// Returns (resolvedID, matched, error).
func (r *EntityResolver) claudeFallback(ctx context.Context, extracted models.Entity, candidates []EntityResult, conversationContext string) (string, bool, error) {
	if r.client == nil {
		return "", false, fmt.Errorf("claude API not configured")
	}

	// Build numbered list of candidates
	var sb strings.Builder
	for i := range candidates {
		fmt.Fprintf(&sb, "%d. ID=%s, Name=%s, Type=%s\n",
			i+1,
			xmlutil.Escape(candidates[i].ID),
			xmlutil.Escape(candidates[i].Name),
			xmlutil.Escape(candidates[i].Type))
	}

	prompt := fmt.Sprintf(entityResolutionPromptTemplate,
		xmlutil.Escape(extracted.Name),
		xmlutil.Escape(string(extracted.Type)),
		sb.String(),
		xmlutil.Escape(conversationContext),
	)

	responseText, apiErr := r.client.Complete(ctx, r.model,
		"You are a precise entity resolution system. Output only valid JSON.",
		prompt,
		256,
	)
	if apiErr != nil {
		return "", false, fmt.Errorf("claude API call: %w", apiErr)
	}

	if responseText == "" {
		return "", false, fmt.Errorf("empty response from Claude")
	}

	responseText = llm.StripCodeFences(responseText)
	var result resolutionResponse
	if jsonErr := json.Unmarshal([]byte(responseText), &result); jsonErr != nil {
		return "", false, fmt.Errorf("parsing Claude response: %w (raw: %s)", jsonErr, responseText)
	}

	if result.IsDuplicate && result.ExistingID != "" {
		// Validate that Claude returned an ID from our candidate set
		found := false
		for _, c := range candidates {
			if c.ID == result.ExistingID {
				found = true
				break
			}
		}
		if !found {
			return extracted.ID, true, nil // treat as new if hallucinated ID
		}
		return result.ExistingID, true, nil
	}

	return "", false, nil
}
