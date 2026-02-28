package models

import (
	"time"
)

// MemoryType classifies the kind of memory.
type MemoryType string

const (
	MemoryTypeRule       MemoryType = "rule"
	MemoryTypeFact       MemoryType = "fact"
	MemoryTypeEpisode    MemoryType = "episode"
	MemoryTypeProcedure  MemoryType = "procedure"
	MemoryTypePreference MemoryType = "preference"
)

// ValidMemoryTypes is the set of all valid memory types.
var ValidMemoryTypes = []MemoryType{
	MemoryTypeRule,
	MemoryTypeFact,
	MemoryTypeEpisode,
	MemoryTypeProcedure,
	MemoryTypePreference,
}

// IsValid returns true if the memory type is recognized.
func (mt MemoryType) IsValid() bool {
	for _, v := range ValidMemoryTypes {
		if mt == v {
			return true
		}
	}
	return false
}

// MemoryScope defines the persistence scope of a memory.
type MemoryScope string

const (
	ScopePermanent MemoryScope = "permanent"
	ScopeProject   MemoryScope = "project"
	ScopeSession   MemoryScope = "session"
	ScopeTTL       MemoryScope = "ttl"
)

// ValidMemoryScopes is the set of all valid memory scopes.
var ValidMemoryScopes = []MemoryScope{
	ScopePermanent,
	ScopeProject,
	ScopeSession,
	ScopeTTL,
}

// IsValid returns true if the memory scope is recognized.
func (ms MemoryScope) IsValid() bool {
	for _, v := range ValidMemoryScopes {
		if ms == v {
			return true
		}
	}
	return false
}

// MemoryVisibility controls access to a memory.
type MemoryVisibility string

const (
	VisibilityPrivate   MemoryVisibility = "private"
	VisibilityShared    MemoryVisibility = "shared"
	VisibilitySensitive MemoryVisibility = "sensitive"
)

// Memory is the core data structure for a stored memory.
type Memory struct {
	ID           string           `json:"id"`
	Type         MemoryType       `json:"type"`
	Scope        MemoryScope      `json:"scope"`
	Visibility   MemoryVisibility `json:"visibility"`
	Content      string           `json:"content"`
	Confidence   float64          `json:"confidence"`
	Source       string           `json:"source"`
	Tags         []string         `json:"tags"`
	Project      string           `json:"project,omitempty"`
	TTLSeconds   int64            `json:"ttl_seconds,omitempty"`
	CreatedAt    time.Time        `json:"created_at"`
	UpdatedAt    time.Time        `json:"updated_at"`
	LastAccessed time.Time        `json:"last_accessed"`
	AccessCount  int64            `json:"access_count"`
	Metadata     map[string]any   `json:"metadata,omitempty"`
	SupersedesID string           `json:"supersedes_id,omitempty"` // ID of memory this replaces
	ValidUntil   time.Time        `json:"valid_until,omitempty"`   // zero = never expires
}

// SearchResult wraps a Memory with its similarity score.
type SearchResult struct {
	Memory Memory  `json:"memory"`
	Score  float64 `json:"score"`
}

// RecallResult wraps a Memory with multi-factor ranking details.
type RecallResult struct {
	Memory          Memory  `json:"memory"`
	SimilarityScore float64 `json:"similarity_score"`
	RecencyScore    float64 `json:"recency_score"`
	FrequencyScore  float64 `json:"frequency_score"`
	TypeBoost       float64 `json:"type_boost"`
	ScopeBoost      float64 `json:"scope_boost"`
	FinalScore      float64 `json:"final_score"`
}

// CapturedMemory is a memory extracted from a conversation by the LLM.
type CapturedMemory struct {
	Content    string     `json:"content"`
	Type       MemoryType `json:"type"`
	Confidence float64    `json:"confidence"`
	Tags       []string   `json:"tags"`
}

// CollectionStats holds summary statistics about the memory collection.
type CollectionStats struct {
	TotalMemories int64            `json:"total_memories"`
	ByType        map[string]int64 `json:"by_type"`
	ByScope       map[string]int64 `json:"by_scope"`
}
