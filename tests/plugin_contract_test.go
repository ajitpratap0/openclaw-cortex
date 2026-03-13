package tests

// plugin_contract_test.go validates the JSON serialization contract between
// the openclaw-cortex Go binary and the TypeScript plugin. These are regression
// tests that catch field-name mismatches before they reach production.

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

// sampleMemory returns a fully-populated Memory for use in contract tests.
func sampleMemory() models.Memory {
	now := time.Now().UTC()
	return models.Memory{
		ID:           "abc123",
		Type:         models.MemoryTypeRule,
		Scope:        models.ScopePermanent,
		Visibility:   models.VisibilityPrivate,
		Content:      "Always use context.Context as the first argument",
		Confidence:   0.9,
		Source:       "test",
		Tags:         []string{"go", "best-practice"},
		Project:      "openclaw-cortex",
		CreatedAt:    now,
		UpdatedAt:    now,
		LastAccessed: now,
		AccessCount:  5,
	}
}

// TestPluginContract_RecallResultFieldNames validates that models.RecallResult
// serializes with the exact JSON keys the TypeScript RecallResult interface
// expects. The critical regression: "score" must NOT appear at the top level —
// only "final_score" does.
func TestPluginContract_RecallResultFieldNames(t *testing.T) {

	result := models.RecallResult{
		Memory:          sampleMemory(),
		SimilarityScore: 0.85,
		RecencyScore:    0.72,
		FrequencyScore:  0.50,
		TypeBoost:       1.50,
		ScopeBoost:      1.20,
		FinalScore:      0.88,
	}

	data, marshalErr := json.Marshal(result)
	require.NoError(t, marshalErr)

	var m map[string]interface{}
	unmarshalErr := json.Unmarshal(data, &m)
	require.NoError(t, unmarshalErr)

	// Fields the TypeScript RecallResult interface requires at the top level.
	assert.Contains(t, m, "memory", "RecallResult must have 'memory' field")
	assert.Contains(t, m, "similarity_score", "RecallResult must have 'similarity_score' field")
	assert.Contains(t, m, "recency_score", "RecallResult must have 'recency_score' field")
	assert.Contains(t, m, "frequency_score", "RecallResult must have 'frequency_score' field")
	assert.Contains(t, m, "type_boost", "RecallResult must have 'type_boost' field")
	assert.Contains(t, m, "scope_boost", "RecallResult must have 'scope_boost' field")
	assert.Contains(t, m, "final_score", "RecallResult must have 'final_score' field")

	// Critical regression: "score" must NOT exist at the top level of RecallResult.
	// The plugin accesses r.final_score — a bare "score" field would silently be ignored.
	assert.NotContains(t, m, "score", "RecallResult must NOT have a top-level 'score' field (use 'final_score')")

	// Verify numeric types are actually numbers, not strings.
	assert.IsType(t, float64(0), m["similarity_score"], "similarity_score must be a number")
	assert.IsType(t, float64(0), m["recency_score"], "recency_score must be a number")
	assert.IsType(t, float64(0), m["frequency_score"], "frequency_score must be a number")
	assert.IsType(t, float64(0), m["type_boost"], "type_boost must be a number")
	assert.IsType(t, float64(0), m["scope_boost"], "scope_boost must be a number")
	assert.IsType(t, float64(0), m["final_score"], "final_score must be a number")

	// "memory" must be a nested object, not a primitive.
	assert.IsType(t, map[string]interface{}{}, m["memory"], "memory must be a nested object")
}

// TestPluginContract_SearchResultFieldNames validates that models.SearchResult
// serializes with the exact JSON keys the TypeScript search path expects:
// { memory: CortexMemory; score: number }.
func TestPluginContract_SearchResultFieldNames(t *testing.T) {

	result := models.SearchResult{
		Memory: sampleMemory(),
		Score:  0.91,
	}

	data, marshalErr := json.Marshal(result)
	require.NoError(t, marshalErr)

	var m map[string]interface{}
	unmarshalErr := json.Unmarshal(data, &m)
	require.NoError(t, unmarshalErr)

	assert.Contains(t, m, "memory", "SearchResult must have 'memory' field")
	assert.Contains(t, m, "score", "SearchResult must have 'score' field")
	assert.IsType(t, float64(0), m["score"], "score must be a number")
	assert.IsType(t, map[string]interface{}{}, m["memory"], "memory must be a nested object")
}

