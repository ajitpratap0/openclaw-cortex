package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/ajitpratap0/openclaw-cortex/internal/config"
	"github.com/ajitpratap0/openclaw-cortex/internal/embedder"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

var cfg *config.Config

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)

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
		getCmd(),
		exportCmd(),
		healthCmd(),
		entitiesCmd(),
		serveCmd(),
		hookCmd(),
		mcpCmd(),
	)

	rootCmd.SetContext(ctx)

	err := rootCmd.Execute()
	stop()
	if err != nil {
		os.Exit(1)
	}
}

func newLogger() *slog.Logger {
	level := slog.LevelInfo
	if cfg != nil && cfg.Logging.Level == "debug" {
		level = slog.LevelDebug
	}
	opts := &slog.HandlerOptions{Level: level}
	if cfg != nil && cfg.Logging.Format == "json" {
		return slog.New(slog.NewJSONHandler(os.Stderr, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stderr, opts))
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
	runes := []rune(s)
	if len(runes) > maxLen {
		return string(runes[:maxLen]) + "..."
	}
	return s
}
