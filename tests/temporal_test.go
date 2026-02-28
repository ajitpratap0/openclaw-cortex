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

// TestGetChain_FollowsSupersedes verifies that GetChain follows the SupersedesID
// chain and returns memories newest-first.
func TestGetChain_FollowsSupersedes(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	// oldest is superseded by middle, which is superseded by newest.
	oldest := models.Memory{
		ID:         "chain-oldest",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Visibility: models.VisibilityShared,
		Content:    "oldest fact",
		Confidence: 0.7,
		CreatedAt:  time.Now().UTC().Add(-72 * time.Hour),
		UpdatedAt:  time.Now().UTC().Add(-72 * time.Hour),
	}
	middle := models.Memory{
		ID:           "chain-middle",
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopePermanent,
		Visibility:   models.VisibilityShared,
		Content:      "middle fact",
		Confidence:   0.8,
		SupersedesID: oldest.ID,
		CreatedAt:    time.Now().UTC().Add(-36 * time.Hour),
		UpdatedAt:    time.Now().UTC().Add(-36 * time.Hour),
	}
	newest := models.Memory{
		ID:           "chain-newest",
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopePermanent,
		Visibility:   models.VisibilityShared,
		Content:      "newest fact",
		Confidence:   0.95,
		SupersedesID: middle.ID,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}

	require.NoError(t, s.Upsert(ctx, oldest, testVector(0.1)))
	require.NoError(t, s.Upsert(ctx, middle, testVector(0.2)))
	require.NoError(t, s.Upsert(ctx, newest, testVector(0.3)))

	chain, err := s.GetChain(ctx, newest.ID)
	require.NoError(t, err)
	require.Len(t, chain, 3, "chain should contain all three memories")

	assert.Equal(t, newest.ID, chain[0].ID, "first in chain should be newest")
	assert.Equal(t, middle.ID, chain[1].ID, "second in chain should be middle")
	assert.Equal(t, oldest.ID, chain[2].ID, "third in chain should be oldest")
}

