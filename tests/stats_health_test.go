package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

func TestStats_EmptyStore(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	stats, err := s.Stats(ctx)
	require.NoError(t, err)

	assert.Equal(t, int64(0), stats.TotalMemories)
	assert.Nil(t, stats.OldestMemory)
	assert.Nil(t, stats.NewestMemory)
	assert.Empty(t, stats.TopAccessed)
	assert.Equal(t, int64(0), stats.ActiveConflicts)
	assert.Equal(t, int64(0), stats.PendingTTLExpiry)
	assert.Equal(t, int64(0), stats.StorageEstimate)
	assert.NotNil(t, stats.ReinforcementTiers)
}

func TestStats_PopulatedStore(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	oldest := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	newest := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)

	mem1 := models.Memory{
		ID:         "mem-1",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Visibility: models.VisibilityShared,
		Content:    "The oldest memory in the store",
		Confidence: 0.9,
		CreatedAt:  oldest,
		UpdatedAt:  oldest,
	}
	mem2 := models.Memory{
		ID:         "mem-2",
		Type:       models.MemoryTypeRule,
		Scope:      models.ScopeProject,
		Visibility: models.VisibilityShared,
		Content:    "The newest memory in the store",
		Confidence: 0.8,
		CreatedAt:  newest,
		UpdatedAt:  newest,
	}

	require.NoError(t, s.Upsert(ctx, mem1, testVector(0.1)))
	require.NoError(t, s.Upsert(ctx, mem2, testVector(0.2)))

	stats, err := s.Stats(ctx)
	require.NoError(t, err)

	assert.Equal(t, int64(2), stats.TotalMemories)
	require.NotNil(t, stats.OldestMemory)
	require.NotNil(t, stats.NewestMemory)
	assert.Equal(t, oldest, *stats.OldestMemory)
	assert.Equal(t, newest, *stats.NewestMemory)
	assert.Equal(t, int64(2), stats.ByType["fact"]+stats.ByType["rule"])
	assert.Equal(t, int64(2), stats.ByScope["permanent"]+stats.ByScope["project"])
	// Storage estimate: 2 * 768 * 4 = 6144
	assert.Equal(t, int64(6144), stats.StorageEstimate)
}

func TestStats_TopAccessedOrdering(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	// Create 7 memories with varying access counts
	for i := 0; i < 7; i++ {
		mem := models.Memory{
			ID:          fmt.Sprintf("mem-%d", i),
			Type:        models.MemoryTypeFact,
			Scope:       models.ScopePermanent,
			Visibility:  models.VisibilityShared,
			Content:     fmt.Sprintf("Memory number %d", i),
			Confidence:  0.9,
			AccessCount: int64(i * 10),
			CreatedAt:   time.Now().UTC(),
			UpdatedAt:   time.Now().UTC(),
		}
		require.NoError(t, s.Upsert(ctx, mem, testVector(float32(i)*0.1+0.1)))
	}

	stats, err := s.Stats(ctx)
	require.NoError(t, err)

	// Should only have top 5
	require.Len(t, stats.TopAccessed, 5)

	// Verify descending order
	for i := 1; i < len(stats.TopAccessed); i++ {
		assert.GreaterOrEqual(t, stats.TopAccessed[i-1].AccessCount, stats.TopAccessed[i].AccessCount,
			"top accessed should be sorted descending by access_count")
	}

	// The highest access count should be 60 (index 6)
	assert.Equal(t, int64(60), stats.TopAccessed[0].AccessCount)
}

func TestStats_ReinforcementTierBucketing(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	testCases := []struct {
		id              string
		reinforcedCount int
		expectedTier    string
	}{
		{"rc-0", 0, "0"},
		{"rc-1", 1, "1-3"},
		{"rc-3", 3, "1-3"},
		{"rc-4", 4, "4-10"},
		{"rc-10", 10, "4-10"},
		{"rc-11", 11, "10+"},
		{"rc-50", 50, "10+"},
	}

	for _, tc := range testCases {
		mem := models.Memory{
			ID:              tc.id,
			Type:            models.MemoryTypeFact,
			Scope:           models.ScopePermanent,
			Visibility:      models.VisibilityShared,
			Content:         "test memory " + tc.id,
			Confidence:      0.9,
			ReinforcedCount: tc.reinforcedCount,
			CreatedAt:       time.Now().UTC(),
			UpdatedAt:       time.Now().UTC(),
		}
		require.NoError(t, s.Upsert(ctx, mem, testVector(0.5)))
	}

	stats, err := s.Stats(ctx)
	require.NoError(t, err)

	assert.Equal(t, int64(1), stats.ReinforcementTiers["0"])
	assert.Equal(t, int64(2), stats.ReinforcementTiers["1-3"])
	assert.Equal(t, int64(2), stats.ReinforcementTiers["4-10"])
	assert.Equal(t, int64(2), stats.ReinforcementTiers["10+"])
}

