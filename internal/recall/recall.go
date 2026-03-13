package recall

import (
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
)

const (
	// maxBoostMultiplier is the maximum raw boost value — used to normalize to [0,1].
	maxBoostMultiplier = 1.5

	// recencyHalfLifeHours is the exponential decay half-life (7 days) for recency scoring.
	recencyHalfLifeHours = 168.0

	// ln2 is the natural log of 2, used in exponential decay calculations.
	ln2 = 0.693

	// SupersessionPenaltyFactor is the multiplicative penalty for memories that have
	// been superseded by another memory present in the same result set.
	SupersessionPenaltyFactor = 0.3

	// ConflictPenaltyFactor is the multiplicative penalty for memories with an
	// active (unresolved) conflict status.
	ConflictPenaltyFactor = 0.8
)

// Weights controls the relative importance of each ranking factor.
type Weights struct {
	Similarity    float64 `json:"similarity" mapstructure:"similarity"`
	Recency       float64 `json:"recency" mapstructure:"recency"`
	Frequency     float64 `json:"frequency" mapstructure:"frequency"`
	TypeBoost     float64 `json:"type_boost" mapstructure:"type_boost"`
	ScopeBoost    float64 `json:"scope_boost" mapstructure:"scope_boost"`
	Confidence    float64 `json:"confidence" mapstructure:"confidence"`
	Reinforcement float64 `json:"reinforcement" mapstructure:"reinforcement"`
	TagAffinity   float64 `json:"tag_affinity" mapstructure:"tag_affinity"`
}

// DefaultWeights returns sensible default ranking weights.
func DefaultWeights() Weights {
	return Weights{
		Similarity:    0.35,
		Recency:       0.15,
		Frequency:     0.10,
		TypeBoost:     0.10,
		ScopeBoost:    0.08,
		Confidence:    0.10,
		Reinforcement: 0.07,
		TagAffinity:   0.05,
	}
}

// Validate checks that the weights are non-negative and sum to approximately 1.0.
func (w Weights) Validate() error {
	fields := []struct {
		name  string
		value float64
	}{
		{"similarity", w.Similarity},
		{"recency", w.Recency},
		{"frequency", w.Frequency},
		{"type_boost", w.TypeBoost},
		{"scope_boost", w.ScopeBoost},
		{"confidence", w.Confidence},
		{"reinforcement", w.Reinforcement},
		{"tag_affinity", w.TagAffinity},
	}
	for i := range fields {
		if fields[i].value < 0 {
			return fmt.Errorf("recall weight %q must be >= 0, got %f", fields[i].name, fields[i].value)
		}
	}
	sum := w.Similarity + w.Recency + w.Frequency + w.TypeBoost + w.ScopeBoost +
		w.Confidence + w.Reinforcement + w.TagAffinity
	const epsilon = 0.01
	if sum < 1.0-epsilon || sum > 1.0+epsilon {
		return fmt.Errorf("recall weights must sum to 1.0 (±%.2f), got %.4f", epsilon, sum)
	}
	return nil
}

// TypePriority maps memory types to their raw priority multipliers (before normalization).
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
// If the weights are invalid, a warning is logged and defaults are used.
func NewRecaller(weights Weights, logger *slog.Logger) *Recaller {
	if err := weights.Validate(); err != nil {
		logger.Warn("invalid recall weights, using defaults", "error", err)
		weights = DefaultWeights()
	}
	return &Recaller{
		weights: weights,
		logger:  logger,
	}
}

// Rank re-ranks search results using multi-factor scoring.
// All component scores are normalized to [0,1] before weighting.
func (r *Recaller) Rank(results []models.SearchResult, project string, query string) []models.RecallResult {
	now := time.Now().UTC()
	ranked := make([]models.RecallResult, 0, len(results))

	// Build set of superseded IDs by scanning all results.
	supersededIDs := make(map[string]struct{}, len(results))
	for i := range results {
		if results[i].Memory.SupersedesID != "" {
			supersededIDs[results[i].Memory.SupersedesID] = struct{}{}
		}
	}

	for i := range results {
		sr := &results[i]

		confScore := confidenceScore(&sr.Memory)
		reinfScore := reinforcementScore(&sr.Memory)
		tagScore := tagAffinityScore(&sr.Memory, query)

		// Multiplicative penalties
		supersessionPen := 1.0
		if _, superseded := supersededIDs[sr.Memory.ID]; superseded {
			supersessionPen = SupersessionPenaltyFactor
		}

		conflictPen := 1.0
		if sr.Memory.ConflictStatus == models.ConflictStatusActive {
			conflictPen = ConflictPenaltyFactor
		}

		simScore := sr.Score
		recScore := recencyScore(sr.Memory.LastAccessed, now)
		freqScore := frequencyScore(sr.Memory.AccessCount)
		tBoost := typeBoostScore(sr.Memory.Type)
		sBoost := scopeBoostScore(sr.Memory, project)

		weightedSum := r.weights.Similarity*simScore +
			r.weights.Recency*recScore +
			r.weights.Frequency*freqScore +
			r.weights.TypeBoost*tBoost +
			r.weights.ScopeBoost*sBoost +
			r.weights.Confidence*confScore +
			r.weights.Reinforcement*reinfScore +
			r.weights.TagAffinity*tagScore

		finalScore := weightedSum * supersessionPen * conflictPen

		rr := models.RecallResult{
			Memory:              sr.Memory,
			SimilarityScore:     simScore,
			RecencyScore:        recScore,
			FrequencyScore:      freqScore,
			TypeBoost:           tBoost,
			ScopeBoost:          sBoost,
			ConfidenceScore:     confScore,
			ReinforcementScore:  reinfScore,
			TagAffinityScore:    tagScore,
			SupersessionPenalty: supersessionPen,
			ConflictPenalty:     conflictPen,
			FinalScore:          finalScore,
		}

		ranked = append(ranked, rr)
	}

	// Sort by final score descending
	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].FinalScore > ranked[j].FinalScore
	})

	return ranked
}

