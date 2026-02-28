package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/ajitpratap0/openclaw-cortex/internal/embedder"
	"github.com/ajitpratap0/openclaw-cortex/internal/metrics"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

const pageSize uint64 = 500

// maxListAllMemories is a safety cap to prevent unbounded memory loading.
const maxListAllMemories = 50000

// consolidationThreshold is the cosine similarity above which two permanent memories are
// considered near-duplicates and eligible for merging.
const consolidationThreshold = 0.92

// Report summarizes the results of a lifecycle run.
type Report struct {
	Expired      int `json:"expired"`
	Decayed      int `json:"decayed"`
	Consolidated int `json:"consolidated"`
	Retired      int `json:"retired"`
}

// Manager handles memory lifecycle operations.
type Manager struct {
	store  store.Store
	emb    embedder.Embedder
	logger *slog.Logger
}

// NewManager creates a new lifecycle manager.
// emb may be nil; when nil, the consolidation phase is skipped.
func NewManager(st store.Store, emb embedder.Embedder, logger *slog.Logger) *Manager {
	return &Manager{
		store:  st,
		emb:    emb,
		logger: logger,
	}
}

// Run executes all lifecycle operations and collects errors from all phases.
// Partial results are preserved even when some phases fail.
func (m *Manager) Run(ctx context.Context, dryRun bool) (*Report, error) {
	report := &Report{}
	var errs []error

	// 1. TTL expiry
	expired, err := m.expireTTL(ctx, dryRun)
	if err != nil {
		m.logger.Error("lifecycle: TTL expiry failed", "error", err)
		errs = append(errs, fmt.Errorf("TTL expiry: %w", err))
	}
	report.Expired = expired

	// 2. Decay old session memories
	decayed, err := m.decaySessions(ctx, dryRun)
	if err != nil {
		m.logger.Error("lifecycle: session decay failed", "error", err)
		errs = append(errs, fmt.Errorf("session decay: %w", err))
	}
	report.Decayed = decayed

	// 3. Consolidate near-duplicate permanent memories
	consolidated, consolidateErr := m.consolidate(ctx, dryRun)
	if consolidateErr != nil {
		m.logger.Error("lifecycle: consolidation failed", "error", consolidateErr)
		errs = append(errs, fmt.Errorf("consolidation: %w", consolidateErr))
	}
	report.Consolidated = consolidated

	// 4. Retire memories whose ValidUntil has passed
	retired, retireErr := m.retireExpiredFacts(ctx, dryRun)
	if retireErr != nil {
		m.logger.Error("lifecycle: fact retirement failed", "error", retireErr)
		errs = append(errs, fmt.Errorf("fact retirement: %w", retireErr))
	}
	report.Retired = retired

	if len(errs) > 0 {
		return report, fmt.Errorf("lifecycle: %w", errors.Join(errs...))
	}
	return report, nil
}

// listAll paginates through all memories matching filters.
// It stops after maxListAllMemories to prevent unbounded memory usage.
func (m *Manager) listAll(ctx context.Context, filters *store.SearchFilters) ([]models.Memory, error) {
	var all []models.Memory
	var cursor string

	for {
		page, nextCursor, err := m.store.List(ctx, filters, pageSize, cursor)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		if uint64(len(all)) >= maxListAllMemories {
			m.logger.Warn("listAll hit safety cap, results truncated",
				"cap", maxListAllMemories,
				"loaded", len(all),
			)
			all = all[:maxListAllMemories]
			break
		}
		cursor = nextCursor
		if cursor == "" {
			break
		}
	}

	return all, nil
}

// expireTTL removes memories past their TTL.
func (m *Manager) expireTTL(ctx context.Context, dryRun bool) (int, error) {
	scope := models.ScopeTTL
	filters := &store.SearchFilters{Scope: &scope}

	memories, err := m.listAll(ctx, filters)
	if err != nil {
		return 0, fmt.Errorf("listing TTL memories: %w", err)
	}

	now := time.Now().UTC()
	expired := 0

	for i := range memories {
		mem := &memories[i]
		if mem.TTLSeconds <= 0 {
			continue
		}

		expiresAt := mem.CreatedAt.Add(time.Duration(mem.TTLSeconds) * time.Second)
		if now.After(expiresAt) {
			m.logger.Info("expiring TTL memory", "id", mem.ID, "created", mem.CreatedAt, "ttl_seconds", mem.TTLSeconds)
			if !dryRun {
				if delErr := m.store.Delete(ctx, mem.ID); delErr != nil {
					m.logger.Error("deleting expired memory", "id", mem.ID, "error", delErr)
					continue
				}
				metrics.Inc(metrics.LifecycleExpired) // only incremented on actual deletes, not dry-run
			}
			expired++
		}
	}

	return expired, nil
}

