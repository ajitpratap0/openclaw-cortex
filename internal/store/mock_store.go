package store

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/pkg/vecmath"
)

// MockStore is an in-memory implementation of Store for testing.
type MockStore struct {
	mu       sync.RWMutex
	memories map[string]*storedMemory
	entities map[string]*models.Entity
}

type storedMemory struct {
	memory models.Memory
	vector []float32
}

// NewMockStore creates a new mock store.
func NewMockStore() *MockStore {
	return &MockStore{
		memories: make(map[string]*storedMemory),
		entities: make(map[string]*models.Entity),
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
		score := vecmath.CosineSimilarity(vector, sm.vector)
		mem := sm.memory
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
		results = append(results, models.SearchResult{
			Memory: mem,
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
		all = append(all, mem)
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
		score := vecmath.CosineSimilarity(vector, sm.vector)
		if score >= threshold {
			mem := sm.memory
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
			results = append(results, models.SearchResult{
				Memory: mem,
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

// UpsertEntity inserts or updates an entity in the mock store.
func (m *MockStore) UpsertEntity(_ context.Context, entity models.Entity) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Deep-copy mutable fields to prevent external mutation.
	e := entity
	if len(entity.Aliases) > 0 {
		aliases := make([]string, len(entity.Aliases))
		copy(aliases, entity.Aliases)
		e.Aliases = aliases
	}
	if len(entity.MemoryIDs) > 0 {
		ids := make([]string, len(entity.MemoryIDs))
		copy(ids, entity.MemoryIDs)
		e.MemoryIDs = ids
	}
	if len(entity.Metadata) > 0 {
		meta := make(map[string]any, len(entity.Metadata))
		for k, v := range entity.Metadata {
			meta[k] = v
		}
		e.Metadata = meta
	}

	m.entities[e.ID] = &e
	return nil
}

// GetEntity retrieves a single entity by ID.
func (m *MockStore) GetEntity(_ context.Context, id string) (*models.Entity, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	e, ok := m.entities[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
	}

	// Deep-copy mutable fields.
	out := *e
	if len(e.Aliases) > 0 {
		aliases := make([]string, len(e.Aliases))
		copy(aliases, e.Aliases)
		out.Aliases = aliases
	}
	if len(e.MemoryIDs) > 0 {
		ids := make([]string, len(e.MemoryIDs))
		copy(ids, e.MemoryIDs)
		out.MemoryIDs = ids
	}
	if len(e.Metadata) > 0 {
		meta := make(map[string]any, len(e.Metadata))
		for k, v := range e.Metadata {
			meta[k] = v
		}
		out.Metadata = meta
	}

	return &out, nil
}

// SearchEntities finds entities whose Name or any Alias contains the given
// substring (case-insensitive).
func (m *MockStore) SearchEntities(_ context.Context, name string) ([]models.Entity, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	nameLower := strings.ToLower(name)
	var results []models.Entity
	for _, e := range m.entities {
		matched := strings.Contains(strings.ToLower(e.Name), nameLower)
		if !matched {
			for _, alias := range e.Aliases {
				if strings.Contains(strings.ToLower(alias), nameLower) {
					matched = true
					break
				}
			}
		}

		if matched {
			cp := *e
			if len(e.Aliases) > 0 {
				cp.Aliases = make([]string, len(e.Aliases))
				copy(cp.Aliases, e.Aliases)
			}
			if len(e.MemoryIDs) > 0 {
				cp.MemoryIDs = make([]string, len(e.MemoryIDs))
				copy(cp.MemoryIDs, e.MemoryIDs)
			}
			if len(e.Metadata) > 0 {
				cp.Metadata = make(map[string]any, len(e.Metadata))
				for k, v := range e.Metadata {
					cp.Metadata[k] = v
				}
			}
			results = append(results, cp)
		}
	}
	return results, nil
}

// LinkMemoryToEntity adds a memory ID to an entity's MemoryIDs list.
func (m *MockStore) LinkMemoryToEntity(_ context.Context, entityID, memoryID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	e, ok := m.entities[entityID]
	if !ok {
		return fmt.Errorf("entity %s not found", entityID)
	}

	// Check for duplicates.
	for _, id := range e.MemoryIDs {
		if id == memoryID {
			return nil
		}
	}

	e.MemoryIDs = append(e.MemoryIDs, memoryID)
	return nil
}

// GetChain follows the SupersedesID chain and returns the full history.
// The chain is returned newest first. Stops when SupersedesID is empty or the
// referenced memory is not found. A visited set prevents infinite loops.
func (m *MockStore) GetChain(ctx context.Context, id string) ([]models.Memory, error) {
	var chain []models.Memory
	visited := make(map[string]bool)
	currentID := id

	for currentID != "" {
		if visited[currentID] {
			break
		}
		visited[currentID] = true

		mem, err := m.Get(ctx, currentID)
		if err != nil {
			// Stop at a missing link — not an error for the caller.
			break
		}
		chain = append(chain, *mem)
		currentID = mem.SupersedesID
	}

	return chain, nil
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
