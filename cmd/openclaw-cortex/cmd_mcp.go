package main

import (
	"log"
	"os"

	"github.com/spf13/cobra"

	mcpserver "github.com/mark3labs/mcp-go/server"

	cortexmcp "github.com/ajitpratap0/openclaw-cortex/internal/mcp"
)

func mcpCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Start the MCP (Model Context Protocol) server over stdio",
		Long: `Starts an MCP JSON-RPC 2.0 server that reads from stdin and writes to stdout.
All diagnostic logs go to stderr so that stdout remains exclusively MCP protocol traffic.

Tools exposed:
  remember  — store a memory (embed + upsert)
  recall    — retrieve memories with multi-factor ranking
  forget    — delete a memory by ID
  search    — raw semantic search with scores
  stats     — collection statistics

If Qdrant or Ollama are unavailable at startup the server still starts;
individual tool calls will return MCP error responses on failure.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := newLogger()

			emb := newEmbedder(logger)

			st, storeErr := newStore(logger)
			if storeErr != nil {
				// Log to stderr and continue with a nil store.
				// Tool calls will return per-call errors rather than crashing.
				logger.Error("mcp: failed to connect to store; tool calls requiring storage will fail",
					"error", storeErr)
			}

			srv := cortexmcp.NewServer(st, emb, logger)

			// Use a standard log.Logger pointing at stderr for the mcp-go error logger.
			errLogger := log.New(os.Stderr, "mcp: ", log.LstdFlags)

			logger.Info("mcp: openclaw-cortex MCP server starting", "transport", "stdio")

			return mcpserver.ServeStdio(
				srv.MCPServer(),
				mcpserver.WithErrorLogger(errLogger),
			)
		},
	}

	return cmd
}
