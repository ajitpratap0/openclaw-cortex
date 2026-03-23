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
	LLM      *bool             `json:"llm,omitempty"`
	Errors   map[string]string `json:"errors,omitempty"`
	Skipped  []string          `json:"skipped,omitempty"`
}

func healthCmd() *cobra.Command {
	var skipLLMPing bool
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
			if skipLLMPing {
				// result.LLM remains nil (not checked); excluded from the OK gate.
				result.Skipped = append(result.Skipped, "llm")
			} else {
				llmCtx, llmCancel := context.WithTimeout(ctx, 5*time.Second)
				defer llmCancel() // belt-and-suspenders: explicit call below; defer covers early-return paths
				model := cfg.Claude.Model
				if model == "" {
					model = "claude-haiku-4-5-20251001"
				}
				switch {
				case cfg.Claude.GatewayURL != "" && cfg.Claude.GatewayToken != "":
					client := llm.NewGatewayClient(cfg.Claude.GatewayURL, cfg.Claude.GatewayToken, 0) // no http-level timeout; rely on llmCtx
					if _, err := client.Complete(llmCtx, model, "ping", "respond with ok", 5); err != nil {
						result.LLM = boolPtr(false)
						if result.Errors == nil {
							result.Errors = make(map[string]string)
						}
						result.Errors["llm"] = fmt.Sprintf("gateway ping failed: %v", err)
					} else {
						result.LLM = boolPtr(true)
					}
				case cfg.Claude.APIKey != "":
					client := llm.NewAnthropicClient(cfg.Claude.APIKey)
					if _, err := client.Complete(llmCtx, model, "ping", "respond with ok", 5); err != nil {
						result.LLM = boolPtr(false)
						if result.Errors == nil {
							result.Errors = make(map[string]string)
						}
						result.Errors["llm"] = fmt.Sprintf("api key ping failed: %v", err)
					} else {
						result.LLM = boolPtr(true)
					}
				default:
					result.LLM = boolPtr(false)
					if result.Errors == nil {
						result.Errors = make(map[string]string)
					}
					result.Errors["llm"] = "no API key or gateway configured"
				}
				llmCancel()
			}

			llmOK := result.LLM == nil || *result.LLM
			result.OK = result.Memgraph && result.Ollama && llmOK

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

			switch {
			case skipLLMPing:
				fmt.Println("Claude LLM: SKIP (--skip-llm-ping)")
			case result.LLM != nil && *result.LLM:
				switch {
				case cfg.Claude.GatewayURL != "" && cfg.Claude.GatewayToken != "":
					fmt.Printf("Claude LLM: OK (via gateway %s)\n", cfg.Claude.GatewayURL)
				default:
					fmt.Println("Claude LLM: OK (API key)")
				}
			default:
				fmt.Printf("Claude LLM: FAIL (%s)\n", result.Errors["llm"])
			}

			if !result.OK {
				return fmt.Errorf("one or more health checks failed")
			}
			return nil
		},
	}
	cmd.Flags().Bool("json", false, "Output health status as JSON")
	cmd.Flags().BoolVar(&skipLLMPing, "skip-llm-ping", false, "skip LLM API ping (avoids billing; for use in monitoring scripts)")
	return cmd
}
