package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/ajitpratap0/openclaw-cortex/internal/config"
	"github.com/ajitpratap0/openclaw-cortex/internal/embedder"
	"github.com/ajitpratap0/openclaw-cortex/internal/memgraph"
	"github.com/ajitpratap0/openclaw-cortex/internal/sentry"
)

var version = "0.11.0"

var cfg *config.Config

func main() {
	if err := run(); err != nil {
		sentry.Flush(2 * time.Second)
		os.Exit(1)
	}
	sentry.Flush(2 * time.Second)
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)

	rootCmd := &cobra.Command{
		Use:     "openclaw-cortex",
		Short:   "OpenClaw Cortex — hybrid layered memory system for AI agents",
		Long:    "Cortex combines file-based structured memory with vector-based semantic memory for compaction-proof, searchable, classified memory.",
		Version: version,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			var err error
			cfg, err = config.Load()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			sentry.Init(cfg.Sentry.DSN, cfg.Sentry.Environment, version)
			return nil
		},
	}

	rootCmd.AddCommand(
		indexCmd(),
		searchCmd(),
		storeCmd(),
		storeBatchCmd(),
		forgetCmd(),
		listCmd(),
		captureCmd(),
		recallCmd(),
		statsCmd(),
		consolidateCmd(),
		lifecycleCmd(),
		getCmd(),
		updateCmd(),
		exportCmd(),
		importCmd(),
		healthCmd(),
		entitiesCmd(),
		serveCmd(),
		hookCmd(),
		mcpCmd(),
		migrateCmd(),
		resetCmd(),
	)

	rootCmd.SetContext(ctx)

	err := rootCmd.Execute()
	stop()
	return err
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
	emb, err := embedder.New(cfg.Ollama, cfg.Embedder, int(cfg.Memory.VectorDimension), logger)
	if err != nil {
		logger.Error("failed to initialize embedder", "error", err)
		os.Exit(1)
	}
	return emb
}

func newMemgraphStore(ctx context.Context, logger *slog.Logger) (*memgraph.MemgraphStore, error) {
	return memgraph.New(ctx,
		cfg.Memgraph.URI, cfg.Memgraph.Username, cfg.Memgraph.Password, cfg.Memgraph.Database,
		int(cfg.Memory.VectorDimension),
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
