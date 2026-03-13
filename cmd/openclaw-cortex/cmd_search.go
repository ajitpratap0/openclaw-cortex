package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
)

func searchCmd() *cobra.Command {
	var (
		memType  string
		memScope string
		tagsFlag string
		limit    uint64
		project  string
		jsonFlag bool
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

			filters, filterErr := buildSearchFilters("search", memType, memScope, project, tagsFlag)
			if filterErr != nil {
				return filterErr
			}

			results, err := st.Search(ctx, vec, limit, filters)
			if err != nil {
				return fmt.Errorf("search: querying store: %w", err)
			}

			if jsonFlag {
				if results == nil {
					results = []models.SearchResult{}
				}
				out, marshalErr := json.MarshalIndent(results, "", "  ")
				if marshalErr != nil {
					return fmt.Errorf("search: marshaling results: %w", marshalErr)
				}
				fmt.Println(string(out))
				return nil
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
	cmd.Flags().StringVar(&memScope, "scope", "", "filter by scope (permanent|project|session|ttl)")
	cmd.Flags().StringVar(&tagsFlag, "tags", "", "filter by tags (comma-separated)")
	cmd.Flags().Uint64Var(&limit, "limit", 10, "max results")
	cmd.Flags().StringVar(&project, "project", "", "filter by project")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "output results as JSON")
	return cmd
}
