package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func resetCmd() *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Delete all memories and entities from the store",
		Long: `WARNING: Deletes every Memory, Entity, and Episode from the graph database.
This operation is irreversible. Intended for eval benchmark isolation.
Pass --yes to skip the confirmation prompt.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "This will permanently delete ALL memories. Pass --yes to confirm.")
				return nil
			}
			logger := newLogger()
			ctx := cmd.Context()
			st, err := newMemgraphStore(ctx, logger)
			if err != nil {
				return cmdErr("reset: connecting to store", err)
			}
			defer func() { _ = st.Close() }()

			if err := st.DeleteAllMemories(ctx); err != nil {
				return cmdErr("reset: deleting memories", err)
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "All memories deleted.")
			return nil
		},
	}

	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation and proceed with deletion")
	return cmd
}
