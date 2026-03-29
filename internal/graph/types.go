package graph

// FactAction represents the resolution outcome for a new fact.
type FactAction string

const (
	FactActionInsert     FactAction = "insert"     // new fact, no duplicates
	FactActionSkip       FactAction = "skip"       // exact duplicate, append episode
	FactActionInvalidate FactAction = "invalidate" // contradicts existing, invalidate old
)

// EntityResult is a search result from the graph entity index.
type EntityResult struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Type  string  `json:"type"`
	Score float64 `json:"score"`
}

// FactResult is a search result from the graph fact index.
type FactResult struct {
	ID              string   `json:"id"`
	Fact            string   `json:"fact"`
	SourceEntityID  string   `json:"source_entity_id"`
	TargetEntityID  string   `json:"target_entity_id"`
	SourceMemoryIDs []string `json:"source_memory_ids"`
	Score           float64  `json:"score"`
}

// SubgraphNode represents a single entity node returned by GetSubgraph.
type SubgraphNode struct {
	EntityID   string `json:"entity_id"`
	EntityName string `json:"entity_name"`
	EntityType string `json:"entity_type"`
	Distance   int    `json:"distance"` // hops from seed entity
}

// SubgraphEdge represents a directed relationship edge returned by GetSubgraph.
type SubgraphEdge struct {
	FactID         string `json:"fact_id"`
	SourceEntityID string `json:"source_entity_id"`
	TargetEntityID string `json:"target_entity_id"`
	RelationType   string `json:"relation_type"`
	Fact           string `json:"fact"`
}

// SubgraphResult is the complete neighborhood returned by GetSubgraph for a
// single seed entity.
type SubgraphResult struct {
	SeedEntityID string         `json:"seed_entity_id"`
	Nodes        []SubgraphNode `json:"nodes"`
	Edges        []SubgraphEdge `json:"edges"`
}
