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

// lifecycleMockEmbedder is a simple in-memory embedder for testing consolidation.
// It returns a pre-registered vector for each text, or a zero vector if unrecognized.
type lifecycleMockEmbedder struct {
	vectors map[string][]float32
	dim     int
}

func newLifecycleMockEmbedder(dim int) *lifecycleMockEmbedder {
	return &lifecycleMockEmbedder{vectors: make(map[string][]float32), dim: dim}
}

func (e *lifecycleMockEmbedder) Register(text string, vec []float32) {
	e.vectors[text] = vec
}

func (e *lifecycleMockEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	if v, ok := e.vectors[text]; ok {
		return v, nil
	}
	// Return a zero vector so callers don't error out.
	return make([]float32, e.dim), nil
}

func (e *lifecycleMockEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v, _ := e.Embed(context.Background(), t)
		out[i] = v
	}
	return out, nil
}

func (e *lifecycleMockEmbedder) Dimension() int { return e.dim }

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
	_ = s.Upsert(ctx, expired, testVector(0.1))

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
	_ = s.Upsert(ctx, fresh, testVector(0.2))

	// Create a permanent memory (should not be touched)
	permanent := models.Memory{
		ID:         "perm-1",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Visibility: models.VisibilityShared,
		Content:    "Permanent memory",
		CreatedAt:  time.Now().UTC().Add(-720 * time.Hour),
	}
	_ = s.Upsert(ctx, permanent, testVector(0.3))

	lm := lifecycle.NewManager(s, nil, logger)
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
	_ = s.Upsert(ctx, old, testVector(0.1))

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
	_ = s.Upsert(ctx, recent, testVector(0.2))

	lm := lifecycle.NewManager(s, nil, logger)
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
	_ = s.Upsert(ctx, expired, testVector(0.1))

	lm := lifecycle.NewManager(s, nil, logger)
	report, err := lm.Run(ctx, true)
	require.NoError(t, err)

	assert.Equal(t, 1, report.Expired, "should report 1 expired even in dry run")

	// But the memory should still exist
	_, err = s.Get(ctx, "dry-run-ttl")
	assert.NoError(t, err, "memory should still exist after dry run")
}

// nearIdenticalVector returns two vectors with cosine similarity > 0.92.
// We do this by using the same base and adding a tiny orthogonal perturbation.
func nearIdenticalVector(base float32, dim int) ([]float32, []float32) {
	a := make([]float32, dim)
	b := make([]float32, dim)
	for i := range a {
		a[i] = base * float32(i+1) / float32(dim)
		b[i] = a[i] // identical — cosine similarity == 1.0
	}
	// Introduce a tiny difference in one component so they are not literally
	// the same object in memory, but similarity remains well above 0.92.
	b[0] += 0.0001
	return a, b
}

// distinctVector returns a vector orthogonal to testVector(base), giving similarity ~0.
func distinctVector(dim int) []float32 {
	v := make([]float32, dim)
	// Alternating +1/-1 is orthogonal to monotonically-increasing vectors.
	for i := range v {
		if i%2 == 0 {
			v[i] = 1.0
		} else {
			v[i] = -1.0
		}
	}
	return v
}

