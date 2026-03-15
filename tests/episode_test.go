package tests

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/graph"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
)

// TestEpisodeModel verifies the Episode struct fields are correctly populated.
func TestEpisodeModel(t *testing.T) {
	ep := models.Episode{
		UUID:         uuid.New().String(),
		SessionID:    "session-abc",
		UserMsg:      "What is the capital of France?",
		AssistantMsg: "Paris is the capital of France.",
		CapturedAt:   time.Now().UTC(),
		MemoryIDs:    []string{"mem-1", "mem-2"},
		FactIDs:      []string{"fact-1"},
	}

	assert.NotEmpty(t, ep.UUID)
	assert.Equal(t, "session-abc", ep.SessionID)
	assert.Equal(t, "What is the capital of France?", ep.UserMsg)
	assert.Equal(t, "Paris is the capital of France.", ep.AssistantMsg)
	assert.False(t, ep.CapturedAt.IsZero())
	assert.Equal(t, 2, len(ep.MemoryIDs))
	assert.Equal(t, 1, len(ep.FactIDs))
}

// TestMockGraphClientCreateEpisode verifies Episode CRUD on MockGraphClient.
func TestMockGraphClientCreateEpisode(t *testing.T) {
	ctx := context.Background()
	gc := graph.NewMockGraphClient()

	ep := models.Episode{
		UUID:         uuid.New().String(),
		SessionID:    "sess-001",
		UserMsg:      "user message",
		AssistantMsg: "assistant response",
		CapturedAt:   time.Now().UTC(),
		MemoryIDs:    []string{"mem-a", "mem-b"},
		FactIDs:      []string{"fact-x"},
	}

	err := gc.CreateEpisode(ctx, ep)
	require.NoError(t, err)

	// Verify it appears in GetEpisodesForMemory
	episodes, err := gc.GetEpisodesForMemory(ctx, "mem-a")
	require.NoError(t, err)
	require.Len(t, episodes, 1)
	assert.Equal(t, ep.UUID, episodes[0].UUID)
	assert.Equal(t, ep.SessionID, episodes[0].SessionID)
	assert.Equal(t, ep.MemoryIDs, episodes[0].MemoryIDs)
}

// TestMockGraphClientGetEpisodesForMemory_NoMatch returns empty for unknown memory.
func TestMockGraphClientGetEpisodesForMemory_NoMatch(t *testing.T) {
	ctx := context.Background()
	gc := graph.NewMockGraphClient()

	episodes, err := gc.GetEpisodesForMemory(ctx, "nonexistent-memory")
	require.NoError(t, err)
	assert.Empty(t, episodes)
}

// TestMockGraphClientMultipleEpisodesSameMemory ensures multiple episodes linking
// to the same memory are all returned.
func TestMockGraphClientMultipleEpisodesSameMemory(t *testing.T) {
	ctx := context.Background()
	gc := graph.NewMockGraphClient()

	sharedMemID := "mem-shared"

	for i := 0; i < 3; i++ {
		ep := models.Episode{
			UUID:      uuid.New().String(),
			SessionID: "session-" + string(rune('A'+i)),
			MemoryIDs: []string{sharedMemID},
			FactIDs:   []string{},
		}
		require.NoError(t, gc.CreateEpisode(ctx, ep))
	}

	episodes, err := gc.GetEpisodesForMemory(ctx, sharedMemID)
	require.NoError(t, err)
	assert.Len(t, episodes, 3)
}

// TestEpisodeTripleExtractionPipeline is an integration-style test that
// verifies the capture pipeline logic: memories → facts → episode.
// Uses MockGraphClient to avoid needing a real Memgraph instance.
func TestEpisodeTripleExtractionPipeline(t *testing.T) {
	ctx := context.Background()
	gc := graph.NewMockGraphClient()

	// Simulate what cmd_capture.go does after extraction:
	// 1. Upsert entities
	entity1 := models.Entity{
		ID:   uuid.New().String(),
		Name: "Ajit",
		Type: "Person",
	}
	entity2 := models.Entity{
		ID:   uuid.New().String(),
		Name: "Booking.com",
		Type: "Organization",
	}
	require.NoError(t, gc.UpsertEntity(ctx, entity1))
	require.NoError(t, gc.UpsertEntity(ctx, entity2))

	// 2. Upsert a fact
	now := time.Now().UTC()
	fact := models.Fact{
		ID:              uuid.New().String(),
		SourceEntityID:  entity1.ID,
		TargetEntityID:  entity2.ID,
		RelationType:    "WORKS_AT",
		Fact:            "Ajit works at Booking.com",
		CreatedAt:       now,
		ValidAt:         &now,
		SourceMemoryIDs: []string{"mem-1"},
		Confidence:      0.95,
	}
	require.NoError(t, gc.UpsertFact(ctx, fact))
	require.NoError(t, gc.AppendMemoryToFact(ctx, fact.ID, "mem-1"))
	require.NoError(t, gc.AppendEpisode(ctx, fact.ID, "sess-xyz"))

	// 3. Create Episode
	ep := models.Episode{
		UUID:         uuid.New().String(),
		SessionID:    "sess-xyz",
		UserMsg:      "Where does Ajit work?",
		AssistantMsg: "Ajit works at Booking.com as Engineering Manager.",
		CapturedAt:   now,
		MemoryIDs:    []string{"mem-1"},
		FactIDs:      []string{fact.ID},
	}
	require.NoError(t, gc.CreateEpisode(ctx, ep))

	// 4. Verify Episode is linked to memory
	episodes, err := gc.GetEpisodesForMemory(ctx, "mem-1")
	require.NoError(t, err)
	require.Len(t, episodes, 1)
	assert.Equal(t, ep.UUID, episodes[0].UUID)
	assert.Contains(t, episodes[0].FactIDs, fact.ID)

	// 5. Verify facts are stored and linked
	facts, err := gc.GetFactsBetween(ctx, entity1.ID, entity2.ID)
	require.NoError(t, err)
	require.Len(t, facts, 1)
	assert.Equal(t, "WORKS_AT", facts[0].RelationType)
	assert.Contains(t, facts[0].SourceMemoryIDs, "mem-1")

	// 6. Verify GetEpisodes returns our episode
	all := gc.GetEpisodes()
	assert.Len(t, all, 1)
}
