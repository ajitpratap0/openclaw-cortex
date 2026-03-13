package tests

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

// TestUpdateLineage_CreatesNewMemoryWithSupersedesID verifies that a lineage-preserving
// update creates a new memory whose SupersedesID points to the old one.
func TestUpdateLineage_CreatesNewMemoryWithSupersedesID(t *testing.T) {
	st := store.NewMockStore()
	ctx := context.Background()

	now := time.Now().UTC()
	oldID := uuid.New().String()
	oldMem := models.Memory{
		ID:              oldID,
		Type:            models.MemoryTypeFact,
		Scope:           models.ScopePermanent,
		Visibility:      models.VisibilityShared,
		Content:         "Go was created at Google",
		Confidence:      0.9,
		Source:          "explicit",
		Tags:            []string{"golang", "history"},
		Project:         "myproject",
		CreatedAt:       now.Add(-24 * time.Hour),
		UpdatedAt:       now.Add(-24 * time.Hour),
		LastAccessed:    now.Add(-1 * time.Hour),
		AccessCount:     42,
		ReinforcedCount: 3,
	}
	vec := make([]float32, 768)
	for i := range vec {
		vec[i] = 0.1
	}
	require.NoError(t, st.Upsert(ctx, oldMem, vec))

	// Simulate what the CLI update command does: fetch old, create new with supersedes.
	old, err := st.Get(ctx, oldID)
	require.NoError(t, err)

	newID := uuid.New().String()
	newContent := "Go was created at Google in 2009"
	newNow := time.Now().UTC()
	newMem := models.Memory{
		ID:              newID,
		Type:            old.Type,
		Scope:           old.Scope,
		Visibility:      old.Visibility,
		Content:         newContent,
		Confidence:      old.Confidence,
		Source:          old.Source,
		Tags:            old.Tags,
		Project:         old.Project,
		CreatedAt:       newNow,
		UpdatedAt:       newNow,
		LastAccessed:    newNow,
		AccessCount:     old.AccessCount,
		ReinforcedCount: old.ReinforcedCount,
		SupersedesID:    oldID,
	}
	newVec := make([]float32, 768)
	for i := range newVec {
		newVec[i] = 0.2
	}
	require.NoError(t, st.Upsert(ctx, newMem, newVec))

	// Verify new memory has SupersedesID set.
	got, err := st.Get(ctx, newID)
	require.NoError(t, err)
	assert.Equal(t, oldID, got.SupersedesID)
	assert.Equal(t, newContent, got.Content)
}

// TestUpdateLineage_OldMemoryStillExists verifies the old memory is not deleted.
func TestUpdateLineage_OldMemoryStillExists(t *testing.T) {
	st := store.NewMockStore()
	ctx := context.Background()

	oldID := uuid.New().String()
	oldMem := models.Memory{
		ID:         oldID,
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Visibility: models.VisibilityShared,
		Content:    "original content",
		Confidence: 0.8,
		Source:     "explicit",
	}
	require.NoError(t, st.Upsert(ctx, oldMem, make([]float32, 768)))

	// Create new version.
	newMem := models.Memory{
		ID:           uuid.New().String(),
		Type:         oldMem.Type,
		Scope:        oldMem.Scope,
		Visibility:   oldMem.Visibility,
		Content:      "updated content",
		Confidence:   oldMem.Confidence,
		Source:       oldMem.Source,
		SupersedesID: oldID,
	}
	require.NoError(t, st.Upsert(ctx, newMem, make([]float32, 768)))

	// Old memory must still be retrievable.
	old, err := st.Get(ctx, oldID)
	require.NoError(t, err)
	assert.Equal(t, "original content", old.Content)
}

// TestUpdateLineage_CarriesForwardAccessAndReinforcedCount verifies that
// access_count and reinforced_count are preserved on the new memory.
func TestUpdateLineage_CarriesForwardAccessAndReinforcedCount(t *testing.T) {
	st := store.NewMockStore()
	ctx := context.Background()

	oldID := uuid.New().String()
	oldMem := models.Memory{
		ID:              oldID,
		Type:            models.MemoryTypeRule,
		Scope:           models.ScopePermanent,
		Visibility:      models.VisibilityShared,
		Content:         "always wrap errors",
		Confidence:      0.95,
		Source:          "explicit",
		AccessCount:     15,
		ReinforcedCount: 5,
	}
	require.NoError(t, st.Upsert(ctx, oldMem, make([]float32, 768)))

	old, err := st.Get(ctx, oldID)
	require.NoError(t, err)

	newID := uuid.New().String()
	newMem := models.Memory{
		ID:              newID,
		Type:            old.Type,
		Scope:           old.Scope,
		Visibility:      old.Visibility,
		Content:         "always wrap errors with fmt.Errorf",
		Confidence:      old.Confidence,
		Source:          old.Source,
		AccessCount:     old.AccessCount,
		ReinforcedCount: old.ReinforcedCount,
		SupersedesID:    oldID,
	}
	require.NoError(t, st.Upsert(ctx, newMem, make([]float32, 768)))

	got, err := st.Get(ctx, newID)
	require.NoError(t, err)
	assert.Equal(t, int64(15), got.AccessCount)
	assert.Equal(t, 5, got.ReinforcedCount)
	assert.Equal(t, oldID, got.SupersedesID)
}

