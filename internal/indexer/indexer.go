package indexer

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ajitpratap0/openclaw-cortex/internal/embedder"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

const (
	// dedupThreshold is the cosine similarity threshold above which a chunk is considered duplicate.
	dedupThreshold = 0.95

	// defaultIndexedConfidence is the confidence score assigned to file-indexed memories.
	defaultIndexedConfidence = 0.8
)

// Indexer scans markdown files, chunks them, generates embeddings, and stores them.
type Indexer struct {
	embedder     embedder.Embedder
	store        store.Store
	chunkSize    int
	chunkOverlap int
	logger       *slog.Logger
}

// Chunk represents a section of text extracted from a file.
type Chunk struct {
	Content      string
	Source       string
	Heading      string
	SectionPath  string // full " / "-delimited path from document root
	SectionDepth int    // 1=H1, 2=H2, 3=H3, 4=H4; 0 if no heading
	Tags         []string
	Metadata     map[string]any
}

// NewIndexer creates a new file indexer.
func NewIndexer(emb embedder.Embedder, st store.Store, chunkSize, chunkOverlap int, logger *slog.Logger) *Indexer {
	return &Indexer{
		embedder:     emb,
		store:        st,
		chunkSize:    chunkSize,
		chunkOverlap: chunkOverlap,
		logger:       logger,
	}
}

// IndexDirectory scans a directory for markdown files and indexes them.
func (idx *Indexer) IndexDirectory(ctx context.Context, dir string) (int, error) {
	files, err := FindMarkdownFiles(dir)
	if err != nil {
		return 0, fmt.Errorf("finding markdown files in %s: %w", dir, err)
	}

	idx.logger.Info("found markdown files", "count", len(files), "dir", dir)

	totalChunks := 0
	for _, file := range files {
		select {
		case <-ctx.Done():
			return totalChunks, ctx.Err()
		default:
		}

		n, err := idx.IndexFile(ctx, file)
		if err != nil {
			idx.logger.Error("indexing file", "file", file, "error", err)
			continue
		}
		totalChunks += n
	}

	return totalChunks, nil
}

// IndexFile reads a single markdown file, chunks it, and indexes each chunk.
func (idx *Indexer) IndexFile(ctx context.Context, filePath string) (int, error) {
	chunks, err := idx.chunkFile(filePath)
	if err != nil {
		return 0, fmt.Errorf("chunking file %s: %w", filePath, err)
	}
	if len(chunks) == 0 {
		return 0, nil
	}

	idx.logger.Info("chunked file", "file", filePath, "chunks", len(chunks))

	// Batch-embed all chunks in one call.
	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Content
	}
	vecs, err := idx.embedder.EmbedBatch(ctx, texts)
	if err != nil {
		return 0, fmt.Errorf("batch embedding chunks from %s: %w", filePath, err)
	}

	indexed := 0
	for i, chunk := range chunks {
		select {
		case <-ctx.Done():
			return indexed, ctx.Err()
		default:
		}

		vec := vecs[i]

		// Check for duplicates before inserting.
		dupes, err := idx.store.FindDuplicates(ctx, vec, dedupThreshold)
		if err != nil {
			idx.logger.Warn("dedup check failed, proceeding with store", "error", err)
		} else if len(dupes) > 0 {
			idx.logger.Debug("skipping duplicate chunk", "source", chunk.Source, "similar_to", dupes[0].Memory.ID)
			continue
		}

		now := time.Now().UTC()
		mem := models.Memory{
			ID:           uuid.New().String(),
			Type:         models.MemoryTypeFact,
			Scope:        models.ScopePermanent,
			Visibility:   models.VisibilityShared,
			Content:      chunk.Content,
			Confidence:   defaultIndexedConfidence,
			Source:       fmt.Sprintf("file:%s", chunk.Source),
			Tags:         chunk.Tags,
			CreatedAt:    now,
			UpdatedAt:    now,
			LastAccessed: now,
			AccessCount:  0,
			Metadata:     chunk.Metadata,
		}

		if err := idx.store.Upsert(ctx, mem, vec); err != nil {
			idx.logger.Error("storing chunk", "source", chunk.Source, "error", err)
			continue
		}
		indexed++
	}

	return indexed, nil
}

// chunkFile reads a markdown file, parses it into a section tree, and produces
// Chunks with structural metadata (section_path, section_depth, word_count).
func (idx *Indexer) chunkFile(filePath string) ([]Chunk, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}

	tree := ParseMarkdownTree(string(data))

	var chunks []Chunk
	var walkNode func(node *SectionNode)
	walkNode = func(node *SectionNode) {
		if node.Content != "" {
			textChunks := splitBySize(node.Content, idx.chunkSize, idx.chunkOverlap)
			tags := extractTags(node.Title, filePath)
			for _, tc := range textChunks {
				chunks = append(chunks, Chunk{
					Content:      tc,
					Source:       filePath,
					Heading:      node.Title,
					SectionPath:  node.Path,
					SectionDepth: node.Depth,
					Tags:         tags,
					Metadata: map[string]any{
						"heading":       node.Title,
						"section_path":  node.Path,
						"section_depth": node.Depth,
						"word_count":    node.WordCount,
						"file_path":     filePath,
					},
				})
			}
		}
		for _, child := range node.Children {
			walkNode(child)
		}
	}

	for _, root := range tree {
		walkNode(root)
	}

	return chunks, nil
}

func FindMarkdownFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && (strings.HasSuffix(path, ".md") || strings.HasSuffix(path, ".markdown")) {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

// splitBySize splits text into chunks of approximately maxSize characters with overlap.
func splitBySize(text string, maxSize, overlap int) []string {
	if len(text) <= maxSize {
		return []string{text}
	}

	var chunks []string
	words := strings.Fields(text)
	var current []string
	currentLen := 0

	for _, word := range words {
		wordLen := len(word) + 1 // +1 for space
		if currentLen+wordLen > maxSize && len(current) > 0 {
			chunks = append(chunks, strings.Join(current, " "))

			// Keep overlap words
			overlapWords := 0
			overlapLen := 0
			for i := len(current) - 1; i >= 0 && overlapLen < overlap; i-- {
				overlapLen += len(current[i]) + 1
				overlapWords++
			}
			current = current[len(current)-overlapWords:]
			currentLen = overlapLen
		}
		current = append(current, word)
		currentLen += wordLen
	}

	if len(current) > 0 {
		chunks = append(chunks, strings.Join(current, " "))
	}

	return chunks
}

func extractTags(heading, filePath string) []string {
	var tags []string

	// Extract filename as tag
	base := filepath.Base(filePath)
	name := strings.TrimSuffix(base, filepath.Ext(base))
	if name != "" {
		tags = append(tags, name)
	}

	// Extract heading words as tags
	if heading != "" {
		words := strings.Fields(strings.ToLower(heading))
		for _, w := range words {
			w = strings.Trim(w, ".,;:!?()[]{}\"'`")
			if len(w) > 2 {
				tags = append(tags, w)
			}
		}
	}

	return tags
}
