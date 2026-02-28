package tests

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/capture"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

// --- Entity model tests ---

func TestEntityType_IsValid(t *testing.T) {
	valid := []models.EntityType{
		models.EntityTypePerson,
		models.EntityTypeProject,
		models.EntityTypeSystem,
		models.EntityTypeDecision,
		models.EntityTypeConcept,
	}
	for i := range valid {
		assert.True(t, valid[i].IsValid(), "expected %q to be valid", valid[i])
	}

	assert.False(t, models.EntityType("unknown").IsValid())
	assert.False(t, models.EntityType("").IsValid())
}

func TestEntityJSON_RoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	entity := models.Entity{
		ID:        "ent-1",
		Name:      "Go Language",
		Type:      models.EntityTypeConcept,
		Aliases:   []string{"golang", "Go"},
		MemoryIDs: []string{"mem-1", "mem-2"},
		CreatedAt: now,
		UpdatedAt: now,
		Metadata:  map[string]any{"source": "test"},
	}

	data, err := json.Marshal(entity)
	require.NoError(t, err)

	var decoded models.Entity
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, entity.ID, decoded.ID)
	assert.Equal(t, entity.Name, decoded.Name)
	assert.Equal(t, entity.Type, decoded.Type)
	assert.Equal(t, entity.Aliases, decoded.Aliases)
	assert.Equal(t, entity.MemoryIDs, decoded.MemoryIDs)
}

// --- MockStore entity CRUD tests ---