// TestPluginContract_MemoryFieldNames validates that models.Memory serializes
// with every field the TypeScript CortexMemory interface requires.
func TestPluginContract_MemoryFieldNames(t *testing.T) {

	mem := sampleMemory()

	data, marshalErr := json.Marshal(mem)
	require.NoError(t, marshalErr)

	var m map[string]interface{}
	unmarshalErr := json.Unmarshal(data, &m)
	require.NoError(t, unmarshalErr)

	// Every field declared in the TypeScript CortexMemory interface.
	requiredFields := []string{
		"id",
		"content",
		"type",
		"scope",
		"visibility",
		"confidence",
		"source",
		"tags",
		"project",
		"created_at",
		"updated_at",
		"last_accessed",
		"access_count",
	}
	for i := range requiredFields {
		assert.Contains(t, m, requiredFields[i], "Memory must have field '%s'", requiredFields[i])
	}

	// Validate specific types.
	assert.IsType(t, "", m["id"], "id must be a string")
	assert.IsType(t, "", m["content"], "content must be a string")
	assert.IsType(t, "", m["type"], "type must be a string")
	assert.IsType(t, "", m["scope"], "scope must be a string")
	assert.IsType(t, "", m["visibility"], "visibility must be a string")
	assert.IsType(t, "", m["source"], "source must be a string")
	assert.IsType(t, "", m["project"], "project must be a string")
	assert.IsType(t, float64(0), m["confidence"], "confidence must be a number")
	assert.IsType(t, []interface{}{}, m["tags"], "tags must be an array")
	assert.IsType(t, float64(0), m["access_count"], "access_count must be a number")
}

// TestPluginContract_MemoryOptionalFields validates that optional Memory fields
// are present in the JSON output when they are populated.
func TestPluginContract_MemoryOptionalFields(t *testing.T) {
	mem := sampleMemory()
	mem.TTLSeconds = 3600
	mem.ReinforcedCount = 5
	mem.ReinforcedAt = time.Now().UTC()
	mem.SupersedesID = "old-memory-id"
	mem.ConflictGroupID = "conflict-group-1"
	mem.ConflictStatus = models.ConflictStatusActive
	mem.ValidUntil = time.Now().Add(24 * time.Hour).UTC()
	mem.Metadata = map[string]any{"key": "value"}

	data, marshalErr := json.Marshal(mem)
	require.NoError(t, marshalErr)

	var m map[string]interface{}
	unmarshalErr := json.Unmarshal(data, &m)
	require.NoError(t, unmarshalErr)

	// Optional fields should be present when populated.
	assert.Contains(t, m, "ttl_seconds")
	assert.Contains(t, m, "reinforced_count")
	assert.Contains(t, m, "reinforced_at")
	assert.Contains(t, m, "supersedes_id")
	assert.Contains(t, m, "conflict_group_id")
	assert.Contains(t, m, "conflict_status")
	assert.Contains(t, m, "valid_until")
	assert.Contains(t, m, "metadata")
}

// TestPluginContract_SearchFiltersScope validates that store.SearchFilters
// serializes the Scope field with the key "scope". This confirms the CLI
// --scope flag wires up to the correct JSON key.
func TestPluginContract_SearchFiltersScope(t *testing.T) {

	scope := models.ScopeProject
	filters := store.SearchFilters{
		Scope: &scope,
	}

	data, marshalErr := json.Marshal(filters)
	require.NoError(t, marshalErr)

	var m map[string]interface{}
	unmarshalErr := json.Unmarshal(data, &m)
	require.NoError(t, unmarshalErr)

	assert.Contains(t, m, "scope", "SearchFilters must serialize the Scope field as 'scope'")
	assert.Equal(t, "project", m["scope"], "scope value must match the MemoryScope constant")
}

// TestPluginContract_RecallResultFinalScorePopulated verifies that the
// FinalScore field is correctly marshaled as a non-zero float. This guards
// against accidental zero-value omission or field renaming.
func TestPluginContract_RecallResultFinalScorePopulated(t *testing.T) {

	result := models.RecallResult{
		Memory:          sampleMemory(),
		SimilarityScore: 0.80,
		RecencyScore:    0.60,
		FrequencyScore:  0.40,
		TypeBoost:       1.50,
		ScopeBoost:      1.00,
		FinalScore:      0.77,
	}

	data, marshalErr := json.Marshal(result)
	require.NoError(t, marshalErr)

	var m map[string]interface{}
	unmarshalErr := json.Unmarshal(data, &m)
	require.NoError(t, unmarshalErr)

	finalScore, ok := m["final_score"].(float64)
	require.True(t, ok, "final_score must be a float64")
	assert.InDelta(t, 0.77, finalScore, 1e-9, "final_score must round-trip correctly")
	assert.NotZero(t, finalScore, "final_score must not be zero for a populated RecallResult")
}

