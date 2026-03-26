package store

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// DedupResult describes the outcome of a store-time deduplication check.
type DedupResult struct {
	// IsDuplicate is true when a near-identical memory was found and the new
	// content is not richer — the store operation should be skipped.
	IsDuplicate bool

	// IsUpdated is true when the existing memory was updated in place because
	// the new content is longer (and therefore richer) than the existing one.
	IsUpdated bool

	// ExistingID is the ID of the matched duplicate or the memory that was
	// updated. Empty when no duplicate was found.
	ExistingID string
}

// CheckAndHandleDuplicate checks for near-duplicate memories above threshold.
// It encapsulates the three-way store-time dedup logic:
//
//   - No match (similarity < threshold) → returns zero DedupResult; caller
//     should proceed with a normal store.
//   - Match found, newContent is shorter or equal in length → returns
//     DedupResult{IsDuplicate: true, ExistingID: …}; caller should skip.
//   - Match found, newContent is longer → updates the existing memory in place
//     with the richer content (using vec as its new embedding) and returns
//     DedupResult{IsUpdated: true, ExistingID: …}.
//
// threshold is typically cfg.Memory.DedupThreshold (default 0.92).
// The vec parameter must be the embedding of newContent (already computed by
// the caller); it is reused when updating the existing memory to avoid a
// redundant embedding call.
func CheckAndHandleDuplicate(ctx context.Context, st Store, vec []float32, newContent string, threshold float64) (DedupResult, error) {
	dupes, err := st.FindDuplicates(ctx, vec, threshold)
	if err != nil {
		return DedupResult{}, fmt.Errorf("dedup: finding duplicates: %w", err)
	}
	if len(dupes) == 0 {
		return DedupResult{}, nil
	}

	// Sort by descending similarity so dupes[0] is always the closest match.
	sort.Slice(dupes, func(i, j int) bool { return dupes[i].Score > dupes[j].Score })

	best := dupes[0]
	existingID := best.Memory.ID

	// NOTE: "longer in bytes" is a proxy for richness, not a semantic measure.
	// It catches the common case (same fact with more detail appended) but will
	// misfire when the new content is verbose but semantically thinner, or when
	// the existing content is a dense summary. Callers may override via
	// --skip-dedup when the heuristic is unsuitable.
	if len(newContent) <= len(best.Memory.Content) {
		// New content is not richer — skip the store.
		return DedupResult{IsDuplicate: true, ExistingID: existingID}, nil
	}

	// New content is longer — update the existing memory with the richer text.
	updated := best.Memory
	updated.Content = newContent
	updated.UpdatedAt = time.Now().UTC()
	if upsertErr := st.Upsert(ctx, updated, vec); upsertErr != nil {
		return DedupResult{}, fmt.Errorf("dedup: updating existing memory %s: %w", existingID, upsertErr)
	}
	return DedupResult{IsUpdated: true, ExistingID: existingID}, nil
}
