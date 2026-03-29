package graph

import (
	"context"
	"time"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
)

// Client defines the interface for graph storage operations.
// Resolution logic lives in EntityResolver and FactResolver (separate types),
// matching the pattern where ConflictDetector is separate from Store.
type Client interface {
	// EnsureSchema creates indexes and constraints if they don't exist.
	// vectorDim is injected into the vector index DDL (e.g. 768 for nomic-embed-text).
	EnsureSchema(ctx context.Context, vectorDim int) error

	// UpsertEntity creates or updates an entity node.
	UpsertEntity(ctx context.Context, entity models.Entity) error

	// SearchEntities finds entities by fulltext + embedding similarity.
	SearchEntities(ctx context.Context, query string, embedding []float32, project string, limit int) ([]EntityResult, error)

	// GetEntity retrieves a single entity by ID.
	GetEntity(ctx context.Context, id string) (*models.Entity, error)

	// UpsertFact creates a RELATES_TO edge between two entities.
	UpsertFact(ctx context.Context, fact models.Fact) error

	// SearchFacts finds facts by hybrid search (BM25 + cosine + BFS).
	SearchFacts(ctx context.Context, query string, embedding []float32, limit int) ([]FactResult, error)

	// InvalidateFact sets ExpiredAt and InvalidAt on a fact (never deletes).
	InvalidateFact(ctx context.Context, id string, expiredAt, invalidAt time.Time) error

	// GetFactsBetween returns all active facts between two entities.
	GetFactsBetween(ctx context.Context, sourceID, targetID string) ([]models.Fact, error)

	// GetFactsForEntity returns all active facts involving an entity.
	GetFactsForEntity(ctx context.Context, entityID string) ([]models.Fact, error)

	// AppendEpisode adds an episode/session ID to a fact's episodes list.
	AppendEpisode(ctx context.Context, factID, episodeID string) error

	// AppendMemoryToFact adds a memory ID to a fact's source_memory_ids.
	AppendMemoryToFact(ctx context.Context, factID, memoryID string) error

	// GetMemoryFacts returns all facts derived from a given memory.
	GetMemoryFacts(ctx context.Context, memoryID string) ([]models.Fact, error)

	// RecallByGraph returns memory IDs relevant to a query via graph traversal.
	RecallByGraph(ctx context.Context, query string, embedding []float32, limit int) ([]string, error)

	// GetSubgraph returns the neighborhood of nodes and edges reachable from
	// entityID within depth hops.
	GetSubgraph(ctx context.Context, entityID string, depth int) (SubgraphResult, error)

	// GetCommunitiesForEntity returns the community IDs (int64) that the given entity
	// belongs to (populated by a MAGE community-detection algorithm).
	GetCommunitiesForEntity(ctx context.Context, entityID string) ([]int64, error)

	// GetMemoriesForCommunity returns the memory IDs associated with all entities
	// in the given community (identified by its int64 community_id).
	GetMemoriesForCommunity(ctx context.Context, communityID int64) ([]string, error)

	// CreateEpisode stores an episode node in the graph.
	CreateEpisode(ctx context.Context, episode models.Episode) error

	// GetEpisodesForMemory returns all episodes linked to a given memory ID.
	GetEpisodesForMemory(ctx context.Context, memoryID string) ([]models.Episode, error)

	// Healthy returns true if the graph database is reachable.
	Healthy(ctx context.Context) bool

	// Close releases resources.
	Close() error
}
