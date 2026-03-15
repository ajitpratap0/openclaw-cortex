// Package capture provides memory extraction and analysis pipelines.
package capture

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ajitpratap0/openclaw-cortex/internal/embedder"
	"github.com/ajitpratap0/openclaw-cortex/internal/graph"
	"github.com/ajitpratap0/openclaw-cortex/internal/llm"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
	"github.com/ajitpratap0/openclaw-cortex/pkg/xmlutil"
)

// exclusivePairs maps relation types that are mutually exclusive — a given subject
// entity can only have one value for these predicates at any point in time.
// When a new memory asserts one of these and a candidate has the same predicate
// for the same entity but a different object, it is a contradiction.
var exclusivePairs = map[string]bool{
	"WORKS_AT":    true,
	"HAS_ROLE":    true,
	"LOCATED_IN":  true,
	"MARRIED_TO":  true,
	"REPORTS_TO":  true,
	"EMPLOYED_BY": true, // alias that normalises to WORKS_AT
	"LIVES_IN":    true,
	"BASED_IN":    true,
	"CEO_OF":      true,
	"LEADS":       true,
}

// ContradictionConfig controls the contradiction detection pipeline.
type ContradictionConfig struct {
	// Enabled gates the entire pipeline. Default: true.
	Enabled bool `mapstructure:"enabled"`

	// SimilarityThreshold is the minimum cosine similarity for a candidate memory
	// to be considered in Stage 1. Default: 0.75.
	SimilarityThreshold float64 `mapstructure:"similarity_threshold"`

	// MaxCandidates caps the Stage 1 candidate pool. Default: 20.
	MaxCandidates int `mapstructure:"max_candidates"`

	// LLMConfirmThreshold is the cosine similarity above which Stage 3 LLM
	// confirmation is skipped and the contradiction is auto-confirmed.
	// Default: 0.82.
	LLMConfirmThreshold float64 `mapstructure:"llm_confirm_threshold"`

	// LLMTimeoutMs is the maximum milliseconds to wait for the Stage 3 LLM call.
	// If exceeded the candidate is skipped (safe default). Default: 150.
	LLMTimeoutMs int `mapstructure:"llm_timeout_ms"`
}

// DefaultContradictionConfig returns sensible production defaults.
func DefaultContradictionConfig() ContradictionConfig {
	return ContradictionConfig{
		Enabled:             true,
		SimilarityThreshold: 0.75,
		MaxCandidates:       20,
		LLMConfirmThreshold: 0.82,
		LLMTimeoutMs:        150,
	}
}

// contradictionLLMResponse is the JSON schema the LLM returns for Stage 3.
type contradictionLLMResponse struct {
	Contradicts bool   `json:"contradicts"`
	Reason      string `json:"reason"`
}

// stage3Prompt is the prompt template for Stage 3 LLM confirmation.
const stage3Prompt = `You are a memory contradiction detector for an AI agent.

Does MEMORY B directly contradict MEMORY A?

A contradiction means one memory asserts something that is factually incompatible with the other
(e.g., different employers, different roles, different locations for the same person).
Expansions, clarifications, or updates with additional details are NOT contradictions.

<memory_a>%s</memory_a>
<memory_b>%s</memory_b>

Return ONLY valid JSON: {"contradicts": <bool>, "reason": "<one sentence>"}`

// ContradictionResult holds the detection outcome for a single candidate.
type ContradictionResult struct {
	CandidateID string
	Reason      string
}

// MemoryContradictionDetector implements the 3-stage contradiction pipeline described
// in the design spec (section 4.2, 6.1):
//
//	Stage 1 (~10ms)  — candidate retrieval via vector search + entity-linked facts
//	Stage 2 (~2ms)   — heuristic filter: shared entities + exclusive predicate conflict
//	Stage 3 (~50-150ms, optional) — LLM confirmation for ambiguous cases
//
// All stages degrade gracefully; errors cause candidates to be skipped rather than
// blocking the store operation.
type MemoryContradictionDetector struct {
	store       store.Store
	graphClient graph.Client // may be nil — entity lookup is best-effort
	embedder    embedder.Embedder
	llmClient   llm.LLMClient // may be nil — Stage 3 skipped without one
	model       string
	cfg         ContradictionConfig
	logger      *slog.Logger
}

// NewContradictionDetector creates a MemoryContradictionDetector.
// graphClient and llmClient may be nil; the detector degrades gracefully.
func NewContradictionDetector(
	st store.Store,
	graphClient graph.Client,
	emb embedder.Embedder,
	llmCli llm.LLMClient,
	model string,
	cfg ContradictionConfig,
	logger *slog.Logger,
) *MemoryContradictionDetector {
	if logger == nil {
		logger = slog.Default()
	}
	return &MemoryContradictionDetector{
		store:       st,
		graphClient: graphClient,
		embedder:    emb,
		llmClient:   llmCli,
		model:       model,
		cfg:         cfg,
		logger:      logger,
	}
}

