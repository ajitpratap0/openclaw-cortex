package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ajitpratap0/openclaw-cortex/internal/indexer"
)

func indexCmd() *cobra.Command {
	var path string

	cmd := &cobra.Command{
		Use:   "index",
		Short: "Index markdown memory files into vector store",
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := newLogger()
			ctx := cmd.Context()

			emb := newEmbedder(logger)
			st, err := newStore(logger)
			if err != nil {
				return fmt.Errorf("index: connecting to store: %w", err)
			}
			defer func() { _ = st.Close() }()

			if err := st.EnsureCollection(ctx); err != nil {
				return fmt.Errorf("index: ensuring collection: %w", err)
			}

			idx := indexer.NewIndexer(emb, st, cfg.Memory.ChunkSize, cfg.Memory.ChunkOverlap, logger)

			if path == "" {
				path = cfg.Memory.MemoryDir
			}

			count, err := idx.IndexDirectory(ctx, path)
			if err != nil {
				return fmt.Errorf("index: indexing directory: %w", err)
			}

			fmt.Printf("Indexed %d chunks from %s\n", count, path)
			return nil
		},
	}

	cmd.Flags().StringVar(&path, "path", "", "directory to index (default: configured memory_dir)")
	return cmd
}
