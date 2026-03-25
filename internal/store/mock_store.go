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

// Compile-time assertions that MockStore implements both Store and ResettableStore.
var _ Store = (*MockStore)(nil)
var _ ResettableStore = (*MockStore)(nil)

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
	// Auto-invalidate superseded memory.
	if memory.SupersedesID != "" {
		if sm, ok := m.memories[memory.SupersedesID]; ok {
			now := time.Now().UTC()
			sm.memory.ValidTo = &now
			sm.memory.IsCurrentVersion = false
		}
	}

	// Set valid_from if not already set.
	if memory.ValidFrom.IsZero() {
		if !memory.CreatedAt.IsZero() {
			memory.ValidFrom = memory.CreatedAt
		} else {
			memory.ValidFrom = time.Now().UTC()
		}
	}
	memory.IsCurrentVersion = memory.ValidTo == nil

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
	mem.IsCurrentVersion = mem.ValidTo == nil
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
		return fmt.Errorf("mock update access metadata: %s: %w", id, ErrNotFound)
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
		TotalMemories:      int64(len(m.memories)),
		ByType:             make(map[string]int64),
		ByScope:            make(map[string]int64),
		ReinforcementTiers: make(map[string]int64),
	}

	now := time.Now().UTC()
	ttlDeadline := now.Add(24 * time.Hour)

	// Collect all memories sorted by access_count for top-accessed.
	type idxMem struct {
		id  string
		mem models.Memory
	}
	all := make([]idxMem, 0, len(m.memories))

	for id, sm := range m.memories {
		mem := sm.memory
		stats.ByType[string(mem.Type)]++
		stats.ByScope[string(mem.Scope)]++

		// Temporal range
		if !mem.CreatedAt.IsZero() {
			if stats.OldestMemory == nil || mem.CreatedAt.Before(*stats.OldestMemory) {
				t := mem.CreatedAt
				stats.OldestMemory = &t
			}
			if stats.NewestMemory == nil || mem.CreatedAt.After(*stats.NewestMemory) {
				t := mem.CreatedAt
				stats.NewestMemory = &t
			}
		}

		// Reinforcement tiers
		rc := mem.ReinforcedCount
		switch {
		case rc == 0:
			stats.ReinforcementTiers["0"]++
		case rc >= 1 && rc <= 3:
			stats.ReinforcementTiers["1-3"]++
		case rc >= 4 && rc <= 10:
			stats.ReinforcementTiers["4-10"]++
		default:
			stats.ReinforcementTiers["10+"]++
		}

		// Active conflicts
		if mem.ConflictStatus == models.ConflictStatusActive {
			stats.ActiveConflicts++
		}

		// Pending TTL expiry
		if mem.Scope == models.ScopeTTL && !mem.ValidUntil.IsZero() && mem.ValidUntil.Before(ttlDeadline) {
			stats.PendingTTLExpiry++
		}

		all = append(all, idxMem{id: id, mem: mem})
	}

	// Sort by access_count descending (bubble sort, fine for mock)
	for i := 0; i < len(all); i++ {
		for j := i + 1; j < len(all); j++ {
			if all[j].mem.AccessCount > all[i].mem.AccessCount {
				all[i], all[j] = all[j], all[i]
			}
		}
	}

	limit := 5
	if len(all) < limit {
		limit = len(all)
	}
	for i := 0; i < limit; i++ {
		content := all[i].mem.Content
		if len(content) > 80 {
			content = content[:80]
		}
		stats.TopAccessed = append(stats.TopAccessed, models.MemoryPreview{
			ID:          all[i].id,
			Content:     content,
			AccessCount: all[i].mem.AccessCount,
		})
	}

	// Storage estimate: points * 768 dimensions * 4 bytes per float32
	stats.StorageEstimate = stats.TotalMemories * 768 * 4

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
// substring (case-insensitive). entityType filters by type (empty = all).
// limit caps results (0 = no cap).
func (m *MockStore) SearchEntities(_ context.Context, name, entityType string, limit int) ([]models.Entity, error) {
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
			if entityType != "" && string(e.Type) != entityType {
				continue
			}
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
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

// LinkMemoryToEntity adds a memory ID to an entity's MemoryIDs list.
func (m *MockStore) LinkMemoryToEntity(_ context.Context, entityID, memoryID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	e, ok := m.entities[entityID]
	if !ok {
		return fmt.Errorf("mock link memory to entity: entity %s: %w", entityID, ErrNotFound)
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

// UpdateConflictFields sets ConflictGroupID and ConflictStatus on an existing memory.
func (m *MockStore) UpdateConflictFields(_ context.Context, id, groupID, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	sm, ok := m.memories[id]
	if !ok {
		return ErrNotFound
	}
	sm.memory.ConflictGroupID = groupID
	sm.memory.ConflictStatus = models.ConflictStatus(status)
	return nil
}

// UpdateReinforcement boosts the confidence of an existing memory (capped at 1.0)
// and increments ReinforcedCount.
func (m *MockStore) UpdateReinforcement(_ context.Context, id string, boost float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	sm, ok := m.memories[id]
	if !ok {
		return ErrNotFound
	}
	sm.memory.Confidence += boost
	if sm.memory.Confidence > 1.0 {
		sm.memory.Confidence = 1.0
	}
	sm.memory.ReinforcedCount++
	sm.memory.ReinforcedAt = time.Now()
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

// InvalidateMemory sets valid_to on a memory without deleting it.
func (m *MockStore) InvalidateMemory(_ context.Context, id string, validTo time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	sm, ok := m.memories[id]
	if !ok {
		return ErrNotFound
	}
	sm.memory.ValidTo = &validTo
	sm.memory.IsCurrentVersion = false
	return nil
}

// GetHistory returns all versions of a memory chain (follows SupersedesID).
func (m *MockStore) GetHistory(ctx context.Context, id string) ([]models.Memory, error) {
	return m.GetChain(ctx, id)
}

// MigrateTemporalFields is a no-op in the mock store.
func (m *MockStore) MigrateTemporalFields(_ context.Context) error {
	return nil
}

// DeleteAllMemories clears all in-memory data from the mock store.
// MockStore's complete mutable state is exactly two maps — memories and
// entities — both reset here. There are no relationship or episode maps:
// MemgraphStore stores episodes and relationships as graph nodes/edges removed
// by MATCH (n) DETACH DELETE n; MockStore has no equivalent structures, so
// resetting memories + entities is a full wipe and matches the contract.
func (m *MockStore) DeleteAllMemories(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.memories = make(map[string]*storedMemory)
	m.entities = make(map[string]*models.Entity)
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
		// Default temporal filter: exclude invalidated memories.
		return mem.ValidTo == nil
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
	if f.UserID != "" && mem.UserID != f.UserID {
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
	if f.ConflictStatus != nil && mem.ConflictStatus != *f.ConflictStatus {
		return false
	}

	// Temporal filtering.
	if f.AsOf != nil {
		if !mem.ValidFrom.IsZero() && mem.ValidFrom.After(*f.AsOf) {
			return false
		}
		if mem.ValidTo != nil && !mem.ValidTo.After(*f.AsOf) {
			return false
		}
	} else if !f.IncludeInvalidated {
		if mem.ValidTo != nil {
			return false
		}
	}

	return true
}