// FindContradictions runs the 3-stage pipeline and returns the IDs of memories
// that the new memory content contradicts. Callers should invalidate those memories.
//
// The function returns (nil, nil) when no contradictions are found, and never
// returns errors for graceful degradation failures (those are logged as warnings).
func (d *MemoryContradictionDetector) FindContradictions(
	ctx context.Context,
	newContent string,
	newEmbedding []float32,
) ([]ContradictionResult, error) {
	if !d.cfg.Enabled {
		return nil, nil
	}

	// ── Stage 1: Candidate Retrieval ─────────────────────────────────────────
	candidates, scores, err := d.retrieveCandidates(ctx, newContent, newEmbedding)
	if err != nil {
		d.logger.Warn("contradiction_detector: stage1 retrieval failed", "error", err)
		return nil, nil // degrade gracefully
	}
	if len(candidates) == 0 {
		return nil, nil
	}

	// ── Stage 2: Heuristic Filter ─────────────────────────────────────────────
	heuristic := d.heuristicFilter(ctx, newContent, candidates)
	if len(heuristic) == 0 {
		return nil, nil
	}

	// ── Stage 3: LLM Confirmation ─────────────────────────────────────────────
	results := d.llmConfirm(ctx, newContent, heuristic, candidates, scores)
	return results, nil
}

// ── Stage 1 ──────────────────────────────────────────────────────────────────

// retrieveCandidates gathers candidate memories via vector search and entity-linked
// fact lookup. Returns a merged, deduplicated slice plus a score map.
func (d *MemoryContradictionDetector) retrieveCandidates(
	ctx context.Context,
	_ string,
	embedding []float32,
) ([]models.Memory, map[string]float64, error) {
	byID := make(map[string]models.Memory)
	scores := make(map[string]float64)

	// 1a. Vector search for similar memories.
	limit := uint64(d.cfg.MaxCandidates)
	results, err := d.store.Search(ctx, embedding, limit, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("vector search: %w", err)
	}
	for i := range results {
		if results[i].Score >= d.cfg.SimilarityThreshold {
			byID[results[i].Memory.ID] = results[i].Memory
			scores[results[i].Memory.ID] = results[i].Score
		}
	}

	// 1b. Entity-based lookup (best-effort, requires graph client).
	if d.graphClient != nil {
		d.enrichFromGraph(ctx, embedding, byID, scores)
	}

	if len(byID) == 0 {
		return nil, scores, nil
	}

	out := make([]models.Memory, 0, len(byID))
	for k := range byID {
		out = append(out, byID[k])
	}
	return out, scores, nil
}

// enrichFromGraph adds entity-linked memories to the candidate pool.
// Errors are logged and ignored — this is purely additive.
func (d *MemoryContradictionDetector) enrichFromGraph(
	ctx context.Context,
	_ []float32,
	byID map[string]models.Memory,
	scores map[string]float64,
) {
	// Retrieve facts linked to the graph for recently accessed memory IDs.
	// We collect entity IDs from existing vector candidates, then expand via graph.
	entityIDs := make(map[string]bool)
	for mid := range byID {
		facts, err := d.graphClient.GetMemoryFacts(ctx, byID[mid].ID)
		if err != nil {
			continue
		}
		for i := range facts {
			entityIDs[facts[i].SourceEntityID] = true
			entityIDs[facts[i].TargetEntityID] = true
		}
	}

	// For each entity, find memory IDs linked via facts.
	for eid := range entityIDs {
		facts, err := d.graphClient.GetFactsForEntity(ctx, eid)
		if err != nil {
			continue
		}
		for fi := range facts {
			for _, mid := range facts[fi].SourceMemoryIDs {
				if _, already := byID[mid]; already {
					continue
				}
				mem, err := d.store.Get(ctx, mid)
				if err != nil || mem == nil {
					continue
				}
				byID[mid] = *mem
				scores[mid] = 0.75 // baseline for entity-linked candidates
			}
		}
	}
}

// ── Stage 2 ──────────────────────────────────────────────────────────────────

// heuristicFilter returns the subset of candidates that pass the entity-overlap
// + exclusive-predicate heuristic. Fast, no LLM calls.
func (d *MemoryContradictionDetector) heuristicFilter(
	ctx context.Context,
	newContent string,
	candidates []models.Memory,
) []models.Memory {
	if d.graphClient == nil {
		// Without graph, fall back to simple keyword overlap heuristic.
		return d.keywordHeuristicFilter(newContent, candidates)
	}

	var heuristic []models.Memory
	for i := range candidates {
		if d.hasExclusivePredicateConflict(ctx, newContent, candidates[i]) {
			heuristic = append(heuristic, candidates[i])
		}
	}
	return heuristic
}

