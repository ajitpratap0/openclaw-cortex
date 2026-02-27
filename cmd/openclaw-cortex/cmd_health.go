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

			// Check Claude API key
			if cfg.Claude.APIKey == "" {
				fmt.Println("Claude API: FAIL (no API key configured)")
				allOK = false
			} else {
				fmt.Println("Claude API: OK")
			}

			if !allOK {
				return fmt.Errorf("one or more health checks failed")
			}
			return nil
		},
	}
}
