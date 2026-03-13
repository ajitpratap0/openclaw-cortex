package models

import "time"

// Fact represents a relationship between two entities with bi-temporal validity.
// Inspired by Graphiti's EntityEdge — facts are first-class search units with
// their own embeddings, enabling semantic search over relationships.
type Fact struct {
	ID             string    `json:"id"`
	SourceEntityID string    `json:"source_entity_id"`
	TargetEntityID string    `json:"target_entity_id"`
	RelationType   string    `json:"relation_type"`
	Fact           string    `json:"fact"`
	FactEmbedding  []float32 `json:"fact_embedding,omitempty"`

	// Bi-temporal fields: system time vs world time
	CreatedAt time.Time  `json:"created_at"`
	ExpiredAt *time.Time `json:"expired_at,omitempty"`
	ValidAt   *time.Time `json:"valid_at,omitempty"`
	InvalidAt *time.Time `json:"invalid_at,omitempty"`

	// Provenance
	SourceMemoryIDs []string `json:"source_memory_ids,omitempty"`
	Episodes        []string `json:"episodes,omitempty"`
	Confidence      float64  `json:"confidence"`
}
