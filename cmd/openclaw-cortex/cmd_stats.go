package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func statsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stats",
		Short: "Show memory collection statistics",
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := newLogger()
			ctx := cmd.Context()

			st, err := newStore(logger)
			if err != nil {
				return fmt.Errorf("stats: connecting to store: %w", err)
			}
			defer func() { _ = st.Close() }()

			stats, err := st.Stats(ctx)
			if err != nil {
				return fmt.Errorf("stats: fetching statistics: %w", err)
			}

			fmt.Printf("Total memories: %d\n\n", stats.TotalMemories)

			fmt.Println("By type:")
			for t, c := range stats.ByType {
				fmt.Printf("  %-12s %d\n", t, c)
			}

			fmt.Println("\nBy scope:")
			for s, c := range stats.ByScope {
				fmt.Printf("  %-12s %d\n", s, c)
			}

			return nil
		},
	}
}
