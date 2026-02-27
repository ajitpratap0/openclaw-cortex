package tests

import (
	"context"
	"fmt"
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

func lifecycleLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestLifecycle_NoExpiredMemories(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	// Only fresh TTL memory (should NOT expire)
	fresh := models.Memory{
		ID:           "ttl-not-expired",
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopeTTL,
		Visibility:   models.VisibilityShared,
		Content:      "Fresh TTL memory",
		TTLSeconds:   3600,
		CreatedAt:    time.Now().UTC().Add(-30 * time.Minute), // 30 min ago, expires in 30 min
		LastAccessed: time.Now().UTC().Add(-30 * time.Minute),
	}
	_ = s.Upsert(ctx, fresh, testVector(0.5))

	lm := lifecycle.NewManager(s, lifecycleLogger())
	report, err := lm.Run(ctx, false)
	require.NoError(t, err)

	assert.Equal(t, 0, report.Expired, "no memories should be expired")
	assert.Equal(t, 0, report.Decayed, "no sessions should be decayed")

	// Fresh TTL should still exist
	_, err = s.Get(ctx, "ttl-not-expired")
	assert.NoError(t, err)
}

func TestLifecycle_ZeroTTLSkipped(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	// TTL memory with TTLSeconds=0 should be skipped
	zeroTTL := models.Memory{
		ID:         "ttl-zero",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopeTTL,
		Visibility: models.VisibilityShared,
		Content:    "Zero TTL memory",
		TTLSeconds: 0, // No TTL set
		CreatedAt:  time.Now().UTC().Add(-24 * time.Hour),
	}
	_ = s.Upsert(ctx, zeroTTL, testVector(0.5))

	lm := lifecycle.NewManager(s, lifecycleLogger())
	report, err := lm.Run(ctx, false)
	require.NoError(t, err)

	assert.Equal(t, 0, report.Expired, "zero-TTL memories should not be expired")

	// Memory should still exist since TTL=0 means skip
	_, err = s.Get(ctx, "ttl-zero")
	assert.NoError(t, err)
}

func TestLifecycle_MultipleExpiredAndDecayed(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	// Two expired TTL memories
	for i, id := range []string{"exp-1", "exp-2"} {
		mem := models.Memory{
			ID:           id,
			Type:         models.MemoryTypeFact,
			Scope:        models.ScopeTTL,
			Visibility:   models.VisibilityShared,
			Content:      "Expired memory " + id,
			TTLSeconds:   3600,
			CreatedAt:    time.Now().UTC().Add(-2 * time.Hour),
			LastAccessed: time.Now().UTC().Add(-2 * time.Hour),
		}
		_ = s.Upsert(ctx, mem, testVector(float32(i)*0.1))
	}

	// Two old session memories
	for i, id := range []string{"sess-1", "sess-2"} {
		mem := models.Memory{
			ID:           id,
			Type:         models.MemoryTypeEpisode,
			Scope:        models.ScopeSession,
			Visibility:   models.VisibilityShared,
			Content:      "Old session memory " + id,
			CreatedAt:    time.Now().UTC().Add(-48 * time.Hour),
			LastAccessed: time.Now().UTC().Add(-48 * time.Hour),
		}
		_ = s.Upsert(ctx, mem, testVector(float32(i)*0.1+0.5))
	}

	lm := lifecycle.NewManager(s, lifecycleLogger())
	report, err := lm.Run(ctx, false)
	require.NoError(t, err)

	assert.Equal(t, 2, report.Expired, "should expire 2 TTL memories")
	assert.Equal(t, 2, report.Decayed, "should decay 2 session memories")
}

func TestLifecycle_SessionDecay_ZeroLastAccessed_UsesCreatedAt(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	// Session memory with zero LastAccessed — should fall back to CreatedAt
	oldSession := models.Memory{
		ID:      "sess-zero-last-accessed",
		Type:    models.MemoryTypeEpisode,
		Scope:   models.ScopeSession,
		Content: "Old session with zero last accessed",
		// LastAccessed is zero value
		CreatedAt: time.Now().UTC().Add(-48 * time.Hour), // old enough to decay
	}
	_ = s.Upsert(ctx, oldSession, testVector(0.5))

	lm := lifecycle.NewManager(s, lifecycleLogger())
	report, err := lm.Run(ctx, false)
	require.NoError(t, err)

	assert.Equal(t, 1, report.Decayed, "should decay memory using CreatedAt when LastAccessed is zero")

	_, err = s.Get(ctx, "sess-zero-last-accessed")
	assert.Error(t, err, "decayed memory should be removed")
}

func TestLifecycle_DryRun_SessionDecay(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	oldSession := models.Memory{
		ID:           "dry-sess-1",
		Type:         models.MemoryTypeEpisode,
		Scope:        models.ScopeSession,
		Visibility:   models.VisibilityShared,
		Content:      "Old session data",
		CreatedAt:    time.Now().UTC().Add(-48 * time.Hour),
		LastAccessed: time.Now().UTC().Add(-48 * time.Hour),
	}
	_ = s.Upsert(ctx, oldSession, testVector(0.5))

	lm := lifecycle.NewManager(s, lifecycleLogger())
	report, err := lm.Run(ctx, true) // dryRun=true
	require.NoError(t, err)

	assert.Equal(t, 1, report.Decayed, "should report 1 decayed in dry run")

	// Memory should still exist in dry run
	_, err = s.Get(ctx, "dry-sess-1")
	assert.NoError(t, err, "memory should still exist after dry run")
}

func TestLifecycle_EmptyStore(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	lm := lifecycle.NewManager(s, lifecycleLogger())
	report, err := lm.Run(ctx, false)
	require.NoError(t, err)

	assert.Equal(t, 0, report.Expired)
	assert.Equal(t, 0, report.Decayed)
}

// TestLifecycle_ManySessionsMultiPage tests the listAll pagination path
// by adding more than pageSize (500) session memories.
func TestLifecycle_ManySessionsMultiPage(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	// Add 501 old session memories to trigger multi-page listing
	oldTime := time.Now().UTC().Add(-48 * time.Hour)
	for i := 0; i < 501; i++ {
		mem := models.Memory{
			ID:           "bulk-sess-" + fmt.Sprintf("%04d", i),
			Type:         models.MemoryTypeEpisode,
			Scope:        models.ScopeSession,
			Visibility:   models.VisibilityShared,
			Content:      "Old session memory",
			CreatedAt:    oldTime,
			LastAccessed: oldTime,
		}
		_ = s.Upsert(ctx, mem, testVector(float32(i%100)*0.01))
	}

	lm := lifecycle.NewManager(s, lifecycleLogger())
	report, err := lm.Run(ctx, true) // dryRun=true to avoid deleting 501 items
	require.NoError(t, err)

	assert.Equal(t, 501, report.Decayed, "should report all 501 sessions as decayed")
}
