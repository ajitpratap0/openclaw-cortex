package lifecycle

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

// Report summarizes the results of a lifecycle run.
type Report struct {
	Expired      int `json:"expired"`
	Decayed      int `json:"decayed"`
	Consolidated int `json:"consolidated"`
}

// Manager handles memory lifecycle operations.
type Manager struct {
	store  store.Store
	logger *slog.Logger
}

// NewManager creates a new lifecycle manager.
func NewManager(st store.Store, logger *slog.Logger) *Manager {
	return &Manager{
		store:  st,
		logger: logger,
	}
}

// Run executes all lifecycle operations.
func (m *Manager) Run(ctx context.Context, dryRun bool) (*Report, error) {
	report := &Report{}

	// 1. TTL expiry
	expired, err := m.expireTTL(ctx, dryRun)
	if err != nil {
		m.logger.Error("TTL expiry failed", "error", err)
	}
	report.Expired = expired

	// 2. Decay old session memories
	decayed, err := m.decaySessions(ctx, dryRun)
	if err != nil {
		m.logger.Error("session decay failed", "error", err)
	}
	report.Decayed = decayed

	return report, nil
}

// expireTTL removes memories past their TTL.
func (m *Manager) expireTTL(ctx context.Context, dryRun bool) (int, error) {
	scope := models.ScopeTTL
	filters := &store.SearchFilters{Scope: &scope}

	memories, err := m.store.List(ctx, filters, 1000, 0)
	if err != nil {
		return 0, fmt.Errorf("listing TTL memories: %w", err)
	}

	now := time.Now().UTC()
	expired := 0

	for _, mem := range memories {
		if mem.TTLSeconds <= 0 {
			continue
		}

		expiresAt := mem.CreatedAt.Add(time.Duration(mem.TTLSeconds) * time.Second)
		if now.After(expiresAt) {
			m.logger.Info("expiring TTL memory", "id", mem.ID, "created", mem.CreatedAt, "ttl_seconds", mem.TTLSeconds)
			if !dryRun {
				if err := m.store.Delete(ctx, mem.ID); err != nil {
					m.logger.Error("deleting expired memory", "id", mem.ID, "error", err)
					continue
				}
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

	memories, err := m.store.List(ctx, filters, 1000, 0)
	if err != nil {
		return 0, fmt.Errorf("listing session memories: %w", err)
	}

	now := time.Now().UTC()
	decayed := 0
	decayThreshold := 24 * time.Hour // Session memories expire after 24h without access

	for _, mem := range memories {
		lastAccess := mem.LastAccessed
		if lastAccess.IsZero() {
			lastAccess = mem.CreatedAt
		}

		if now.Sub(lastAccess) > decayThreshold {
			m.logger.Info("decaying session memory", "id", mem.ID, "last_accessed", lastAccess)
			if !dryRun {
				if err := m.store.Delete(ctx, mem.ID); err != nil {
					m.logger.Error("deleting decayed memory", "id", mem.ID, "error", err)
					continue
				}
			}
			decayed++
		}
	}

	return decayed, nil
}
