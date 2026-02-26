package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

func listCmd() *cobra.Command {
	var (
		memType string
		scope   string
		limit   uint64
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List stored memories",
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := newLogger()
			ctx := context.Background()

			st, err := newStore(logger)
			if err != nil {
				return fmt.Errorf("list: connecting to store: %w", err)
			}
			defer func() { _ = st.Close() }()

			var filters *store.SearchFilters
			if memType != "" || scope != "" {
				filters = &store.SearchFilters{}
				if memType != "" {
					mt := models.MemoryType(memType)
					filters.Type = &mt
				}
				if scope != "" {
					sc := models.MemoryScope(scope)
					filters.Scope = &sc
				}
			}

			memories, err := st.List(ctx, filters, limit, 0)
			if err != nil {
				return fmt.Errorf("list: fetching memories: %w", err)
			}

			for i, m := range memories {
				fmt.Printf("[%d] [%s/%s] %s\n", i+1, m.Type, m.Scope, truncate(m.Content, 100))
				fmt.Printf("    ID: %s | Source: %s | Confidence: %.2f\n", m.ID, m.Source, m.Confidence)
			}

			if len(memories) == 0 {
				fmt.Println("No memories found.")
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&memType, "type", "", "filter by type")
	cmd.Flags().StringVar(&scope, "scope", "", "filter by scope")
	cmd.Flags().Uint64Var(&limit, "limit", 50, "max results")
	return cmd
}