// decaySessions removes old session-scoped memories that haven't been accessed recently.
func (m *Manager) decaySessions(ctx context.Context, dryRun bool) (int, error) {
	scope := models.ScopeSession
	filters := &store.SearchFilters{Scope: &scope}

	memories, err := m.listAll(ctx, filters)
	if err != nil {
		return 0, fmt.Errorf("listing session memories: %w", err)
	}

	now := time.Now().UTC()
	decayed := 0
	decayThreshold := 24 * time.Hour // Session memories expire after 24h without access

	for i := range memories {
		mem := &memories[i]
		lastAccess := mem.LastAccessed
		if lastAccess.IsZero() {
			lastAccess = mem.CreatedAt
		}

		if now.Sub(lastAccess) > decayThreshold {
			m.logger.Info("decaying session memory", "id", mem.ID, "last_accessed", lastAccess)
			if !dryRun {
				if delErr := m.store.Delete(ctx, mem.ID); delErr != nil {
					m.logger.Error("deleting decayed memory", "id", mem.ID, "error", delErr)
					continue
				}
				metrics.Inc(metrics.LifecycleDecayed) // only incremented on actual deletes, not dry-run
			}
			decayed++
		}
	}

	return decayed, nil
}

// consolidate merges near-duplicate permanent memories, keeping the higher-confidence one.
func (m *Manager) consolidate(ctx context.Context, dryRun bool) (int, error) {
	if m.emb == nil {
		m.logger.Debug("lifecycle: consolidation skipped (no embedder configured)")
		return 0, nil
	}

	scope := models.ScopePermanent
	filters := &store.SearchFilters{Scope: &scope}
	memories, err := m.listAll(ctx, filters)
	if err != nil {
		return 0, fmt.Errorf("listing permanent memories: %w", err)
	}

	consolidated := 0
	deleted := make(map[string]bool)

	for i := range memories {
		if deleted[memories[i].ID] {
			continue
		}
		vecA, embedErr := m.emb.Embed(ctx, memories[i].Content)
		if embedErr != nil {
			m.logger.Warn("consolidate: embed failed", "id", memories[i].ID, "error", embedErr)
			continue
		}
		for j := i + 1; j < len(memories); j++ {
			if deleted[memories[j].ID] {
				continue
			}
			vecB, embedErrB := m.emb.Embed(ctx, memories[j].Content)
			if embedErrB != nil {
				continue
			}
			sim := cosineSimilarity(vecA, vecB)
			if sim > consolidationThreshold {
				// Keep higher confidence, delete the other
				keepIdx, deleteIdx := i, j
				if memories[j].Confidence > memories[i].Confidence {
					keepIdx, deleteIdx = j, i
				}
				m.logger.Info("consolidating duplicate memories",
					"keep", memories[keepIdx].ID,
					"delete", memories[deleteIdx].ID,
					"similarity", sim,
				)
				if !dryRun {
					if delErr := m.store.Delete(ctx, memories[deleteIdx].ID); delErr != nil {
						m.logger.Error("consolidate: delete failed", "id", memories[deleteIdx].ID, "error", delErr)
						continue
					}
				}
				deleted[memories[deleteIdx].ID] = true
				consolidated++
				// If the outer anchor was deleted, stop comparing against its stale vector.
				if deleteIdx == i {
					break
				}
			}
		}
	}

	return consolidated, nil
}

// retireExpiredFacts deletes memories whose ValidUntil has passed.
// It scans permanent and project memories (TTL-scoped memories are handled by expireTTL).
// Returns the count of deleted memories.
func (m *Manager) retireExpiredFacts(ctx context.Context, dryRun bool) (int, error) {
	now := time.Now().UTC()
	retired := 0

	for _, scope := range []models.MemoryScope{models.ScopePermanent, models.ScopeProject} {
		sc := scope
		filters := &store.SearchFilters{Scope: &sc}
		memories, err := m.listAll(ctx, filters)
		if err != nil {
			return retired, fmt.Errorf("retireExpiredFacts: listing %s memories: %w", scope, err)
		}

		for i := range memories {
			mem := &memories[i]
			if mem.ValidUntil.IsZero() {
				continue
			}
			if !mem.ValidUntil.Before(now) {
				continue
			}
			m.logger.Info("retiring expired fact", "id", mem.ID, "valid_until", mem.ValidUntil)
			if !dryRun {
				if delErr := m.store.Delete(ctx, mem.ID); delErr != nil {
					m.logger.Error("deleting retired memory", "id", mem.ID, "error", delErr)
					continue
				}
				metrics.Inc(metrics.LifecycleRetired) // only incremented on actual deletes, not dry-run
			}
			retired++
		}
	}

	return retired, nil
}

// cosineSimilarity computes the cosine similarity between two float32 vectors.
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
