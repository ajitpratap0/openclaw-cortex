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
	// FactEmbedding holds the stored embedding for cosine re-ranking.
	// Only populated when SearchFacts is called with a non-nil query embedding.
	FactEmbedding []float32 `json:"fact_embedding,omitempty"`
}
