package tests

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

// newSearchMemory builds a memory with configurable scope, type, and project.
func newSearchMemory(id string, memType models.MemoryType, scope models.MemoryScope, project string) models.Memory {
	now := time.Now().UTC()
	return models.Memory{
		ID:           id,
		Type:         memType,
		Scope:        scope,
		Visibility:   models.VisibilityShared,
		Content:      "content for " + id,
		Confidence:   0.9,
		Source:       "test",
		Project:      project,
		Tags:         []string{"search-test"},
		CreatedAt:    now,
		UpdatedAt:    now,
		LastAccessed: now,
	}
}

// TestSearchJSONOutputShape verifies that []models.SearchResult serializes with
// "memory" and "score" top-level keys — not "final_score" or any other name.
func TestSearchJSONOutputShape(t *testing.T) {

	ctx := context.Background()
	s := store.NewMockStore()

	mem := newSearchMemory("json-1", models.MemoryTypeFact, models.ScopePermanent, "")
	vec := testVector(0.8)
	require.NoError(t, s.Upsert(ctx, mem, vec))

	results, err := s.Search(ctx, vec, 10, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)

	// Marshal the way cmd_search.go does it.
	out, marshalErr := json.MarshalIndent(results, "", "  ")
	require.NoError(t, marshalErr)

	// Unmarshal into a raw map slice to inspect keys without assumptions.
	var raw []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(out, &raw))
	require.Len(t, raw, 1)

	item := raw[0]
	assert.Contains(t, item, "memory", "SearchResult JSON must have a 'memory' key")
	assert.Contains(t, item, "score", "SearchResult JSON must have a 'score' key")
	assert.NotContains(t, item, "final_score", "SearchResult must NOT have 'final_score' (that belongs to RecallResult)")
	assert.NotContains(t, item, "similarity_score", "SearchResult must NOT have 'similarity_score'")
}

// TestSearchJSONRoundTrip verifies that JSON output round-trips cleanly back
// into []models.SearchResult with correct field values.
func TestSearchJSONRoundTrip(t *testing.T) {

	ctx := context.Background()
	s := store.NewMockStore()

	mem := newSearchMemory("rt-1", models.MemoryTypeRule, models.ScopePermanent, "")
	vec := testVector(0.7)
	require.NoError(t, s.Upsert(ctx, mem, vec))

	results, err := s.Search(ctx, vec, 10, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)

	data, marshalErr := json.Marshal(results)
	require.NoError(t, marshalErr)

	var decoded []models.SearchResult
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.Len(t, decoded, 1)

	assert.Equal(t, "rt-1", decoded[0].Memory.ID)
	assert.Equal(t, models.MemoryTypeRule, decoded[0].Memory.Type)
	assert.InDelta(t, results[0].Score, decoded[0].Score, 0.0001)
}

// TestSearchScopeFilter verifies that the scope filter wires correctly:
// only memories matching the requested scope are returned.
func TestSearchScopeFilter(t *testing.T) {

	scopes := []models.MemoryScope{
		models.ScopePermanent,
		models.ScopeProject,
		models.ScopeSession,
		models.ScopeTTL,
	}

	for i := range scopes {
		targetScope := scopes[i]
		t.Run(string(targetScope), func(t *testing.T) {
			ctx := context.Background()
			s := store.NewMockStore()

			// Insert one memory per scope.
			for j := range scopes {
				sc := scopes[j]
				id := "scope-" + string(sc)
				mem := newSearchMemory(id, models.MemoryTypeFact, sc, "proj-x")
				require.NoError(t, s.Upsert(ctx, mem, testVector(0.6)))
			}

			filters := &store.SearchFilters{Scope: &targetScope}
			results, err := s.Search(ctx, testVector(0.6), 20, filters)
			require.NoError(t, err)
			require.Len(t, results, 1, "expected exactly one result for scope=%s", targetScope)
			assert.Equal(t, targetScope, results[0].Memory.Scope)
		})
	}
}

// TestSearchTypeFilter verifies that type filtering with JSON output works.
func TestSearchTypeFilter(t *testing.T) {

	ctx := context.Background()
	s := store.NewMockStore()

	types := []models.MemoryType{
		models.MemoryTypeRule,
		models.MemoryTypeFact,
		models.MemoryTypeEpisode,
		models.MemoryTypeProcedure,
		models.MemoryTypePreference,
	}

	for i := range types {
		mt := types[i]
		mem := newSearchMemory("type-"+string(mt), mt, models.ScopePermanent, "")
		require.NoError(t, s.Upsert(ctx, mem, testVector(0.5)))
	}

	// Filter for "rule" only.
	ruleType := models.MemoryTypeRule
	filters := &store.SearchFilters{Type: &ruleType}
	results, err := s.Search(ctx, testVector(0.5), 20, filters)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, models.MemoryTypeRule, results[0].Memory.Type)

	// Verify JSON output carries the type through correctly.
	data, marshalErr := json.Marshal(results)
	require.NoError(t, marshalErr)

	var decoded []models.SearchResult
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.Len(t, decoded, 1)
	assert.Equal(t, models.MemoryTypeRule, decoded[0].Memory.Type)
}

