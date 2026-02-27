package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

func getCmd() *cobra.Command {
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "get [memory-id]",
		Short: "Retrieve a single memory by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := newLogger()
			ctx := cmd.Context()

			st, err := newStore(logger)
			if err != nil {
				return fmt.Errorf("get: connecting to store: %w", err)
			}
			defer func() { _ = st.Close() }()

			mem, err := st.Get(ctx, args[0])
			if err != nil {
				return fmt.Errorf("get: %w", err)
			}

			if outputJSON {
				out, err := json.MarshalIndent(mem, "", "  ")
				if err != nil {
					return fmt.Errorf("get: marshaling JSON: %w", err)
				}
				fmt.Println(string(out))
				return nil
			}

			fmt.Printf("ID:         %s\n", mem.ID)
			fmt.Printf("Type:       %s\n", mem.Type)
			fmt.Printf("Scope:      %s\n", mem.Scope)
			fmt.Printf("Confidence: %.2f\n", mem.Confidence)
			fmt.Printf("Project:    %s\n", mem.Project)
			fmt.Printf("Tags:       %v\n", mem.Tags)
			fmt.Printf("Created:    %s\n", mem.CreatedAt.Format("2006-01-02 15:04:05"))
			fmt.Printf("Accesses:   %d\n", mem.AccessCount)
			fmt.Printf("\nContent:\n%s\n", mem.Content)
			return nil
		},
	}

	cmd.Flags().BoolVar(&outputJSON, "json", false, "output as JSON")
	return cmd
}
