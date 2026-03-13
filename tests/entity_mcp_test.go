package tests

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cortexmcp "github.com/ajitpratap0/openclaw-cortex/internal/mcp"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
)

func TestMCPEntitySearch_ReturnsMatchingEntities(t *testing.T) {
	srv, ms := newMCPServer(t)
	ctx := context.Background()

	require.NoError(t, ms.UpsertEntity(ctx, models.Entity{ID: "e1", Name: "Alice", Type: models.EntityTypePerson}))
	require.NoError(t, ms.UpsertEntity(ctx, models.Entity{ID: "e2", Name: "Bob", Type: models.EntityTypePerson}))

	result, err := srv.HandleEntitySearch(ctx, makeReq("entity_search", map[string]any{"query": "Alice"}))
	require.NoError(t, err)
	assert.False(t, result.IsError, "expected no error: %s", textContent(t, result))

	var entities []models.Entity
	require.NoError(t, json.Unmarshal([]byte(textContent(t, result)), &entities))
	require.Len(t, entities, 1)
	assert.Equal(t, "Alice", entities[0].Name)
}

func TestMCPEntitySearch_EmptyQuery(t *testing.T) {
	srv, _ := newMCPServer(t)
	ctx := context.Background()

	result, err := srv.HandleEntitySearch(ctx, makeReq("entity_search", map[string]any{"query": ""}))
	require.NoError(t, err)
	assert.True(t, result.IsError, "expected error for empty query")
}

func TestMCPEntitySearch_NoResults(t *testing.T) {
	srv, _ := newMCPServer(t)
	ctx := context.Background()

	result, err := srv.HandleEntitySearch(ctx, makeReq("entity_search", map[string]any{"query": "nonexistent"}))
	require.NoError(t, err)
	assert.False(t, result.IsError)

	// Should return null or empty array JSON
	raw := textContent(t, result)
	var entities []models.Entity
	require.NoError(t, json.Unmarshal([]byte(raw), &entities))
	assert.Empty(t, entities)
}

func TestMCPEntitySearch_NilStore(t *testing.T) {
	srv := newMCPServerNilStore(t)
	ctx := context.Background()

	result, err := srv.HandleEntitySearch(ctx, makeReq("entity_search", map[string]any{"query": "Alice"}))
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

func TestMCPEntityGet_ReturnsEntity(t *testing.T) {
	srv, ms := newMCPServer(t)
	ctx := context.Background()

	require.NoError(t, ms.UpsertEntity(ctx, models.Entity{ID: "e1", Name: "Alice", Type: models.EntityTypePerson}))

	result, err := srv.HandleEntityGet(ctx, makeReq("entity_get", map[string]any{"id": "e1"}))
	require.NoError(t, err)
	assert.False(t, result.IsError, "expected no error: %s", textContent(t, result))

	var entity models.Entity
	require.NoError(t, json.Unmarshal([]byte(textContent(t, result)), &entity))
	assert.Equal(t, "Alice", entity.Name)
	assert.Equal(t, models.EntityTypePerson, entity.Type)
}

func TestMCPEntityGet_EmptyID(t *testing.T) {
	srv, _ := newMCPServer(t)
	ctx := context.Background()

	result, err := srv.HandleEntityGet(ctx, makeReq("entity_get", map[string]any{"id": ""}))
	require.NoError(t, err)
	assert.True(t, result.IsError, "expected error for empty id")
}

func TestMCPEntityGet_NotFound(t *testing.T) {
	srv, _ := newMCPServer(t)
	ctx := context.Background()

	result, err := srv.HandleEntityGet(ctx, makeReq("entity_get", map[string]any{"id": "nonexistent"}))
	require.NoError(t, err)
	assert.True(t, result.IsError, "expected error for missing entity")
}

func TestMCPEntityGet_NilStore(t *testing.T) {
	srv := newMCPServerNilStore(t)
	ctx := context.Background()

	result, err := srv.HandleEntityGet(ctx, makeReq("entity_get", map[string]any{"id": "e1"}))
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

// newMCPServerNilStore returns a Server with a nil store for nil-guard testing.
func newMCPServerNilStore(t *testing.T) *cortexmcp.Server {
	t.Helper()
	return cortexmcp.NewServer(nil, nil, nil, nil)
}
