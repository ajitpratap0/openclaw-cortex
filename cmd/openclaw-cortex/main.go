package main

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ajitpratap0/openclaw-cortex/internal/config"
	"github.com/ajitpratap0/openclaw-cortex/internal/embedder"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

var cfg *config.Config

func main() {
	rootCmd := &cobra.Command{
		Use:   "openclaw-cortex",
		Short: "OpenClaw Cortex — hybrid layered memory system for AI agents",
		Long:  "Cortex combines file-based structured memory with vector-based semantic memory for compaction-proof, searchable, classified memory.",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			var err error
			cfg, err = config.Load()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			return nil
		},
	}

	rootCmd.AddCommand(
		indexCmd(),
		searchCmd(),
		storeCmd(),
		forgetCmd(),
		listCmd(),
		captureCmd(),
		recallCmd(),
		statsCmd(),
		consolidateCmd(),
	)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func newLogger() *slog.Logger {
	level := slog.LevelInfo
	if cfg != nil && cfg.Logging.Level == "debug" {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}

func newEmbedder(logger *slog.Logger) embedder.Embedder {
	return embedder.NewOllamaEmbedder(
		cfg.Ollama.BaseURL,
		cfg.Ollama.Model,
		int(cfg.Memory.VectorDimension),
		logger,
	)
}

func newStore(logger *slog.Logger) (store.Store, error) {
	return store.NewQdrantStore(
		cfg.Qdrant.Host,
		cfg.Qdrant.GRPCPort,
		cfg.Qdrant.Collection,
		cfg.Memory.VectorDimension,
		cfg.Qdrant.UseTLS,
		logger,
	)
}

func truncate(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}
