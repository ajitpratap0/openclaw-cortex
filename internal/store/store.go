package store

import (
	"context"
	"errors"
	"time"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
)

// ErrNotFound is returned by Get, Delete, GetEntity, and related lookups when
// the requested resource does not exist.
var ErrNotFound = errors.New("not found")

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
	// entityType filters by entity type (empty = all types).
	// limit caps the number of results (0 = use implementation default).
	SearchEntities(ctx context.Context, name, entityType string, limit int) ([]models.Entity, error)

	// LinkMemoryToEntity adds a memory ID to an entity's memory list.
	LinkMemoryToEntity(ctx context.Context, entityID, memoryID string) error

	// GetChain follows the SupersedesID chain and returns the full history.
	// The chain is returned newest first, stopping when SupersedesID is empty
	// or the referenced memory is not found.
	GetChain(ctx context.Context, id string) ([]models.Memory, error)

	// UpdateConflictFields sets ConflictGroupID and ConflictStatus on an existing memory
	// without requiring a re-embed.
	UpdateConflictFields(ctx context.Context, id, conflictGroupID, conflictStatus string) error

	// UpdateReinforcement boosts the confidence of an existing memory (capped at 1.0)
	// and increments ReinforcedCount. Used when a near-duplicate is captured.
	UpdateReinforcement(ctx context.Context, id string, confidenceBoost float64) error

	// InvalidateMemory sets valid_to on a memory without deleting it.
	// Used when a superseding memory is stored (temporal versioning).
	InvalidateMemory(ctx context.Context, id string, validTo time.Time) error

	// GetHistory returns all versions of a memory chain, including invalidated ones.
	// Uses SupersedesID chain traversal. Newest version first.
	GetHistory(ctx context.Context, id string) ([]models.Memory, error)

	// MigrateTemporalFields backfills valid_from = created_at for all memories
	// that do not yet have valid_from set. Idempotent.
	MigrateTemporalFields(ctx context.Context) error

	// Close cleans up resources.
	Close() error
}

// ResettableStore extends Store with a full-wipe operation. Keeping
// DeleteAllMemories out of the main Store interface prevents production code
// paths (capture, recall, lifecycle) from accidentally calling it. Only
// cmd_reset.go and eval/benchmark harnesses interact with this interface.
type ResettableStore interface {
	Store
	// DeleteAllMemories removes all data from the store (memories, entities,
	// episodes, and any relationships between them). Intended for eval/test
	// isolation — destructive, use with care.
	DeleteAllMemories(ctx context.Context) error
}

// ContradictionHit describes a memory that contradicts a new one being stored.
type ContradictionHit struct {
	CandidateID string
	Reason      string
}

// ContradictionDetector is the interface the store uses to detect contradictions.
// Implemented by capture.MemoryContradictionDetector.
type ContradictionDetector interface {
	FindContradictions(ctx context.Context, content string, embedding []float32) ([]ContradictionHit, error)
}

// SearchFilters allows filtering search results.
type SearchFilters struct {
	Type           *models.MemoryType       `json:"type,omitempty"`
	Scope          *models.MemoryScope      `json:"scope,omitempty"`
	Visibility     *models.MemoryVisibility `json:"visibility,omitempty"`
	Project        *string                  `json:"project,omitempty"`
	Tags           []string                 `json:"tags,omitempty"`
	Source         *string                  `json:"source,omitempty"`
	ConflictStatus *models.ConflictStatus   `json:"conflict_status,omitempty"` // filter by conflict status ("active", "resolved", "")

	// UserID filters results to memories owned by this user. Empty = no filter (returns all).
	UserID string `json:"user_id,omitempty"`

	// IncludeInvalidated includes memories with valid_to set (historical versions).
	// Default: false (only return currently-valid memories).
	IncludeInvalidated bool `json:"include_invalidated,omitempty"`

	// AsOf returns memories valid at a specific point in time.
	// valid_from <= AsOf AND (valid_to IS NULL OR valid_to > AsOf)
	AsOf *time.Time `json:"as_of,omitempty"`
}
