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

// ConflictStatus represents the conflict resolution state of a memory.
type ConflictStatus string

const (
	ConflictStatusNone     ConflictStatus = ""
	ConflictStatusActive   ConflictStatus = "active"
	ConflictStatusResolved ConflictStatus = "resolved"
)

// ValidConflictStatuses lists all valid non-empty conflict statuses.
var ValidConflictStatuses = []ConflictStatus{
	ConflictStatusActive,
	ConflictStatusResolved,
}

// IsValid returns true if cs is a recognized conflict status (including empty).
func (cs ConflictStatus) IsValid() bool {
	if cs == ConflictStatusNone {
		return true
	}
	for i := range ValidConflictStatuses {
		if ValidConflictStatuses[i] == cs {
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

	// ReinforcedAt is the last time this memory's confidence was boosted
	// because a similar memory was captured again.
	ReinforcedAt time.Time `json:"reinforced_at,omitempty"`

	// ReinforcedCount is how many times this memory has been reinforced.
	ReinforcedCount int `json:"reinforced_count,omitempty"`

	Metadata     map[string]any `json:"metadata,omitempty"`
	SupersedesID string         `json:"supersedes_id,omitempty"` // ID of memory this replaces
	ValidUntil   time.Time      `json:"valid_until,omitempty"`   // zero = never expires

	// ValidFrom is when this memory version became the current truth.
	// Set to CreatedAt on first write. Immutable after initial store.
	ValidFrom time.Time `json:"valid_from,omitempty"`

	// ValidTo is set when this memory is superseded by a newer version or contradicted.
	// Zero/nil = currently valid.
	ValidTo *time.Time `json:"valid_to,omitempty"`

	// IsCurrentVersion is derived (not stored): ValidTo == nil.
	// Used for display only.
	IsCurrentVersion bool `json:"is_current_version,omitempty"`

	// ConflictGroupID links memories that contradict each other.
	// All memories in a conflict group share the same non-empty UUID.
	ConflictGroupID string `json:"conflict_group_id,omitempty"`

	// ConflictStatus tracks resolution: "" (no conflict), "active" (unresolved), "resolved".
	ConflictStatus ConflictStatus `json:"conflict_status,omitempty"`
}

// SearchResult wraps a Memory with its similarity score.
type SearchResult struct {
	Memory Memory  `json:"memory"`
	Score  float64 `json:"score"`
}

// RecallResult wraps a Memory with multi-factor ranking details.
type RecallResult struct {
	Memory              Memory  `json:"memory"`
	SimilarityScore     float64 `json:"similarity_score"`
	RecencyScore        float64 `json:"recency_score"`
	FrequencyScore      float64 `json:"frequency_score"`
	TypeBoost           float64 `json:"type_boost"`
	ScopeBoost          float64 `json:"scope_boost"`
	ConfidenceScore     float64 `json:"confidence_score"`
	ReinforcementScore  float64 `json:"reinforcement_score"`
	TagAffinityScore    float64 `json:"tag_affinity_score"`
	SupersessionPenalty float64 `json:"supersession_penalty"`
	ConflictPenalty     float64 `json:"conflict_penalty"`
	FinalScore          float64 `json:"final_score"`
}

// CapturedMemory is a memory extracted from a conversation by the LLM.
type CapturedMemory struct {
	Content    string     `json:"content"`
	Type       MemoryType `json:"type"`
	Confidence float64    `json:"confidence"`
	Tags       []string   `json:"tags"`
}

// MemoryPreview is a lightweight summary of a memory used in stats output.
type MemoryPreview struct {
	ID          string `json:"id"`
	Content     string `json:"content"`
	AccessCount int64  `json:"access_count"`
}

// CollectionStats holds summary statistics about the memory collection.
type CollectionStats struct {
	TotalMemories int64            `json:"total_memories"`
	ByType        map[string]int64 `json:"by_type"`
	ByScope       map[string]int64 `json:"by_scope"`

	// Health metrics
	OldestMemory       *time.Time       `json:"oldest_memory,omitempty"`
	NewestMemory       *time.Time       `json:"newest_memory,omitempty"`
	TopAccessed        []MemoryPreview  `json:"top_accessed,omitempty"`
	ReinforcementTiers map[string]int64 `json:"reinforcement_tiers,omitempty"`
	ActiveConflicts    int64            `json:"active_conflicts"`
	PendingTTLExpiry   int64            `json:"pending_ttl_expiry"`
	StorageEstimate    int64            `json:"storage_estimate_bytes"`
}
