package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ajitpratap0/openclaw-cortex/internal/lifecycle"
)

func consolidateCmd() *cobra.Command {
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "consolidate",
		Short: "Run lifecycle management (TTL expiry, decay, consolidation)",
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := newLogger()
			ctx := cmd.Context()

			st, err := newStore(logger)
			if err != nil {
				return fmt.Errorf("consolidate: connecting to store: %w", err)
			}
			defer func() { _ = st.Close() }()

			lm := lifecycle.NewManager(st, newEmbedder(logger), logger)
			report, err := lm.Run(ctx, dryRun)
			if err != nil {
				return fmt.Errorf("consolidate: running lifecycle: %w", err)
			}

			fmt.Printf("Lifecycle report:\n")
			fmt.Printf("  Expired (TTL):  %d\n", report.Expired)
			fmt.Printf("  Decayed:        %d\n", report.Decayed)
			fmt.Printf("  Consolidated:   %d\n", report.Consolidated)
			if dryRun {
				fmt.Println("  (dry run — no changes applied)")
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview changes without applying")
	return cmd
}

func forgetCmd() *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "forget [memory-id]",
		Short: "Delete a memory by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			if !yes {
				fmt.Printf("Delete memory %s? [y/N] ", id)
				var response string
				if _, err := fmt.Scanln(&response); err != nil || strings.ToLower(strings.TrimSpace(response)) != "y" {
					fmt.Println("Aborted.")
					return nil
				}
			}

			logger := newLogger()
			ctx := cmd.Context()

			st, err := newStore(logger)
			if err != nil {
				return fmt.Errorf("forget: connecting to store: %w", err)
			}
			defer func() { _ = st.Close() }()

			if err := st.Delete(ctx, id); err != nil {
				return fmt.Errorf("forget: deleting memory: %w", err)
			}

			fmt.Printf("Deleted memory %s\n", id)
			return nil
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmation prompt")
	return cmd
}
