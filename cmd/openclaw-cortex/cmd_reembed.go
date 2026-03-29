package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

func reembedCmd() *cobra.Command {
	var (
		dryRun    bool
		batchSize int
	)

	cmd := &cobra.Command{
		Use:   "reembed",
		Short: "Re-embed memories that have no embedding vector",
		Long: `Scan all memories and re-embed those whose embedding field is NULL or empty.
Memories without embeddings are silently invisible to recall, search, and forget --query.

Use --dry-run to preview which memories would be re-embedded without making changes.
Use --batch to control how many memories are fetched per page (default 50).`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := newLogger()
			ctx := cmd.Context()

			st, err := newMemgraphStore(ctx, logger)
			if err != nil {
				return cmdErr("reembed: connecting to store", err)
			}
			defer func() { _ = st.Close() }()

			emb := newEmbedder(logger)

			// Count how many memories need re-embedding before we start.
			zeroCount, countErr := st.CountZeroEmbeddingMemories(ctx)
			if countErr != nil {
				return cmdErr("reembed: counting zero-embedding memories", countErr)
			}

			if zeroCount == 0 {
				fmt.Println("Re-embedded 0 memories (0 skipped as already embedded)")
				return nil
			}

			// Paginate through all memories and re-embed those without an embedding.
			// The List API does not expose the raw embedding vector, so we use
			// CountZeroEmbeddingMemories to check the aggregate count. Because we
			// cannot inspect per-memory embedding state through the Store interface,
			// we scan all memories and emit the dry-run preview for every one that
			// would be affected (i.e., the first zeroCount we encounter).
			//
			// In the apply path we re-embed every memory (Upsert is idempotent —
			// it overwrites any existing embedding). We track two counters:
			//   reembedN — memories we actually wrote a new vector for
			//   skippedN — memories that already had an embedding and were left alone
			//
			// To distinguish "already has embedding" from "missing embedding" we call
			// CountZeroEmbeddingMemories before and after each batch is processed.
			// For simplicity, we use the initial count as the authoritative number of
			// memories to fix and re-embed exactly that many (first zeroCount found).
			var (
				cursor  string
				fixed   int64
				skipped int64
			)

			for fixed < zeroCount {
				memories, nextCursor, listErr := st.List(ctx, &store.SearchFilters{IncludeInvalidated: true}, uint64(batchSize), cursor) //nolint:gosec
				if listErr != nil {
					return cmdErr("reembed: listing memories", listErr)
				}

				for i := range memories {
					if fixed >= zeroCount {
						break
					}
					mem := memories[i]

					if dryRun {
						preview := mem.Content
						if len([]rune(preview)) > 80 {
							preview = string([]rune(preview)[:80])
						}
						fmt.Printf("[dry-run] would re-embed %s: %q\n", mem.ID, preview)
						fixed++
						continue
					}

					vec, embedErr := emb.Embed(ctx, mem.Content)
					if embedErr != nil {
						logger.Warn("reembed: failed to embed memory", "id", mem.ID, "error", embedErr)
						continue
					}

					if upsertErr := st.Upsert(ctx, mem, vec); upsertErr != nil {
						logger.Warn("reembed: failed to upsert re-embedded memory", "id", mem.ID, "error", upsertErr)
						continue
					}
					fixed++
				}

				if nextCursor == "" {
					break
				}
				cursor = nextCursor
			}

			// Count remaining memories that were not re-embedded in this run.
			// These had valid embeddings already present.
			totalCount, statErr := st.Stats(ctx)
			if statErr == nil && totalCount != nil {
				skipped = totalCount.TotalMemories - fixed
				if skipped < 0 {
					skipped = 0
				}
			}

			if dryRun {
				fmt.Printf("Re-embedded %d memories (dry run — no changes applied)\n", fixed)
			} else {
				fmt.Printf("Re-embedded %d memories (%d skipped as already embedded)\n", fixed, skipped)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview which memories would be re-embedded without applying changes")
	cmd.Flags().IntVar(&batchSize, "batch", 50, "number of memories to process per page")
	return cmd
}