// TestPluginContract_ScoreFieldDistinction makes it explicit that SearchResult
// uses "score" while RecallResult uses "final_score". Both are tested side by
// side so a future refactor cannot accidentally unify them under the wrong name.
func TestPluginContract_ScoreFieldDistinction(t *testing.T) {

	mem := sampleMemory()

	// SearchResult — plugin does: parsed.map(r => r.memory)  and  r.score
	searchResult := models.SearchResult{Memory: mem, Score: 0.91}
	searchData, searchMarshalErr := json.Marshal(searchResult)
	require.NoError(t, searchMarshalErr)

	var searchMap map[string]interface{}
	searchUnmarshalErr := json.Unmarshal(searchData, &searchMap)
	require.NoError(t, searchUnmarshalErr)

	assert.Contains(t, searchMap, "score", "SearchResult top-level must have 'score'")
	assert.NotContains(t, searchMap, "final_score", "SearchResult must NOT have 'final_score'")

	// RecallResult — plugin does: r.final_score
	recallResult := models.RecallResult{
		Memory:     mem,
		FinalScore: 0.88,
	}
	recallData, recallMarshalErr := json.Marshal(recallResult)
	require.NoError(t, recallMarshalErr)

	var recallMap map[string]interface{}
	recallUnmarshalErr := json.Unmarshal(recallData, &recallMap)
	require.NoError(t, recallUnmarshalErr)

	assert.Contains(t, recallMap, "final_score", "RecallResult top-level must have 'final_score'")
	assert.NotContains(t, recallMap, "score", "RecallResult must NOT have a bare top-level 'score'")
}

// TestPluginContract_CapturedMemoryJSONShape validates that models.CapturedMemory
// serializes with the fields expected by capture output consumers.
func TestPluginContract_CapturedMemoryJSONShape(t *testing.T) {

	captured := models.CapturedMemory{
		Content:    "Prefer small, focused functions over large ones",
		Type:       models.MemoryTypeRule,
		Confidence: 0.85,
		Tags:       []string{"style", "go"},
	}

	data, marshalErr := json.Marshal(captured)
	require.NoError(t, marshalErr)

	var m map[string]interface{}
	unmarshalErr := json.Unmarshal(data, &m)
	require.NoError(t, unmarshalErr)

	requiredFields := []string{"content", "type", "confidence", "tags"}
	for i := range requiredFields {
		assert.Contains(t, m, requiredFields[i], "CapturedMemory must have field '%s'", requiredFields[i])
	}

	assert.IsType(t, "", m["content"], "content must be a string")
	assert.IsType(t, "", m["type"], "type must be a string")
	assert.IsType(t, float64(0), m["confidence"], "confidence must be a number")
	assert.IsType(t, []interface{}{}, m["tags"], "tags must be an array")

	// Validate round-trip value correctness.
	assert.Equal(t, "rule", m["type"])
	assert.InDelta(t, 0.85, m["confidence"].(float64), 1e-9)
}

// TestPluginContract_CollectionStatsJSONShape validates that models.CollectionStats
// serializes with the expected summary fields.
func TestPluginContract_CollectionStatsJSONShape(t *testing.T) {

	stats := models.CollectionStats{
		TotalMemories: 42,
		ByType: map[string]int64{
			"rule":       10,
			"fact":       20,
			"episode":    5,
			"procedure":  4,
			"preference": 3,
		},
		ByScope: map[string]int64{
			"permanent": 30,
			"project":   8,
			"session":   3,
			"ttl":       1,
		},
	}

	data, marshalErr := json.Marshal(stats)
	require.NoError(t, marshalErr)

	var m map[string]interface{}
	unmarshalErr := json.Unmarshal(data, &m)
	require.NoError(t, unmarshalErr)

	assert.Contains(t, m, "total_memories", "CollectionStats must have 'total_memories'")
	assert.Contains(t, m, "by_type", "CollectionStats must have 'by_type'")
	assert.Contains(t, m, "by_scope", "CollectionStats must have 'by_scope'")

	assert.IsType(t, float64(0), m["total_memories"], "total_memories must be a number")
	assert.IsType(t, map[string]interface{}{}, m["by_type"], "by_type must be an object")
	assert.IsType(t, map[string]interface{}{}, m["by_scope"], "by_scope must be an object")

	totalMemories := m["total_memories"].(float64)
	assert.Equal(t, float64(42), totalMemories)

	byType, ok := m["by_type"].(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, byType, "rule")
	assert.Contains(t, byType, "fact")

	byScope, ok := m["by_scope"].(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, byScope, "permanent")
	assert.Contains(t, byScope, "session")
}

