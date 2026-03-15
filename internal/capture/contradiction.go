// Package capture provides memory extraction and analysis pipelines.
package capture

import (
	"context"
	"strings"
	"time"

	"github.com/ajitpratap0/openclaw-cortex/internal/embedder"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

// predicateSignals maps exclusive predicates to natural-language phrases that signal them.
// These predicates are mutually exclusive — a given subject can only have one value at a time.
var predicateSignals = map[string][]string{
	"WORKS_AT":    {"works at", "employed at", "employed by", "joined", "started at"},
	"EMPLOYED_BY": {"works at", "employed at", "employed by", "joined", "started at"},
	"HAS_ROLE":    {"role is", "position is", "title is", "works as"},
	"LOCATED_IN":  {"lives in", "located in", "based in", "moved to", "relocated to"},
	"LIVES_IN":    {"lives in", "located in", "based in", "moved to"},
	"BASED_IN":    {"lives in", "located in", "based in", "moved to"},
	"MARRIED_TO":  {"married to", "spouse is", "wife is", "husband is"},
	"REPORTS_TO":  {"reports to", "manager is", "boss is"},
}

// ContradictionCandidate is returned by DetectContradictions.
type ContradictionCandidate struct {
	MemoryID   string
	Confidence float64
	Reason     string
}

// MemoryContradictionDetector detects contradictions in two stages:
//
//	Stage 1: vector search top N candidates with similarity ≥ threshold
//	Stage 2: keyword heuristic — both share an exclusive-predicate signal but
//	         appear to express different values (divergent content)
//
// It implements store.ContradictionDetector.
type MemoryContradictionDetector struct {
	st            store.Store
	threshold     float64
	maxCandidates int
}

// NewContradictionDetector creates a MemoryContradictionDetector.
// threshold is the minimum cosine similarity (default 0.75).
// maxCandidates caps the Stage 1 pool (default 20).
func NewContradictionDetector(st store.Store, threshold float64, maxCandidates int) *MemoryContradictionDetector {
	if threshold <= 0 {
		threshold = 0.75
	}
	if maxCandidates <= 0 {
		maxCandidates = 20
	}
	return &MemoryContradictionDetector{
		st:            st,
		threshold:     threshold,
		maxCandidates: maxCandidates,
	}
}

// FindContradictions implements store.ContradictionDetector.
// Returns memories that contradict newContent. Best-effort: on error returns nil.
func (d *MemoryContradictionDetector) FindContradictions(
	ctx context.Context,
	newContent string,
	newEmbedding []float32,
) ([]store.ContradictionHit, error) {
	// Stage 1: vector search.
	results, err := d.st.Search(ctx, newEmbedding, uint64(d.maxCandidates), nil)
	if err != nil {
		return nil, nil // degrade gracefully
	}

	var candidates []models.SearchResult
	for _, r := range results {
		if r.Score >= d.threshold {
			candidates = append(candidates, r)
		}
	}
	if len(candidates) == 0 {
		return nil, nil
	}

	// Stage 2: exclusive-predicate heuristic.
	newLower := strings.ToLower(newContent)
	newSigs := activeSignals(newLower)
	if len(newSigs) == 0 {
		return nil, nil // no exclusive-predicate signal in new memory
	}

	var hits []store.ContradictionHit
	for _, r := range candidates {
		candLower := strings.ToLower(r.Memory.Content)
		candSigs := activeSignals(candLower)
		for pred := range newSigs {
			if !candSigs[pred] {
				continue
			}
			// Both memories share the same exclusive-predicate signal.
			// If their significant words diverge, the values differ → contradiction.
			if contentDiverges(newLower, candLower) {
				hits = append(hits, store.ContradictionHit{
					CandidateID: r.Memory.ID,
					Reason:      "exclusive predicate '" + pred + "' conflicts with existing memory",
				})
				break
			}
		}
	}
	return hits, nil
}

// activeSignals returns the set of exclusive predicates present in contentLower.
func activeSignals(contentLower string) map[string]bool {
	found := make(map[string]bool)
	for pred, sigs := range predicateSignals {
		for _, sig := range sigs {
			if strings.Contains(contentLower, sig) {
				found[pred] = true
				break
			}
		}
	}
	return found
}

// contentDiverges returns true when fewer than half the significant words of a
// appear in b — indicating they describe different values for the same predicate.
func contentDiverges(a, b string) bool {
	wordsA := significantWords(a)
	if len(wordsA) == 0 {
		return false
	}
	bSet := make(map[string]bool, len(significantWords(b)))
	for _, w := range significantWords(b) {
		bSet[w] = true
	}
	matches := 0
	for _, w := range wordsA {
		if bSet[w] {
			matches++
		}
	}
	return matches < (len(wordsA)+1)/2
}

// significantWords returns lowercase words ≥ 4 chars, excluding common stop words.
func significantWords(s string) []string {
	stop := map[string]bool{
		"that": true, "this": true, "with": true, "from": true,
		"have": true, "been": true, "they": true, "their": true,
	}
	var out []string
	for _, w := range strings.Fields(s) {
		w = strings.Trim(w, ".,;:\"'()")
		if len(w) >= 4 && !stop[w] {
			out = append(out, w)
		}
	}
	return out
}

// DetectContradictions is the simple free-function API.
// Creates a default detector (vector search + keyword heuristic, no LLM) and runs it.
//
//   - ctx: context for cancellation
//   - st: the memory store used for vector search
//   - emb: embedder (unused — embedding is pre-computed)
//   - newMemory: the memory being stored
//   - newEmbedding: the pre-computed embedding for newMemory.Content
func DetectContradictions(
	ctx context.Context,
	st store.Store,
	_ embedder.Embedder,
	newMemory models.Memory,
	newEmbedding []float32,
) ([]ContradictionCandidate, error) {
	detector := NewContradictionDetector(st, 0.75, 20)
	hits, err := detector.FindContradictions(ctx, newMemory.Content, newEmbedding)
	if err != nil {
		return nil, err
	}
	out := make([]ContradictionCandidate, 0, len(hits))
	for _, h := range hits {
		out = append(out, ContradictionCandidate{
			MemoryID:   h.CandidateID,
			Confidence: 0.85,
			Reason:     h.Reason,
		})
	}
	return out, nil
}

// InvalidateContradictions is a convenience helper that calls InvalidateMemory
// for each hit. Best-effort — individual errors are ignored.
func InvalidateContradictions(ctx context.Context, st store.Store, hits []store.ContradictionHit) {
	now := time.Now().UTC()
	for _, h := range hits {
		_ = st.InvalidateMemory(ctx, h.CandidateID, now)
	}
}
