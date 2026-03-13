package main

import (
	"fmt"
	"strings"

	"github.com/ajitpratap0/openclaw-cortex/internal/config"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/recall"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

// recallWeightsFromConfig converts the config weights struct into the recall
// package's Weights type, avoiding an 8-line struct literal duplicated across
// every command that creates a Recaller.
func recallWeightsFromConfig(c config.RecallWeightsConfig) recall.Weights {
	return recall.Weights{
		Similarity:    c.Similarity,
		Recency:       c.Recency,
		Frequency:     c.Frequency,
		TypeBoost:     c.TypeBoost,
		ScopeBoost:    c.ScopeBoost,
		Confidence:    c.Confidence,
		Reinforcement: c.Reinforcement,
		TagAffinity:   c.TagAffinity,
	}
}

// buildSearchFilters constructs a SearchFilters from optional CLI flag values.
// Returns nil if all inputs are empty.
func buildSearchFilters(cmdName, memType, memScope, project, tagsFlag string) (*store.SearchFilters, error) {
	if memType == "" && memScope == "" && project == "" && tagsFlag == "" {
		return nil, nil
	}
	filters := &store.SearchFilters{}
	if memType != "" {
		mt := models.MemoryType(memType)
		if !mt.IsValid() {
			return nil, fmt.Errorf("%s: invalid type %q (want: %s)", cmdName, memType, validTypesString())
		}
		filters.Type = &mt
	}
	if memScope != "" {
		ms := models.MemoryScope(memScope)
		if !ms.IsValid() {
			return nil, fmt.Errorf("%s: invalid scope %q (want: %s)", cmdName, memScope, validScopesString())
		}
		filters.Scope = &ms
	}
	if project != "" {
		filters.Project = &project
	}
	if tagsFlag != "" {
		filters.Tags = parseTags(tagsFlag)
	}
	return filters, nil
}

// parseTags splits a comma-separated tags string into trimmed individual tags.
func parseTags(tagsStr string) []string {
	parts := strings.Split(tagsStr, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}
