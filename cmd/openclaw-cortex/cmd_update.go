package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
)

func updateCmd() *cobra.Command {
	var (
		content    string
		memType    string
		scope      string
		tags       string
		project    string
		confidence float64
	)

	cmd := &cobra.Command{
		Use:   "update <memory-id>",
		Short: "Update fields of an existing memory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := newLogger()
			ctx := cmd.Context()
			id := args[0]

			emb := newEmbedder(logger)
			st, err := newStore(logger)
			if err != nil {
				return fmt.Errorf("update: connecting to store: %w", err)
			}
			defer func() { _ = st.Close() }()

			mem, err := st.Get(ctx, id)
			if err != nil {
				return fmt.Errorf("update: fetching memory: %w", err)
			}

			// Apply provided flag values.
			if cmd.Flags().Changed("type") {
				mt := models.MemoryType(memType)
				if !mt.IsValid() {
					return fmt.Errorf("update: invalid --type %q: must be one of %s",
						memType, validTypesString())
				}
				mem.Type = mt
			}

			if cmd.Flags().Changed("scope") {
				ms := models.MemoryScope(scope)
				if !ms.IsValid() {
					return fmt.Errorf("update: invalid --scope %q: must be one of %s",
						scope, validScopesString())
				}
				mem.Scope = ms
			}

			if cmd.Flags().Changed("project") {
				mem.Project = project
			}

			if cmd.Flags().Changed("confidence") {
				mem.Confidence = confidence
			}

			if cmd.Flags().Changed("tags") {
				var tagList []string
				if tags != "" {
					tagList = strings.Split(tags, ",")
					for i := range tagList {
						tagList[i] = strings.TrimSpace(tagList[i])
					}
				}
				mem.Tags = tagList
			}

			var vec []float32
			if cmd.Flags().Changed("content") {
				mem.Content = content
				vec, err = emb.Embed(ctx, content)
				if err != nil {
					return fmt.Errorf("update: embedding new content: %w", err)
				}
			} else {
				// Re-embed existing content to preserve the vector (no content change).
				vec, err = emb.Embed(ctx, mem.Content)
				if err != nil {
					return fmt.Errorf("update: re-embedding existing content: %w", err)
				}
			}

			mem.UpdatedAt = time.Now().UTC()
			if upsertErr := st.Upsert(ctx, *mem, vec); upsertErr != nil {
				return fmt.Errorf("update: saving memory: %w", upsertErr)
			}

			fmt.Printf("Updated memory %s [%s/%s]\n", mem.ID, mem.Type, mem.Scope)
			return nil
		},
	}

	cmd.Flags().StringVar(&content, "content", "", "new content for the memory")
	cmd.Flags().StringVar(&memType, "type", "", "memory type (rule|fact|episode|procedure|preference)")
	cmd.Flags().StringVar(&scope, "scope", "", "memory scope (permanent|project|session|ttl)")
	cmd.Flags().StringVar(&tags, "tags", "", "comma-separated tags (replaces existing tags)")
	cmd.Flags().StringVar(&project, "project", "", "project name")
	cmd.Flags().Float64Var(&confidence, "confidence", 0, "confidence score (0.0-1.0)")
	return cmd
}