// TestUpdateLineage_GetChainReturnsHistory verifies GetChain follows the
// supersession chain correctly after an update.
func TestUpdateLineage_GetChainReturnsHistory(t *testing.T) {
	st := store.NewMockStore()
	ctx := context.Background()

	// Create a chain: v3 -> v2 -> v1.
	v1ID := uuid.New().String()
	v1 := models.Memory{
		ID:         v1ID,
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Visibility: models.VisibilityShared,
		Content:    "version 1",
		Confidence: 0.8,
		Source:     "explicit",
	}
	require.NoError(t, st.Upsert(ctx, v1, make([]float32, 768)))

	v2ID := uuid.New().String()
	v2 := models.Memory{
		ID:           v2ID,
		Type:         v1.Type,
		Scope:        v1.Scope,
		Visibility:   v1.Visibility,
		Content:      "version 2",
		Confidence:   v1.Confidence,
		Source:       v1.Source,
		SupersedesID: v1ID,
	}
	require.NoError(t, st.Upsert(ctx, v2, make([]float32, 768)))

	v3ID := uuid.New().String()
	v3 := models.Memory{
		ID:           v3ID,
		Type:         v2.Type,
		Scope:        v2.Scope,
		Visibility:   v2.Visibility,
		Content:      "version 3",
		Confidence:   v2.Confidence,
		Source:       v2.Source,
		SupersedesID: v2ID,
	}
	require.NoError(t, st.Upsert(ctx, v3, make([]float32, 768)))

	chain, err := st.GetChain(ctx, v3ID)
	require.NoError(t, err)
	require.Len(t, chain, 3)

	// Chain is newest first.
	assert.Equal(t, v3ID, chain[0].ID)
	assert.Equal(t, v2ID, chain[1].ID)
	assert.Equal(t, v1ID, chain[2].ID)

	assert.Equal(t, "version 3", chain[0].Content)
	assert.Equal(t, "version 2", chain[1].Content)
	assert.Equal(t, "version 1", chain[2].Content)
}

// TestUpdateLineage_TypeOverride verifies that --type overrides the original type.
func TestUpdateLineage_TypeOverride(t *testing.T) {
	st := store.NewMockStore()
	ctx := context.Background()

	oldID := uuid.New().String()
	oldMem := models.Memory{
		ID:         oldID,
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Visibility: models.VisibilityShared,
		Content:    "original fact",
		Confidence: 0.8,
		Source:     "explicit",
	}
	require.NoError(t, st.Upsert(ctx, oldMem, make([]float32, 768)))

	newMem := models.Memory{
		ID:           uuid.New().String(),
		Type:         models.MemoryTypeRule, // changed from fact to rule
		Scope:        oldMem.Scope,
		Visibility:   oldMem.Visibility,
		Content:      "this is now a rule",
		Confidence:   oldMem.Confidence,
		Source:       oldMem.Source,
		SupersedesID: oldID,
	}
	require.NoError(t, st.Upsert(ctx, newMem, make([]float32, 768)))

	got, err := st.Get(ctx, newMem.ID)
	require.NoError(t, err)
	assert.Equal(t, models.MemoryTypeRule, got.Type)
	assert.Equal(t, oldID, got.SupersedesID)
}

// TestUpdateLineage_TagsOverride verifies that --tags replaces original tags.
func TestUpdateLineage_TagsOverride(t *testing.T) {
	st := store.NewMockStore()
	ctx := context.Background()

	oldID := uuid.New().String()
	oldMem := models.Memory{
		ID:         oldID,
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Visibility: models.VisibilityShared,
		Content:    "tagged memory",
		Confidence: 0.8,
		Source:     "explicit",
		Tags:       []string{"old-tag"},
	}
	require.NoError(t, st.Upsert(ctx, oldMem, make([]float32, 768)))

	newMem := models.Memory{
		ID:           uuid.New().String(),
		Type:         oldMem.Type,
		Scope:        oldMem.Scope,
		Visibility:   oldMem.Visibility,
		Content:      "updated tagged memory",
		Confidence:   oldMem.Confidence,
		Source:       oldMem.Source,
		Tags:         []string{"new-a", "new-b"},
		SupersedesID: oldID,
	}
	require.NoError(t, st.Upsert(ctx, newMem, make([]float32, 768)))

	got, err := st.Get(ctx, newMem.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"new-a", "new-b"}, got.Tags)
	assert.Equal(t, oldID, got.SupersedesID)
}
