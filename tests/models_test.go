package tests

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
)

func TestMemoryTypeIsValid(t *testing.T) {
	validTypes := []models.MemoryType{
		models.MemoryTypeRule,
		models.MemoryTypeFact,
		models.MemoryTypeEpisode,
		models.MemoryTypeProcedure,
		models.MemoryTypePreference,
	}
	for _, mt := range validTypes {
		t.Run(string(mt), func(t *testing.T) {
			assert.True(t, mt.IsValid())
		})
	}

	invalid := models.MemoryType("bogus")
	assert.False(t, invalid.IsValid())
}

func TestMemoryScopeIsValid(t *testing.T) {
	validScopes := []models.MemoryScope{
		models.ScopePermanent,
		models.ScopeProject,
		models.ScopeSession,
		models.ScopeTTL,
	}
	for _, ms := range validScopes {
		t.Run(string(ms), func(t *testing.T) {
			assert.True(t, ms.IsValid())
		})
	}

	invalid := models.MemoryScope("unknown")
	assert.False(t, invalid.IsValid())
}

func TestValidMemoryTypesContainsAll(t *testing.T) {
	expected := []models.MemoryType{
		models.MemoryTypeRule,
		models.MemoryTypeFact,
		models.MemoryTypeEpisode,
		models.MemoryTypeProcedure,
		models.MemoryTypePreference,
	}
	assert.Len(t, models.ValidMemoryTypes, len(expected))
	for _, e := range expected {
		assert.Contains(t, models.ValidMemoryTypes, e)
	}
}

func TestValidMemoryScopesContainsAll(t *testing.T) {
	expected := []models.MemoryScope{
		models.ScopePermanent,
		models.ScopeProject,
		models.ScopeSession,
		models.ScopeTTL,
	}
	assert.Len(t, models.ValidMemoryScopes, len(expected))
	for _, e := range expected {
		assert.Contains(t, models.ValidMemoryScopes, e)
	}
}
