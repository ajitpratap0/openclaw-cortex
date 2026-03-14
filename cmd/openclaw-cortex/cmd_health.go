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

			// Check Qdrant
			st, err := newStore(logger)
			if err != nil {
				fmt.Printf("Qdrant: FAIL (%v)\n", err)
				allOK = false
			} else {
				defer func() { _ = st.Close() }()
				if err := st.EnsureCollection(ctx); err != nil {
					fmt.Printf("Qdrant: FAIL (%v)\n", err)
					allOK = false
				} else {
					fmt.Println("Qdrant: OK")
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
			if cfg.Claude.GatewayURL != "" && cfg.Claude.GatewayToken != "" {
				fmt.Printf("Claude LLM: OK (via gateway %s)\n", cfg.Claude.GatewayURL)
			} else if cfg.Claude.APIKey != "" {
				fmt.Println("Claude LLM: OK (API key)")
			} else {
				fmt.Println("Claude LLM: FAIL (no API key or gateway configured)")
				allOK = false
			}

			// Check Neo4j (optional)
			if cfg.Graph.Enabled {
				gc, gcErr := newGraphClient(ctx, logger)
				if gcErr != nil {
					fmt.Printf("Neo4j:  FAIL (%v)\n", gcErr)
					allOK = false
				} else {
					if gc.Healthy(ctx) {
						fmt.Println("Neo4j:  OK")
					} else {
						fmt.Println("Neo4j:  FAIL (unhealthy)")
						allOK = false
					}
					_ = gc.Close()
				}
			} else {
				fmt.Println("Neo4j:  SKIP (graph.enabled=false)")
			}

			if !allOK {
				return fmt.Errorf("one or more health checks failed")
			}
			return nil
		},
	}
}