// TestPluginContract_RecallResultNewFields validates that the new enhanced
// scoring fields are present in the JSON serialization and have the correct types.
func TestPluginContract_RecallResultNewFields(t *testing.T) {
	result := models.RecallResult{
		Memory:              sampleMemory(),
		SimilarityScore:     0.85,
		RecencyScore:        0.72,
		FrequencyScore:      0.50,
		TypeBoost:           1.00,
		ScopeBoost:          0.67,
		ConfidenceScore:     0.90,
		ReinforcementScore:  0.60,
		TagAffinityScore:    0.75,
		SupersessionPenalty: 1.0,
		ConflictPenalty:     0.8,
		FinalScore:          0.65,
	}

	data, marshalErr := json.Marshal(result)
	require.NoError(t, marshalErr)

	var m map[string]interface{}
	unmarshalErr := json.Unmarshal(data, &m)
	require.NoError(t, unmarshalErr)

	// New fields must exist in JSON output
	newFields := []string{
		"confidence_score",
		"reinforcement_score",
		"tag_affinity_score",
		"supersession_penalty",
		"conflict_penalty",
	}
	for i := range newFields {
		assert.Contains(t, m, newFields[i], "RecallResult must have '%s' field", newFields[i])
		assert.IsType(t, float64(0), m[newFields[i]], "%s must be a float64", newFields[i])
	}

	// Old fields must still exist (backwards compatibility)
	oldFields := []string{
		"memory",
		"similarity_score",
		"recency_score",
		"frequency_score",
		"type_boost",
		"scope_boost",
		"final_score",
	}
	for i := range oldFields {
		assert.Contains(t, m, oldFields[i], "RecallResult must still have '%s' field", oldFields[i])
	}
}

// TestPluginContract_RecallResultNewFieldsRoundTrip validates that all new
// RecallResult fields survive a JSON marshal/unmarshal round trip.
func TestPluginContract_RecallResultNewFieldsRoundTrip(t *testing.T) {
	original := models.RecallResult{
		Memory:              sampleMemory(),
		SimilarityScore:     0.85,
		RecencyScore:        0.72,
		FrequencyScore:      0.50,
		TypeBoost:           1.00,
		ScopeBoost:          0.67,
		ConfidenceScore:     0.91,
		ReinforcementScore:  0.63,
		TagAffinityScore:    0.75,
		SupersessionPenalty: 0.3,
		ConflictPenalty:     0.8,
		FinalScore:          0.42,
	}

	data, marshalErr := json.Marshal(original)
	require.NoError(t, marshalErr)

	var roundTripped models.RecallResult
	unmarshalErr := json.Unmarshal(data, &roundTripped)
	require.NoError(t, unmarshalErr)

	assert.InDelta(t, original.ConfidenceScore, roundTripped.ConfidenceScore, 1e-9)
	assert.InDelta(t, original.ReinforcementScore, roundTripped.ReinforcementScore, 1e-9)
	assert.InDelta(t, original.TagAffinityScore, roundTripped.TagAffinityScore, 1e-9)
	assert.InDelta(t, original.SupersessionPenalty, roundTripped.SupersessionPenalty, 1e-9)
	assert.InDelta(t, original.ConflictPenalty, roundTripped.ConflictPenalty, 1e-9)

	// Verify old fields also round-trip
	assert.InDelta(t, original.SimilarityScore, roundTripped.SimilarityScore, 1e-9)
	assert.InDelta(t, original.RecencyScore, roundTripped.RecencyScore, 1e-9)
	assert.InDelta(t, original.FrequencyScore, roundTripped.FrequencyScore, 1e-9)
	assert.InDelta(t, original.TypeBoost, roundTripped.TypeBoost, 1e-9)
	assert.InDelta(t, original.ScopeBoost, roundTripped.ScopeBoost, 1e-9)
	assert.InDelta(t, original.FinalScore, roundTripped.FinalScore, 1e-9)
}