// hasExclusivePredicateConflict checks whether the candidate's fact graph shows
// an exclusive-predicate mismatch with the new memory content.
func (d *MemoryContradictionDetector) hasExclusivePredicateConflict(
	ctx context.Context,
	newContent string,
	cand models.Memory,
) bool {
	facts, err := d.graphClient.GetMemoryFacts(ctx, cand.ID)
	if err != nil || len(facts) == 0 {
		return false
	}

	newLower := strings.ToLower(newContent)

	for fi := range facts {
		normRel := strings.ToUpper(facts[fi].RelationType)
		if !exclusivePairs[normRel] {
			continue
		}
		// Check if the new content mentions the same entity but suggests a
		// different value for this exclusive predicate.
		// Heuristic: if the target entity name appears in the candidate fact
		// and the new content contains the same predicate keyword but NOT the
		// same target value, flag it.
		if d.contentSuggestsConflict(newLower, normRel, facts[fi]) {
			d.logger.Debug("contradiction_detector: heuristic hit",
				"candidate_id", cand.ID,
				"relation", normRel,
				"fact", facts[fi].Fact)
			return true
		}
	}
	return false
}

// contentSuggestsConflict checks if the new content contains signals for the
// same exclusive predicate but a different object than the fact describes.
func (d *MemoryContradictionDetector) contentSuggestsConflict(newLower, relType string, f models.Fact) bool {
	// Map relation types to keyword signals present in natural-language memory content.
	signals := exclusivePredicateSignals(relType)
	hasSignal := false
	for _, sig := range signals {
		if strings.Contains(newLower, sig) {
			hasSignal = true
			break
		}
	}
	if !hasSignal {
		return false
	}

	// If the existing fact's object/description appears in the new content, it's
	// consistent (not a contradiction). If it doesn't, treat as potential conflict.
	factLower := strings.ToLower(f.Fact)
	_ = factLower // used below

	// We consider it a conflict only if:
	// (a) the new content has the signal keyword AND
	// (b) the new content does NOT contain key words from the existing fact
	//     (suggesting a different value).
	factWords := significantWords(f.Fact)
	matchCount := 0
	for _, w := range factWords {
		if strings.Contains(newLower, w) {
			matchCount++
		}
	}
	// If fewer than half the significant words match, the content is likely divergent.
	return len(factWords) > 0 && matchCount < (len(factWords)+1)/2
}

// exclusivePredicateSignals maps a canonical relation type to natural-language
// keywords that typically indicate that predicate in memory content.
func exclusivePredicateSignals(relType string) []string {
	switch relType {
	case "WORKS_AT", "EMPLOYED_BY":
		return []string{"works at", "employed at", "employed by", "joined", "started at", "job at"}
	case "HAS_ROLE", "POSITION_AT", "TITLE_AT":
		return []string{"role is", "position is", "title is", "works as", "his role", "her role", "their role"}
	case "LOCATED_IN", "LIVES_IN", "BASED_IN":
		return []string{"lives in", "located in", "based in", "moved to", "relocated to"}
	case "MARRIED_TO":
		return []string{"married to", "spouse is", "wife is", "husband is", "partner is"}
	case "REPORTS_TO":
		return []string{"reports to", "manager is", "boss is"}
	case "CEO_OF", "LEADS":
		return []string{"ceo of", "leads ", "leading ", "head of"}
	default:
		return nil
	}
}

// significantWords returns lowercase words from s that are at least 4 chars
// and not stop words — used for rough content overlap detection.
func significantWords(s string) []string {
	stop := map[string]bool{
		"the": true, "and": true, "for": true, "that": true, "this": true,
		"with": true, "from": true, "have": true, "been": true, "they": true,
	}
	var out []string
	for _, w := range strings.Fields(strings.ToLower(s)) {
		w = strings.Trim(w, ".,;:\"'()")
		if len(w) >= 4 && !stop[w] {
			out = append(out, w)
		}
	}
	return out
}

// keywordHeuristicFilter is the fallback when no graph client is available.
// It uses simple content overlap: candidates with high similarity but divergent
// key-phrase signals are flagged.
func (d *MemoryContradictionDetector) keywordHeuristicFilter(newContent string, candidates []models.Memory) []models.Memory {
	newLower := strings.ToLower(newContent)
	var out []models.Memory
	for ci := range candidates {
		candLower := strings.ToLower(candidates[ci].Content)
		// Check for any exclusive-predicate signal in both contents.
		for rel := range exclusivePairs {
			sigs := exclusivePredicateSignals(rel)
			newHasSig, candHasSig := false, false
			for _, sig := range sigs {
				if strings.Contains(newLower, sig) {
					newHasSig = true
				}
				if strings.Contains(candLower, sig) {
					candHasSig = true
				}
			}
			if newHasSig && candHasSig {
				// Both mention the same predicate — potential conflict; send to Stage 3.
				out = append(out, candidates[ci])
				break
			}
		}
	}
	return out
}

