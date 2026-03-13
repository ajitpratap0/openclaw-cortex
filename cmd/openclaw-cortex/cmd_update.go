package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
)

func updateCmd() *cobra.Command {
	var (
		content    string
		memType    string
		tags       string
		outputJSON bool
	)

	cmd := &cobra.Command{
		Use:   "update <memory-id>",
		Short: "Update a memory with lineage preservation (creates new version, old stays for history)",
		Long: `Create a new memory that supersedes an existing one.

The old memory remains in the store for history. The new memory carries forward
access_count and reinforced_count from the original, and sets supersedes_id to
link back to it. Superseded memories are automatically demoted during recall.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := newLogger()
			ctx := cmd.Context()
			oldID := args[0]

			if !cmd.Flags().Changed("content") {
				return fmt.Errorf("update: --content is required")
			}

			emb := newEmbedder(logger)
			st, storeErr := newStore(logger)
			if storeErr != nil {
				return fmt.Errorf("update: connecting to store: %w", storeErr)
			}
			defer func() { _ = st.Close() }()

			// Fetch the old memory.
			old, getErr := st.Get(ctx, oldID)
			if getErr != nil {
				return fmt.Errorf("update: fetching memory %s: %w", oldID, getErr)
			}

			// Build new memory, carrying forward fields from old.
			now := time.Now().UTC()
			newMem := models.Memory{
				ID:              uuid.New().String(),
				Type:            old.Type,
				Scope:           old.Scope,
				Visibility:      old.Visibility,
				Content:         content,
				Confidence:      old.Confidence,
				Source:          old.Source,
				Tags:            old.Tags,
				Project:         old.Project,
				TTLSeconds:      old.TTLSeconds,
				CreatedAt:       now,
				UpdatedAt:       now,
				LastAccessed:    now,
				AccessCount:     old.AccessCount,
				ReinforcedCount: old.ReinforcedCount,
				SupersedesID:    oldID,
				ValidUntil:      old.ValidUntil,
				Metadata:        old.Metadata,
			}

			// Apply optional overrides.
			if cmd.Flags().Changed("type") {
				mt := models.MemoryType(memType)
				if !mt.IsValid() {
					return fmt.Errorf("update: invalid --type %q: must be one of %s",
						memType, validTypesString())
				}
				newMem.Type = mt
			}

			if cmd.Flags().Changed("tags") {
				if tags != "" {
					newMem.Tags = parseTags(tags)
				} else {
					newMem.Tags = nil
				}
			}

			// Embed new content.
			vec, embedErr := emb.Embed(ctx, content)
			if embedErr != nil {
				return fmt.Errorf("update: embedding new content: %w", embedErr)
			}

			// Ensure collection exists before upserting.
			if ensureErr := st.EnsureCollection(ctx); ensureErr != nil {
				return fmt.Errorf("update: ensuring collection: %w", ensureErr)
			}

			if upsertErr := st.Upsert(ctx, newMem, vec); upsertErr != nil {
				return fmt.Errorf("update: saving new memory: %w", upsertErr)
			}

			if outputJSON {
				out, marshalErr := json.MarshalIndent(newMem, "", "  ")
				if marshalErr != nil {
					return fmt.Errorf("update: marshaling JSON: %w", marshalErr)
				}
				fmt.Println(string(out))
			} else {
				fmt.Printf("Updated memory %s -> %s [%s/%s]\n", oldID, newMem.ID, newMem.Type, newMem.Scope)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&content, "content", "", "new content for the memory (required)")
	cmd.Flags().StringVar(&memType, "type", "", "memory type (rule|fact|episode|procedure|preference)")
	cmd.Flags().StringVar(&tags, "tags", "", "comma-separated tags (replaces existing tags)")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "output the new memory as JSON")
	return cmd
}
