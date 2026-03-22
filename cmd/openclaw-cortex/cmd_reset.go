package main

import (
	"fmt"

	"github.com/ajitpratap0/openclaw-cortex/internal/store"
	"github.com/spf13/cobra"
)

func resetCmd() *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Delete all memories and entities from the store",
		Long: `WARNING: Deletes every Memory, Entity, and Episode from the graph database.
This operation is irreversible. Intended for eval benchmark isolation.
Pass --yes to confirm; without it the command exits non-zero without deleting anything.

Note: deletion runs as a single Bolt transaction (MATCH (n) DETACH DELETE n). On
large stores this may exhaust Memgraph's transaction-memory budget and return an
error, leaving the store partially wiped. If that happens, restart Memgraph and
re-run reset. Batched deletion is tracked in issue #91.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				return fmt.Errorf("this will permanently delete ALL memories — pass --yes to confirm")
			}
			logger := newLogger()
			ctx := cmd.Context()
			st, err := newMemgraphStore(ctx, logger)
			if err != nil {
				return cmdErr("reset: connecting to store", err)
			}
			defer func() {
				if cerr := st.Close(); cerr != nil {
					logger.Warn("reset: close store", "error", cerr)
				}
			}()

			// Use the ResettableStore interface as documented: only cmd_reset.go
			// and eval benchmark harnesses should call DeleteAllMemories. The
			// compile-time assertion in internal/memgraph/store.go guarantees
			// *memgraph.MemgraphStore implements this interface.
			var rs store.ResettableStore = st
			if err := rs.DeleteAllMemories(ctx); err != nil {
				return cmdErr("reset: deleting memories", err)
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "All memories deleted.")
			return nil
		},
	}

	cmd.Flags().BoolVar(&yes, "yes", false, "Confirm deletion; without this flag the command exits non-zero without deleting anything")
	return cmd
}
