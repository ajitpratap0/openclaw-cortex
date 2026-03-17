package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ajitpratap0/openclaw-cortex/internal/metrics"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
)

func statsCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show memory collection statistics",
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := newLogger()
			ctx := cmd.Context()

			st, err := newMemgraphStore(ctx, logger)
			if err != nil {
				return fmt.Errorf("stats: connecting to store: %w", err)
			}
			defer func() { _ = st.Close() }()

			stats, err := st.Stats(ctx)
			if err != nil {
				return fmt.Errorf("stats: fetching statistics: %w", err)
			}

			// Entity count (count via SearchEntities with empty query)
			entities, entErr := st.SearchEntities(ctx, "", "", 100)
			entityCount := 0
			if entErr == nil {
				entityCount = len(entities)
			}

			if jsonOutput {
				output := struct {
					*models.CollectionStats
					EntityCount int `json:"entity_count"`
				}{
					CollectionStats: stats,
					EntityCount:     entityCount,
				}
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				if encErr := enc.Encode(output); encErr != nil {
					return fmt.Errorf("stats: encoding JSON: %w", encErr)
				}
				return nil
			}

			fmt.Printf("Total memories: %d\n\n", stats.TotalMemories)
			fmt.Printf("Entities:       %d\n", entityCount)

			fmt.Println("By type:")
			for t, c := range stats.ByType {
				fmt.Printf("  %-12s %d\n", t, c)
			}

			fmt.Println("\nBy scope:")
			for s, c := range stats.ByScope {
				fmt.Printf("  %-12s %d\n", s, c)
			}

			// Health metrics
			fmt.Println("\nHealth:")
			if stats.OldestMemory != nil {
				fmt.Printf("  %-24s %s\n", "oldest_memory", stats.OldestMemory.Format("2006-01-02T15:04:05Z"))
			}
			if stats.NewestMemory != nil {
				fmt.Printf("  %-24s %s\n", "newest_memory", stats.NewestMemory.Format("2006-01-02T15:04:05Z"))
			}
			fmt.Printf("  %-24s %d\n", "active_conflicts", stats.ActiveConflicts)
			fmt.Printf("  %-24s %d\n", "pending_ttl_expiry", stats.PendingTTLExpiry)
			fmt.Printf("  %-24s %d bytes\n", "storage_estimate", stats.StorageEstimate)

			if len(stats.ReinforcementTiers) > 0 {
				fmt.Println("\nReinforcement tiers:")
				for tier, count := range stats.ReinforcementTiers {
					fmt.Printf("  %-12s %d\n", tier, count)
				}
			}

			if len(stats.TopAccessed) > 0 {
				fmt.Println("\nTop accessed:")
				for i := range stats.TopAccessed {
					p := stats.TopAccessed[i]
					fmt.Printf("  %d. [%s] %s (access_count: %d)\n", i+1, p.ID[:8], p.Content, p.AccessCount)
				}
			}

			// Print expvar counters
			fmt.Println("\nRuntime metrics (since process start):")
			fmt.Printf("  %-30s %d\n", "recall_total", metrics.RecallTotal.Value())
			fmt.Printf("  %-30s %d\n", "capture_total", metrics.CaptureTotal.Value())
			fmt.Printf("  %-30s %d\n", "store_total:", metrics.StoreTotal.Value())
			fmt.Printf("  %-30s %d\n", "dedup_skipped_total", metrics.DedupSkipped.Value())
			fmt.Printf("  %-30s %d\n", "lifecycle_expired_total", metrics.LifecycleExpired.Value())
			fmt.Printf("  %-30s %d\n", "lifecycle_decayed_total", metrics.LifecycleDecayed.Value())

			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output stats as JSON")
	return cmd
}
