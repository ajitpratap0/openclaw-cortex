package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ajitpratap0/openclaw-cortex/internal/lifecycle"
)

func lifecycleCmd() *cobra.Command {
	var (
		dryRun   bool
		jsonFlag bool
	)

	cmd := &cobra.Command{
		Use:   "lifecycle",
		Short: "Run all lifecycle operations (TTL expiry, session decay, consolidation, fact retirement, conflict resolution)",
		Long: `Run memory lifecycle management. This executes all lifecycle phases in order:
  1. TTL expiry     — delete memories past their time-to-live
  2. Session decay  — remove session memories not accessed within 24h
  3. Consolidation  — merge near-duplicate permanent memories
  4. Fact retirement — delete memories whose ValidUntil has passed
  5. Conflict resolution — pick winners in active conflict groups

Use --dry-run to preview what would change without modifying data.
Use --json for machine-readable output.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := newLogger()
			ctx := cmd.Context()

			st, storeErr := newStore(logger)
			if storeErr != nil {
				return fmt.Errorf("lifecycle: connecting to store: %w", storeErr)
			}
			defer func() { _ = st.Close() }()

			lm := lifecycle.NewManager(st, newEmbedder(logger), logger)
			report, runErr := lm.Run(ctx, dryRun)
			if runErr != nil {
				// Report is still usable with partial results even when some phases fail.
				logger.Warn("lifecycle: some phases failed", "error", runErr)
			}

			if jsonFlag {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				if encErr := enc.Encode(report); encErr != nil {
					return fmt.Errorf("lifecycle: encoding JSON: %w", encErr)
				}
				return runErr
			}

			w := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(w, "Lifecycle report:\n")
			_, _ = fmt.Fprintf(w, "  Expired (TTL):       %d\n", report.Expired)
			_, _ = fmt.Fprintf(w, "  Decayed (session):   %d\n", report.Decayed)
			_, _ = fmt.Fprintf(w, "  Consolidated:        %d\n", report.Consolidated)
			_, _ = fmt.Fprintf(w, "  Retired (facts):     %d\n", report.Retired)
			_, _ = fmt.Fprintf(w, "  Conflicts resolved:  %d\n", report.ConflictsResolved)
			if dryRun {
				_, _ = fmt.Fprintln(w, "  (dry run — no changes applied)")
			}

			return runErr
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview changes without applying")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "output report as JSON")
	return cmd
}

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
