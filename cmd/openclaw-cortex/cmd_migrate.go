package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func migrateCmd() *cobra.Command {
	var (
		addTemporalIndexes bool
		dryRun             bool
	)

	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Run schema migrations",
		Long:  "Apply schema migrations to Memgraph. Use flags to select which migrations to run.",
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := newLogger()
			ctx := cmd.Context()

			st, err := newMemgraphStore(ctx, logger)
			if err != nil {
				return cmdErr("migrate: connecting to store", err)
			}
			defer func() { _ = st.Close() }()

			if addTemporalIndexes {
				if dryRun {
					fmt.Println("[dry-run] Would set valid_from = created_at for all memories where valid_from IS NULL")
					fmt.Println("[dry-run] Would create indexes on :Memory(valid_from) and :Memory(valid_to)")
					return nil
				}

				fmt.Println("Running temporal versioning migration...")
				if migrateErr := st.MigrateTemporalFields(ctx); migrateErr != nil {
					return cmdErr("migrate: temporal fields", migrateErr)
				}
				fmt.Println("✓ Temporal migration complete: valid_from backfilled, indexes created.")
			} else {
				fmt.Println("No migration flags specified. Use --add-temporal-indexes to run temporal migration.")
				fmt.Println("Available flags:")
				fmt.Println("  --add-temporal-indexes  Backfill valid_from=created_at for existing memories")
				fmt.Println("  --dry-run               Preview changes without applying")
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&addTemporalIndexes, "add-temporal-indexes", false, "backfill valid_from=created_at for existing memories and ensure temporal indexes")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview changes without applying")
	return cmd
}
