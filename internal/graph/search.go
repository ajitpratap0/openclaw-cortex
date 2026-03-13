package graph

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

const rrfK = 60 // Reciprocal Rank Fusion constant (Cormack et al. 2009)

// SearchFunc is a function that returns a ranked list of fact results.
type SearchFunc func(ctx context.Context, limit int) ([]FactResult, error)

// RRFMerge implements Reciprocal Rank Fusion across multiple ranked lists.
// Each list contains FactResults sorted by their individual method score.
// Returns merged results sorted by combined RRF score, deduped by ID.
func RRFMerge(lists [][]FactResult, limit int) []FactResult {
	type scored struct {
		fact  FactResult
		score float64
	}

	scores := make(map[string]*scored)

	for i := range lists {
		for rank := range lists[i] {
			id := lists[i][rank].ID
			if _, ok := scores[id]; !ok {
				scores[id] = &scored{fact: lists[i][rank]}
			}
			scores[id].score += 1.0 / float64(rank+1+rrfK)
		}
	}

	results := make([]FactResult, 0, len(scores))
	for _, s := range scores {
		s.fact.Score = s.score
		results = append(results, s.fact)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	return results
}

// HybridSearch runs multiple search functions in parallel and merges with RRF.
func HybridSearch(ctx context.Context, fns []SearchFunc, limit int) ([]FactResult, error) {
	var (
		mu    sync.Mutex
		lists [][]FactResult
		errs  []error
		wg    sync.WaitGroup
	)

	for i := range fns {
		wg.Add(1)
		go func(fn SearchFunc) {
			defer wg.Done()
			results, searchErr := fn(ctx, limit*2) // fetch 2x for better RRF merge
			mu.Lock()
			defer mu.Unlock()
			if searchErr != nil {
				errs = append(errs, searchErr)
				return
			}
			lists = append(lists, results)
		}(fns[i])
	}

	wg.Wait()

	// Return results even if some methods failed (graceful degradation)
	if len(lists) == 0 && len(errs) > 0 {
		return nil, fmt.Errorf("hybrid search: all methods failed: %w", errs[0])
	}

	return RRFMerge(lists, limit), nil
}
