package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

// batchStoreInput is the JSON schema for each element in the stdin array.
type batchStoreInput struct {
	Content    string   `json:"content"`
	Type       string   `json:"type"`
	Scope      string   `json:"scope"`
	Tags       []string `json:"tags"`
	Confidence float64  `json:"confidence"`
}

// batchStoreResult is the JSON schema for each element in the output array.
type batchStoreResult struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Error   string `json:"error,omitempty"`
	Content string `json:"content,omitempty"`
}

func storeBatchCmd() *cobra.Command {
	var (
		project        string
		skipDedup      bool
		dedupThreshold float64
	)

	cmd := &cobra.Command{
		Use:   "store-batch",
		Short: "Store multiple memories in a single batch (reads JSON array from stdin)",
		Long: `Reads a JSON array of memory objects from stdin and stores them in bulk.

Each object must have a "content" field. Optional fields: type, scope, tags, confidence.

Example input:
  [{"content": "Go uses goroutines", "type": "fact", "scope": "permanent", "tags": ["go"]}]

Output is a JSON array of results with id and status ("created", "duplicate", "updated", or "error").`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := newLogger()
			ctx := cmd.Context()

			// Read all stdin.
			data, readErr := io.ReadAll(os.Stdin)
			if readErr != nil {
				return cmdErr("store-batch: reading stdin", readErr)
			}

			var inputs []batchStoreInput
			if unmarshalErr := json.Unmarshal(data, &inputs); unmarshalErr != nil {
				return cmdErr("store-batch: parsing JSON input", unmarshalErr)
			}

			if len(inputs) == 0 {
				// Empty batch: output empty array.
				fmt.Println("[]")
				return nil
			}

			// Validate all entries before doing any work.
			for i := range inputs {
				inp := &inputs[i]
				if inp.Content == "" {
					return fmt.Errorf("store-batch: entry %d: content is required", i)
				}
				if err := store.ValidateContentLength(inp.Content); err != nil {
					return fmt.Errorf("store-batch: entry %d: %w", i, err)
				}
				if inp.Type == "" {
					inp.Type = "fact"
				}
				mt := models.MemoryType(inp.Type)
				if !mt.IsValid() {
					return fmt.Errorf("store-batch: entry %d: invalid type %q: must be one of %s",
						i, inp.Type, validTypesString())
				}
				if inp.Scope == "" {
					inp.Scope = "permanent"
				}
				ms := models.MemoryScope(inp.Scope)
				if !ms.IsValid() {
					return fmt.Errorf("store-batch: entry %d: invalid scope %q: must be one of %s",
						i, inp.Scope, validScopesString())
				}
				if inp.Confidence == 0 {
					inp.Confidence = 0.9
				}
			}

			emb := newEmbedder(logger)
			st, storeErr := newMemgraphStore(ctx, logger)
			if storeErr != nil {
				return cmdErr("store-batch: connecting to store", storeErr)
			}
			defer func() { _ = st.Close() }()

			if collErr := st.EnsureCollection(ctx); collErr != nil {
				return cmdErr("store-batch: ensuring collection", collErr)
			}

			// Collect all content strings for batch embedding.
			contents := make([]string, len(inputs))
			for i := range inputs {
				contents[i] = inputs[i].Content
			}

			vectors, embedErr := emb.EmbedBatch(ctx, contents)
			if embedErr != nil {
				return cmdErr("store-batch: embedding batch", embedErr)
			}

			if len(vectors) != len(inputs) {
				return fmt.Errorf("store-batch: embedding returned %d vectors for %d inputs",
					len(vectors), len(inputs))
			}

			// Process each memory: dedup check then upsert.
			results := make([]batchStoreResult, len(inputs))
			now := time.Now().UTC()

			// Resolve effective dedup threshold once (only needed when dedup is active).
			var effectiveThreshold float64
			if !skipDedup {
				effectiveThreshold = cfg.Memory.DedupThreshold
				if cmd.Flags().Changed("dedup-threshold") {
					if err := store.ValidateDedupThreshold(dedupThreshold); err != nil {
						return fmt.Errorf("store-batch: --dedup-threshold: %w", err)
					}
					effectiveThreshold = dedupThreshold
				} else if err := store.ValidateDedupThreshold(effectiveThreshold); err != nil {
					return fmt.Errorf("store-batch: config dedup_threshold: %w", err)
				}
			}

			for i := range inputs {
				inp := &inputs[i]
				vec := vectors[i]

				// Store-time dedup: check for near-identical memories.
				// Bypassed per-entry when --skip-dedup is set.
				if !skipDedup {
					dedupRes, dedupErr := store.CheckAndHandleDuplicate(ctx, st, vec, inp.Content, effectiveThreshold)
					if dedupErr != nil {
						// Dedup is an optimisation, not a correctness gate — fail open
						// so a transient Memgraph hiccup does not block all stores.
						logger.Warn("store-batch: dedup check failed, proceeding without dedup",
							"index", i, "error", dedupErr)
					} else {
						if dedupRes.IsDuplicate {
							results[i] = batchStoreResult{
								ID:      dedupRes.ExistingID,
								Status:  "duplicate",
								Content: truncate(inp.Content, 80),
							}
							continue
						}
						if dedupRes.IsUpdated {
							results[i] = batchStoreResult{
								ID:      dedupRes.ExistingID,
								Status:  "updated",
								Content: truncate(inp.Content, 80),
							}
							continue
						}
					}
				}

				var tagList []string
				if len(inp.Tags) > 0 {
					tagList = make([]string, len(inp.Tags))
					for j := range inp.Tags {
						tagList[j] = strings.TrimSpace(inp.Tags[j])
					}
				}

				mem := models.Memory{
					ID:           uuid.New().String(),
					Type:         models.MemoryType(inp.Type),
					Scope:        models.MemoryScope(inp.Scope),
					Visibility:   models.VisibilityShared,
					Content:      inp.Content,
					Confidence:   inp.Confidence,
					Source:       "explicit",
					Tags:         tagList,
					Project:      project,
					CreatedAt:    now,
					UpdatedAt:    now,
					LastAccessed: now,
				}

				if upsertErr := st.Upsert(ctx, mem, vec); upsertErr != nil {
					results[i] = batchStoreResult{
						ID:     "",
						Status: "error",
						Error:  upsertErr.Error(),
					}
					continue
				}

				results[i] = batchStoreResult{
					ID:      mem.ID,
					Status:  "created",
					Content: truncate(inp.Content, 80),
				}
			}

			out, marshalErr := json.Marshal(results)
			if marshalErr != nil {
				return cmdErr("store-batch: marshaling results", marshalErr)
			}
			fmt.Println(string(out))
			return nil
		},
	}

	cmd.Flags().StringVar(&project, "project", "", "project name for all memories in this batch")
	cmd.Flags().BoolVar(&skipDedup, "skip-dedup", false, "bypass store-time dedup check (always store as new memories)")
	cmd.Flags().Float64Var(&dedupThreshold, "dedup-threshold", 0, "override cosine similarity dedup threshold for this call (range (0.0, 1.0]; omit to use config default)")
	return cmd
}