// TestSearchCombinedFilters verifies that --type, --scope, and --project applied
// together return only memories that match all three criteria.
func TestSearchCombinedFilters(t *testing.T) {

	ctx := context.Background()
	s := store.NewMockStore()

	project := "cortex-test"

	// The one memory that should match all filters.
	match := newSearchMemory("combo-match", models.MemoryTypeRule, models.ScopeProject, project)
	require.NoError(t, s.Upsert(ctx, match, testVector(0.6)))

	// Memories that miss on type, scope, or project.
	missType := newSearchMemory("combo-wrong-type", models.MemoryTypeFact, models.ScopeProject, project)
	require.NoError(t, s.Upsert(ctx, missType, testVector(0.6)))

	missScope := newSearchMemory("combo-wrong-scope", models.MemoryTypeRule, models.ScopePermanent, project)
	require.NoError(t, s.Upsert(ctx, missScope, testVector(0.6)))

	missProject := newSearchMemory("combo-wrong-project", models.MemoryTypeRule, models.ScopeProject, "other-project")
	require.NoError(t, s.Upsert(ctx, missProject, testVector(0.6)))

	ruleType := models.MemoryTypeRule
	projScope := models.ScopeProject
	filters := &store.SearchFilters{
		Type:    &ruleType,
		Scope:   &projScope,
		Project: &project,
	}

	results, err := s.Search(ctx, testVector(0.6), 20, filters)
	require.NoError(t, err)
	require.Len(t, results, 1, "only the memory matching all three filters should be returned")
	assert.Equal(t, "combo-match", results[0].Memory.ID)
}

// TestSearchJSONEmptyResults verifies that an empty result set marshals to an
// empty JSON array "[]" and NOT null.
func TestSearchJSONEmptyResults(t *testing.T) {

	ctx := context.Background()
	s := store.NewMockStore()

	// Insert one memory but search with a filter that matches nothing.
	mem := newSearchMemory("empty-1", models.MemoryTypeFact, models.ScopePermanent, "")
	require.NoError(t, s.Upsert(ctx, mem, testVector(0.5)))

	ruleType := models.MemoryTypeRule // no rule memories stored
	filters := &store.SearchFilters{Type: &ruleType}
	results, err := s.Search(ctx, testVector(0.5), 10, filters)
	require.NoError(t, err)
	assert.Empty(t, results)

	// Verify the nil-guard that cmd_search.go applies before marshaling:
	// nil slice must be converted to empty slice so JSON output is [] not null.
	if results == nil {
		results = []models.SearchResult{}
	}
	data, marshalErr := json.Marshal(results)
	require.NoError(t, marshalErr)
	assert.Equal(t, "[]", string(data), "empty results must serialize as [] not null")

	// Also verify that a nil slice without the guard would produce "null" — this is
	// why the guard in cmd_search.go is essential.
	var nilSlice []models.SearchResult
	nilData, nilMarshalErr := json.Marshal(nilSlice)
	require.NoError(t, nilMarshalErr)
	assert.Equal(t, "null", string(nilData), "nil slice marshals to null without guard")
}

// TestRecallResultJSONShape is the critical regression test that would have caught
// the plugin bug: RecallResult must serialize with "final_score", not "score".
func TestRecallResultJSONShape(t *testing.T) {

	rr := models.RecallResult{
		Memory: models.Memory{
			ID:      "rr-1",
			Type:    models.MemoryTypeRule,
			Scope:   models.ScopePermanent,
			Content: "always write tests",
		},
		SimilarityScore: 0.95,
		RecencyScore:    0.80,
		FrequencyScore:  0.10,
		TypeBoost:       1.50,
		ScopeBoost:      1.20,
		FinalScore:      0.72,
	}

	data, err := json.Marshal(rr)
	require.NoError(t, err)

	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &raw))

	// Must have "final_score".
	assert.Contains(t, raw, "final_score", "RecallResult JSON must have 'final_score'")

	// Must NOT have a bare "score" key (that belongs to SearchResult).
	assert.NotContains(t, raw, "score", "RecallResult JSON must NOT have a bare 'score' key")

	// Check the component score fields are present.
	assert.Contains(t, raw, "similarity_score")
	assert.Contains(t, raw, "recency_score")
	assert.Contains(t, raw, "frequency_score")
	assert.Contains(t, raw, "type_boost")
	assert.Contains(t, raw, "scope_boost")

	// Verify the actual value round-trips.
	var decoded models.RecallResult
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.InDelta(t, 0.72, decoded.FinalScore, 0.0001)
}

