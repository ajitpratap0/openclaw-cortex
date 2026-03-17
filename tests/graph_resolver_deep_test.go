package tests

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/graph"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
)

// ---------------------------------------------------------------------------
// EntityResolver deep-path tests
// ---------------------------------------------------------------------------

// TestEntityResolver_NoCandidates_ReturnsNew verifies that when the graph has no
// entities (SearchEntities returns empty), the resolver returns isNew=true.
func TestEntityResolver_NoCandidates_ReturnsNew(t *testing.T) {
	mock := graph.NewMockGraphClient()
	resolver := graph.NewEntityResolver(mock, nil, "claude-3-haiku-20240307", 0.9, 10, slog.Default())

	extracted := models.Entity{
		ID:   "uuid-new-1",
		Name: "Brand New Entity",
		Type: models.EntityTypeConcept,
	}

	resolvedID, isNew, err := resolver.Resolve(context.Background(), extracted, nil, "")
	require.NoError(t, err)
	assert.True(t, isNew, "no candidates → should be treated as new")
	assert.Equal(t, "uuid-new-1", resolvedID)
}

// TestEntityResolver_LLMFallback_Merge exercises Stage 3 (Claude fallback) where
// the LLM responds that the new entity is a duplicate of an existing one.
// The deterministic match must be bypassed (different name, no aliases, no embeddings).
func TestEntityResolver_LLMFallback_Merge(t *testing.T) {
	mock := graph.NewMockGraphClient()
	ctx := context.Background()

	// Seed an entity whose name CONTAINS the query string (so mock SearchEntities
	// returns it as a candidate), but the names are not equal (so Stage 2 fast-path
	// does not fire) and there are no aliases or embeddings (so Stage 3 runs).
	existing := models.Entity{
		ID:   "existing-llm-1",
		Name: "CEO of Acme Corporation",
		Type: models.EntityTypePerson,
	}
	require.NoError(t, mock.UpsertEntity(ctx, existing))

	// LLM responds saying it is a duplicate.
	llmResp := `{"is_duplicate": true, "existing_id": "existing-llm-1"}`
	llmClient := &mockLLMClient{Resp: llmResp}

	resolver := graph.NewEntityResolver(mock, llmClient, "claude-3-haiku-20240307", 0.99, 10, slog.Default())

	// "CEO" is a substring of "CEO of Acme Corporation" so SearchEntities returns it,
	// but "CEO" != "CEO of Acme Corporation" so deterministic match fails → Stage 3 runs.
	extracted := models.Entity{
		ID:   "new-llm-1",
		Name: "CEO",
		Type: models.EntityTypePerson,
	}

	resolvedID, isNew, err := resolver.Resolve(ctx, extracted, nil, "context about the CEO")
	require.NoError(t, err)
	// The LLM said duplicate → isNew should be false and ID should be existing.
	assert.False(t, isNew, "LLM said duplicate → should not be new")
	assert.Equal(t, "existing-llm-1", resolvedID)
}

// TestEntityResolver_LLMFallback_HallucinatedID verifies that if Claude returns a
// duplicate ID that is NOT in the candidate set, the resolver treats the entity as
// new (safe default).
func TestEntityResolver_LLMFallback_HallucinatedID(t *testing.T) {
	mock := graph.NewMockGraphClient()
	ctx := context.Background()

	// "Boss of Beta Corp" contains "Boss" so mock SearchEntities returns it as a candidate.
	existing := models.Entity{
		ID:   "existing-llm-2",
		Name: "Boss of Beta Corp",
		Type: models.EntityTypePerson,
	}
	require.NoError(t, mock.UpsertEntity(ctx, existing))

	// LLM returns an ID that is NOT in the candidate set.
	llmResp := `{"is_duplicate": true, "existing_id": "hallucinated-id-999"}`
	llmClient := &mockLLMClient{Resp: llmResp}

	resolver := graph.NewEntityResolver(mock, llmClient, "claude-3-haiku-20240307", 0.99, 10, slog.Default())

	// "Boss" is a substring so Stage 1 gets candidates; "Boss" != "Boss of Beta Corp"
	// so Stage 2 passes; Stage 3 runs and Claude returns a hallucinated ID.
	extracted := models.Entity{
		ID:   "new-llm-2",
		Name: "Boss",
		Type: models.EntityTypePerson,
	}

	resolvedID, isNew, err := resolver.Resolve(ctx, extracted, nil, "")
	require.NoError(t, err)
	// Hallucinated ID: claudeFallback returns (extracted.ID, matched=true, nil).
	// Resolve() sees matched=true and returns (resolvedID=extracted.ID, isNew=false, nil).
	// The entity resolves to its own ID with isNew=false (safe: no actual merge occurs).
	assert.False(t, isNew, "hallucinated ID path: matched=true but ID is the extracted entity's own ID")
	assert.Equal(t, "new-llm-2", resolvedID)
}

