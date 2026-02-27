package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ajitpratap0/openclaw-cortex/internal/recall"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
	"github.com/ajitpratap0/openclaw-cortex/pkg/tokenizer"
)

func recallCmd() *cobra.Command {
	var (
		budget  int
		ctxJSON string
		project string
	)

	cmd := &cobra.Command{
		Use:   "recall [current message]",
		Short: "Recall relevant memories with multi-factor ranking",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := newLogger()
			ctx := cmd.Context()
			query := args[0]

			emb := newEmbedder(logger)
			st, err := newStore(logger)
			if err != nil {
				return fmt.Errorf("recall: connecting to store: %w", err)
			}
			defer func() { _ = st.Close() }()

			vec, err := emb.Embed(ctx, query)
			if err != nil {
				return fmt.Errorf("recall: embedding query: %w", err)
			}

			var filters *store.SearchFilters
			if project != "" {
				filters = &store.SearchFilters{Project: &project}
			}

			// Fetch more results than needed for re-ranking
			searchLimit := uint64(50)
			results, err := st.Search(ctx, vec, searchLimit, filters)
			if err != nil {
				return fmt.Errorf("recall: searching store: %w", err)
			}

			// Re-rank with multi-factor scoring
			recaller := recall.NewRecaller(recall.DefaultWeights(), logger)
			ranked := recaller.Rank(results, project)

			// Apply token budget
			var contents []string
			for i := range ranked {
				contents = append(contents, ranked[i].Memory.Content)
			}

			output, count := tokenizer.FormatMemoriesWithBudget(contents, budget)

			if ctxJSON != "" {
				// Output as JSON
				jsonResults := ranked
				if count < len(ranked) {
					jsonResults = ranked[:count]
				}
				out, err := json.MarshalIndent(jsonResults, "", "  ")
				if err != nil {
					return fmt.Errorf("recall: marshaling JSON output: %w", err)
				}
				fmt.Println(string(out))
			} else {
				fmt.Printf("Recalled %d memories (budget: %d tokens):\n\n", count, budget)
				fmt.Println(output)
			}

			// Update access metadata for returned memories
			for i := 0; i < count && i < len(ranked); i++ {
				_ = st.UpdateAccessMetadata(ctx, ranked[i].Memory.ID)
			}

			return nil
		},
	}

	cmd.Flags().IntVar(&budget, "budget", 2000, "token budget")
	cmd.Flags().StringVar(&ctxJSON, "context", "", "output as JSON context")
	cmd.Flags().StringVar(&project, "project", "", "project context for scope boosting")
	return cmd
}
