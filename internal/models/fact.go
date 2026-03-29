package models

import (
	"strings"
	"time"
)

// RelationshipType is a canonical Cypher relationship label used between entity nodes.
// Using typed constants prevents arbitrary strings from being written into the graph
// and keeps the schema stable across extractions.
type RelationshipType string

const (
	RelTypeWorksAt          RelationshipType = "WORKS_AT"
	RelTypeHasRole          RelationshipType = "HAS_ROLE"
	RelTypeLocatedIn        RelationshipType = "LOCATED_IN"
	RelTypeMarriedTo        RelationshipType = "MARRIED_TO"
	RelTypeReportsTo        RelationshipType = "REPORTS_TO"
	RelTypeEmployedBy       RelationshipType = "EMPLOYED_BY"
	RelTypeLivesIn          RelationshipType = "LIVES_IN"
	RelTypeBasedIn          RelationshipType = "BASED_IN"
	RelTypeCEOOf            RelationshipType = "CEO_OF"
	RelTypeLeads            RelationshipType = "LEADS"
	RelTypeUses             RelationshipType = "USES"
	RelTypeDependsOn        RelationshipType = "DEPENDS_ON"
	RelTypeDecidedTo        RelationshipType = "DECIDED_TO"
	RelTypeKnows            RelationshipType = "KNOWS"
	RelTypeHasSkill         RelationshipType = "HAS_SKILL"
	RelTypePartOf           RelationshipType = "PART_OF"
	RelTypeCollaboratesWith RelationshipType = "COLLABORATES_WITH"
	RelTypeImplements       RelationshipType = "IMPLEMENTS"
	RelTypeManages          RelationshipType = "MANAGES"
	RelTypeRelatesTo        RelationshipType = "RELATES_TO" // fallback
)

// ValidRelationshipTypes is a set of all canonical relationship type strings for O(1) validation.
var ValidRelationshipTypes = map[string]bool{
	string(RelTypeWorksAt):          true,
	string(RelTypeHasRole):          true,
	string(RelTypeLocatedIn):        true,
	string(RelTypeMarriedTo):        true,
	string(RelTypeReportsTo):        true,
	string(RelTypeEmployedBy):       true,
	string(RelTypeLivesIn):          true,
	string(RelTypeBasedIn):          true,
	string(RelTypeCEOOf):            true,
	string(RelTypeLeads):            true,
	string(RelTypeUses):             true,
	string(RelTypeDependsOn):        true,
	string(RelTypeDecidedTo):        true,
	string(RelTypeKnows):            true,
	string(RelTypeHasSkill):         true,
	string(RelTypePartOf):           true,
	string(RelTypeCollaboratesWith): true,
	string(RelTypeImplements):       true,
	string(RelTypeManages):          true,
	string(RelTypeRelatesTo):        true,
}

// NormalizeRelType returns s if it is a known canonical relationship type (case-insensitive
// match after upper-casing), otherwise it returns "RELATES_TO" as a safe fallback.
func NormalizeRelType(s string) string {
	upper := strings.ToUpper(strings.TrimSpace(s))
	if ValidRelationshipTypes[upper] {
		return upper
	}
	return string(RelTypeRelatesTo)
}

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