func newTestEntity(id, name string, et models.EntityType) models.Entity {
	now := time.Now().UTC()
	return models.Entity{
		ID:        id,
		Name:      name,
		Type:      et,
		Aliases:   []string{},
		MemoryIDs: []string{},
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func TestMockStore_UpsertAndGetEntity(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	entity := newTestEntity("ent-get-1", "Alice Smith", models.EntityTypePerson)
	err := s.UpsertEntity(ctx, entity)
	require.NoError(t, err)

	got, err := s.GetEntity(ctx, "ent-get-1")
	require.NoError(t, err)
	require.NotNil(t, got)

	assert.Equal(t, "ent-get-1", got.ID)
	assert.Equal(t, "Alice Smith", got.Name)
	assert.Equal(t, models.EntityTypePerson, got.Type)
}

func TestMockStore_GetEntity_NotFound(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	got, err := s.GetEntity(ctx, "nonexistent-id")
	require.Error(t, err)
	assert.True(t, errors.Is(err, store.ErrNotFound), "expected ErrNotFound, got: %v", err)
	assert.Nil(t, got)
}

func TestMockStore_UpsertEntity_Update(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	entity := newTestEntity("ent-upd-1", "Original Name", models.EntityTypeProject)
	require.NoError(t, s.UpsertEntity(ctx, entity))

	entity.Name = "Updated Name"
	entity.Aliases = []string{"alias-1"}
	require.NoError(t, s.UpsertEntity(ctx, entity))

	got, err := s.GetEntity(ctx, "ent-upd-1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "Updated Name", got.Name)
	assert.Equal(t, []string{"alias-1"}, got.Aliases)
}

func TestMockStore_SearchEntities(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	entities := []models.Entity{
		newTestEntity("ent-s-1", "Go Language", models.EntityTypeConcept),
		newTestEntity("ent-s-2", "Go Runtime", models.EntityTypeSystem),
		newTestEntity("ent-s-3", "Python", models.EntityTypeConcept),
	}
	for i := range entities {
		require.NoError(t, s.UpsertEntity(ctx, entities[i]))
	}

	results, err := s.SearchEntities(ctx, "Go")
	require.NoError(t, err)
	assert.Len(t, results, 2)

	results, err = s.SearchEntities(ctx, "Python")
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "Python", results[0].Name)

	results, err = s.SearchEntities(ctx, "Nonexistent")
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestMockStore_SearchEntities_CaseInsensitive(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	entity := newTestEntity("ent-ci-1", "OpenClaw Cortex", models.EntityTypeProject)
	require.NoError(t, s.UpsertEntity(ctx, entity))

	results, err := s.SearchEntities(ctx, "openclaw")
	require.NoError(t, err)
	assert.Len(t, results, 1)

	results, err = s.SearchEntities(ctx, "CORTEX")
	require.NoError(t, err)
	assert.Len(t, results, 1)
}

func TestMockStore_LinkMemoryToEntity(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	entity := newTestEntity("ent-link-1", "Test Entity", models.EntityTypeConcept)
	require.NoError(t, s.UpsertEntity(ctx, entity))

	err := s.LinkMemoryToEntity(ctx, "ent-link-1", "mem-abc")
	require.NoError(t, err)

	got, err := s.GetEntity(ctx, "ent-link-1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Contains(t, got.MemoryIDs, "mem-abc")
}

func TestMockStore_LinkMemoryToEntity_NoDuplicates(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	entity := newTestEntity("ent-dup-1", "Test Entity", models.EntityTypeConcept)
	require.NoError(t, s.UpsertEntity(ctx, entity))

	require.NoError(t, s.LinkMemoryToEntity(ctx, "ent-dup-1", "mem-xyz"))
	require.NoError(t, s.LinkMemoryToEntity(ctx, "ent-dup-1", "mem-xyz"))

	got, err := s.GetEntity(ctx, "ent-dup-1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Len(t, got.MemoryIDs, 1, "duplicate memory ID should not be added twice")
}

func TestMockStore_LinkMemoryToEntity_NotFound(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	err := s.LinkMemoryToEntity(ctx, "nonexistent-entity", "mem-1")
	assert.Error(t, err)
}

func TestMockStore_EntityIsolation(t *testing.T) {
	// Ensure GetEntity returns a copy and callers cannot mutate stored state.
	ctx := context.Background()
	s := store.NewMockStore()

	entity := newTestEntity("ent-iso-1", "Isolated Entity", models.EntityTypeConcept)
	entity.Aliases = []string{"alias-a"}
	require.NoError(t, s.UpsertEntity(ctx, entity))

	got, err := s.GetEntity(ctx, "ent-iso-1")
	require.NoError(t, err)
	require.NotNil(t, got)

	// Mutate the returned value.
	got.Aliases = append(got.Aliases, "alias-b")

	// The stored entity should be unaffected.
	got2, err := s.GetEntity(ctx, "ent-iso-1")
	require.NoError(t, err)
	assert.Len(t, got2.Aliases, 1, "stored entity should not be mutated by caller")
}

// --- EntityExtractor graceful degradation test ---

func TestEntityExtractor_GracefulDegradation(t *testing.T) {
	// Using a fake API key triggers an authentication error from the Claude API.
	// The extractor should log a warning and return (nil, nil) rather than propagating the error.
	if testing.Short() {
		t.Skip("skipping: requires network (graceful degradation path hits Claude API)")
	}

	ctx := context.Background()
	extractor := capture.NewEntityExtractor("sk-fake-key-for-testing", "claude-haiku-4-5", nil)
	entities, err := extractor.Extract(ctx, "Alice works on the OpenClaw project.")

	// On API error, extractor should degrade gracefully: no error, no entities.
	assert.NoError(t, err)
	assert.Nil(t, entities)
}

func TestEntityExtractor_ParseResponse(t *testing.T) {
	// Test the JSON parsing shape used by EntityExtractor independently.
	responseText := `[{"name":"Alice","type":"person","aliases":["Al"]},{"name":"OpenClaw","type":"project","aliases":[]}]`

	type capturedEntity struct {
		Name    string   `json:"name"`
		Type    string   `json:"type"`
		Aliases []string `json:"aliases"`
	}

	var raw []capturedEntity
	err := json.Unmarshal([]byte(responseText), &raw)
	require.NoError(t, err)
	require.Len(t, raw, 2)

	assert.Equal(t, "Alice", raw[0].Name)
	assert.Equal(t, "person", raw[0].Type)
	assert.Equal(t, []string{"Al"}, raw[0].Aliases)

	assert.Equal(t, "OpenClaw", raw[1].Name)
	assert.Equal(t, "project", raw[1].Type)
}

func TestValidEntityTypes_Coverage(t *testing.T) {
	assert.Len(t, models.ValidEntityTypes, 5)
	for i := range models.ValidEntityTypes {
		assert.True(t, models.ValidEntityTypes[i].IsValid())
	}
}