func TestStats_ActiveConflicts(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	mem1 := models.Memory{
		ID:             "conflict-1",
		Type:           models.MemoryTypeFact,
		Scope:          models.ScopePermanent,
		Visibility:     models.VisibilityShared,
		Content:        "conflicting memory 1",
		Confidence:     0.8,
		ConflictStatus: "active",
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}
	mem2 := models.Memory{
		ID:             "conflict-2",
		Type:           models.MemoryTypeFact,
		Scope:          models.ScopePermanent,
		Visibility:     models.VisibilityShared,
		Content:        "conflicting memory 2",
		Confidence:     0.8,
		ConflictStatus: "active",
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}
	mem3 := models.Memory{
		ID:             "resolved-1",
		Type:           models.MemoryTypeFact,
		Scope:          models.ScopePermanent,
		Visibility:     models.VisibilityShared,
		Content:        "resolved conflict",
		Confidence:     0.8,
		ConflictStatus: "resolved",
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}
	mem4 := models.Memory{
		ID:         "no-conflict",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Visibility: models.VisibilityShared,
		Content:    "normal memory",
		Confidence: 0.8,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}

	require.NoError(t, s.Upsert(ctx, mem1, testVector(0.1)))
	require.NoError(t, s.Upsert(ctx, mem2, testVector(0.2)))
	require.NoError(t, s.Upsert(ctx, mem3, testVector(0.3)))
	require.NoError(t, s.Upsert(ctx, mem4, testVector(0.4)))

	stats, err := s.Stats(ctx)
	require.NoError(t, err)

	assert.Equal(t, int64(2), stats.ActiveConflicts)
}

func TestStats_PendingTTLExpiry(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	now := time.Now().UTC()

	// TTL memory expiring in 1 hour (within 24h window)
	mem1 := models.Memory{
		ID:         "ttl-soon",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopeTTL,
		Visibility: models.VisibilityShared,
		Content:    "expiring soon",
		Confidence: 0.8,
		ValidUntil: now.Add(1 * time.Hour),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	// TTL memory expiring in 48 hours (outside 24h window)
	mem2 := models.Memory{
		ID:         "ttl-later",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopeTTL,
		Visibility: models.VisibilityShared,
		Content:    "expiring later",
		Confidence: 0.8,
		ValidUntil: now.Add(48 * time.Hour),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	// Non-TTL memory (should not count)
	mem3 := models.Memory{
		ID:         "permanent-1",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Visibility: models.VisibilityShared,
		Content:    "permanent memory",
		Confidence: 0.8,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	require.NoError(t, s.Upsert(ctx, mem1, testVector(0.1)))
	require.NoError(t, s.Upsert(ctx, mem2, testVector(0.2)))
	require.NoError(t, s.Upsert(ctx, mem3, testVector(0.3)))

	stats, err := s.Stats(ctx)
	require.NoError(t, err)

	assert.Equal(t, int64(1), stats.PendingTTLExpiry)
}

func TestStats_JSONContract(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	now := time.Now().UTC()
	mem := models.Memory{
		ID:              "json-test",
		Type:            models.MemoryTypeFact,
		Scope:           models.ScopePermanent,
		Visibility:      models.VisibilityShared,
		Content:         "test memory for JSON contract",
		Confidence:      0.9,
		AccessCount:     5,
		ReinforcedCount: 2,
		ConflictStatus:  "active",
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	require.NoError(t, s.Upsert(ctx, mem, testVector(0.5)))

	stats, err := s.Stats(ctx)
	require.NoError(t, err)

	// Marshal to JSON and back to verify contract
	data, marshalErr := json.Marshal(stats)
	require.NoError(t, marshalErr)

	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &parsed))

	// Verify all expected top-level keys exist
	expectedKeys := []string{
		"total_memories",
		"by_type",
		"by_scope",
		"oldest_memory",
		"newest_memory",
		"top_accessed",
		"reinforcement_tiers",
		"active_conflicts",
		"pending_ttl_expiry",
		"storage_estimate_bytes",
	}
	for _, key := range expectedKeys {
		_, ok := parsed[key]
		assert.True(t, ok, "JSON output missing expected key: %s", key)
	}

	// Verify top_accessed has expected shape
	topAccessed, ok := parsed["top_accessed"].([]interface{})
	require.True(t, ok, "top_accessed should be an array")
	require.Len(t, topAccessed, 1)

	first, ok := topAccessed[0].(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, first, "id")
	assert.Contains(t, first, "content")
	assert.Contains(t, first, "access_count")
}

func TestStats_ContentTruncation(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	longContent := ""
	for i := 0; i < 20; i++ {
		longContent += "12345"
	}
	// 100 chars, should be truncated to 80

	mem := models.Memory{
		ID:          "trunc-test",
		Type:        models.MemoryTypeFact,
		Scope:       models.ScopePermanent,
		Visibility:  models.VisibilityShared,
		Content:     longContent,
		Confidence:  0.9,
		AccessCount: 10,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	require.NoError(t, s.Upsert(ctx, mem, testVector(0.5)))

	stats, err := s.Stats(ctx)
	require.NoError(t, err)

	require.Len(t, stats.TopAccessed, 1)
	assert.Len(t, stats.TopAccessed[0].Content, 80)
}
