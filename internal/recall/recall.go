package recall

import (
	"log/slog"
	"math"
	"sort"
	"time"

	"github.com/ajitpratap0/cortex/internal/models"
)

// Weights controls the relative importance of each ranking factor.
type Weights struct {
	Similarity float64 `json:"similarity" mapstructure:"similarity"`
	Recency    float64 `json:"recency" mapstructure:"recency"`
	Frequency  float64 `json:"frequency" mapstructure:"frequency"`
	TypeBoost  float64 `json:"type_boost" mapstructure:"type_boost"`
	ScopeBoost float64 `json:"scope_boost" mapstructure:"scope_boost"`
}

// DefaultWeights returns sensible default ranking weights.
func DefaultWeights() Weights {
	return Weights{
		Similarity: 0.5,
		Recency:    0.2,
		Frequency:  0.1,
		TypeBoost:  0.1,
		ScopeBoost: 0.1,
	}
}

// TypePriority maps memory types to their priority multipliers.
var TypePriority = map[models.MemoryType]float64{
	models.MemoryTypeRule:       1.5,
	models.MemoryTypeProcedure:  1.3,
	models.MemoryTypeFact:       1.0,
	models.MemoryTypeEpisode:    0.8,
	models.MemoryTypePreference: 0.7,
}

// Recaller performs multi-factor ranking of search results.
type Recaller struct {
	weights Weights
	logger  *slog.Logger
}

// NewRecaller creates a new recaller with the given weights.
func NewRecaller(weights Weights, logger *slog.Logger) *Recaller {
	return &Recaller{
		weights: weights,
		logger:  logger,
	}
}

// Rank re-ranks search results using multi-factor scoring.
func (r *Recaller) Rank(results []models.SearchResult, project string) []models.RecallResult {
	now := time.Now().UTC()
	ranked := make([]models.RecallResult, 0, len(results))

	for _, sr := range results {
		rr := models.RecallResult{
			Memory:          sr.Memory,
			SimilarityScore: sr.Score,
			RecencyScore:    recencyScore(sr.Memory.LastAccessed, now),
			FrequencyScore:  frequencyScore(sr.Memory.AccessCount),
			TypeBoost:       typeBoostScore(sr.Memory.Type),
			ScopeBoost:      scopeBoostScore(sr.Memory, project),
		}

		rr.FinalScore = r.weights.Similarity*rr.SimilarityScore +
			r.weights.Recency*rr.RecencyScore +
			r.weights.Frequency*rr.FrequencyScore +
			r.weights.TypeBoost*rr.TypeBoost +
			r.weights.ScopeBoost*rr.ScopeBoost

		ranked = append(ranked, rr)
	}

	// Sort by final score descending
	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].FinalScore > ranked[j].FinalScore
	})

	return ranked
}

// recencyScore uses exponential decay. Half-life of 7 days.
func recencyScore(lastAccessed time.Time, now time.Time) float64 {
	if lastAccessed.IsZero() {
		return 0.1
	}
	hoursAgo := now.Sub(lastAccessed).Hours()
	if hoursAgo < 0 {
		hoursAgo = 0
	}
	// Half-life of 168 hours (7 days)
	halfLife := 168.0
	return math.Exp(-0.693 * hoursAgo / halfLife)
}

// frequencyScore uses log scale on access count.
func frequencyScore(accessCount int64) float64 {
	if accessCount <= 0 {
		return 0.0
	}
	// log2(count+1) normalized to [0,1] assuming max ~1000 accesses
	return math.Min(1.0, math.Log2(float64(accessCount)+1)/10.0)
}

// typeBoostScore returns the priority multiplier for the memory type.
func typeBoostScore(mt models.MemoryType) float64 {
	if boost, ok := TypePriority[mt]; ok {
		return boost
	}
	return 1.0
}

// scopeBoostScore boosts project-scoped memories when a project context is provided.
func scopeBoostScore(mem models.Memory, project string) float64 {
	if project == "" {
		return 1.0
	}

	if mem.Scope == models.ScopeProject && mem.Project == project {
		return 1.5
	}

	if mem.Scope == models.ScopePermanent {
		return 1.0
	}

	return 0.8
}
