package recall

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/ajitpratap0/openclaw-cortex/internal/graph"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/sentry"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
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
	Similarity      float64 `json:"similarity" mapstructure:"similarity"`
	Recency         float64 `json:"recency" mapstructure:"recency"`
	Frequency       float64 `json:"frequency" mapstructure:"frequency"`
	TypeBoost       float64 `json:"type_boost" mapstructure:"type_boost"`
	ScopeBoost      float64 `json:"scope_boost" mapstructure:"scope_boost"`
	Confidence      float64 `json:"confidence" mapstructure:"confidence"`
	Reinforcement   float64 `json:"reinforcement" mapstructure:"reinforcement"`
	TagAffinity     float64 `json:"tag_affinity" mapstructure:"tag_affinity"`
	GraphProximity  float64 `json:"graph_proximity" mapstructure:"graph_proximity"`
}

// DefaultWeights returns sensible default ranking weights.
// Similarity is weighted heavily so that semantic relevance dominates;
// recency, frequency, and confidence are reduced to prevent access-pattern
// inflation from drowning out genuinely relevant but less-accessed memories.
func DefaultWeights() Weights {
	return Weights{
		Similarity:     0.45,
		Recency:        0.08,
		Frequency:      0.05,
		TypeBoost:      0.10,
		ScopeBoost:     0.08,
		Confidence:     0.07,
		Reinforcement:  0.07,
		TagAffinity:    0.05,
		GraphProximity: 0.05,
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
		{"graph_proximity", w.GraphProximity},
	}
	for i := range fields {
		if fields[i].value < 0 {
			return fmt.Errorf("recall weight %q must be >= 0, got %f", fields[i].name, fields[i].value)
		}
	}
	sum := w.Similarity + w.Recency + w.Frequency + w.TypeBoost + w.ScopeBoost +
		w.Confidence + w.Reinforcement + w.TagAffinity + w.GraphProximity
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

// defaultGraphDepth is the default traversal depth for graph-aware recall.
const defaultGraphDepth = 2

// defaultVectorWeight and defaultGraphWeight control the RRF blend when merging
// vector search and graph traversal results.
// vector_score * defaultVectorWeight + graph_score * defaultGraphWeight
const (
	defaultVectorWeight = 0.6
	defaultGraphWeight  = 0.4
)

// rrfK is the constant for Reciprocal Rank Fusion (Cormack et al. 2009).
const rrfK = 60

// Recaller performs multi-factor ranking of search results.
type Recaller struct {
	weights       Weights
	logger        *slog.Logger
	graphClient   graph.Client
	store         store.Store
	graphBudgetMs int
	graphDepth    int
	vectorWeight  float64
	graphWeight   float64
}

// SetGraphClient attaches an optional graph client and backing store to the
// recaller. budgetMs is the maximum time in milliseconds allowed for the graph
// recall call; if the call exceeds the budget it is canceled and only vector
// results are used.
func (r *Recaller) SetGraphClient(gc graph.Client, st store.Store, budgetMs int) {
	r.graphClient = gc
	r.store = st
	r.graphBudgetMs = budgetMs
}

// SetGraphDepth configures the traversal depth for RecallByGraph (default: 2).
func (r *Recaller) SetGraphDepth(depth int) {
	if depth >= 1 {
		r.graphDepth = depth
	}
}

// SetGraphWeights overrides the default vector/graph blend weights.
// Both values must be non-negative; they are normalised internally.
func (r *Recaller) SetGraphWeights(vectorWeight, graphWeight float64) {
	if vectorWeight >= 0 && graphWeight >= 0 {
		r.vectorWeight = vectorWeight
		r.graphWeight = graphWeight
	}
}

// graphDepthOrDefault returns graphDepth if set, otherwise the package default.
func (r *Recaller) graphDepthOrDefault() int {
	if r.graphDepth > 0 {
		return r.graphDepth
	}
	return defaultGraphDepth
}

// graphVectorWeight returns the effective vector weight.
func (r *Recaller) graphVectorWeight() float64 {
	if r.vectorWeight > 0 {
		return r.vectorWeight
	}
	return defaultVectorWeight
}

// graphGraphWeight returns the effective graph weight.
func (r *Recaller) graphGraphWeight() float64 {
	if r.graphWeight > 0 {
		return r.graphWeight
	}
	return defaultGraphWeight
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
// This is a thin wrapper around RankWithGraphProximity with a nil proximity map.
func (r *Recaller) Rank(results []models.SearchResult, project string, query string) []models.RecallResult {
	return r.RankWithGraphProximity(results, project, query, nil)
}

// RankWithGraphProximity ranks memories using all scoring factors including
// a graph proximity bonus for memories whose entities are connected to query entities.
// proximityMap maps memoryID → proximity score (1.0=1-hop, 0.5=2-hop, 0.25=3-hop, 0.0=none).
// Pass nil proximityMap to disable graph proximity scoring (equivalent to Rank).
func (r *Recaller) RankWithGraphProximity(
	results []models.SearchResult,
	project, query string,
	proximityMap map[string]float64,
) []models.RecallResult {
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

		// Graph proximity score: 1.0 for 1-hop, 0.5 for 2-hop, 0.25 for 3-hop, 0.0 for none.
		graphProximityScore := 0.0
		if proximityMap != nil {
			graphProximityScore = proximityMap[sr.Memory.ID]
		}

		// Multiplicative penalties
		supersessionPen := 1.0
		if _, superseded := supersededIDs[sr.Memory.ID]; superseded {
			supersessionPen = SupersessionPenaltyFactor
		}

		conflictPen := 1.0
		if sr.Memory.ConflictStatus == models.ConflictStatusActive {
			conflictPen = ConflictPenaltyFactor
		}

		// Use OriginalSimilarity when available (set by RecallWithGraph to preserve
		// the actual vector similarity before the RRF blend overwrites Score).
		simScore := sr.Score
		if sr.OriginalSimilarity != nil {
			simScore = *sr.OriginalSimilarity
		}
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
			r.weights.TagAffinity*tagScore +
			r.weights.GraphProximity*graphProximityScore

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
			GraphProximityScore: graphProximityScore,
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

// depthRecaller is the optional interface for graph clients that support
// configurable traversal depth.
type depthRecaller interface {
	RecallByGraphWithDepth(ctx context.Context, query string, embedding []float32, limit int, depth int) ([]string, error)
}

// depthRecallerWithHops is the optional interface for graph clients that return
// per-memory hop distances in addition to the sorted ID list. When a client
// implements this interface, RecallWithGraph uses the hop distances to build a
// proximityMap for RankWithGraphProximity.
type depthRecallerWithHops interface {
	RecallByGraphWithHops(ctx context.Context, query string, embedding []float32, limit int, depth int) ([]string, map[string]int, error)
}

// RecallWithGraph merges graph-traversal results with vector search results using
// Reciprocal Rank Fusion (RRF), then applies multi-factor ranking.
//
// Merge formula (per spec §6.2):
//
//	blended_score = vector_rrf * vectorWeight + graph_rrf * graphWeight
//
// Default weights: vector=0.6, graph=0.4 (configurable via SetGraphWeights).
// Default traversal depth: 2 (configurable via SetGraphDepth).
//
// If GraphClient is nil the function is equivalent to calling Rank(searchResults, project, query).
// If the graph call exceeds the latency budget, the function falls back to vector-only results.
func (r *Recaller) RecallWithGraph(
	ctx context.Context,
	query string,
	embedding []float32,
	searchResults []models.SearchResult,
	project string,
) []models.RecallResult {
	finish := sentry.StartSpan(ctx, "recall.with_graph", "Recaller.RecallWithGraph")
	defer finish()
	if r.graphClient == nil {
		return r.Rank(searchResults, project, query)
	}

	// Call graph with a deadline derived from the latency budget.
	budgetMs := r.graphBudgetMs
	if budgetMs <= 0 {
		budgetMs = 200 // fallback default
	}
	gCtx, cancel := context.WithTimeout(ctx, time.Duration(budgetMs)*time.Millisecond)
	defer cancel()

	// Support configurable depth with optional hop-distance tracking.
	// Prefer depthRecallerWithHops (returns hop distances) → depthRecaller → base interface.
	var graphIDs []string
	var graphMemoryHops map[string]int
	var err error
	if dh, ok := r.graphClient.(depthRecallerWithHops); ok {
		graphIDs, graphMemoryHops, err = dh.RecallByGraphWithHops(gCtx, query, embedding, 50, r.graphDepthOrDefault())
	} else if dr, ok := r.graphClient.(depthRecaller); ok {
		graphIDs, err = dr.RecallByGraphWithDepth(gCtx, query, embedding, 50, r.graphDepthOrDefault())
	} else {
		graphIDs, err = r.graphClient.RecallByGraph(gCtx, query, embedding, 50)
	}
	if err != nil {
		r.logger.Warn("graph recall failed, falling back to vector-only results", "error", err)
		return r.Rank(searchResults, project, query)
	}

	// Build a proximityMap from hop distances (when available).
	// 1-hop → 1.0, 2-hop → 0.5, 3+-hop → 0.25.
	var proximityMap map[string]float64
	if graphMemoryHops != nil {
		proximityMap = make(map[string]float64, len(graphMemoryHops))
		for memID, hopDist := range graphMemoryHops {
			switch hopDist {
			case 1:
				proximityMap[memID] = 1.0
			case 2:
				proximityMap[memID] = 0.5
			default:
				proximityMap[memID] = 0.25
			}
		}
	}

	// RRF merge: compute blended scores from vector rank + graph rank.
	// Map: memoryID → blended RRF score.
	blended := r.rrfBlend(searchResults, graphIDs)

	// Build unified search result set with blended scores.
	// Start with vector results (already have memory objects).
	existing := make(map[string]struct{}, len(searchResults))
	merged := make([]models.SearchResult, 0, len(blended))

	for i := range searchResults {
		id := searchResults[i].Memory.ID
		// Preserve the raw vector similarity before the RRF blend overwrites Score.
		origSim := searchResults[i].OriginalSimilarity
		if origSim == nil {
			rawSim := searchResults[i].Score
			origSim = &rawSim
		}
		merged = append(merged, models.SearchResult{
			Memory:             searchResults[i].Memory,
			Score:              blended[id],
			OriginalSimilarity: origSim,
		})
		existing[id] = struct{}{}
	}

	// Add graph-only memories (not in vector results).
	for _, id := range graphIDs {
		if _, ok := existing[id]; ok {
			continue
		}
		mem, fetchErr := r.store.Get(ctx, id)
		if fetchErr != nil {
			r.logger.Warn("failed to fetch graph memory", "id", id, "error", fetchErr)
			continue
		}
		merged = append(merged, models.SearchResult{
			Memory: *mem,
			Score:  blended[id],
		})
		existing[id] = struct{}{}
	}

	// Community sweep: when the query is short and targets exactly one known entity,
	// pull in memories from that entity's community (MAGE community detection).
	// Gated by mageAvailableChecker interface to avoid errors when MAGE is absent.
	type mageAvailableChecker interface {
		IsMageAvailable() bool
	}
	if mac, ok := r.graphClient.(mageAvailableChecker); ok && mac.IsMageAvailable() {
		merged = r.communitySweep(ctx, query, merged, existing, blended)
	}

	return r.RankWithGraphProximity(merged, project, query, proximityMap)
}

// communitySweep applies a broad entity query heuristic: when the query is short
// (< 10 words) and exactly one known entity name is found in the query, it fetches
// memories from that entity's community and merges them into the result set.
//
// Returns the (potentially extended) merged slice. existing and blended are
// mutated in-place; the returned slice may be a new backing array if it grew.
func (r *Recaller) communitySweep(
	ctx context.Context,
	query string,
	merged []models.SearchResult,
	existing map[string]struct{},
	blended map[string]float64,
) []models.SearchResult {
	words := strings.Fields(query)
	if len(words) >= 10 {
		return merged
	}

	// Find entity candidates matching the query.
	entityResults, err := r.graphClient.SearchEntities(ctx, query, nil, "", 5)
	if err != nil || len(entityResults) != 1 {
		// Only proceed when exactly one entity matches to avoid over-broad sweeps.
		return merged
	}

	entityID := entityResults[0].ID
	communities, err := r.graphClient.GetCommunitiesForEntity(ctx, entityID)
	if err != nil || len(communities) == 0 {
		return merged
	}

	// Use the first community ID for the sweep.
	communityID := communities[0]
	communityMemIDs, err := r.graphClient.GetMemoriesForCommunity(ctx, communityID)
	if err != nil {
		r.logger.Warn("community sweep: failed to get memories for community", "community_id", communityID, "error", err)
		return merged
	}

	for _, memID := range communityMemIDs {
		if _, ok := existing[memID]; ok {
			continue
		}
		mem, fetchErr := r.store.Get(ctx, memID)
		if fetchErr != nil {
			r.logger.Warn("community sweep: failed to fetch memory", "id", memID, "error", fetchErr)
			continue
		}
		// Assign a modest blended score (lower than graph-traversal results).
		blended[memID] = 1.0 / float64(1+rrfK)
		merged = append(merged, models.SearchResult{
			Memory: *mem,
			Score:  blended[memID],
		})
		existing[memID] = struct{}{}
	}
	return merged
}

// rrfBlend computes a blended Reciprocal Rank Fusion score for each memory ID
// from the vector result list and the graph traversal ID list.
//
// Formula: blended = (1/(k+vectorRank)) * vectorWeight + (1/(k+graphRank)) * graphWeight
//
// IDs that appear only in one list receive a score of 0 from the absent list.
func (r *Recaller) rrfBlend(vectorResults []models.SearchResult, graphIDs []string) map[string]float64 {
	scores := make(map[string]float64, len(vectorResults)+len(graphIDs))

	vw := r.graphVectorWeight()
	gw := r.graphGraphWeight()

	// Vector list contribution.
	for rank := range vectorResults {
		rrfScore := 1.0 / float64(rank+1+rrfK)
		scores[vectorResults[rank].Memory.ID] += rrfScore * vw
	}

	// Graph list contribution.
	for rank, id := range graphIDs {
		rrfScore := 1.0 / float64(rank+1+rrfK)
		scores[id] += rrfScore * gw
	}

	return scores
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
