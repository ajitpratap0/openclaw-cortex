package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/ajitpratap0/openclaw-cortex/internal/embedder"
	"github.com/ajitpratap0/openclaw-cortex/internal/memgraph"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
)

// batchRecord is the JSON schema for a single line in the JSONL input file.
type batchRecord struct {
	Content    string   `json:"content"`
	Type       string   `json:"type,omitempty"`
	Scope      string   `json:"scope,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	Confidence float64  `json:"confidence,omitempty"`
	Project    string   `json:"project,omitempty"`
	UserID     string   `json:"user_id,omitempty"`
}

func captureBatchCmd() *cobra.Command {
	var (
		inputPath   string
		userID      string
		project     string
		dryRun      bool
		stopOnError bool
	)

	cmd := &cobra.Command{
		Use:   "capture-batch",
		Short: "Bulk-import memories from a JSONL file without running the LLM extraction pipeline",
		Long: `Reads a JSONL file (one JSON object per line), parses each line as a memory,
embeds it, and stores it directly. Useful for bulk imports that bypass the
LLM extraction pipeline.

Each line must be a JSON object with at least a "content" string field.
Optional fields: type, scope, tags, confidence, project, user_id.

Use --input - to read from stdin.

Example:
  echo '{"content":"Go uses goroutines","type":"fact"}' | openclaw-cortex capture-batch --input -`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := newLogger()
			ctx := cmd.Context()

			// Open input source.
			var reader io.Reader
			if inputPath == "-" {
				reader = os.Stdin
			} else {
				f, openErr := os.Open(inputPath)
				if openErr != nil {
					return fmt.Errorf("capture-batch: opening input file: %w", openErr)
				}
				defer func() { _ = f.Close() }()
				reader = f
			}

			// Parse all JSONL lines, counting parse errors separately.
			var records []batchRecord
			parseErrors := 0
			scanner := bufio.NewScanner(reader)
			lineNum := 0
			for scanner.Scan() {
				lineNum++
				line := strings.TrimSpace(scanner.Text())
				if line == "" {
					continue
				}
				var rec batchRecord
				if unmarshalErr := json.Unmarshal([]byte(line), &rec); unmarshalErr != nil {
					logger.Error("capture-batch: invalid JSON",
						"line", lineNum, "error", unmarshalErr)
					parseErrors++
					if stopOnError {
						return fmt.Errorf("capture-batch: invalid JSON on line %d: %w", lineNum, unmarshalErr)
					}
					continue
				}
				records = append(records, rec)
			}
			if scanErr := scanner.Err(); scanErr != nil {
				return fmt.Errorf("capture-batch: reading input: %w", scanErr)
			}

			imported, storeErrors := captureBatchProcess(
				ctx, records, project, userID, dryRun, stopOnError,
				logger, newEmbedder(logger),
			)

			totalErrors := parseErrors + storeErrors
			fmt.Printf("Imported %d memories (%d errors)\n", imported, totalErrors)
			return nil
		},
	}

	cmd.Flags().StringVarP(&inputPath, "input", "i", "", "path to JSONL input file (use - for stdin)")
	cmd.Flags().StringVar(&userID, "user-id", "", "set user_id on all imported memories (overrides per-record value)")
	cmd.Flags().StringVar(&project, "project", "", "set project on all imported memories (overrides per-record value)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "parse and validate without writing to store")
	cmd.Flags().BoolVar(&stopOnError, "stop-on-error", false, "abort on first error instead of continuing")
	_ = cmd.MarkFlagRequired("input")
	return cmd
}

// captureBatchProcess embeds and stores each record. It is factored out so
// that tests can exercise the logic without wiring live external services.
// When dryRun is true the store is never contacted.
func captureBatchProcess(
	ctx context.Context,
	records []batchRecord,
	overrideProject, overrideUserID string,
	dryRun, stopOnError bool,
	logger *slog.Logger,
	emb embedder.Embedder,
) (imported, storeErrors int) {
	// In dry-run mode we skip store setup entirely.
	var st *memgraph.MemgraphStore
	if !dryRun {
		var storeErr error
		st, storeErr = newMemgraphStore(ctx, logger)
		if storeErr != nil {
			logger.Error("capture-batch: connecting to store", "error", storeErr)
			// Count every record as an error and return early.
			return 0, len(records)
		}
		defer func() { _ = st.Close() }()

		if collErr := st.EnsureCollection(ctx); collErr != nil {
			logger.Error("capture-batch: ensuring collection", "error", collErr)
			return 0, len(records)
		}
	}

	now := time.Now().UTC()

	for i := range records {
		rec := &records[i]

		if rec.Content == "" {
			logger.Error("capture-batch: record has empty content", "index", i)
			storeErrors++
			if stopOnError {
				return
			}
			continue
		}

		// Apply flag overrides.
		if overrideProject != "" {
			rec.Project = overrideProject
		}
		if overrideUserID != "" {
			rec.UserID = overrideUserID
		}

		// Default confidence.
		if rec.Confidence == 0 {
			rec.Confidence = 0.9
		}

		// Resolve memory type (default to fact).
		memType := models.MemoryType(rec.Type)
		if !memType.IsValid() {
			memType = models.MemoryTypeFact
		}

		// Resolve memory scope (default to permanent).
		memScope := models.MemoryScope(rec.Scope)
		if !memScope.IsValid() {
			memScope = models.ScopePermanent
		}

		// Build tag list.
		var tagList []string
		if len(rec.Tags) > 0 {
			tagList = make([]string, len(rec.Tags))
			for j := range rec.Tags {
				tagList[j] = strings.TrimSpace(rec.Tags[j])
			}
		}

		if dryRun {
			// Count as imported without touching the store.
			imported++
			continue
		}

		// Embed content.
		vec, embedErr := emb.Embed(ctx, rec.Content)
		if embedErr != nil {
			logger.Error("capture-batch: embedding failed",
				"index", i, "error", embedErr)
			storeErrors++
			if stopOnError {
				return
			}
			continue
		}

		mem := models.Memory{
			ID:           uuid.New().String(),
			Type:         memType,
			Scope:        memScope,
			Visibility:   models.VisibilityShared,
			Content:      rec.Content,
			Confidence:   rec.Confidence,
			Source:       "batch-import",
			Tags:         tagList,
			Project:      rec.Project,
			UserID:       rec.UserID,
			CreatedAt:    now,
			UpdatedAt:    now,
			LastAccessed: now,
		}

		if upsertErr := st.Upsert(ctx, mem, vec); upsertErr != nil {
			logger.Error("capture-batch: upsert failed",
				"index", i, "id", mem.ID, "error", upsertErr)
			storeErrors++
			if stopOnError {
				return
			}
			continue
		}
		imported++
	}
	return
}
