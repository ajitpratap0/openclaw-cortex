package graph

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
)

// MockGraphClient implements Client with in-memory maps for testing.
type MockGraphClient struct {
	mu       sync.RWMutex
	entities map[string]models.Entity
	facts    map[string]models.Fact
	episodes map[string]models.Episode
}

// Compile-time interface assertion.
var _ Client = (*MockGraphClient)(nil)

// NewMockGraphClient creates a new MockGraphClient.
func NewMockGraphClient() *MockGraphClient {
	return &MockGraphClient{
		entities: make(map[string]models.Entity),
		facts:    make(map[string]models.Fact),
		episodes: make(map[string]models.Episode),
	}
}

func (m *MockGraphClient) EnsureSchema(_ context.Context, _ int) error {
	return nil
}

func (m *MockGraphClient) UpsertEntity(_ context.Context, entity models.Entity) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entities[entity.ID] = entity
	return nil
}

func (m *MockGraphClient) SearchEntities(_ context.Context, query string, _ []float32, _ string, limit int) ([]EntityResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var results []EntityResult
	for id := range m.entities {
		e := m.entities[id]
		if query == "" || strings.Contains(strings.ToLower(e.Name), strings.ToLower(query)) {
			results = append(results, EntityResult{
				ID:    id,
				Name:  e.Name,
				Type:  string(e.Type),
				Score: 1.0,
			})
		}
		if limit > 0 && len(results) >= limit {
			break
		}
	}
	return results, nil
}

func (m *MockGraphClient) GetEntity(_ context.Context, id string) (*models.Entity, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	e, ok := m.entities[id]
	if !ok {
		return nil, fmt.Errorf("entity %s not found", id)
	}
	return &e, nil
}

func (m *MockGraphClient) UpsertFact(_ context.Context, fact models.Fact) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.facts[fact.ID] = fact
	return nil
}

func (m *MockGraphClient) SearchFacts(_ context.Context, _ string, _ []float32, limit int) ([]FactResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var results []FactResult
	for id := range m.facts {
		f := m.facts[id]
		if f.ExpiredAt != nil {
			continue
		}
		results = append(results, FactResult{
			ID:              f.ID,
			Fact:            f.Fact,
			SourceEntityID:  f.SourceEntityID,
			TargetEntityID:  f.TargetEntityID,
			SourceMemoryIDs: f.SourceMemoryIDs,
			Score:           1.0,
		})
		if limit > 0 && len(results) >= limit {
			break
		}
	}
	return results, nil
}

func (m *MockGraphClient) InvalidateFact(_ context.Context, id string, expiredAt, invalidAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	f, ok := m.facts[id]
	if !ok {
		return fmt.Errorf("fact %s not found", id)
	}
	f.ExpiredAt = &expiredAt
	f.InvalidAt = &invalidAt
	m.facts[id] = f
	return nil
}

func (m *MockGraphClient) GetFactsBetween(_ context.Context, sourceID, targetID string) ([]models.Fact, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var results []models.Fact
	for id := range m.facts {
		f := m.facts[id]
		if f.ExpiredAt != nil {
			continue
		}
		if (f.SourceEntityID == sourceID && f.TargetEntityID == targetID) ||
			(f.SourceEntityID == targetID && f.TargetEntityID == sourceID) {
			results = append(results, f)
		}
	}
	return results, nil
}

func (m *MockGraphClient) GetFactsForEntity(_ context.Context, entityID string) ([]models.Fact, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var results []models.Fact
	for id := range m.facts {
		f := m.facts[id]
		if f.ExpiredAt != nil {
			continue
		}
		if f.SourceEntityID == entityID || f.TargetEntityID == entityID {
			results = append(results, f)
		}
	}
	return results, nil
}

func (m *MockGraphClient) AppendEpisode(_ context.Context, factID, episodeID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	f, ok := m.facts[factID]
	if !ok {
		return fmt.Errorf("fact %s not found", factID)
	}
	for _, e := range f.Episodes {
		if e == episodeID {
			return nil // already present
		}
	}
	f.Episodes = append(f.Episodes, episodeID)
	m.facts[factID] = f
	return nil
}

func (m *MockGraphClient) AppendMemoryToFact(_ context.Context, factID, memoryID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	f, ok := m.facts[factID]
	if !ok {
		return fmt.Errorf("fact %s not found", factID)
	}
	for _, mid := range f.SourceMemoryIDs {
		if mid == memoryID {
			return nil // already present
		}
	}
	f.SourceMemoryIDs = append(f.SourceMemoryIDs, memoryID)
	m.facts[factID] = f
	return nil
}

func (m *MockGraphClient) GetMemoryFacts(_ context.Context, memoryID string) ([]models.Fact, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var results []models.Fact
	for id := range m.facts {
		f := m.facts[id]
		for _, mid := range f.SourceMemoryIDs {
			if mid == memoryID {
				results = append(results, f)
				break
			}
		}
	}
	return results, nil
}

func (m *MockGraphClient) RecallByGraph(_ context.Context, _ string, _ []float32, _ int) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	seen := make(map[string]bool)
	var memoryIDs []string
	for id := range m.facts {
		f := m.facts[id]
		if f.ExpiredAt != nil {
			continue
		}
		for _, mid := range f.SourceMemoryIDs {
			if !seen[mid] {
				seen[mid] = true
				memoryIDs = append(memoryIDs, mid)
			}
		}
	}
	return memoryIDs, nil
}

// CreateEpisode stores an episode in the mock.
func (m *MockGraphClient) CreateEpisode(_ context.Context, episode models.Episode) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	ep := episode
	if len(episode.MemoryIDs) > 0 {
		ids := make([]string, len(episode.MemoryIDs))
		copy(ids, episode.MemoryIDs)
		ep.MemoryIDs = ids
	}
	if len(episode.FactIDs) > 0 {
		ids := make([]string, len(episode.FactIDs))
		copy(ids, episode.FactIDs)
		ep.FactIDs = ids
	}
	m.episodes[ep.UUID] = ep
	return nil
}

// GetEpisodesForMemory returns all episodes that reference the given memory ID.
func (m *MockGraphClient) GetEpisodesForMemory(_ context.Context, memoryID string) ([]models.Episode, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var results []models.Episode
	for i := range m.episodes {
		for _, id := range m.episodes[i].MemoryIDs {
			if id == memoryID {
				results = append(results, m.episodes[i])
				break
			}
		}
	}
	return results, nil
}

// GetEpisodes returns all stored episodes (test helper).
func (m *MockGraphClient) GetEpisodes() []models.Episode {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]models.Episode, 0, len(m.episodes))
	for i := range m.episodes {
		out = append(out, m.episodes[i])
	}
	return out
}

func (m *MockGraphClient) Healthy(_ context.Context) bool {
	return true
}

func (m *MockGraphClient) Close() error {
	return nil
}
