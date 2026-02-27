package store

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
)

// MockStore is an in-memory implementation of Store for testing.
type MockStore struct {
	mu       sync.RWMutex
	memories map[string]*storedMemory
}

type storedMemory struct {
	memory models.Memory
	vector []float32
}

// NewMockStore creates a new mock store.
func NewMockStore() *MockStore {
	return &MockStore{
		memories: make(map[string]*storedMemory),
	}
}

// EnsureCollection is a no-op for the mock store.
func (m *MockStore) EnsureCollection(_ context.Context) error {
	return nil
}

// Upsert inserts or updates a memory in the mock store.
func (m *MockStore) Upsert(_ context.Context, memory models.Memory, vector []float32) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Deep-copy mutable fields to prevent external mutation of stored data.
	if len(memory.Tags) > 0 {
		tags := make([]string, len(memory.Tags))
		copy(tags, memory.Tags)
		memory.Tags = tags
	}
	if len(memory.Metadata) > 0 {
		meta := make(map[string]any, len(memory.Metadata))
		for k, v := range memory.Metadata {
			meta[k] = v
		}
		memory.Metadata = meta
	}
	m.memories[memory.ID] = &storedMemory{memory: memory, vector: vector}
	return nil
}

// Search finds memories by cosine similarity to the query vector.
func (m *MockStore) Search(_ context.Context, vector []float32, limit uint64, filters *SearchFilters) ([]models.SearchResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var results []models.SearchResult
	for _, sm := range m.memories {
		if !matchesFilters(sm.memory, filters) {
			continue
		}
		score := cosineSimilarity(vector, sm.vector)
		results = append(results, models.SearchResult{
			Memory: sm.memory,
			Score:  score,
		})
	}

	// Sort by score descending (simple bubble sort for tests)
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].Score > results[i].Score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	if uint64(len(results)) > limit {
		results = results[:limit]
	}

	return results, nil
}

// Get retrieves a single memory by ID.
func (m *MockStore) Get(_ context.Context, id string) (*models.Memory, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	sm, ok := m.memories[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	mem := sm.memory
	// Deep-copy mutable fields to prevent callers from mutating stored data.
	if len(mem.Tags) > 0 {
		tags := make([]string, len(mem.Tags))
		copy(tags, mem.Tags)
		mem.Tags = tags
	}
	if len(mem.Metadata) > 0 {
		meta := make(map[string]any, len(mem.Metadata))
		for k, v := range mem.Metadata {
			meta[k] = v
		}
		mem.Metadata = meta
	}
	return &mem, nil
}

// Delete removes a memory by ID.
func (m *MockStore) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.memories[id]; !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	delete(m.memories, id)
	return nil
}

// List returns memories matching filters with cursor-based pagination.
func (m *MockStore) List(_ context.Context, filters *SearchFilters, limit uint64, cursor string) ([]models.Memory, string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var all []models.Memory
	for _, sm := range m.memories {
		if !matchesFilters(sm.memory, filters) {
			continue
		}
		all = append(all, sm.memory)
	}

	// Sort by ID for deterministic pagination.
	for i := 0; i < len(all); i++ {
		for j := i + 1; j < len(all); j++ {
			if all[j].ID < all[i].ID {
				all[i], all[j] = all[j], all[i]
			}
		}
	}

	// Skip past the cursor (ID of last item from previous page).
	if cursor != "" {
		found := false
		for i := range all {
			if all[i].ID == cursor {
				all = all[i+1:]
				found = true
				break
			}
		}
		if !found {
			return nil, "", nil
		}
	}

	// Apply limit.
	var nextCursor string
	if limit > 0 && uint64(len(all)) > limit {
		all = all[:limit]
		nextCursor = all[len(all)-1].ID
	}

	return all, nextCursor, nil
}

// FindDuplicates returns memories with cosine similarity above the threshold.
func (m *MockStore) FindDuplicates(_ context.Context, vector []float32, threshold float64) ([]models.SearchResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var results []models.SearchResult
	for _, sm := range m.memories {
		score := cosineSimilarity(vector, sm.vector)
		if score >= threshold {
			results = append(results, models.SearchResult{
				Memory: sm.memory,
				Score:  score,
			})
		}
	}
	return results, nil
}

// UpdateAccessMetadata updates the last-accessed timestamp and increments the access count.
func (m *MockStore) UpdateAccessMetadata(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	sm, ok := m.memories[id]
	if !ok {
		return fmt.Errorf("memory %s not found", id)
	}
	sm.memory.LastAccessed = time.Now().UTC()
	sm.memory.AccessCount++
	return nil
}

// Stats returns collection statistics computed from the in-memory store.
func (m *MockStore) Stats(_ context.Context) (*models.CollectionStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := &models.CollectionStats{
		TotalMemories: int64(len(m.memories)),
		ByType:        make(map[string]int64),
		ByScope:       make(map[string]int64),
	}

	for _, sm := range m.memories {
		stats.ByType[string(sm.memory.Type)]++
		stats.ByScope[string(sm.memory.Scope)]++
	}

	return stats, nil
}

// Close is a no-op for the mock store.
func (m *MockStore) Close() error {
	return nil
}

// --- helpers ---

func matchesFilters(mem models.Memory, f *SearchFilters) bool {
	// Sensitive memories are opt-in: only returned when explicitly requested.
	if mem.Visibility == models.VisibilitySensitive {
		if f == nil || f.Visibility == nil || *f.Visibility != models.VisibilitySensitive {
			return false
		}
	}
	if f == nil {
		return true
	}
	if f.Type != nil && mem.Type != *f.Type {
		return false
	}
	if f.Scope != nil && mem.Scope != *f.Scope {
		return false
	}
	if f.Visibility != nil && mem.Visibility != *f.Visibility {
		return false
	}
	if f.Project != nil && mem.Project != *f.Project {
		return false
	}
	if f.Source != nil && mem.Source != *f.Source {
		return false
	}
	for _, required := range f.Tags {
		found := false
		for _, t := range mem.Tags {
			if t == required {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
