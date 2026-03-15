package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func healthCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "health",
		Short: "Check connectivity to required services",
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := newLogger()
			ctx := cmd.Context()
			allOK := true

			// Check Memgraph
			st, err := newMemgraphStore(ctx, logger)
			if err != nil {
				fmt.Printf("Memgraph: FAIL (%v)\n", err)
				allOK = false
			} else {
				defer func() { _ = st.Close() }()
				if err := st.EnsureCollection(ctx); err != nil {
					fmt.Printf("Memgraph: FAIL (schema: %v)\n", err)
					allOK = false
				} else {
					fmt.Println("Memgraph: OK")
				}
			}

			// Check Ollama
			emb := newEmbedder(logger)
			if _, err := emb.Embed(ctx, "health check"); err != nil {
				fmt.Printf("Ollama: FAIL (%v)\n", err)
				allOK = false
			} else {
				fmt.Println("Ollama: OK")
			}

			// Check Claude LLM access (API key or gateway)
			switch {
			case cfg.Claude.GatewayURL != "" && cfg.Claude.GatewayToken != "":
				fmt.Printf("Claude LLM: OK (via gateway %s)\n", cfg.Claude.GatewayURL)
			case cfg.Claude.APIKey != "":
				fmt.Println("Claude LLM: OK (API key)")
			default:
				fmt.Println("Claude LLM: FAIL (no API key or gateway configured)")
				allOK = false
			}

			if !allOK {
				return fmt.Errorf("one or more health checks failed")
			}
			return nil
		},
	}
}
