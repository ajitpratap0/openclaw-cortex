package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

func searchCmd() *cobra.Command {
	var (
		memType string
		limit   uint64
		project string
	)

	cmd := &cobra.Command{
		Use:   "search [query]",
		Short: "Search memories by semantic similarity",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := newLogger()
			ctx := cmd.Context()
			query := args[0]

			emb := newEmbedder(logger)
			st, err := newStore(logger)
			if err != nil {
				return fmt.Errorf("search: connecting to store: %w", err)
			}
			defer func() { _ = st.Close() }()

			vec, err := emb.Embed(ctx, query)
			if err != nil {
				return fmt.Errorf("search: embedding query: %w", err)
			}

			var filters *store.SearchFilters
			if memType != "" || project != "" {
				filters = &store.SearchFilters{}
				if memType != "" {
					mt := models.MemoryType(memType)
					filters.Type = &mt
				}
				if project != "" {
					filters.Project = &project
				}
			}

			results, err := st.Search(ctx, vec, limit, filters)
			if err != nil {
				return fmt.Errorf("search: querying store: %w", err)
			}

			for i := range results {
				r := &results[i]
				fmt.Printf("[%d] (%.4f) [%s] %s\n", i+1, r.Score, r.Memory.Type, truncate(r.Memory.Content, 120))
				fmt.Printf("    ID: %s | Source: %s\n", r.Memory.ID, r.Memory.Source)
			}

			if len(results) == 0 {
				fmt.Println("No results found.")
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&memType, "type", "", "filter by memory type (rule|fact|episode|procedure|preference)")
	cmd.Flags().Uint64Var(&limit, "limit", 10, "max results")
	cmd.Flags().StringVar(&project, "project", "", "filter by project")
	return cmd
}
