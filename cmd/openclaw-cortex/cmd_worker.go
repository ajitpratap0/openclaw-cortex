package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/ajitpratap0/openclaw-cortex/internal/async"
	"github.com/ajitpratap0/openclaw-cortex/internal/llm"
	"github.com/ajitpratap0/openclaw-cortex/internal/memgraph"
)

func workerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "worker",
		Short: "Manage the async graph processing queue",
	}
	cmd.AddCommand(workerStatusCmd(), workerDrainCmd())
	return cmd
}

func workerStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show async queue depth and processing statistics",
		RunE: func(cmd *cobra.Command, _ []string) error {
			walPath, err := resolveWALPath(cfg.Async.WALPath)
			if err != nil {
				return cmdErr("worker status: resolve WAL path", err)
			}

			// Open the queue in read-only mode: no channel replay, no WAL
			// compaction, no mutations — safe for a status-only inspection.
			q, err := async.NewQueueReadOnly(walPath)
			if err != nil {
				return cmdErr("worker status: open queue", err)
			}

			st := q.Status()
			w := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(w, "Async queue status:\n")
			_, _ = fmt.Fprintf(w, "  pending:       %d\n", st.TotalPending)
			_, _ = fmt.Fprintf(w, "  failed:        %d\n", st.TotalFailed)
			_, _ = fmt.Fprintf(w, "  channel depth: %d\n", st.ChannelDepth)
			_, _ = fmt.Fprintf(w, "  async disabled: %v\n", cfg.Async.Disabled)
			return nil
		},
	}
}

func workerDrainCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "drain",
		Short: "Process all pending queue items synchronously",
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := newLogger()
			ctx := cmd.Context()

			walPath, err := resolveWALPath(cfg.Async.WALPath)
			if err != nil {
				return cmdErr("worker drain: resolve WAL path", err)
			}

			q, err := async.NewQueue(walPath, cfg.Async.QueueCapacity, cfg.Async.WALCompactEvery)
			if err != nil {
				return cmdErr("worker drain: open queue", err)
			}
			defer q.Close()

			// Count how many items were pending before we drain.
			beforeStatus := q.Status()
			pending := beforeStatus.TotalPending

			// Build the GraphProcessor (same dependencies as initAsyncQueue).
			emb := newEmbedder(logger)

			st, stErr := newMemgraphStore(ctx, logger)
			if stErr != nil {
				return cmdErr("worker drain: connecting to store", stErr)
			}
			defer func() { _ = st.Close() }()

			gc := memgraph.NewGraphAdapter(st)
			lc := llm.NewClient(cfg.Claude)
			if lc == nil {
				return cmdErr("worker drain", fmt.Errorf("no LLM credentials configured: set ANTHROPIC_API_KEY or configure claude.gateway_url + claude.gateway_token"))
			}

			retryDelay := time.Duration(cfg.Async.RetryDelaySeconds) * time.Second
			gp := async.NewGraphProcessor(st, gc, emb, lc, cfg.Claude.Model, logger)
			pool := async.NewPool(q, gp, 1, cfg.Async.MaxRetries, retryDelay, logger)
			pool.Start(ctx)

			// Poll until the WAL pending count reaches zero.  We also
			// respect context cancellation so that SIGINT/SIGTERM causes a
			// clean exit rather than an infinite loop.
			for {
				select {
				case <-ctx.Done():
					// Graceful exit on signal; still attempt a clean shutdown below.
					goto shutdown
				default:
				}
				s := q.Status()
				if s.TotalPending == 0 && s.ChannelDepth == 0 {
					break
				}
				time.Sleep(100 * time.Millisecond)
			}

		shutdown:
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if shutdownErr := pool.Shutdown(shutdownCtx); shutdownErr != nil {
				return cmdErr("worker drain: shutdown", shutdownErr)
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Processed %d items.\n", pending)
			return nil
		},
	}
}

// resolveWALPath returns the WAL file path from config or falls back to the
// default location under ~/.openclaw/.
func resolveWALPath(configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	walDir := filepath.Join(homeDir, ".openclaw")
	if mkErr := os.MkdirAll(walDir, 0o700); mkErr != nil {
		return "", fmt.Errorf("create WAL dir: %w", mkErr)
	}
	return filepath.Join(walDir, "graph_wal.jsonl"), nil
}
