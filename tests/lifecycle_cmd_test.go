package tests

import (
	"bytes"
	"context"
	"encoding/json"
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

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// TestLifecycleCmd_EmptyStore verifies that the lifecycle manager runs
// successfully on an empty store and returns a zero report.
func TestLifecycleCmd_EmptyStore(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	mgr := lifecycle.NewManager(s, nil, quietLogger())
	report, err := mgr.Run(ctx, false)
	require.NoError(t, err)

	assert.Equal(t, 0, report.Expired)
	assert.Equal(t, 0, report.Decayed)
	assert.Equal(t, 0, report.Consolidated)
	assert.Equal(t, 0, report.Retired)
	assert.Equal(t, 0, report.ConflictsResolved)
}

// TestLifecycleCmd_ExpiredTTL_Cleaned verifies that expired TTL memories
// are cleaned up by a non-dry-run lifecycle execution.
func TestLifecycleCmd_ExpiredTTL_Cleaned(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	expired := models.Memory{
		ID:           "cmd-ttl-expired",
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopeTTL,
		Visibility:   models.VisibilityShared,
		Content:      "This TTL memory is expired",
		TTLSeconds:   3600, // 1 hour
		CreatedAt:    time.Now().UTC().Add(-2 * time.Hour),
		UpdatedAt:    time.Now().UTC().Add(-2 * time.Hour),
		LastAccessed: time.Now().UTC().Add(-2 * time.Hour),
	}
	require.NoError(t, s.Upsert(ctx, expired, testVector(0.1)))

	mgr := lifecycle.NewManager(s, nil, quietLogger())
	report, err := mgr.Run(ctx, false)
	require.NoError(t, err)

	assert.Equal(t, 1, report.Expired)

	// Memory should be deleted.
	_, getErr := s.Get(ctx, "cmd-ttl-expired")
	assert.Error(t, getErr)
}

// TestLifecycleCmd_DryRun_NoDeletes verifies that dry-run reports affected
// counts but does not actually delete anything.
func TestLifecycleCmd_DryRun_NoDeletes(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	// Insert an expired TTL memory.
	expired := models.Memory{
		ID:           "cmd-dry-ttl",
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopeTTL,
		Visibility:   models.VisibilityShared,
		Content:      "Should survive dry run",
		TTLSeconds:   3600,
		CreatedAt:    time.Now().UTC().Add(-2 * time.Hour),
		UpdatedAt:    time.Now().UTC().Add(-2 * time.Hour),
		LastAccessed: time.Now().UTC().Add(-2 * time.Hour),
	}
	require.NoError(t, s.Upsert(ctx, expired, testVector(0.1)))

	// Insert an old session memory.
	oldSession := models.Memory{
		ID:           "cmd-dry-sess",
		Type:         models.MemoryTypeEpisode,
		Scope:        models.ScopeSession,
		Visibility:   models.VisibilityShared,
		Content:      "Old session should survive dry run",
		CreatedAt:    time.Now().UTC().Add(-48 * time.Hour),
		LastAccessed: time.Now().UTC().Add(-48 * time.Hour),
	}
	require.NoError(t, s.Upsert(ctx, oldSession, testVector(0.2)))

	mgr := lifecycle.NewManager(s, nil, quietLogger())
	report, err := mgr.Run(ctx, true) // dry run
	require.NoError(t, err)

	assert.Equal(t, 1, report.Expired, "should report 1 expired")
	assert.Equal(t, 1, report.Decayed, "should report 1 decayed")

	// Both memories should still exist.
	_, getErr := s.Get(ctx, "cmd-dry-ttl")
	assert.NoError(t, getErr, "TTL memory should survive dry run")
	_, getErr = s.Get(ctx, "cmd-dry-sess")
	assert.NoError(t, getErr, "session memory should survive dry run")
}

// TestLifecycleCmd_JSONOutput verifies that the Report struct serializes
// to the expected JSON shape.
func TestLifecycleCmd_JSONOutput(t *testing.T) {
	report := &lifecycle.Report{
		Expired:           3,
		Decayed:           2,
		Consolidated:      1,
		Retired:           4,
		ConflictsResolved: 5,
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	require.NoError(t, enc.Encode(report))

	var decoded lifecycle.Report
	require.NoError(t, json.Unmarshal(buf.Bytes(), &decoded))

	assert.Equal(t, 3, decoded.Expired)
	assert.Equal(t, 2, decoded.Decayed)
	assert.Equal(t, 1, decoded.Consolidated)
	assert.Equal(t, 4, decoded.Retired)
	assert.Equal(t, 5, decoded.ConflictsResolved)
}

// TestLifecycleCmd_RetiredFacts verifies that memories with expired ValidUntil
// are retired by the lifecycle manager.
func TestLifecycleCmd_RetiredFacts(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	// Permanent fact with ValidUntil in the past.
	retired := models.Memory{
		ID:         "cmd-retire-1",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Visibility: models.VisibilityShared,
		Content:    "Fact that has expired its validity",
		Confidence: 0.9,
		CreatedAt:  time.Now().UTC().Add(-72 * time.Hour),
		ValidUntil: time.Now().UTC().Add(-24 * time.Hour), // expired yesterday
	}
	require.NoError(t, s.Upsert(ctx, retired, testVector(0.3)))

	// Permanent fact with ValidUntil in the future (should not be retired).
	valid := models.Memory{
		ID:         "cmd-valid-1",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Visibility: models.VisibilityShared,
		Content:    "Fact still valid",
		Confidence: 0.9,
		CreatedAt:  time.Now().UTC().Add(-72 * time.Hour),
		ValidUntil: time.Now().UTC().Add(24 * time.Hour), // valid until tomorrow
	}
	require.NoError(t, s.Upsert(ctx, valid, testVector(0.4)))

	mgr := lifecycle.NewManager(s, nil, quietLogger())
	report, err := mgr.Run(ctx, false)
	require.NoError(t, err)

	assert.Equal(t, 1, report.Retired, "should retire 1 expired fact")

	_, getErr := s.Get(ctx, "cmd-retire-1")
	assert.Error(t, getErr, "retired fact should be deleted")

	_, getErr = s.Get(ctx, "cmd-valid-1")
	assert.NoError(t, getErr, "valid fact should still exist")
}

// TestLifecycleCmd_AllPhases verifies that all lifecycle phases run together
// and the report aggregates counts from each phase.
func TestLifecycleCmd_AllPhases(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	now := time.Now().UTC()

	// 1. Expired TTL memory
	require.NoError(t, s.Upsert(ctx, models.Memory{
		ID:           "all-ttl",
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopeTTL,
		Visibility:   models.VisibilityShared,
		Content:      "Expired TTL",
		TTLSeconds:   3600,
		CreatedAt:    now.Add(-2 * time.Hour),
		LastAccessed: now.Add(-2 * time.Hour),
	}, testVector(0.1)))

	// 2. Old session memory
	require.NoError(t, s.Upsert(ctx, models.Memory{
		ID:           "all-sess",
		Type:         models.MemoryTypeEpisode,
		Scope:        models.ScopeSession,
		Visibility:   models.VisibilityShared,
		Content:      "Old session",
		CreatedAt:    now.Add(-48 * time.Hour),
		LastAccessed: now.Add(-48 * time.Hour),
	}, testVector(0.2)))

	// 3. Retired fact
	require.NoError(t, s.Upsert(ctx, models.Memory{
		ID:         "all-retire",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Visibility: models.VisibilityShared,
		Content:    "Retired fact",
		Confidence: 0.9,
		CreatedAt:  now.Add(-72 * time.Hour),
		ValidUntil: now.Add(-24 * time.Hour),
	}, testVector(0.3)))

	// 4. Conflict group
	require.NoError(t, s.Upsert(ctx, models.Memory{
		ID: "all-c1", Content: "Conflict winner", Confidence: 0.95,
		Type: models.MemoryTypeFact, Scope: models.ScopePermanent,
		ConflictGroupID: "grp-all", ConflictStatus: "active",
		CreatedAt: now,
	}, testVector(0.4)))
	require.NoError(t, s.Upsert(ctx, models.Memory{
		ID: "all-c2", Content: "Conflict loser", Confidence: 0.6,
		Type: models.MemoryTypeFact, Scope: models.ScopePermanent,
		ConflictGroupID: "grp-all", ConflictStatus: "active",
		CreatedAt: now.Add(-1 * time.Hour),
	}, testVector(0.5)))

	mgr := lifecycle.NewManager(s, nil, quietLogger())
	report, err := mgr.Run(ctx, false)
	require.NoError(t, err)

	assert.Equal(t, 1, report.Expired, "should expire 1 TTL memory")
	assert.Equal(t, 1, report.Decayed, "should decay 1 session memory")
	assert.Equal(t, 1, report.Retired, "should retire 1 fact")
	assert.Equal(t, 1, report.ConflictsResolved, "should resolve 1 conflict")
}
