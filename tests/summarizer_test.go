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

func TestSectionSummarizer_SummarizeTree_EmptyTree(t *testing.T) {
	// With an empty tree, SummarizeTree should return empty results without error
	s := indexer.NewSectionSummarizer("fake-key", "claude-haiku-4-5-20251001", slog.Default())
	ctx := context.Background()

	memories, err := s.SummarizeTree(ctx, nil, "test-source")
	require.NoError(t, err)
	assert.Empty(t, memories)
}

func TestSectionSummarizer_SummarizeNode_BelowThreshold(t *testing.T) {
	// A node with fewer than 20 words should return nil, nil (below threshold)
	s := indexer.NewSectionSummarizer("fake-key", "claude-haiku-4-5-20251001", slog.Default())
	ctx := context.Background()

	// Create a node with less than 20 words (summaryMinWords = 20)
	node := &indexer.SectionNode{
		Title:     "Short Section",
		Content:   "This is short content.", // < 20 words
		Path:      "Short Section",
		Depth:     1,
		WordCount: 4,
	}

	mem, err := s.SummarizeNode(ctx, node, "test-source.md")
	require.NoError(t, err)
	assert.Nil(t, mem, "node below word threshold should return nil memory")
}

func TestSectionSummarizer_SummarizeTree_ShortSections(t *testing.T) {
	// All nodes below word threshold should produce no summaries
	s := indexer.NewSectionSummarizer("fake-key", "claude-haiku-4-5-20251001", slog.Default())
	ctx := context.Background()

	// These sections all have fewer than 20 words
	nodes := []*indexer.SectionNode{
		{Title: "A", Content: "Short.", WordCount: 1, Path: "A", Depth: 1},
		{Title: "B", Content: "Also short.", WordCount: 2, Path: "B", Depth: 1},
	}

	memories, err := s.SummarizeTree(ctx, nodes, "test.md")
	require.NoError(t, err)
	// No API calls made since all nodes are below threshold
	assert.Empty(t, memories)
}

func TestSectionSummarizer_SummarizeTree_CanceledContext(t *testing.T) {
	// Canceled context should return context error
	s := indexer.NewSectionSummarizer("fake-key", "claude-haiku-4-5-20251001", slog.Default())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	// With short node (below threshold), no Claude call is made so ctx cancellation
	// is only checked at the top of the walk function
	node := &indexer.SectionNode{
		Title:     "Test",
		Content:   "Short content below threshold.",
		WordCount: 4,
		Path:      "Test",
		Depth:     1,
	}

	// Tree walk checks ctx.Done() before processing each node
	_, err := s.SummarizeTree(ctx, []*indexer.SectionNode{node}, "test.md")
	// With a canceled context, should return context error
	assert.Error(t, err)
}

func TestSectionSummarizer_SummarizeTree_APIError_Continues(t *testing.T) {
	// Node with >= 20 words triggers Claude API call which fails with fake key.
	// SummarizeTree should log warning and continue (graceful degradation).
	s := indexer.NewSectionSummarizer("fake-key", "claude-haiku-4-5-20251001", slog.Default())
	ctx := context.Background()

	// This section has enough words to exceed the threshold
	longContent := "This section has more than twenty words to exceed the minimum word count threshold for summarization processing."
	node := &indexer.SectionNode{
		Title:     "Long Section",
		Content:   longContent,
		Path:      "Long Section",
		Depth:     1,
		WordCount: 20, // exactly at threshold
		Children:  nil,
	}

	// With invalid API key, Claude call fails — SummarizeTree should return empty, not error
	memories, err := s.SummarizeTree(ctx, []*indexer.SectionNode{node}, "test.md")
	// Should not propagate the API error — graceful degradation
	require.NoError(t, err)
	assert.Empty(t, memories, "failed API call should produce no memories")
}

func TestSectionSummarizer_SummarizeDirectory_EmptyDir(t *testing.T) {
	// Empty directory should return 0 summaries without error
	s := indexer.NewSectionSummarizer("fake-key", "claude-haiku-4-5-20251001", slog.Default())
	ctx := context.Background()

	dir := t.TempDir()
	st := store.NewMockStore()
	emb := &mockEmbedder{dimension: 768}

	count, err := s.SummarizeDirectory(ctx, dir, emb, st)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestSectionSummarizer_SummarizeDirectory_CanceledContext(t *testing.T) {
	// Canceled context should propagate through SummarizeDirectory for long sections
	s := indexer.NewSectionSummarizer("fake-key", "claude-haiku-4-5-20251001", slog.Default())

	dir := t.TempDir()
	// File with very short sections (below threshold) — no Claude calls needed
	mdPath := filepath.Join(dir, "short.md")
	content := "# Section\nFew words.\n"
	err := os.WriteFile(mdPath, []byte(content), 0644)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before calling

	st := store.NewMockStore()
	emb := &mockEmbedder{dimension: 768}

	// With canceled context and short sections, should return ctx error
	_, err = s.SummarizeDirectory(ctx, dir, emb, st)
	// Context cancellation propagates through SummarizeTree
	assert.Error(t, err)
}

func TestSectionSummarizer_SummarizeDirectory_OnlyShortSections(t *testing.T) {
	// Files with only short sections (< 20 words) should produce no summaries
	s := indexer.NewSectionSummarizer("fake-key", "claude-haiku-4-5-20251001", slog.Default())
	ctx := context.Background()

	dir := t.TempDir()
	// Create markdown file with very short sections
	mdPath := filepath.Join(dir, "short.md")
	content := "# Section One\nFew words.\n# Section Two\nAlso few.\n"
	err := os.WriteFile(mdPath, []byte(content), 0644)
	require.NoError(t, err)

	st := store.NewMockStore()
	emb := &mockEmbedder{dimension: 768}

	count, err := s.SummarizeDirectory(ctx, dir, emb, st)
	require.NoError(t, err)
	// Short sections (< 20 words) should not call Claude, so 0 summaries
	assert.Equal(t, 0, count)
}
