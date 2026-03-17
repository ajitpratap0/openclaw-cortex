package tests

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/ajitpratap0/openclaw-cortex/internal/indexer"
)

// longContent has more than 20 words to exceed summaryMinWords.
const longContent = "This section describes the architecture of the system in detail. " +
	"It covers all major components including the store, embedder, and recall engine."

func TestSectionSummarizerSummarizeNode(t *testing.T) {
	ctx := context.Background()

	t.Run("short content returns nil", func(t *testing.T) {
		client := &mockLLMClient{Resp: "A summary sentence."}
		s := indexer.NewSectionSummarizer(client, "test-model", slog.Default())
		node := &indexer.SectionNode{
			Title:     "Short Section",
			Path:      "Short Section",
			Content:   "Too short.",
			WordCount: 2,
		}
		mem, err := s.SummarizeNode(ctx, node, "source.md")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mem != nil {
			t.Errorf("expected nil memory for short content, got %+v", mem)
		}
	})

	t.Run("long content returns summary memory", func(t *testing.T) {
		client := &mockLLMClient{Resp: "A concise summary of the section."}
		s := indexer.NewSectionSummarizer(client, "test-model", slog.Default())
		node := &indexer.SectionNode{
			Title:     "Architecture",
			Path:      "Architecture",
			Content:   longContent,
			WordCount: len(strings.Fields(longContent)),
		}
		mem, err := s.SummarizeNode(ctx, node, "doc.md")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mem == nil {
			t.Fatal("expected a memory, got nil")
		}
		if mem.Content == "" {
			t.Error("expected non-empty memory content")
		}
	})

	t.Run("LLM error propagates without panic", func(t *testing.T) {
		client := &mockLLMClient{Err: errors.New("LLM unavailable")}
		s := indexer.NewSectionSummarizer(client, "test-model", slog.Default())
		node := &indexer.SectionNode{
			Title:     "Architecture",
			Path:      "Architecture",
			Content:   longContent,
			WordCount: len(strings.Fields(longContent)),
		}
		mem, err := s.SummarizeNode(ctx, node, "doc.md")
		if err == nil {
			t.Error("expected error from LLM failure, got nil")
		}
		if mem != nil {
			t.Errorf("expected nil memory on LLM error, got %+v", mem)
		}
	})

	t.Run("empty LLM response returns nil memory without panic", func(t *testing.T) {
		client := &mockLLMClient{Resp: ""}
		s := indexer.NewSectionSummarizer(client, "test-model", slog.Default())
		node := &indexer.SectionNode{
			Title:     "Architecture",
			Path:      "Architecture",
			Content:   longContent,
			WordCount: len(strings.Fields(longContent)),
		}
		mem, err := s.SummarizeNode(ctx, node, "doc.md")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mem != nil {
			t.Errorf("expected nil memory for empty LLM response, got %+v", mem)
		}
	})
}
