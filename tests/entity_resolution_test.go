package tests

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/graph"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
)

func TestEntityResolver_ExactNameMatch(t *testing.T) {
	mock := graph.NewMockGraphClient()
	ctx := context.Background()

	// Seed an existing entity
	existing := models.Entity{
		ID:   "existing-1",
		Name: "John Smith",
		Type: models.EntityTypePerson,
	}
	require.NoError(t, mock.UpsertEntity(ctx, existing))

	resolver := graph.NewEntityResolver(mock, "", "claude-3-haiku-20240307", 0.9, 10, slog.Default())

	extracted := models.Entity{
		ID:   "new-1",
		Name: "john smith", // case differs
		Type: models.EntityTypePerson,
	}

	resolvedID, isNew, err := resolver.Resolve(ctx, extracted, nil, "")
	require.NoError(t, err)
	assert.False(t, isNew, "should resolve to existing entity")
	assert.Equal(t, "existing-1", resolvedID)
}

func TestEntityResolver_AliasMatch(t *testing.T) {
	mock := graph.NewMockGraphClient()
	ctx := context.Background()

	// Seed an existing entity with an alias
	existing := models.Entity{
		ID:      "existing-2",
		Name:    "OpenClaw Cortex",
		Type:    models.EntityTypeProject,
		Aliases: []string{"cortex", "openclaw"},
	}
	require.NoError(t, mock.UpsertEntity(ctx, existing))

	resolver := graph.NewEntityResolver(mock, "", "claude-3-haiku-20240307", 0.9, 10, slog.Default())

	// The new entity's name matches an alias of the existing entity
	extracted := models.Entity{
		ID:   "new-2",
		Name: "Cortex",
		Type: models.EntityTypeProject,
	}

	resolvedID, isNew, err := resolver.Resolve(ctx, extracted, nil, "")
	require.NoError(t, err)
	assert.False(t, isNew, "should resolve via alias match")
	assert.Equal(t, "existing-2", resolvedID)
}

func TestEntityResolver_NoMatch_NewEntity(t *testing.T) {
	mock := graph.NewMockGraphClient()
	ctx := context.Background()

	// No entities seeded — empty graph
	resolver := graph.NewEntityResolver(mock, "", "claude-3-haiku-20240307", 0.9, 10, slog.Default())

	extracted := models.Entity{
		ID:   "brand-new",
		Name: "Completely Novel Entity",
		Type: models.EntityTypeConcept,
	}

	resolvedID, isNew, err := resolver.Resolve(ctx, extracted, nil, "")
	require.NoError(t, err)
	assert.True(t, isNew, "should be a new entity when no candidates exist")
	assert.Equal(t, "brand-new", resolvedID)
}

func TestEntityResolver_GracefulDegradation(t *testing.T) {
	mock := graph.NewMockGraphClient()
	ctx := context.Background()

	// Seed an entity with a different name so deterministic match fails
	// but search still returns it as a candidate (substring match in mock)
	existing := models.Entity{
		ID:   "existing-3",
		Name: "The CEO",
		Type: models.EntityTypePerson,
	}
	require.NoError(t, mock.UpsertEntity(ctx, existing))

	// No API key → Claude fallback is disabled → should treat as new
	resolver := graph.NewEntityResolver(mock, "", "claude-3-haiku-20240307", 0.9, 10, slog.Default())

	// Use a name that will be found by search (mock does substring match)
	// but won't match deterministically
	extracted := models.Entity{
		ID:   "new-3",
		Name: "CEO",
		Type: models.EntityTypePerson,
	}

	resolvedID, isNew, err := resolver.Resolve(ctx, extracted, nil, "talking about the CEO")
	require.NoError(t, err)
	assert.True(t, isNew, "should treat as new when Claude fallback fails")
	assert.Equal(t, "new-3", resolvedID)
}