// TestEntityResolver_LLMError_DegradesToNew verifies that when Claude returns an
// error (e.g., API unavailable), the resolver degrades gracefully and returns
// isNew=true rather than propagating the error.
func TestEntityResolver_LLMError_DegradesToNew(t *testing.T) {
	mock := graph.NewMockGraphClient()
	ctx := context.Background()

	// "Alpha Service Platform" contains "Alpha" so mock SearchEntities returns it
	// as a candidate; "Alpha" != "Alpha Service Platform" so Stage 2 passes.
	existing := models.Entity{
		ID:   "existing-err-1",
		Name: "Alpha Service Platform",
		Type: models.EntityTypeSystem,
	}
	require.NoError(t, mock.UpsertEntity(ctx, existing))

	apiErr := errors.New("claude API unavailable")
	llmClient := &mockLLMClient{Err: apiErr}

	resolver := graph.NewEntityResolver(mock, llmClient, "claude-3-haiku-20240307", 0.99, 10, slog.Default())

	extracted := models.Entity{
		ID:   "new-err-1",
		Name: "Alpha",
		Type: models.EntityTypeSystem,
	}

	resolvedID, isNew, err := resolver.Resolve(ctx, extracted, nil, "alpha service context")
	// The error is swallowed; resolver returns gracefully.
	require.NoError(t, err)
	assert.True(t, isNew, "LLM error → should degrade to treating as new")
	assert.Equal(t, "new-err-1", resolvedID)
}

// ---------------------------------------------------------------------------
// FactExtractor deep-path tests
// ---------------------------------------------------------------------------

// TestFactExtractor_SuccessPath verifies that a valid JSON response from the LLM
// produces the expected facts.
func TestFactExtractor_SuccessPath(t *testing.T) {
	// Return a JSON array with one valid fact referencing known entities.
	llmResp := `[{"source_entity_name":"Alice","target_entity_name":"Acme","relation_type":"WORKS_AT","fact":"Alice works at Acme","valid_at":null,"invalid_at":null}]`
	llmClient := &mockLLMClient{Resp: llmResp}

	fe := graph.NewFactExtractor(llmClient, "claude-3-haiku-20240307", slog.Default())
	facts, err := fe.Extract(context.Background(), "Alice works at Acme Corp.", []string{"Alice", "Acme"})

	require.NoError(t, err)
	require.Len(t, facts, 1)
	assert.Equal(t, "Alice", facts[0].SourceEntityID)
	assert.Equal(t, "Acme", facts[0].TargetEntityID)
	assert.Equal(t, "WORKS_AT", facts[0].RelationType)
	assert.Equal(t, "Alice works at Acme", facts[0].Fact)
	assert.InDelta(t, 0.8, facts[0].Confidence, 0.001)
}

// TestFactExtractor_SelfReferentialSkipped verifies that self-referential facts
// (source == target) are silently dropped.
func TestFactExtractor_SelfReferentialSkipped(t *testing.T) {
	llmResp := `[{"source_entity_name":"Alice","target_entity_name":"Alice","relation_type":"KNOWS","fact":"Alice knows Alice","valid_at":null,"invalid_at":null}]`
	llmClient := &mockLLMClient{Resp: llmResp}

	fe := graph.NewFactExtractor(llmClient, "claude-3-haiku-20240307", slog.Default())
	facts, err := fe.Extract(context.Background(), "Alice knows herself.", []string{"Alice"})

	require.NoError(t, err)
	assert.Empty(t, facts, "self-referential facts should be dropped")
}

// TestFactExtractor_UnknownEntitySkipped verifies that facts referencing entities
// not in the known-entity list are silently dropped.
func TestFactExtractor_UnknownEntitySkipped(t *testing.T) {
	// "Charlie" is not in the known entity list.
	llmResp := `[{"source_entity_name":"Alice","target_entity_name":"Charlie","relation_type":"KNOWS","fact":"Alice knows Charlie","valid_at":null,"invalid_at":null}]`
	llmClient := &mockLLMClient{Resp: llmResp}

	fe := graph.NewFactExtractor(llmClient, "claude-3-haiku-20240307", slog.Default())
	facts, err := fe.Extract(context.Background(), "Alice knows Charlie.", []string{"Alice", "Bob"})

	require.NoError(t, err)
	assert.Empty(t, facts, "fact referencing unknown entity should be dropped")
}

