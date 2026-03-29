package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/ajitpratap0/openclaw-cortex/internal/async"
)

func graphAdminCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "graph",
		Short: "Graph database operations",
	}
	cmd.AddCommand(graphRebuildCmd())
	return cmd
}

func graphRebuildCmd() *cobra.Command {
	var (
		dryRun  bool
		batchSz int
	)

	cmd := &cobra.Command{
		Use:   "rebuild",
		Short: "Re-enqueue all memories for async graph reprocessing",
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := newLogger()
			ctx := cmd.Context()

			walPath, err := resolveWALPath(cfg.Async.WALPath)
			if err != nil {
				return cmdErr("graph rebuild: resolve WAL path", err)
			}

			st, stErr := newMemgraphStore(ctx, logger)
			if stErr != nil {
				return cmdErr("graph rebuild: connecting to store", stErr)
			}
			defer func() { _ = st.Close() }()

			q, qErr := async.NewQueue(walPath, cfg.Async.QueueCapacity, cfg.Async.WALCompactEvery)
			if qErr != nil {
				return cmdErr("graph rebuild: open queue", qErr)
			}

			var (
				cursor    string
				enqueued  int
				batchSize = uint64(batchSz)
			)
			if batchSize == 0 {
				batchSize = 100
			}

			for {
				memories, nextCursor, listErr := st.List(ctx, nil, batchSize, cursor)
				if listErr != nil {
					return cmdErr("graph rebuild: list memories", listErr)
				}

				for i := range memories {
					m := &memories[i]
					if dryRun {
						enqueued++
						continue
					}
					item := async.WorkItem{
						MemoryID:   m.ID,
						Content:    m.Content,
						Project:    m.Project,
						EnqueuedAt: time.Now().UTC(),
					}
					if enqErr := q.Enqueue(item); enqErr != nil {
						return cmdErr("graph rebuild: enqueue", enqErr)
					}
					enqueued++
				}

				if nextCursor == "" {
					break
				}
				cursor = nextCursor
			}

			w := cmd.OutOrStdout()
			if dryRun {
				_, _ = fmt.Fprintf(w, "[dry-run] Would enqueue %d items.\n", enqueued)
			} else {
				_, _ = fmt.Fprintf(w, "Enqueued %d items.\n", enqueued)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview without enqueuing")
	cmd.Flags().IntVar(&batchSz, "batch-size", 100, "memories per List page")
	return cmd
}
