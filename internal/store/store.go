package store

import (
	"context"
	"errors"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
)

// ErrNotFound is returned by Get and Delete when the requested memory does not exist.
var ErrNotFound = errors.New("memory not found")

// Store defines the interface for memory persistence with vector search.
type Store interface {
	// EnsureCollection creates the vector collection if it doesn't exist.
	EnsureCollection(ctx context.Context) error

	// Upsert inserts or updates a memory with its embedding vector.
	Upsert(ctx context.Context, memory models.Memory, vector []float32) error

	// Search finds memories similar to the query vector.
	Search(ctx context.Context, vector []float32, limit uint64, filters *SearchFilters) ([]models.SearchResult, error)

	// Get retrieves a single memory by ID.
	Get(ctx context.Context, id string) (*models.Memory, error)

	// Delete removes a memory by ID.
	Delete(ctx context.Context, id string) error

	// List returns memories matching the given filters.
	// The cursor parameter is opaque; pass "" for the first page.
	// The returned cursor is empty when no more results remain.
	List(ctx context.Context, filters *SearchFilters, limit uint64, cursor string) ([]models.Memory, string, error)

	// FindDuplicates returns memories with cosine similarity above the threshold.
	FindDuplicates(ctx context.Context, vector []float32, threshold float64) ([]models.SearchResult, error)

	// UpdateAccessMetadata increments access count and updates last_accessed time.
	UpdateAccessMetadata(ctx context.Context, id string) error

	// Stats returns collection statistics.
	Stats(ctx context.Context) (*models.CollectionStats, error)

	// UpsertEntity inserts or updates an entity.
	UpsertEntity(ctx context.Context, entity models.Entity) error

	// GetEntity retrieves a single entity by ID.
	GetEntity(ctx context.Context, id string) (*models.Entity, error)

	// SearchEntities finds entities whose name contains the given string.
	SearchEntities(ctx context.Context, name string) ([]models.Entity, error)

	// LinkMemoryToEntity adds a memory ID to an entity's memory list.
	LinkMemoryToEntity(ctx context.Context, entityID, memoryID string) error

	// GetChain follows the SupersedesID chain and returns the full history.
	// The chain is returned newest first, stopping when SupersedesID is empty
	// or the referenced memory is not found.
	GetChain(ctx context.Context, id string) ([]models.Memory, error)

	// Close cleans up resources.
	Close() error
}

// SearchFilters allows filtering search results.
type SearchFilters struct {
	Type       *models.MemoryType       `json:"type,omitempty"`
	Scope      *models.MemoryScope      `json:"scope,omitempty"`
	Visibility *models.MemoryVisibility `json:"visibility,omitempty"`
	Project    *string                  `json:"project,omitempty"`
	Tags       []string                 `json:"tags,omitempty"`
	Source     *string                  `json:"source,omitempty"`
}
