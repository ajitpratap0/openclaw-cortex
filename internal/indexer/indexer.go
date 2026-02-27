package indexer

import (
	"bufio"
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
	Content  string
	Source   string
	Heading  string
	Tags     []string
	Metadata map[string]any
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

// chunkFile reads a markdown file and splits it into chunks by headers.
func (idx *Indexer) chunkFile(filePath string) ([]Chunk, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("opening file: %w", err)
	}
	defer func() { _ = f.Close() }()

	relPath := filePath
	var chunks []Chunk
	var currentHeading string
	var currentLines []string

	scanner := bufio.NewScanner(f)
	// Increase buffer size for large lines
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	flush := func() {
		content := strings.TrimSpace(strings.Join(currentLines, "\n"))
		if content == "" {
			return
		}

		// Split large sections into smaller chunks
		textChunks := splitBySize(content, idx.chunkSize, idx.chunkOverlap)
		for _, tc := range textChunks {
			tags := extractTags(currentHeading, relPath)
			chunks = append(chunks, Chunk{
				Content: tc,
				Source:  relPath,
				Heading: currentHeading,
				Tags:    tags,
				Metadata: map[string]any{
					"heading":   currentHeading,
					"file_path": relPath,
				},
			})
		}
	}

	for scanner.Scan() {
		line := scanner.Text()

		if isMarkdownHeader(line) {
			flush()
			currentHeading = strings.TrimSpace(strings.TrimLeft(line, "#"))
			currentLines = nil
		} else {
			currentLines = append(currentLines, line)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanning file: %w", err)
	}

	flush()
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

func isMarkdownHeader(line string) bool {
	return strings.HasPrefix(line, "# ") ||
		strings.HasPrefix(line, "## ") ||
		strings.HasPrefix(line, "### ") ||
		strings.HasPrefix(line, "#### ")
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