// ── Stage 3 ──────────────────────────────────────────────────────────────────

// llmConfirm runs LLM confirmation on Stage 2 candidates. For candidates with
// similarity >= LLMConfirmThreshold, the contradiction is auto-confirmed without
// an LLM call. For the rest, a context-limited LLM call is made.
func (d *MemoryContradictionDetector) llmConfirm(
	ctx context.Context,
	newContent string,
	heuristic []models.Memory,
	_ []models.Memory,
	scores map[string]float64,
) []ContradictionResult {
	var confirmed []ContradictionResult

	for hi := range heuristic {
		sim := scores[heuristic[hi].ID]

		// Auto-confirm if above the high-similarity threshold — skip LLM.
		if sim >= d.cfg.LLMConfirmThreshold {
			d.logger.Info("contradiction_detector: auto-confirmed (high similarity)",
				"candidate_id", heuristic[hi].ID, "similarity", sim)
			confirmed = append(confirmed, ContradictionResult{
				CandidateID: heuristic[hi].ID,
				Reason:      fmt.Sprintf("auto-confirmed: cosine similarity %.3f >= threshold %.3f", sim, d.cfg.LLMConfirmThreshold),
			})
			continue
		}

		// If no LLM client, skip confirmation but still report based on heuristic alone.
		if d.llmClient == nil {
			d.logger.Debug("contradiction_detector: no LLM client, using heuristic result",
				"candidate_id", heuristic[hi].ID)
			confirmed = append(confirmed, ContradictionResult{
				CandidateID: heuristic[hi].ID,
				Reason:      "heuristic: exclusive-predicate signal detected (no LLM confirmation)",
			})
			continue
		}

		// Stage 3 LLM call with timeout budget.
		timeout := time.Duration(d.cfg.LLMTimeoutMs) * time.Millisecond
		llmCtx, cancel := context.WithTimeout(ctx, timeout)
		result := d.callLLM(llmCtx, newContent, heuristic[hi].Content)
		cancel()

		if result == nil {
			d.logger.Debug("contradiction_detector: LLM call skipped or failed",
				"candidate_id", heuristic[hi].ID)
			continue
		}
		if result.Contradicts {
			d.logger.Info("contradiction_detector: LLM confirmed contradiction",
				"candidate_id", heuristic[hi].ID, "reason", result.Reason)
			confirmed = append(confirmed, ContradictionResult{
				CandidateID: heuristic[hi].ID,
				Reason:      result.Reason,
			})
		}
	}

	return confirmed
}

// callLLM invokes the LLM for Stage 3 confirmation. Returns nil on any error or
// timeout so the caller can degrade gracefully.
func (d *MemoryContradictionDetector) callLLM(ctx context.Context, newContent, candidateContent string) *contradictionLLMResponse {
	prompt := fmt.Sprintf(stage3Prompt,
		xmlutil.Escape(candidateContent),
		xmlutil.Escape(newContent),
	)

	raw, err := d.llmClient.Complete(ctx, d.model,
		"You are a precise contradiction detection system. Output only valid JSON.",
		prompt,
		256,
	)
	if err != nil {
		d.logger.Debug("contradiction_detector: LLM error", "error", err)
		return nil
	}

	raw = strings.TrimSpace(llm.StripCodeFences(raw))
	if raw == "" {
		return nil
	}

	var resp contradictionLLMResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		d.logger.Debug("contradiction_detector: JSON parse error", "raw", raw, "error", err)
		return nil
	}
	return &resp
}

// InvalidateContradictions invalidates each contradicted memory by setting
// valid_to = now(). This is a convenience helper for callers that have a store
// but don't want to iterate themselves.
func InvalidateContradictions(ctx context.Context, st store.Store, results []ContradictionResult, logger *slog.Logger) {
	now := time.Now().UTC()
	for _, r := range results {
		if err := st.InvalidateMemory(ctx, r.CandidateID, now); err != nil {
			if logger != nil {
				logger.Warn("contradiction_detector: failed to invalidate contradicted memory",
					"id", r.CandidateID, "error", err)
			}
		} else {
			if logger != nil {
				logger.Info("contradiction_detector: invalidated contradicted memory",
					"id", r.CandidateID, "reason", r.Reason)
			}
		}
	}
}