func TestLifecycle_Consolidate_NearDuplicates(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	s := store.NewMockStore()

	const dim = 768
	contentA := "Go uses static typing for safety"
	contentB := "Go uses static typing for safety and reliability"

	vecA, vecB := nearIdenticalVector(0.8, dim)

	emb := newLifecycleMockEmbedder(dim)
	emb.Register(contentA, vecA)
	emb.Register(contentB, vecB)

	now := time.Now().UTC()

	// memA has lower confidence — it should be deleted.
	memA := models.Memory{
		ID:         "perm-dup-a",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Visibility: models.VisibilityShared,
		Content:    contentA,
		Confidence: 0.7,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	// memB has higher confidence — it should be kept.
	memB := models.Memory{
		ID:         "perm-dup-b",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Visibility: models.VisibilityShared,
		Content:    contentB,
		Confidence: 0.95,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	require.NoError(t, s.Upsert(ctx, memA, vecA))
	require.NoError(t, s.Upsert(ctx, memB, vecB))

	lm := lifecycle.NewManager(s, emb, logger)
	report, err := lm.Run(ctx, false)
	require.NoError(t, err)

	assert.Equal(t, 1, report.Consolidated, "should consolidate 1 near-duplicate pair")

	// The lower-confidence one (perm-dup-a) should be deleted.
	_, errA := s.Get(ctx, "perm-dup-a")
	assert.Error(t, errA, "lower-confidence duplicate should be deleted")

	// The higher-confidence one (perm-dup-b) should still exist.
	_, errB := s.Get(ctx, "perm-dup-b")
	assert.NoError(t, errB, "higher-confidence memory should be kept")
}

func TestLifecycle_Consolidate_NonDuplicates(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	s := store.NewMockStore()

	const dim = 768
	contentX := "Go is statically typed"
	contentY := "Python is dynamically typed"

	vecX := testVector(0.6)
	vecY := distinctVector(dim)

	emb := newLifecycleMockEmbedder(dim)
	emb.Register(contentX, vecX)
	emb.Register(contentY, vecY)

	now := time.Now().UTC()

	memX := models.Memory{
		ID:         "perm-x",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Visibility: models.VisibilityShared,
		Content:    contentX,
		Confidence: 0.9,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	memY := models.Memory{
		ID:         "perm-y",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Visibility: models.VisibilityShared,
		Content:    contentY,
		Confidence: 0.85,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	require.NoError(t, s.Upsert(ctx, memX, vecX))
	require.NoError(t, s.Upsert(ctx, memY, vecY))

	lm := lifecycle.NewManager(s, emb, logger)
	report, err := lm.Run(ctx, false)
	require.NoError(t, err)

	assert.Equal(t, 0, report.Consolidated, "distinct memories should not be consolidated")

	// Both memories should still exist.
	_, errX := s.Get(ctx, "perm-x")
	assert.NoError(t, errX)
	_, errY := s.Get(ctx, "perm-y")
	assert.NoError(t, errY)
}

func TestLifecycle_Consolidate_SkippedWhenNoEmbedder(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	s := store.NewMockStore()

	now := time.Now().UTC()
	mem := models.Memory{
		ID:         "perm-no-emb",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Visibility: models.VisibilityShared,
		Content:    "Some permanent memory",
		Confidence: 0.9,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	require.NoError(t, s.Upsert(ctx, mem, testVector(0.5)))

	// Pass nil embedder — consolidation should be silently skipped.
	lm := lifecycle.NewManager(s, nil, logger)
	report, err := lm.Run(ctx, false)
	require.NoError(t, err)

	assert.Equal(t, 0, report.Consolidated, "consolidation should be skipped when embedder is nil")

	// Memory should still exist.
	_, getErr := s.Get(ctx, "perm-no-emb")
	assert.NoError(t, getErr)
}

func TestLifecycle_Consolidate_DryRun(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	s := store.NewMockStore()

	const dim = 768
	contentA := "Consolidation dry run A"
	contentB := "Consolidation dry run B"

	vecA, vecB := nearIdenticalVector(0.5, dim)

	emb := newLifecycleMockEmbedder(dim)
	emb.Register(contentA, vecA)
	emb.Register(contentB, vecB)

	now := time.Now().UTC()

	memA := models.Memory{
		ID:         "dry-perm-a",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Visibility: models.VisibilityShared,
		Content:    contentA,
		Confidence: 0.6,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	memB := models.Memory{
		ID:         "dry-perm-b",
		Type:       models.MemoryTypeFact,
		Scope:      models.ScopePermanent,
		Visibility: models.VisibilityShared,
		Content:    contentB,
		Confidence: 0.9,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	require.NoError(t, s.Upsert(ctx, memA, vecA))
	require.NoError(t, s.Upsert(ctx, memB, vecB))

	lm := lifecycle.NewManager(s, emb, logger)
	report, err := lm.Run(ctx, true) // dry run
	require.NoError(t, err)

	assert.Equal(t, 1, report.Consolidated, "should report consolidation even in dry run")

	// Both memories should still exist (dry run — no actual deletes).
	_, errA := s.Get(ctx, "dry-perm-a")
	assert.NoError(t, errA, "memory should still exist after consolidation dry run")
	_, errB := s.Get(ctx, "dry-perm-b")
	assert.NoError(t, errB, "memory should still exist after consolidation dry run")
}

func TestMockStore_SensitiveVisibility_ExcludedByDefault(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	// Store a sensitive memory and a shared memory.
	sensitive := newTestMemory("sens-1", models.MemoryTypeFact, "my secret password is 1234")
	sensitive.Visibility = models.VisibilitySensitive
	shared := newTestMemory("shared-1", models.MemoryTypeFact, "Go is great")
	shared.Visibility = models.VisibilityShared

	require.NoError(t, s.Upsert(ctx, sensitive, testVector(0.5)))
	require.NoError(t, s.Upsert(ctx, shared, testVector(0.5)))

	// List with no filters — sensitive must be excluded.
	results, _, err := s.List(ctx, nil, 100, "")
	require.NoError(t, err)
	for _, r := range results {
		assert.NotEqual(t, models.VisibilitySensitive, r.Visibility,
			"sensitive memories must not appear in unfiltered list")
	}
	assert.Equal(t, 1, len(results), "only the shared memory should be returned")

	// Searching with no filters — sensitive must be excluded.
	searchResults, err := s.Search(ctx, testVector(0.5), 100, nil)
	require.NoError(t, err)
	for _, r := range searchResults {
		assert.NotEqual(t, models.VisibilitySensitive, r.Memory.Visibility,
			"sensitive memories must not appear in unfiltered search")
	}
}

func TestMockStore_SensitiveVisibility_ReturnedWhenExplicitlyRequested(t *testing.T) {
	ctx := context.Background()
	s := store.NewMockStore()

	sensitive := newTestMemory("sens-explicit", models.MemoryTypeFact, "top secret content")
	sensitive.Visibility = models.VisibilitySensitive

	require.NoError(t, s.Upsert(ctx, sensitive, testVector(0.5)))

	// List with explicit sensitive filter — should include the sensitive memory.
	vis := models.VisibilitySensitive
	results, _, err := s.List(ctx, &store.SearchFilters{Visibility: &vis}, 100, "")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "sens-explicit", results[0].ID)
}