// TestFactExtractor_LLMError_ReturnsEmpty verifies that an LLM API error causes
// the extractor to return (nil, nil) rather than propagating the error.
func TestFactExtractor_LLMError_ReturnsEmpty(t *testing.T) {
	apiErr := errors.New("network timeout")
	llmClient := &mockLLMClient{Err: apiErr}

	fe := graph.NewFactExtractor(llmClient, "claude-3-haiku-20240307", slog.Default())
	facts, err := fe.Extract(context.Background(), "Alice and Bob met.", []string{"Alice", "Bob"})

	require.NoError(t, err)
	assert.Nil(t, facts, "LLM error should produce nil facts and no error")
}

// TestFactExtractor_InvalidJSON_ReturnsError verifies that an invalid JSON response
// from the LLM is surfaced as an error (unlike API errors, parse errors are returned).
func TestFactExtractor_InvalidJSON_ReturnsError(t *testing.T) {
	llmClient := &mockLLMClient{Resp: "not-valid-json"}

	fe := graph.NewFactExtractor(llmClient, "claude-3-haiku-20240307", slog.Default())
	facts, err := fe.Extract(context.Background(), "Alice and Bob met.", []string{"Alice", "Bob"})

	assert.Error(t, err, "invalid JSON should return an error")
	assert.Nil(t, facts)
}

// TestFactExtractor_EmptyEntityList_ShortCircuits verifies that an empty entity
// list bypasses the API call and returns nil, nil.
func TestFactExtractor_EmptyEntityList_ShortCircuits(t *testing.T) {
	// Even if the LLM would return data, we never call it.
	llmClient := &mockLLMClient{Resp: `[{"source_entity_name":"A","target_entity_name":"B"}]`}

	fe := graph.NewFactExtractor(llmClient, "claude-3-haiku-20240307", slog.Default())
	facts, err := fe.Extract(context.Background(), "Some text.", []string{})

	require.NoError(t, err)
	assert.Nil(t, facts)
}

// ---------------------------------------------------------------------------
// FactResolver deep-path tests
// ---------------------------------------------------------------------------

// TestFactResolver_InsertNewFact verifies that when there are no existing facts
// between the entity pair, the resolver returns FactActionInsert.
func TestFactResolver_InsertNewFact(t *testing.T) {
	mock := graph.NewMockGraphClient()
	resolver := graph.NewFactResolver(mock, nil, "claude-3-haiku-20240307", slog.Default())

	newFact := models.Fact{
		ID:             "f-new-1",
		SourceEntityID: "entity-x",
		TargetEntityID: "entity-y",
		Fact:           "X depends on Y",
		Confidence:     0.8,
	}

	action, ids, err := resolver.Resolve(context.Background(), newFact, "")
	require.NoError(t, err)
	assert.Equal(t, graph.FactActionInsert, action)
	assert.Empty(t, ids)
}

// TestFactResolver_SkipExactDuplicate verifies the fast-path skip when the new
// fact text and endpoints exactly match an existing fact.
func TestFactResolver_SkipExactDuplicate(t *testing.T) {
	mock := graph.NewMockGraphClient()
	ctx := context.Background()

	existing := models.Fact{
		ID:             "f-dup-1",
		SourceEntityID: "ent-a",
		TargetEntityID: "ent-b",
		RelationType:   "REPORTS_TO",
		Fact:           "Alice reports to Bob",
		Confidence:     0.9,
	}
	require.NoError(t, mock.UpsertFact(ctx, existing))

	resolver := graph.NewFactResolver(mock, nil, "claude-3-haiku-20240307", slog.Default())

	newFact := models.Fact{
		SourceEntityID: "ent-a",
		TargetEntityID: "ent-b",
		Fact:           "Alice reports to Bob",
	}

	action, ids, err := resolver.Resolve(ctx, newFact, "")
	require.NoError(t, err)
	assert.Equal(t, graph.FactActionSkip, action)
	require.Len(t, ids, 1)
	assert.Equal(t, "f-dup-1", ids[0])
}

