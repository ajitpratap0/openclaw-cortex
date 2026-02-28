package tests

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

func newPersistenceTestEntity(id, name string, et models.EntityType, aliases ...string) models.Entity {
	now := time.Now().UTC()
	return models.Entity{
		ID:        id,
		Name:      name,
		Type:      et,
		Aliases:   aliases,
		MemoryIDs: []string{},
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// TestEntityPersistence_UpsertAndGetRoundTrip verifies that an entity written via
// UpsertEntity can be retrieved intact via GetEntity.
func TestEntityPersistence_UpsertAndGetRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	entity := newPersistenceTestEntity("ep-1", "Go Language", models.EntityTypeConcept, "golang", "Go")
	entity.Metadata = map[string]any{"source": "test"}

	err := s.UpsertEntity(ctx, entity)
	require.NoError(t, err)

	got, err := s.GetEntity(ctx, "ep-1")
	require.NoError(t, err)
	require.NotNil(t, got)

	assert.Equal(t, "ep-1", got.ID)
	assert.Equal(t, "Go Language", got.Name)
	assert.Equal(t, models.EntityTypeConcept, got.Type)
	assert.Equal(t, []string{"golang", "Go"}, got.Aliases)
}

// TestEntityPersistence_GetEntity_NotFound verifies that GetEntity returns ErrNotFound
// for a missing ID.
func TestEntityPersistence_GetEntity_NotFound(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	got, err := s.GetEntity(ctx, "does-not-exist")
	require.Error(t, err)
	assert.True(t, errors.Is(err, store.ErrNotFound), "expected ErrNotFound, got: %v", err)
	assert.Nil(t, got)
}

// TestEntityPersistence_UpsertUpdates verifies that upserting an entity with an
// existing ID overwrites the stored record.
func TestEntityPersistence_UpsertUpdates(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	entity := newPersistenceTestEntity("ep-2", "Original Name", models.EntityTypeProject)
	require.NoError(t, s.UpsertEntity(ctx, entity))

	entity.Name = "Updated Name"
	entity.Aliases = []string{"alias-new"}
	require.NoError(t, s.UpsertEntity(ctx, entity))

	got, err := s.GetEntity(ctx, "ep-2")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "Updated Name", got.Name)
	assert.Equal(t, []string{"alias-new"}, got.Aliases)
}

// TestEntityPersistence_SearchEntitiesByName verifies that SearchEntities finds
// entities whose Name contains the search string as a case-insensitive substring.
func TestEntityPersistence_SearchEntitiesByName(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	entities := []models.Entity{
		newPersistenceTestEntity("ep-s1", "Go Language", models.EntityTypeConcept),
		newPersistenceTestEntity("ep-s2", "Go Runtime", models.EntityTypeSystem),
		newPersistenceTestEntity("ep-s3", "Python Interpreter", models.EntityTypeConcept),
	}
	for i := range entities {
		require.NoError(t, s.UpsertEntity(ctx, entities[i]))
	}

	results, err := s.SearchEntities(ctx, "Go")
	require.NoError(t, err)
	assert.Len(t, results, 2, "expected 2 entities matching 'Go'")

	results, err = s.SearchEntities(ctx, "python")
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "Python Interpreter", results[0].Name)

	results, err = s.SearchEntities(ctx, "Nonexistent")
	require.NoError(t, err)
	assert.Empty(t, results)
}

// TestEntityPersistence_SearchEntitiesByAlias verifies that SearchEntities also
// matches on entity aliases (case-insensitive substring).
func TestEntityPersistence_SearchEntitiesByAlias(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	entity := newPersistenceTestEntity("ep-a1", "Anthropic Claude", models.EntityTypeSystem, "Claude AI", "Claude")
	require.NoError(t, s.UpsertEntity(ctx, entity))

	results, err := s.SearchEntities(ctx, "claude ai")
	require.NoError(t, err)
	assert.Len(t, results, 1, "expected entity found by alias 'claude ai'")
	assert.Equal(t, "ep-a1", results[0].ID)
}

// TestEntityPersistence_LinkMemoryToEntity verifies that LinkMemoryToEntity appends
// the memory ID to the entity's MemoryIDs list.
func TestEntityPersistence_LinkMemoryToEntity(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	entity := newPersistenceTestEntity("ep-l1", "Linked Entity", models.EntityTypeConcept)
	require.NoError(t, s.UpsertEntity(ctx, entity))

	err := s.LinkMemoryToEntity(ctx, "ep-l1", "mem-001")
	require.NoError(t, err)

	got, err := s.GetEntity(ctx, "ep-l1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Contains(t, got.MemoryIDs, "mem-001")
}

// TestEntityPersistence_LinkMemoryToEntity_NoDuplicates verifies that linking the
// same memory ID twice does not produce duplicate entries.
func TestEntityPersistence_LinkMemoryToEntity_NoDuplicates(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	entity := newPersistenceTestEntity("ep-l2", "Dedup Entity", models.EntityTypeConcept)
	require.NoError(t, s.UpsertEntity(ctx, entity))

	require.NoError(t, s.LinkMemoryToEntity(ctx, "ep-l2", "mem-dup"))
	require.NoError(t, s.LinkMemoryToEntity(ctx, "ep-l2", "mem-dup"))

	got, err := s.GetEntity(ctx, "ep-l2")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Len(t, got.MemoryIDs, 1, "duplicate memory ID must not be stored twice")
}

// TestEntityPersistence_LinkMemoryToEntity_MultipleLinks verifies that multiple
// distinct memory IDs are all stored.
func TestEntityPersistence_LinkMemoryToEntity_MultipleLinks(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	entity := newPersistenceTestEntity("ep-l3", "Multi-Link Entity", models.EntityTypeConcept)
	require.NoError(t, s.UpsertEntity(ctx, entity))

	require.NoError(t, s.LinkMemoryToEntity(ctx, "ep-l3", "mem-alpha"))
	require.NoError(t, s.LinkMemoryToEntity(ctx, "ep-l3", "mem-beta"))
	require.NoError(t, s.LinkMemoryToEntity(ctx, "ep-l3", "mem-gamma"))

	got, err := s.GetEntity(ctx, "ep-l3")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Len(t, got.MemoryIDs, 3)
	assert.Contains(t, got.MemoryIDs, "mem-alpha")
	assert.Contains(t, got.MemoryIDs, "mem-beta")
	assert.Contains(t, got.MemoryIDs, "mem-gamma")
}

// TestEntityPersistence_LinkMemoryToEntity_EntityNotFound verifies that linking a
// memory to a non-existent entity returns an error.
func TestEntityPersistence_LinkMemoryToEntity_EntityNotFound(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	err := s.LinkMemoryToEntity(ctx, "does-not-exist", "mem-001")
	assert.Error(t, err)
}

// TestEntityPersistence_SearchCaseInsensitive verifies case-insensitive matching
// for both upper and lower case queries.
func TestEntityPersistence_SearchCaseInsensitive(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	entity := newPersistenceTestEntity("ep-ci1", "OpenClaw Cortex", models.EntityTypeProject)
	require.NoError(t, s.UpsertEntity(ctx, entity))

	for _, query := range []string{"openclaw", "OPENCLAW", "OpenClaw", "CORTEX", "cortex"} {
		results, err := s.SearchEntities(ctx, query)
		require.NoError(t, err, "query: %q", query)
		assert.Len(t, results, 1, "expected 1 result for query %q", query)
	}
}
