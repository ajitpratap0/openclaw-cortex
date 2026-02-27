package indexer

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/google/uuid"

	"github.com/ajitpratap0/openclaw-cortex/internal/embedder"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

const (
	// summaryMinWords is the minimum word count for a section to be worth summarizing.
	summaryMinWords = 20

	// summaryMaxTokens caps Claude's response length for section summaries.
	summaryMaxTokens = 256

	// summaryConfidence is the confidence assigned to LLM-generated summaries.
	summaryConfidence = 0.95
)

// SectionSummarizer generates concise summary memories for document sections
// using Claude Haiku. Summaries are stored alongside the raw chunk memories and
// surface during broad recall queries where a specific chunk might be missed.
type SectionSummarizer struct {
	client *anthropic.Client
	model  string
	logger *slog.Logger
}

// NewSectionSummarizer creates a SectionSummarizer backed by Claude.
func NewSectionSummarizer(apiKey, model string, logger *slog.Logger) *SectionSummarizer {
	c := anthropic.NewClient(option.WithAPIKey(apiKey))
	return &SectionSummarizer{
		client: &c,
		model:  model,
		logger: logger,
	}
}

// SummarizeNode generates a single-sentence summary Memory for the given
// SectionNode. Returns (nil, nil) for sections below the word count threshold.
func (s *SectionSummarizer) SummarizeNode(ctx context.Context, node *SectionNode, source string) (*models.Memory, error) {
	if node.WordCount < summaryMinWords {
		return nil, nil
	}

	prompt := fmt.Sprintf(
		"Summarize the following document section in one concise sentence (max 25 words). Output ONLY the sentence.\n\nSection: %s\nContent: %s",
		node.Title,
		node.Content,
	)

	resp, err := s.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(s.model),
		MaxTokens: summaryMaxTokens,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("summarizing section %q: %w", node.Title, err)
	}

	var summary string
	for i := range resp.Content {
		if resp.Content[i].Type == "text" {
			summary = strings.TrimSpace(resp.Content[i].Text)
			break
		}
	}
	if summary == "" {
		return nil, nil
	}

	now := time.Now().UTC()
	mem := &models.Memory{
		ID:           uuid.New().String(),
		Type:         models.MemoryTypeFact,
		Scope:        models.ScopePermanent,
		Visibility:   models.VisibilityShared,
		Content:      fmt.Sprintf("[Summary] %s: %s", node.Path, summary),
		Confidence:   summaryConfidence,
		Source:       fmt.Sprintf("file:%s", source),
		Tags:         []string{"summary", "section-summary"},
		CreatedAt:    now,
		UpdatedAt:    now,
		LastAccessed: now,
		Metadata: map[string]any{
			"section_path":  node.Path,
			"section_depth": node.Depth,
			"is_summary":    true,
			"source_file":   source,
		},
	}
	return mem, nil
}

// SummarizeTree recursively summarizes all nodes in the section tree with
// sufficient content and returns the resulting Memory objects.
func (s *SectionSummarizer) SummarizeTree(ctx context.Context, nodes []*SectionNode, source string) ([]*models.Memory, error) {
	var memories []*models.Memory
	var walk func(node *SectionNode) error
	walk = func(node *SectionNode) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		mem, err := s.SummarizeNode(ctx, node, source)
		if err != nil {
			// Log and continue — a failed summary for one section shouldn't abort the rest.
			s.logger.Warn("summarizer: skipping section", "path", node.Path, "error", err)
		} else if mem != nil {
			memories = append(memories, mem)
		}

		for _, child := range node.Children {
			if err := walk(child); err != nil {
				return err
			}
		}
		return nil
	}

	for _, root := range nodes {
		if err := walk(root); err != nil {
			return memories, err
		}
	}
	return memories, nil
}

// SummarizeDirectory walks a directory for markdown files, generates section
// summaries for each, embeds them, and upserts them into the store.
// Returns the total number of summary memories stored.
func (s *SectionSummarizer) SummarizeDirectory(ctx context.Context, dir string, emb embedder.Embedder, st store.Store) (int, error) {
	files, err := FindMarkdownFiles(dir)
	if err != nil {
		return 0, fmt.Errorf("finding markdown files in %s: %w", dir, err)
	}

	total := 0
	for _, filePath := range files {
		select {
		case <-ctx.Done():
			return total, ctx.Err()
		default:
		}

		n, err := s.summarizeFile(ctx, filePath, emb, st)
		if err != nil {
			s.logger.Error("summarizer: failed to summarize file", "file", filePath, "error", err)
			continue
		}
		total += n
		s.logger.Info("summarizer: summarized file", "file", filePath, "summaries", n)
	}
	return total, nil
}

// summarizeFile processes a single file: parse → summarize → embed → upsert.
func (s *SectionSummarizer) summarizeFile(ctx context.Context, filePath string, emb embedder.Embedder, st store.Store) (int, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return 0, fmt.Errorf("reading %s: %w", filePath, err)
	}

	tree := ParseMarkdownTree(string(data))
	memories, err := s.SummarizeTree(ctx, tree, filePath)
	if err != nil {
		return 0, fmt.Errorf("summarizing tree for %s: %w", filePath, err)
	}

	stored := 0
	for _, mem := range memories {
		vec, err := emb.Embed(ctx, mem.Content)
		if err != nil {
			s.logger.Warn("summarizer: embed failed, skipping", "content_prefix", mem.Content[:minInt(40, len(mem.Content))], "error", err)
			continue
		}
		if err := st.Upsert(ctx, *mem, vec); err != nil {
			s.logger.Warn("summarizer: upsert failed, skipping", "error", err)
			continue
		}
		stored++
	}
	return stored, nil
}

// minInt returns the smaller of a and b.
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
