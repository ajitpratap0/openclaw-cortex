package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func entitiesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "entities",
		Short: "Manage named entities linked to memories",
	}

	cmd.AddCommand(
		entitiesListCmd(),
		entitiesGetCmd(),
		entitiesSearchCmd(),
	)

	return cmd
}

func entitiesListCmd() *cobra.Command {
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all entities by searching with an empty name",
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := newLogger()
			ctx := cmd.Context()

			st, err := newStore(logger)
			if err != nil {
				return fmt.Errorf("entities list: connecting to store: %w", err)
			}
			defer func() { _ = st.Close() }()

			entities, err := st.SearchEntities(ctx, "")
			if err != nil {
				return fmt.Errorf("entities list: %w", err)
			}

			if len(entities) == 0 {
				fmt.Println("No entities found.")
				return nil
			}

			if outputJSON {
				out, marshalErr := json.MarshalIndent(entities, "", "  ")
				if marshalErr != nil {
					return fmt.Errorf("entities list: marshaling JSON: %w", marshalErr)
				}
				fmt.Println(string(out))
				return nil
			}

			for i := range entities {
				e := &entities[i]
				aliases := strings.Join(e.Aliases, ", ")
				fmt.Printf("%-36s  %-10s  %-20s  %s\n", e.ID, e.Type, e.Name, aliases)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&outputJSON, "json", false, "output as JSON")
	return cmd
}

func entitiesGetCmd() *cobra.Command {
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "get <entity-id>",
		Short: "Retrieve a single entity by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := newLogger()
			ctx := cmd.Context()

			st, err := newStore(logger)
			if err != nil {
				return fmt.Errorf("entities get: connecting to store: %w", err)
			}
			defer func() { _ = st.Close() }()

			entity, err := st.GetEntity(ctx, args[0])
			if err != nil {
				return fmt.Errorf("entities get: %w", err)
			}
			if entity == nil {
				return fmt.Errorf("entities get: entity %s not found", args[0])
			}

			if outputJSON {
				out, marshalErr := json.MarshalIndent(entity, "", "  ")
				if marshalErr != nil {
					return fmt.Errorf("entities get: marshaling JSON: %w", marshalErr)
				}
				fmt.Println(string(out))
				return nil
			}

			fmt.Printf("ID:        %s\n", entity.ID)
			fmt.Printf("Name:      %s\n", entity.Name)
			fmt.Printf("Type:      %s\n", entity.Type)
			fmt.Printf("Aliases:   %s\n", strings.Join(entity.Aliases, ", "))
			fmt.Printf("Memories:  %d\n", len(entity.MemoryIDs))
			fmt.Printf("Created:   %s\n", entity.CreatedAt.Format("2006-01-02 15:04:05"))
			fmt.Printf("Updated:   %s\n", entity.UpdatedAt.Format("2006-01-02 15:04:05"))
			return nil
		},
	}

	cmd.Flags().BoolVar(&outputJSON, "json", false, "output as JSON")
	return cmd
}

func entitiesSearchCmd() *cobra.Command {
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "search <name>",
		Short: "Search for entities by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := newLogger()
			ctx := cmd.Context()

			st, err := newStore(logger)
			if err != nil {
				return fmt.Errorf("entities search: connecting to store: %w", err)
			}
			defer func() { _ = st.Close() }()

			entities, err := st.SearchEntities(ctx, args[0])
			if err != nil {
				return fmt.Errorf("entities search: %w", err)
			}

			if len(entities) == 0 {
				fmt.Printf("No entities found matching %q.\n", args[0])
				return nil
			}

			if outputJSON {
				out, marshalErr := json.MarshalIndent(entities, "", "  ")
				if marshalErr != nil {
					return fmt.Errorf("entities search: marshaling JSON: %w", marshalErr)
				}
				fmt.Println(string(out))
				return nil
			}

			for i := range entities {
				e := &entities[i]
				aliases := strings.Join(e.Aliases, ", ")
				fmt.Printf("%-36s  %-10s  %-20s  %s\n", e.ID, e.Type, e.Name, aliases)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&outputJSON, "json", false, "output as JSON")
	return cmd
}
