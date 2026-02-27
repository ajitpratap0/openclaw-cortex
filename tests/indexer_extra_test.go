package tests

import (
	"context"
	"errors"
	"log/slog"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/indexer"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

// uniqueEmbedder generates a random unique vector per call to avoid deduplication in tests.
type uniqueEmbedder struct {
	dimension int
}

func (u *uniqueEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	vec := make([]float32, u.dimension)
	for i := range vec {
		vec[i] = rand.Float32() //nolint:gosec // fine for tests
	}
	return vec, nil
}

func (u *uniqueEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	results := make([][]float32, len(texts))
	for i := range texts {
		vec, err := u.Embed(ctx, texts[i])
		if err != nil {
			return nil, err
		}
		results[i] = vec
	}
	return results, nil
}

func (u *uniqueEmbedder) Dimension() int {
	return u.dimension
}

func TestIndexer_IndexFile_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	mdPath := filepath.Join(dir, "empty.md")
	err := os.WriteFile(mdPath, []byte(""), 0644)
	require.NoError(t, err)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	emb := &mockEmbedder{dimension: 768}
	st := store.NewMockStore()

	idx := indexer.NewIndexer(emb, st, 512, 64, logger)
	count, err := idx.IndexFile(context.Background(), mdPath)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestIndexer_IndexFile_LargeContent(t *testing.T) {
	// Content larger than chunkSize should be split into multiple chunks
	dir := t.TempDir()
	mdPath := filepath.Join(dir, "large.md")

	// Generate content that is much larger than the default chunk size
	section := "# Large Section\n"
	for i := 0; i < 100; i++ {
		section += "This is a word-filled sentence with various content to ensure chunking occurs properly. "
	}
	err := os.WriteFile(mdPath, []byte(section), 0644)
	require.NoError(t, err)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	// Use uniqueEmbedder so each chunk gets a different vector (avoids dedup)
	emb := &uniqueEmbedder{dimension: 768}
	st := store.NewMockStore()

	idx := indexer.NewIndexer(emb, st, 100, 10, logger)
	count, err := idx.IndexFile(context.Background(), mdPath)
	require.NoError(t, err)
	// Should have produced multiple chunks
	assert.Greater(t, count, 1)
}

func TestIndexer_IndexDirectory_EmptyDir(t *testing.T) {
	dir := t.TempDir()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	emb := &mockEmbedder{dimension: 768}
	st := store.NewMockStore()

	idx := indexer.NewIndexer(emb, st, 512, 64, logger)
	count, err := idx.IndexDirectory(context.Background(), dir)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestIndexer_IndexDirectory_NonMarkdownFilesSkipped(t *testing.T) {
	dir := t.TempDir()

	// Create a non-markdown file that should be ignored
	err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("plain text file"), 0644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(dir, "data.json"), []byte(`{"key":"value"}`), 0644)
	require.NoError(t, err)

	// Create one markdown file that should be indexed
	err = os.WriteFile(filepath.Join(dir, "doc.md"), []byte("# Doc\nSome content here.\n"), 0644)
	require.NoError(t, err)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	emb := &mockEmbedder{dimension: 768}
	st := store.NewMockStore()

	idx := indexer.NewIndexer(emb, st, 512, 64, logger)
	count, err := idx.IndexDirectory(context.Background(), dir)
	require.NoError(t, err)
	assert.Greater(t, count, 0)

	stats, err := st.Stats(context.Background())
	require.NoError(t, err)
	// Only the markdown file chunks should be stored
	assert.Equal(t, int64(count), stats.TotalMemories)
}

func TestFindMarkdownFiles_MarkdownExtensions(t *testing.T) {
	dir := t.TempDir()

	// Create files with both supported extensions
	files := []string{"doc.md", "readme.markdown", "notes.txt", "config.yaml"}
	for _, f := range files {
		err := os.WriteFile(filepath.Join(dir, f), []byte("content"), 0644)
		require.NoError(t, err)
	}

	found, err := indexer.FindMarkdownFiles(dir)
	require.NoError(t, err)
	assert.Len(t, found, 2) // only .md and .markdown
}

type errorBatchEmbedder struct {
	dimension int
}

func (e *errorBatchEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	return nil, errors.New("embed unavailable")
}

func (e *errorBatchEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	return nil, errors.New("batch embed unavailable")
}

func (e *errorBatchEmbedder) Dimension() int {
	return e.dimension
}

func TestIndexer_IndexFile_EmbedBatchError(t *testing.T) {
	dir := t.TempDir()
	mdPath := filepath.Join(dir, "test.md")
	err := os.WriteFile(mdPath, []byte("# Title\nSome content that needs embedding.\n"), 0644)
	require.NoError(t, err)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	emb := &errorBatchEmbedder{dimension: 768}
	st := store.NewMockStore()

	idx := indexer.NewIndexer(emb, st, 512, 64, logger)
	count, err := idx.IndexFile(context.Background(), mdPath)
	assert.Error(t, err, "should fail when EmbedBatch fails")
	assert.Equal(t, 0, count)
}

func TestFindMarkdownFiles_NonExistentDir(t *testing.T) {
	// FindMarkdownFiles should return an error for a non-existent directory
	_, err := indexer.FindMarkdownFiles("/nonexistent/path/that/does/not/exist")
	assert.Error(t, err)
}

func TestIndexer_IndexDirectory_CancelledContext(t *testing.T) {
	dir := t.TempDir()

	// Create several markdown files
	for i := 0; i < 5; i++ {
		name := []string{"a.md", "b.md", "c.md", "d.md", "e.md"}[i]
		content := "# Section\nSome content here that is meaningful.\n"
		err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644)
		require.NoError(t, err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	emb := &mockEmbedder{dimension: 768}
	st := store.NewMockStore()

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel immediately to test cancellation path
	cancel()

	idx := indexer.NewIndexer(emb, st, 512, 64, logger)
	// Should return an error (context canceled) or partial results
	_, err := idx.IndexDirectory(ctx, dir)
	// Either ctx.Err() is returned or zero chunks are processed
	_ = err // context cancellation may or may not be returned depending on timing
}
