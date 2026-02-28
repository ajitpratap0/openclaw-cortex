package models

import "time"

// EntityType classifies the kind of entity.
type EntityType string

const (
	EntityTypePerson   EntityType = "person"
	EntityTypeProject  EntityType = "project"
	EntityTypeSystem   EntityType = "system"
	EntityTypeDecision EntityType = "decision"
	EntityTypeConcept  EntityType = "concept"
)

// ValidEntityTypes is the set of all valid entity types.
var ValidEntityTypes = []EntityType{
	EntityTypePerson,
	EntityTypeProject,
	EntityTypeSystem,
	EntityTypeDecision,
	EntityTypeConcept,
}

// IsValid returns true if the entity type is recognized.
func (et EntityType) IsValid() bool {
	for i := range ValidEntityTypes {
		if et == ValidEntityTypes[i] {
			return true
		}
	}
	return false
}

// Entity represents a named entity that can be linked to memories.
type Entity struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Type      EntityType     `json:"type"`
	Aliases   []string       `json:"aliases,omitempty"`
	MemoryIDs []string       `json:"memory_ids,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}
