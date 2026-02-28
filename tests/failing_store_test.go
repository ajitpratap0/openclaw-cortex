package tests

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/hooks"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

// failingUpsertStore wraps MockStore but fails on Upsert calls after a threshold.
type failingUpsertStore struct {
	inner *store.MockStore
	err   error
}

func (f *failingUpsertStore) EnsureCollection(ctx context.Context) error {
	return f.inner.EnsureCollection(ctx)
}

func (f *failingUpsertStore) Upsert(_ context.Context, _ models.Memory, _ []float32) error {
	return f.err
}

func (f *failingUpsertStore) Search(ctx context.Context, vector []float32, limit uint64, filters *store.SearchFilters) ([]models.SearchResult, error) {
	return f.inner.Search(ctx, vector, limit, filters)
}

func (f *failingUpsertStore) Get(ctx context.Context, id string) (*models.Memory, error) {
	return f.inner.Get(ctx, id)
}

func (f *failingUpsertStore) Delete(ctx context.Context, id string) error {
	return f.inner.Delete(ctx, id)
}

func (f *failingUpsertStore) List(ctx context.Context, filters *store.SearchFilters, limit uint64, cursor string) ([]models.Memory, string, error) {
	return f.inner.List(ctx, filters, limit, cursor)
}

func (f *failingUpsertStore) FindDuplicates(ctx context.Context, vector []float32, threshold float64) ([]models.SearchResult, error) {
	return f.inner.FindDuplicates(ctx, vector, threshold)
}

func (f *failingUpsertStore) UpdateAccessMetadata(ctx context.Context, id string) error {
	return f.inner.UpdateAccessMetadata(ctx, id)
}

func (f *failingUpsertStore) Stats(ctx context.Context) (*models.CollectionStats, error) {
	return f.inner.Stats(ctx)
}

func (f *failingUpsertStore) UpsertEntity(ctx context.Context, entity models.Entity) error {
	return f.inner.UpsertEntity(ctx, entity)
}

func (f *failingUpsertStore) GetEntity(ctx context.Context, id string) (*models.Entity, error) {
	return f.inner.GetEntity(ctx, id)
}

func (f *failingUpsertStore) SearchEntities(ctx context.Context, name string) ([]models.Entity, error) {
	return f.inner.SearchEntities(ctx, name)
}

func (f *failingUpsertStore) LinkMemoryToEntity(ctx context.Context, entityID, memoryID string) error {
	return f.inner.LinkMemoryToEntity(ctx, entityID, memoryID)
}

func (f *failingUpsertStore) Close() error {
	return f.inner.Close()
}

// TestPostTurnHook_UpsertError_SkipsMemory verifies that when store.Upsert fails,
// that memory is skipped but execution continues without error.
func TestPostTurnHook_UpsertError_SkipsMemory(t *testing.T) {
	ctx := context.Background()
	logger := slog.Default()

	ms := &failingUpsertStore{
		inner: store.NewMockStore(),
		err:   errors.New("storage unavailable"),
	}

	cap := &hookMockCapturer{
		memories: []models.CapturedMemory{
			{Content: "Memory that fails to store", Type: models.MemoryTypeFact, Confidence: 0.9},
		},
	}
	cls := &hookMockClassifier{memType: models.MemoryTypeFact}
	emb := &hookMockEmbedder{dim: 8}

	hook := hooks.NewPostTurnHook(cap, cls, emb, ms, logger, 0.95)
	err := hook.Execute(ctx, hookTestInput())
	// Should NOT error — upsert failures are logged and skipped
	require.NoError(t, err)
}
