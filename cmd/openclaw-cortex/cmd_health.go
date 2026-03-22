package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/ajitpratap0/openclaw-cortex/internal/llm"
)

type healthResult struct {
	OK       bool              `json:"ok"`
	Memgraph bool              `json:"memgraph"`
	Ollama   bool              `json:"ollama"`
	LLM      bool              `json:"llm"`
	Errors   map[string]string `json:"errors,omitempty"`
}

func healthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "health",
		Short: "Check connectivity to required services",
		RunE: func(cmd *cobra.Command, args []string) error {
			jsonOut, _ := cmd.Flags().GetBool("json")
			logger := newLogger()
			ctx := cmd.Context()

			result := healthResult{
				Memgraph: true,
				Ollama:   true,
				LLM:      true,
			}

			// Check Memgraph
			st, err := newMemgraphStore(ctx, logger)
			if err != nil {
				result.Memgraph = false
				if result.Errors == nil {
					result.Errors = make(map[string]string)
				}
				result.Errors["memgraph"] = err.Error()
			} else {
				defer func() { _ = st.Close() }()
				if err := st.EnsureCollection(ctx); err != nil {
					result.Memgraph = false
					if result.Errors == nil {
						result.Errors = make(map[string]string)
					}
					result.Errors["memgraph"] = fmt.Sprintf("schema: %v", err)
				}
			}

			// Check Ollama
			emb := newEmbedder(logger)
			if _, err := emb.Embed(ctx, "health check"); err != nil {
				result.Ollama = false
				if result.Errors == nil {
					result.Errors = make(map[string]string)
				}
				result.Errors["ollama"] = err.Error()
			}

			// Check Claude LLM access: actually test the credentials with a cheap ping.
			llmCtx, llmCancel := context.WithTimeout(ctx, 5*time.Second)
			defer llmCancel()
			switch {
			case cfg.Claude.GatewayURL != "" && cfg.Claude.GatewayToken != "":
				client := llm.NewGatewayClient(cfg.Claude.GatewayURL, cfg.Claude.GatewayToken, 5)
				if _, err := client.Complete(llmCtx, cfg.Claude.Model, "ping", "respond with ok", 5); err != nil {
					result.LLM = false
					if result.Errors == nil {
						result.Errors = make(map[string]string)
					}
					result.Errors["llm"] = fmt.Sprintf("gateway auth failed: %v", err)
				}
			case cfg.Claude.APIKey != "":
				client := llm.NewAnthropicClient(cfg.Claude.APIKey)
				if _, err := client.Complete(llmCtx, cfg.Claude.Model, "ping", "respond with ok", 5); err != nil {
					result.LLM = false
					if result.Errors == nil {
						result.Errors = make(map[string]string)
					}
					result.Errors["llm"] = fmt.Sprintf("api key auth failed: %v", err)
				}
			default:
				result.LLM = false
				if result.Errors == nil {
					result.Errors = make(map[string]string)
				}
				result.Errors["llm"] = "no API key or gateway configured"
			}

			result.OK = result.Memgraph && result.Ollama && result.LLM

			if jsonOut {
				data, err := json.Marshal(result)
				if err != nil {
					return fmt.Errorf("marshal health result: %w", err)
				}
				fmt.Println(string(data))
				if !result.OK {
					return fmt.Errorf("one or more health checks failed")
				}
				return nil
			}

			// Human-readable output
			if result.Memgraph {
				fmt.Println("Memgraph: OK")
			} else {
				fmt.Printf("Memgraph: FAIL (%s)\n", result.Errors["memgraph"])
			}

			if result.Ollama {
				fmt.Println("Ollama: OK")
			} else {
				fmt.Printf("Ollama: FAIL (%s)\n", result.Errors["ollama"])
			}

			if result.LLM {
				switch {
				case cfg.Claude.GatewayURL != "" && cfg.Claude.GatewayToken != "":
					fmt.Printf("Claude LLM: OK (via gateway %s)\n", cfg.Claude.GatewayURL)
				default:
					fmt.Println("Claude LLM: OK (API key)")
				}
			} else {
				fmt.Printf("Claude LLM: FAIL (%s)\n", result.Errors["llm"])
			}

			if !result.OK {
				return fmt.Errorf("one or more health checks failed")
			}
			return nil
		},
	}
	cmd.Flags().Bool("json", false, "Output health status as JSON")
	return cmd
}
