package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/ajitpratap0/openclaw-cortex/internal/config"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/recall"
	"github.com/ajitpratap0/openclaw-cortex/internal/sentry"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

// cmdErr wraps an error with context and reports it to Sentry.
// Returns nil if err is nil.
func cmdErr(context string, err error) error {
	if err == nil {
		return nil
	}
	wrapped := fmt.Errorf("%s: %w", context, err)
	sentry.CaptureException(wrapped)
	return wrapped
}

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

// parseTimeFlag parses a time flag value that can be an ISO 8601 datetime
// (e.g. "2026-03-01" or "2026-03-01T15:00:00Z") or a relative duration
// subtracted from now (e.g. "7d", "1m", "2h" — same syntax as --valid-until).
// Returns an error prefixed with cmdName and flagName for clear CLI error messages.
func parseTimeFlag(cmdName, flagName, s string) (time.Time, error) {
	// Try ISO 8601 date-only first (YYYY-MM-DD).
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.UTC(), nil
	}
	// Try full RFC3339.
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	// Try relative duration (subtract from now).
	dur, err := parseDuration(s)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s: invalid %s %q: must be ISO 8601 date (2006-01-02), RFC3339, or relative duration (7d, 1m, 2h)", cmdName, flagName, s)
	}
	return time.Now().UTC().Add(-dur), nil
}

// parseTags splits a comma-separated tags string into trimmed individual tags.
func parseTags(tagsStr string) []string {
	parts := strings.Split(tagsStr, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}
