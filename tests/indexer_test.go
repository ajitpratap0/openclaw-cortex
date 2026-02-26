package tests

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/indexer"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

// mockEmbedder returns deterministic vectors for testing.
type mockEmbedder struct {
	dimension int
	callCount int
}

func (m *mockEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	m.callCount++
	vec := make([]float32, m.dimension)
	// Generate a unique-ish vector based on text length
	for i := range vec {
		vec[i] = float32(len(text)%100) / 100.0 * float32(i%10) / 10.0
	}
	return vec, nil
}

func (m *mockEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	results := make([][]float32, len(texts))
	for i, t := range texts {
		vec, err := m.Embed(ctx, t)
		if err != nil {
			return nil, err
		}
		results[i] = vec
	}
	return results, nil
}

func (m *mockEmbedder) Dimension() int {
	return m.dimension
}

func TestIndexer_IndexFile(t *testing.T) {
	// Create a temporary markdown file
	dir := t.TempDir()
	mdPath := filepath.Join(dir, "test.md")

	content := `# Project Guidelines

Always use conventional commits for version control.
Code must be reviewed before merging.

## Testing

Write table-driven tests for all public functions.
Use testify for assertions.

## Deployment

Deploy to staging first, then production.
Run smoke tests after each deployment.
`

	err := os.WriteFile(mdPath, []byte(content), 0644)
	require.NoError(t, err)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	emb := &mockEmbedder{dimension: 768}
	st := store.NewMockStore()

	idx := indexer.NewIndexer(emb, st, 512, 64, logger)

	count, err := idx.IndexFile(context.Background(), mdPath)
	require.NoError(t, err)
	assert.Greater(t, count, 0)
	assert.Greater(t, emb.callCount, 0)

	// Verify memories were stored
	stats, err := st.Stats(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(count), stats.TotalMemories)
}

func TestIndexer_IndexDirectory(t *testing.T) {
	dir := t.TempDir()

	// Create multiple markdown files
	files := map[string]string{
		"rules.md": `# Rules
Always write tests.
Never skip code review.
`,
		"facts.md": `# Facts
Go was created at Google.
Qdrant supports gRPC and REST APIs.
`,
		"nested/deep.md": `# Nested
This is a nested file.
`,
	}

	for name, content := range files {
		path := filepath.Join(dir, name)
		err := os.MkdirAll(filepath.Dir(path), 0755)
		require.NoError(t, err)
		err = os.WriteFile(path, []byte(content), 0644)
		require.NoError(t, err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	emb := &mockEmbedder{dimension: 768}
	st := store.NewMockStore()

	idx := indexer.NewIndexer(emb, st, 512, 64, logger)

	count, err := idx.IndexDirectory(context.Background(), dir)
	require.NoError(t, err)
	assert.Greater(t, count, 0)
}

func TestIndexer_DeduplicatesChunks(t *testing.T) {
	dir := t.TempDir()
	mdPath := filepath.Join(dir, "test.md")

	content := `# Test
This is test content.
`
	err := os.WriteFile(mdPath, []byte(content), 0644)
	require.NoError(t, err)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// Use a fixed-vector embedder so dedup fires on second index
	emb := &fixedEmbedder{dimension: 768, value: 0.5}
	st := store.NewMockStore()

	idx := indexer.NewIndexer(emb, st, 512, 64, logger)

	// Index once
	count1, err := idx.IndexFile(context.Background(), mdPath)
	require.NoError(t, err)

	// Index same file again — should detect duplicates
	count2, err := idx.IndexFile(context.Background(), mdPath)
	require.NoError(t, err)
	assert.Equal(t, 0, count2, "second indexing should skip duplicates")

	_ = count1
}

type fixedEmbedder struct {
	dimension int
	value     float32
}

func (f *fixedEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	vec := make([]float32, f.dimension)
	for i := range vec {
		vec[i] = f.value
	}
	return vec, nil
}

func (f *fixedEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	results := make([][]float32, len(texts))
	for i, t := range texts {
		vec, err := f.Embed(ctx, t)
		if err != nil {
			return nil, err
		}
		_ = t
		results[i] = vec
	}
	return results, nil
}

func (f *fixedEmbedder) Dimension() int {
	return f.dimension
}
