package memgraph

import (
	"strings"
	"testing"
	"time"

	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

// TestBuildWhereClause_DefaultExcludesInvalidated verifies that without IncludeInvalidated,
// the generated WHERE clause filters out memories with valid_to set.
func TestBuildWhereClause_DefaultExcludesInvalidated(t *testing.T) {
	f := &store.SearchFilters{}
	clauses, params := buildWhereClause(f, "m")

	found := false
	for _, c := range clauses {
		if strings.Contains(c, "valid_to") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected valid_to IS NULL clause in default filters, got none")
	}
	_ = params
}

// TestBuildWhereClause_IncludeInvalidated verifies that setting IncludeInvalidated=true
// omits the valid_to filter.
func TestBuildWhereClause_IncludeInvalidated(t *testing.T) {
	f := &store.SearchFilters{IncludeInvalidated: true}
	clauses, _ := buildWhereClause(f, "m")

	for _, c := range clauses {
		if strings.Contains(c, "valid_to") {
			t.Errorf("expected no valid_to clause when IncludeInvalidated=true, got: %q", c)
		}
	}
}

// TestBuildWhereClause_AsOf verifies that AsOf generates both valid_from and valid_to range clauses.
func TestBuildWhereClause_AsOf(t *testing.T) {
	asOf := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	f := &store.SearchFilters{AsOf: &asOf}
	clauses, params := buildWhereClause(f, "m")

	hasValidFrom := false
	hasValidTo := false
	for _, c := range clauses {
		if strings.Contains(c, "valid_from") {
			hasValidFrom = true
		}
		if strings.Contains(c, "valid_to") {
			hasValidTo = true
		}
	}

	if !hasValidFrom {
		t.Error("expected valid_from clause for AsOf filter")
	}
	if !hasValidTo {
		t.Error("expected valid_to clause for AsOf filter")
	}
	if _, ok := params["filter_as_of"]; !ok {
		t.Error("expected filter_as_of param for AsOf filter")
	}
}

// TestBuildWhereClause_NilFilters verifies nil filters still exclude sensitive memories
// (matches the opt-in behavior of matchesFilters in MockStore).
func TestBuildWhereClause_NilFilters(t *testing.T) {
	clauses, params := buildWhereClause(nil, "m")
	// Nil filters should produce exactly one clause: the sensitive-memory exclusion.
	if len(clauses) != 1 {
		t.Errorf("expected 1 clause (sensitive exclusion) for nil filters, got %v", clauses)
	}
	if _, ok := params["exclude_sensitive"]; !ok {
		t.Errorf("expected exclude_sensitive param for nil filters, got %v", params)
	}
}