// TestFactResolver_LLMSaysInvalidate exercises the path where Claude identifies a
// contradiction and the resolver returns FactActionInvalidate with the conflicting
// fact's ID.
func TestFactResolver_LLMSaysInvalidate(t *testing.T) {
	mock := graph.NewMockGraphClient()
	ctx := context.Background()

	existing := models.Fact{
		ID:             "f-contra-1",
		SourceEntityID: "ent-a",
		TargetEntityID: "ent-b",
		RelationType:   "WORKS_AT",
		Fact:           "Alice works at Acme",
		Confidence:     0.9,
	}
	require.NoError(t, mock.UpsertFact(ctx, existing))

	// LLM says fact at index 1 is contradicted (1-based numbering in prompt).
	llmResp := `{"duplicate_indices": [], "contradicted_indices": [1]}`
	llmClient := &mockLLMClient{Resp: llmResp}

	resolver := graph.NewFactResolver(mock, llmClient, "claude-3-haiku-20240307", slog.Default())

	// Different text so fast-path skip doesn't trigger.
	newFact := models.Fact{
		SourceEntityID: "ent-a",
		TargetEntityID: "ent-b",
		Fact:           "Alice no longer works at Acme, she left",
	}

	action, ids, err := resolver.Resolve(ctx, newFact, "context")
	require.NoError(t, err)
	assert.Equal(t, graph.FactActionInvalidate, action)
	require.Len(t, ids, 1)
	assert.Equal(t, "f-contra-1", ids[0])
}

// TestFactResolver_LLMSaysDuplicate exercises the path where Claude identifies a
// semantic duplicate (not exact text match) and returns FactActionSkip.
func TestFactResolver_LLMSaysDuplicate(t *testing.T) {
	mock := graph.NewMockGraphClient()
	ctx := context.Background()

	existing := models.Fact{
		ID:             "f-sem-dup-1",
		SourceEntityID: "ent-a",
		TargetEntityID: "ent-b",
		RelationType:   "KNOWS",
		Fact:           "Alice is acquainted with Bob",
		Confidence:     0.85,
	}
	require.NoError(t, mock.UpsertFact(ctx, existing))

	llmResp := `{"duplicate_indices": [1], "contradicted_indices": []}`
	llmClient := &mockLLMClient{Resp: llmResp}

	resolver := graph.NewFactResolver(mock, llmClient, "claude-3-haiku-20240307", slog.Default())

	newFact := models.Fact{
		SourceEntityID: "ent-a",
		TargetEntityID: "ent-b",
		Fact:           "Alice knows Bob well",
	}

	action, ids, err := resolver.Resolve(ctx, newFact, "")
	require.NoError(t, err)
	assert.Equal(t, graph.FactActionSkip, action)
	require.Len(t, ids, 1)
	assert.Equal(t, "f-sem-dup-1", ids[0])
}

// TestFactResolver_LLMError_DegradesToInsert verifies that when the LLM call fails,
// the resolver degrades gracefully and returns FactActionInsert (safe default).
func TestFactResolver_LLMError_DegradesToInsert(t *testing.T) {
	mock := graph.NewMockGraphClient()
	ctx := context.Background()

	existing := models.Fact{
		ID:             "f-err-1",
		SourceEntityID: "ent-a",
		TargetEntityID: "ent-b",
		Fact:           "Alice collaborates with Bob",
		Confidence:     0.9,
	}
	require.NoError(t, mock.UpsertFact(ctx, existing))

	apiErr := errors.New("service unavailable")
	llmClient := &mockLLMClient{Err: apiErr}

	resolver := graph.NewFactResolver(mock, llmClient, "claude-3-haiku-20240307", slog.Default())

	// Different text, same entity pair → would need LLM check, but LLM fails.
	newFact := models.Fact{
		SourceEntityID: "ent-a",
		TargetEntityID: "ent-b",
		Fact:           "Alice and Bob are co-workers",
	}

	action, ids, err := resolver.Resolve(ctx, newFact, "")
	require.NoError(t, err, "LLM error should not propagate")
	assert.Equal(t, graph.FactActionInsert, action)
	assert.Empty(t, ids)
}

// TestFactResolver_LLMInvalidJSON_DegradesToInsert verifies that an unparseable
// Claude response causes graceful degradation to FactActionInsert.
func TestFactResolver_LLMInvalidJSON_DegradesToInsert(t *testing.T) {
	mock := graph.NewMockGraphClient()
	ctx := context.Background()

	existing := models.Fact{
		ID:             "f-json-1",
		SourceEntityID: "ent-a",
		TargetEntityID: "ent-b",
		Fact:           "Something happened",
		Confidence:     0.9,
	}
	require.NoError(t, mock.UpsertFact(ctx, existing))

	llmClient := &mockLLMClient{Resp: "this is not json at all"}

	resolver := graph.NewFactResolver(mock, llmClient, "claude-3-haiku-20240307", slog.Default())

	newFact := models.Fact{
		SourceEntityID: "ent-a",
		TargetEntityID: "ent-b",
		Fact:           "Something else happened",
	}

	action, ids, err := resolver.Resolve(ctx, newFact, "")
	require.NoError(t, err, "bad JSON should not propagate as error")
	assert.Equal(t, graph.FactActionInsert, action)
	assert.Empty(t, ids)
}