// ShouldRerank returns true when the top-4 results are close enough in score
// that Claude re-ranking may improve ordering. Returns false when:
//   - threshold is <= 0 (feature disabled)
//   - fewer than 4 results are provided
//   - the spread between results[0] and results[3] exceeds the threshold
func (r *Recaller) ShouldRerank(results []models.RecallResult, threshold float64) bool {
	if threshold <= 0 || len(results) < 4 {
		return false
	}
	spread := results[0].FinalScore - results[3].FinalScore
	return spread <= threshold
}

// recencyScore uses exponential decay. Half-life of 7 days. Returns [0,1].
func recencyScore(lastAccessed time.Time, now time.Time) float64 {
	if lastAccessed.IsZero() {
		return 0.1
	}
	hoursAgo := now.Sub(lastAccessed).Hours()
	if hoursAgo < 0 {
		hoursAgo = 0
	}
	return math.Exp(-ln2 * hoursAgo / recencyHalfLifeHours)
}

// frequencyScore uses log scale on access count. Returns [0,1].
func frequencyScore(accessCount int64) float64 {
	if accessCount <= 0 {
		return 0.0
	}
	// log2(count+1) normalized to [0,1] assuming max ~1000 accesses
	return math.Min(1.0, math.Log2(float64(accessCount)+1)/10.0)
}

// typeBoostScore returns the normalized priority for the memory type. Returns [0,1].
func typeBoostScore(mt models.MemoryType) float64 {
	raw, ok := TypePriority[mt]
	if !ok {
		raw = 1.0
	}
	return raw / maxBoostMultiplier
}

// scopeBoostScore boosts project-scoped memories when a project context is provided.
// Raw values ≤ maxBoostMultiplier; normalized to [0,1].
func scopeBoostScore(mem models.Memory, project string) float64 {
	var raw float64
	switch {
	case project == "":
		raw = 1.0
	case mem.Scope == models.ScopeProject && mem.Project == project:
		raw = 1.5
	case mem.Scope == models.ScopePermanent:
		raw = 1.0
	default:
		raw = 0.8
	}
	return raw / maxBoostMultiplier
}

// confidenceScore returns the memory's confidence as a score.
// Treats Confidence == 0 as "unknown" (legacy memories) and substitutes 0.7.
func confidenceScore(mem *models.Memory) float64 {
	if mem.Confidence < 0.01 {
		return 0.7
	}
	return mem.Confidence
}

// reinforcementScore computes a log-scaled score from reinforcement count.
// Saturates at ~32 reinforcements.
func reinforcementScore(mem *models.Memory) float64 {
	if mem.ReinforcedCount <= 0 {
		return 0.0
	}
	score := math.Log2(float64(mem.ReinforcedCount)+1) / 5.0
	if score > 1.0 {
		return 1.0
	}
	return score
}

// tagAffinityScore computes the fraction of the memory's tags that match query words.
// Returns 0 if the memory has no tags.
func tagAffinityScore(mem *models.Memory, query string) float64 {
	if len(mem.Tags) == 0 {
		return 0.0
	}

	queryWords := make(map[string]struct{})
	for _, w := range strings.Fields(strings.ToLower(query)) {
		queryWords[w] = struct{}{}
	}

	matched := 0
	for _, tag := range mem.Tags {
		tagLower := strings.ToLower(tag)
		tagWords := strings.Fields(tagLower)
		if len(tagWords) <= 1 {
			// Single-word tag: exact match
			if _, ok := queryWords[tagLower]; ok {
				matched++
			}
		} else {
			// Multi-word tag: all words must appear in query
			allFound := true
			for _, tw := range tagWords {
				if _, ok := queryWords[tw]; !ok {
					allFound = false
					break
				}
			}
			if allFound {
				matched++
			}
		}
	}

	return float64(matched) / float64(len(mem.Tags))
}

// FormatWithConflictAnnotations formats recall results with inline conflict annotations.
// Memories in an active conflict group are annotated with the short IDs of conflicting peers.
func FormatWithConflictAnnotations(results []models.RecallResult, budget int) string {
	groupMembers := make(map[string][]string)
	for i := range results {
		g := results[i].Memory.ConflictGroupID
		if g != "" && results[i].Memory.ConflictStatus == models.ConflictStatusActive {
			groupMembers[g] = append(groupMembers[g], results[i].Memory.ID)
		}
	}
	var sb strings.Builder
	used := 0
	for i := range results {
		mem := results[i].Memory
		line := mem.Content
		if mem.ConflictGroupID != "" && mem.ConflictStatus == models.ConflictStatusActive {
			peers := groupMembers[mem.ConflictGroupID]
			var others []string
			for _, id := range peers {
				if id != mem.ID && len(id) >= 8 {
					others = append(others, id[:8])
				}
			}
			if len(others) > 0 {
				line += fmt.Sprintf(" [conflicts with: %s]", strings.Join(others, ", "))
			}
		}
		if used+len(line)/4 > budget {
			break
		}
		fmt.Fprintf(&sb, "- %s\n", line)
		used += len(line) / 4
	}
	return sb.String()
}
