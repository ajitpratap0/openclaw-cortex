package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
)

func storeCmd() *cobra.Command {
	var (
		memType    string
		scope      string
		tags       string
		project    string
		confidence float64
	)

	cmd := &cobra.Command{
		Use:   "store [memory text]",
		Short: "Store a new memory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := newLogger()
			ctx := cmd.Context()
			content := args[0]

			// Validate memory type.
			mt := models.MemoryType(memType)
			if !mt.IsValid() {
				return fmt.Errorf("store: invalid --type %q: must be one of %s",
					memType, validTypesString())
			}

			// Validate memory scope.
			ms := models.MemoryScope(scope)
			if !ms.IsValid() {
				return fmt.Errorf("store: invalid --scope %q: must be one of %s",
					scope, validScopesString())
			}

			emb := newEmbedder(logger)
			st, err := newStore(logger)
			if err != nil {
				return fmt.Errorf("store: connecting to store: %w", err)
			}
			defer func() { _ = st.Close() }()

			if err = st.EnsureCollection(ctx); err != nil {
				return fmt.Errorf("store: ensuring collection: %w", err)
			}

			vec, err := emb.Embed(ctx, content)
			if err != nil {
				return fmt.Errorf("store: embedding content: %w", err)
			}

			// Check for duplicates
			dupes, err := st.FindDuplicates(ctx, vec, cfg.Memory.DedupThreshold)
			if err == nil && len(dupes) > 0 {
				fmt.Printf("Similar memory already exists (%.2f%% match): %s\n", dupes[0].Score*100, truncate(dupes[0].Memory.Content, 100))
				fmt.Println("Use 'openclaw-cortex forget' to remove it first, or the memory was skipped.")
				return nil
			}

			now := time.Now().UTC()
			var tagList []string
			if tags != "" {
				tagList = strings.Split(tags, ",")
				for i := range tagList {
					tagList[i] = strings.TrimSpace(tagList[i])
				}
			}

			mem := models.Memory{
				ID:           uuid.New().String(),
				Type:         mt,
				Scope:        ms,
				Visibility:   models.VisibilityShared,
				Content:      content,
				Confidence:   confidence,
				Source:       "explicit",
				Tags:         tagList,
				Project:      project,
				CreatedAt:    now,
				UpdatedAt:    now,
				LastAccessed: now,
			}

			if err := st.Upsert(ctx, mem, vec); err != nil {
				return fmt.Errorf("store: upserting memory: %w", err)
			}

			fmt.Printf("Stored memory %s [%s/%s]\n", mem.ID, mem.Type, mem.Scope)
			return nil
		},
	}

	cmd.Flags().StringVar(&memType, "type", "fact", "memory type (rule|fact|episode|procedure|preference)")
	cmd.Flags().StringVar(&scope, "scope", "permanent", "memory scope")
	cmd.Flags().StringVar(&tags, "tags", "", "comma-separated tags")
	cmd.Flags().StringVar(&project, "project", "", "project name")
	cmd.Flags().Float64Var(&confidence, "confidence", 0.9, "confidence score")
	return cmd
}

func validTypesString() string {
	types := make([]string, len(models.ValidMemoryTypes))
	for i, t := range models.ValidMemoryTypes {
		types[i] = string(t)
	}
	return strings.Join(types, "|")
}

func validScopesString() string {
	scopes := make([]string, len(models.ValidMemoryScopes))
	for i, s := range models.ValidMemoryScopes {
		scopes[i] = string(s)
	}
	return strings.Join(scopes, "|")
}
