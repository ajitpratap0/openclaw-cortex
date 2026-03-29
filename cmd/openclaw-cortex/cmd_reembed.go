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

			// Paginate unconditionally through all memories and re-embed only those
			// whose embedding is missing (zero-length). We cannot rely on the initial
			// zeroCount as a loop bound because the List API returns all memories
			// regardless of embedding state — stopping after `fixed >= zeroCount`
			// iterations would skip memories that happen to appear later in the page
			// order and may miss actual zero-embedding nodes while re-embedding ones
			// that already had valid vectors.
			//
			// We track two counters:
			//   fixed   — memories whose embedding was missing and was written
			//   skipped — memories that already had an embedding (left untouched)
			var (
				cursor  string
				fixed   int64
				skipped int64
			)

			for {
				memories, nextCursor, listErr := st.List(ctx, &store.SearchFilters{IncludeInvalidated: true}, uint64(batchSize), cursor) //nolint:gosec
				if listErr != nil {
					return cmdErr("reembed: listing memories", listErr)
				}

				for i := range memories {
					mem := memories[i]
					if mem.HasEmbedding {
						continue // already has embedding, skip
					}

					if dryRun {
						preview := mem.Content
						if len([]rune(preview)) > 80 {
							preview = string([]rune(preview)[:80])
						}
						fmt.Printf("[dry-run] would re-embed missing-vector memory %s: %q\n", mem.ID, preview)
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

			// skipped = total memories that already had a valid embedding.
			// Use the pre-run zeroCount so the calculation is stable regardless of
			// concurrent writes: skipped = total - zeroCount (memories that needed fixing).
			totalCount, statErr := st.Stats(ctx)
			if statErr == nil && totalCount != nil {
				skipped = totalCount.TotalMemories - zeroCount
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
