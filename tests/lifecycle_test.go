package tests

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/lifecycle"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

func TestLifecycle_ExpireTTL(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	s := store.NewMockStore()

	// Create a TTL memory that has expired (created 2 hours ago with 1 hour TTL)
	expired := models.Memory{
		ID:           "ttl-expired",
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopeTTL,
		Visibility:   models.VisibilityShared,
		Content:      "This should expire",
		TTLSeconds:   3600, // 1 hour
		CreatedAt:    time.Now().UTC().Add(-2 * time.Hour),
		UpdatedAt:    time.Now().UTC().Add(-2 * time.Hour),
		LastAccessed: time.Now().UTC().Add(-2 * time.Hour),
	}
	_ = s.Upsert(ctx, expired, testVector(768, 0.1))

	// Create a TTL memory that has NOT expired (created 10 min ago with 1 hour TTL)
	fresh := models.Memory{
		ID:           "ttl-fresh",
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopeTTL,
		Visibility:   models.VisibilityShared,
		Content:      "This should stay",
		TTLSeconds:   3600,
		CreatedAt:    time.Now().UTC().Add(-10 * time.Minute),
		UpdatedAt:    time.Now().UTC().Add(-10 * time.Minute),
		LastAccessed: time.Now().UTC().Add(-10 * time.Minute),
	}
	_ = s.Upsert(ctx, fresh, testVector(768, 0.2))

	// Create a permanent memory (should not be touched)
	permanent := models.Memory{
		ID:         "perm-1",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Visibility: models.VisibilityShared,
		Content:    "Permanent memory",
		CreatedAt:  time.Now().UTC().Add(-720 * time.Hour),
	}
	_ = s.Upsert(ctx, permanent, testVector(768, 0.3))

	lm := lifecycle.NewManager(s, logger)
	report, err := lm.Run(ctx, false)
	require.NoError(t, err)

	assert.Equal(t, 1, report.Expired, "should expire 1 TTL memory")

	// Verify the expired one is gone
	_, err = s.Get(ctx, "ttl-expired")
	assert.Error(t, err)

	// Fresh TTL and permanent should still exist
	_, err = s.Get(ctx, "ttl-fresh")
	assert.NoError(t, err)
	_, err = s.Get(ctx, "perm-1")
	assert.NoError(t, err)
}

func TestLifecycle_DecaySessions(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	s := store.NewMockStore()

	// Old session memory (last accessed > 24h ago)
	old := models.Memory{
		ID:           "session-old",
		Type:         models.MemoryTypeEpisode,
		Scope:        models.ScopeSession,
		Visibility:   models.VisibilityShared,
		Content:      "Old session data",
		CreatedAt:    time.Now().UTC().Add(-48 * time.Hour),
		LastAccessed: time.Now().UTC().Add(-48 * time.Hour),
	}
	_ = s.Upsert(ctx, old, testVector(768, 0.1))

	// Recent session memory
	recent := models.Memory{
		ID:           "session-recent",
		Type:         models.MemoryTypeEpisode,
		Scope:        models.ScopeSession,
		Visibility:   models.VisibilityShared,
		Content:      "Recent session data",
		CreatedAt:    time.Now().UTC().Add(-1 * time.Hour),
		LastAccessed: time.Now().UTC().Add(-1 * time.Hour),
	}
	_ = s.Upsert(ctx, recent, testVector(768, 0.2))

	lm := lifecycle.NewManager(s, logger)
	report, err := lm.Run(ctx, false)
	require.NoError(t, err)

	assert.Equal(t, 1, report.Decayed, "should decay 1 session memory")

	// Old session should be gone
	_, err = s.Get(ctx, "session-old")
	assert.Error(t, err)

	// Recent session should remain
	_, err = s.Get(ctx, "session-recent")
	assert.NoError(t, err)
}

func TestLifecycle_DryRun(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	s := store.NewMockStore()

	expired := models.Memory{
		ID:         "dry-run-ttl",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopeTTL,
		Visibility: models.VisibilityShared,
		Content:    "Should not be deleted in dry run",
		TTLSeconds: 3600,
		CreatedAt:  time.Now().UTC().Add(-2 * time.Hour),
	}
	_ = s.Upsert(ctx, expired, testVector(768, 0.1))

	lm := lifecycle.NewManager(s, logger)
	report, err := lm.Run(ctx, true)
	require.NoError(t, err)

	assert.Equal(t, 1, report.Expired, "should report 1 expired even in dry run")

	// But the memory should still exist
	_, err = s.Get(ctx, "dry-run-ttl")
	assert.NoError(t, err, "memory should still exist after dry run")
}