// TestGetChain_StopsAtMissingLink verifies that GetChain stops gracefully when
// the SupersedesID references a memory that does not exist in the store.
func TestGetChain_StopsAtMissingLink(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	// Points to a memory that was never stored.
	mem := models.Memory{
		ID:           "chain-dangling",
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopePermanent,
		Visibility:   models.VisibilityShared,
		Content:      "has a missing predecessor",
		Confidence:   0.9,
		SupersedesID: "does-not-exist",
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	require.NoError(t, s.Upsert(ctx, mem, testVector(0.5)))

	chain, err := s.GetChain(ctx, mem.ID)
	require.NoError(t, err)
	// Should return only the one memory we stored, stopping at the missing link.
	require.Len(t, chain, 1)
	assert.Equal(t, mem.ID, chain[0].ID)
}

// TestGetChain_PreventsInfiniteLoop verifies that GetChain does not loop forever
// when two memories reference each other (circular supersedes).
func TestGetChain_PreventsInfiniteLoop(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	// a → b → a (circular)
	a := models.Memory{
		ID:           "chain-circ-a",
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopePermanent,
		Visibility:   models.VisibilityShared,
		Content:      "circular a",
		SupersedesID: "chain-circ-b",
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	b := models.Memory{
		ID:           "chain-circ-b",
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopePermanent,
		Visibility:   models.VisibilityShared,
		Content:      "circular b",
		SupersedesID: "chain-circ-a",
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	require.NoError(t, s.Upsert(ctx, a, testVector(0.1)))
	require.NoError(t, s.Upsert(ctx, b, testVector(0.2)))

	// Must terminate — should return at most 2 memories (one loop detected).
	chain, err := s.GetChain(ctx, a.ID)
	require.NoError(t, err)
	// Both a and b appear, but the cycle is broken before revisiting a.
	assert.LessOrEqual(t, len(chain), 2, "circular chain must be bounded")
	assert.GreaterOrEqual(t, len(chain), 1, "at least the starting memory must be returned")
}

// TestGetChain_SingleMemory verifies GetChain works when there is no chain at all.
func TestGetChain_SingleMemory(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	mem := models.Memory{
		ID:         "chain-single",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Visibility: models.VisibilityShared,
		Content:    "standalone fact",
		Confidence: 0.9,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	require.NoError(t, s.Upsert(ctx, mem, testVector(0.5)))

	chain, err := s.GetChain(ctx, mem.ID)
	require.NoError(t, err)
	require.Len(t, chain, 1)
	assert.Equal(t, mem.ID, chain[0].ID)
}

// TestGetChain_MissingRoot verifies GetChain on a non-existent ID returns an empty
// chain without error.
func TestGetChain_MissingRoot(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	chain, err := s.GetChain(ctx, "totally-missing")
	require.NoError(t, err)
	assert.Empty(t, chain, "chain for a missing root should be empty")
}

// TestLifecycle_RetireExpiredFacts verifies that lifecycle.Run deletes memories
// whose ValidUntil is in the past.
func TestLifecycle_RetireExpiredFacts(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	s := store.NewMockStore()

	// A permanent memory whose ValidUntil was 1 hour ago — should be retired.
	expired := models.Memory{
		ID:         "retire-expired",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Visibility: models.VisibilityShared,
		Content:    "this fact is no longer valid",
		Confidence: 0.9,
		ValidUntil: time.Now().UTC().Add(-1 * time.Hour),
		CreatedAt:  time.Now().UTC().Add(-48 * time.Hour),
		UpdatedAt:  time.Now().UTC().Add(-48 * time.Hour),
	}
	require.NoError(t, s.Upsert(ctx, expired, testVector(0.1)))

	// A project-scoped memory with an expired ValidUntil — also retired.
	expiredProject := models.Memory{
		ID:         "retire-expired-project",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopeProject,
		Visibility: models.VisibilityShared,
		Content:    "expired project fact",
		Confidence: 0.85,
		ValidUntil: time.Now().UTC().Add(-30 * time.Minute),
		CreatedAt:  time.Now().UTC().Add(-24 * time.Hour),
		UpdatedAt:  time.Now().UTC().Add(-24 * time.Hour),
	}
	require.NoError(t, s.Upsert(ctx, expiredProject, testVector(0.2)))

	lm := lifecycle.NewManager(s, nil, logger)
	report, err := lm.Run(ctx, false)
	require.NoError(t, err)

	assert.Equal(t, 2, report.Retired, "should retire 2 expired-ValidUntil memories")

	// Both retired memories should be gone.
	_, errExp := s.Get(ctx, "retire-expired")
	assert.Error(t, errExp, "expired permanent memory should be deleted")

	_, errPrj := s.Get(ctx, "retire-expired-project")
	assert.Error(t, errPrj, "expired project memory should be deleted")
}

// TestLifecycle_DoesNotRetireNonExpiredMemories verifies that memories with
// ValidUntil in the future (or zero) are not retired.
func TestLifecycle_DoesNotRetireNonExpiredMemories(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	s := store.NewMockStore()

	// Memory with no ValidUntil set — never expires via this mechanism.
	noExpiry := models.Memory{
		ID:         "retire-no-expiry",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Visibility: models.VisibilityShared,
		Content:    "permanent fact without expiry",
		Confidence: 0.9,
		CreatedAt:  time.Now().UTC().Add(-24 * time.Hour),
		UpdatedAt:  time.Now().UTC().Add(-24 * time.Hour),
	}
	require.NoError(t, s.Upsert(ctx, noExpiry, testVector(0.3)))

	// Memory with ValidUntil in the future — should be kept.
	futureExpiry := models.Memory{
		ID:         "retire-future-expiry",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Visibility: models.VisibilityShared,
		Content:    "fact with future expiry",
		Confidence: 0.9,
		ValidUntil: time.Now().UTC().Add(24 * time.Hour),
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	require.NoError(t, s.Upsert(ctx, futureExpiry, testVector(0.4)))

	lm := lifecycle.NewManager(s, nil, logger)
	report, err := lm.Run(ctx, false)
	require.NoError(t, err)

	assert.Equal(t, 0, report.Retired, "no memories should be retired when none are expired")

	// Both should still exist.
	_, err = s.Get(ctx, "retire-no-expiry")
	assert.NoError(t, err, "memory without expiry should remain")

	_, err = s.Get(ctx, "retire-future-expiry")
	assert.NoError(t, err, "memory with future expiry should remain")
}

// TestLifecycle_RetireExpiredFacts_DryRun verifies that dry-run mode reports
// expired memories without deleting them.
func TestLifecycle_RetireExpiredFacts_DryRun(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	s := store.NewMockStore()

	expired := models.Memory{
		ID:         "retire-dry-expired",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Visibility: models.VisibilityShared,
		Content:    "should not be deleted in dry run",
		Confidence: 0.9,
		ValidUntil: time.Now().UTC().Add(-1 * time.Hour),
		CreatedAt:  time.Now().UTC().Add(-48 * time.Hour),
		UpdatedAt:  time.Now().UTC().Add(-48 * time.Hour),
	}
	require.NoError(t, s.Upsert(ctx, expired, testVector(0.1)))

	lm := lifecycle.NewManager(s, nil, logger)
	report, err := lm.Run(ctx, true) // dry run
	require.NoError(t, err)

	assert.Equal(t, 1, report.Retired, "dry run should report 1 retired memory")

	// The memory should still exist because it's a dry run.
	_, err = s.Get(ctx, "retire-dry-expired")
	assert.NoError(t, err, "memory should still exist after dry-run retirement")
}