// TestSearchResultVsRecallResultFieldNames explicitly documents and enforces the
// distinction between SearchResult ("score") and RecallResult ("final_score").
func TestSearchResultVsRecallResultFieldNames(t *testing.T) {

	sr := models.SearchResult{
		Memory: models.Memory{ID: "sr-1"},
		Score:  0.88,
	}

	rr := models.RecallResult{
		Memory:     models.Memory{ID: "rr-2"},
		FinalScore: 0.77,
	}

	srData, err := json.Marshal(sr)
	require.NoError(t, err)
	rrData, marshalErr := json.Marshal(rr)
	require.NoError(t, marshalErr)

	var srMap map[string]json.RawMessage
	var rrMap map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(srData, &srMap))
	require.NoError(t, json.Unmarshal(rrData, &rrMap))

	// SearchResult uses "score".
	assert.Contains(t, srMap, "score", "SearchResult must use 'score'")
	assert.NotContains(t, srMap, "final_score", "SearchResult must not have 'final_score'")

	// RecallResult uses "final_score".
	assert.Contains(t, rrMap, "final_score", "RecallResult must use 'final_score'")
	assert.NotContains(t, rrMap, "score", "RecallResult must not have a bare 'score'")
}

// TestSearchProjectFilter verifies that the project filter isolates memories
// belonging to a specific project.
func TestSearchProjectFilter(t *testing.T) {

	ctx := context.Background()
	s := store.NewMockStore()

	projectA := "project-alpha"
	projectB := "project-beta"

	memA1 := newSearchMemory("pa-1", models.MemoryTypeFact, models.ScopeProject, projectA)
	memA2 := newSearchMemory("pa-2", models.MemoryTypeRule, models.ScopeProject, projectA)
	memB1 := newSearchMemory("pb-1", models.MemoryTypeFact, models.ScopeProject, projectB)

	require.NoError(t, s.Upsert(ctx, memA1, testVector(0.6)))
	require.NoError(t, s.Upsert(ctx, memA2, testVector(0.6)))
	require.NoError(t, s.Upsert(ctx, memB1, testVector(0.6)))

	filters := &store.SearchFilters{Project: &projectA}
	results, err := s.Search(ctx, testVector(0.6), 20, filters)
	require.NoError(t, err)
	require.Len(t, results, 2)

	for i := range results {
		assert.Equal(t, projectA, results[i].Memory.Project)
	}
}

// TestSearchScopeFilterJSONOutput verifies that scope-filtered results round-trip
// correctly through JSON serialization (integration of filtering + marshaling).
func TestSearchScopeFilterJSONOutput(t *testing.T) {

	ctx := context.Background()
	s := store.NewMockStore()

	sessionMem := newSearchMemory("sess-1", models.MemoryTypeFact, models.ScopeSession, "")
	permMem := newSearchMemory("perm-1", models.MemoryTypeFact, models.ScopePermanent, "")

	require.NoError(t, s.Upsert(ctx, sessionMem, testVector(0.5)))
	require.NoError(t, s.Upsert(ctx, permMem, testVector(0.5)))

	sessionScope := models.ScopeSession
	filters := &store.SearchFilters{Scope: &sessionScope}
	results, err := s.Search(ctx, testVector(0.5), 10, filters)
	require.NoError(t, err)
	require.Len(t, results, 1)

	data, marshalErr := json.MarshalIndent(results, "", "  ")
	require.NoError(t, marshalErr)

	var decoded []models.SearchResult
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.Len(t, decoded, 1)
	assert.Equal(t, "sess-1", decoded[0].Memory.ID)
	assert.Equal(t, models.ScopeSession, decoded[0].Memory.Scope)
}

// TestSearchInvalidTypeValidation verifies that an invalid --type value is rejected
// by the IsValid() check rather than silently returning empty results.
func TestSearchInvalidTypeValidation(t *testing.T) {
	invalidType := models.MemoryType("bogus")
	assert.False(t, invalidType.IsValid(), "bogus must not be a valid MemoryType")

	// All valid types must pass.
	for i := range models.ValidMemoryTypes {
		assert.True(t, models.ValidMemoryTypes[i].IsValid(), "valid type %q must pass IsValid()", models.ValidMemoryTypes[i])
	}
}

// TestSearchInvalidScopeValidation verifies that an invalid --scope value is rejected
// by the IsValid() check rather than silently returning empty results.
func TestSearchInvalidScopeValidation(t *testing.T) {
	invalidScope := models.MemoryScope("bogus")
	assert.False(t, invalidScope.IsValid(), "bogus must not be a valid MemoryScope")

	// All valid scopes must pass.
	for i := range models.ValidMemoryScopes {
		assert.True(t, models.ValidMemoryScopes[i].IsValid(), "valid scope %q must pass IsValid()", models.ValidMemoryScopes[i])
	}
}
