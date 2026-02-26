package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/ajitpratap0/openclaw-cortex/internal/capture"
	"github.com/ajitpratap0/openclaw-cortex/internal/classifier"
	"github.com/ajitpratap0/openclaw-cortex/internal/config"
	"github.com/ajitpratap0/openclaw-cortex/internal/embedder"
	"github.com/ajitpratap0/openclaw-cortex/internal/indexer"
	"github.com/ajitpratap0/openclaw-cortex/internal/lifecycle"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/recall"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
	"github.com/ajitpratap0/openclaw-cortex/pkg/tokenizer"
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

// --- Commands ---

func indexCmd() *cobra.Command {
	var path string

	cmd := &cobra.Command{
		Use:   "index",
		Short: "Index markdown memory files into vector store",
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := newLogger()
			ctx := context.Background()

			emb := newEmbedder(logger)
			st, err := newStore(logger)
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()

			if err := st.EnsureCollection(ctx); err != nil {
				return fmt.Errorf("ensuring collection: %w", err)
			}

			idx := indexer.NewIndexer(emb, st, cfg.Memory.ChunkSize, cfg.Memory.ChunkOverlap, logger)

			if path == "" {
				path = cfg.Memory.MemoryDir
			}

			count, err := idx.IndexDirectory(ctx, path)
			if err != nil {
				return fmt.Errorf("indexing: %w", err)
			}

			fmt.Printf("Indexed %d chunks from %s\n", count, path)
			return nil
		},
	}

	cmd.Flags().StringVar(&path, "path", "", "directory to index (default: configured memory_dir)")
	return cmd
}

func searchCmd() *cobra.Command {
	var (
		memType string
		limit   uint64
		project string
	)

	cmd := &cobra.Command{
		Use:   "search [query]",
		Short: "Search memories by semantic similarity",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := newLogger()
			ctx := context.Background()
			query := args[0]

			emb := newEmbedder(logger)
			st, err := newStore(logger)
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()

			vec, err := emb.Embed(ctx, query)
			if err != nil {
				return fmt.Errorf("embedding query: %w", err)
			}

			var filters *store.SearchFilters
			if memType != "" || project != "" {
				filters = &store.SearchFilters{}
				if memType != "" {
					mt := models.MemoryType(memType)
					filters.Type = &mt
				}
				if project != "" {
					filters.Project = &project
				}
			}

			results, err := st.Search(ctx, vec, limit, filters)
			if err != nil {
				return fmt.Errorf("searching: %w", err)
			}

			for i, r := range results {
				fmt.Printf("[%d] (%.4f) [%s] %s\n", i+1, r.Score, r.Memory.Type, truncate(r.Memory.Content, 120))
				fmt.Printf("    ID: %s | Source: %s\n", r.Memory.ID, r.Memory.Source)
			}

			if len(results) == 0 {
				fmt.Println("No results found.")
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&memType, "type", "", "filter by memory type (rule|fact|episode|procedure|preference)")
	cmd.Flags().Uint64Var(&limit, "limit", 10, "max results")
	cmd.Flags().StringVar(&project, "project", "", "filter by project")
	return cmd
}

func storeCmd() *cobra.Command {
	var (
		memType    string
		scope      string
		tags       string
		project    string
		confidence float64
	)

	cmd := &cobra.Command{
		Use:   "store [memory text]",
		Short: "Store a new memory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := newLogger()
			ctx := context.Background()
			content := args[0]

			emb := newEmbedder(logger)
			st, err := newStore(logger)
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()

			if err := st.EnsureCollection(ctx); err != nil {
				return fmt.Errorf("ensuring collection: %w", err)
			}

			vec, err := emb.Embed(ctx, content)
			if err != nil {
				return fmt.Errorf("embedding: %w", err)
			}

			// Check for duplicates
			dupes, err := st.FindDuplicates(ctx, vec, cfg.Memory.DedupThreshold)
			if err == nil && len(dupes) > 0 {
				fmt.Printf("Similar memory already exists (%.2f%% match): %s\n", dupes[0].Score*100, truncate(dupes[0].Memory.Content, 100))
				fmt.Println("Use 'cortex forget' to remove it first, or the memory was skipped.")
				return nil
			}

			now := time.Now().UTC()
			var tagList []string
			if tags != "" {
				tagList = strings.Split(tags, ",")
				for i := range tagList {
					tagList[i] = strings.TrimSpace(tagList[i])
				}
			}

			mem := models.Memory{
				ID:           uuid.New().String(),
				Type:         models.MemoryType(memType),
				Scope:        models.MemoryScope(scope),
				Visibility:   models.VisibilityShared,
				Content:      content,
				Confidence:   confidence,
				Source:       "explicit",
				Tags:         tagList,
				Project:      project,
				CreatedAt:    now,
				UpdatedAt:    now,
				LastAccessed: now,
			}

			if err := st.Upsert(ctx, mem, vec); err != nil {
				return fmt.Errorf("storing memory: %w", err)
			}

			fmt.Printf("Stored memory %s [%s/%s]\n", mem.ID, mem.Type, mem.Scope)
			return nil
		},
	}

	cmd.Flags().StringVar(&memType, "type", "fact", "memory type")
	cmd.Flags().StringVar(&scope, "scope", "permanent", "memory scope")
	cmd.Flags().StringVar(&tags, "tags", "", "comma-separated tags")
	cmd.Flags().StringVar(&project, "project", "", "project name")
	cmd.Flags().Float64Var(&confidence, "confidence", 0.9, "confidence score")
	return cmd
}

func forgetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "forget [memory-id]",
		Short: "Delete a memory by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := newLogger()
			ctx := context.Background()

			st, err := newStore(logger)
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()

			if err := st.Delete(ctx, args[0]); err != nil {
				return fmt.Errorf("deleting memory: %w", err)
			}

			fmt.Printf("Deleted memory %s\n", args[0])
			return nil
		},
	}
}

func listCmd() *cobra.Command {
	var (
		memType string
		scope   string
		limit   uint64
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List stored memories",
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := newLogger()
			ctx := context.Background()

			st, err := newStore(logger)
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()

			var filters *store.SearchFilters
			if memType != "" || scope != "" {
				filters = &store.SearchFilters{}
				if memType != "" {
					mt := models.MemoryType(memType)
					filters.Type = &mt
				}
				if scope != "" {
					sc := models.MemoryScope(scope)
					filters.Scope = &sc
				}
			}

			memories, err := st.List(ctx, filters, limit, 0)
			if err != nil {
				return fmt.Errorf("listing memories: %w", err)
			}

			for i, m := range memories {
				fmt.Printf("[%d] [%s/%s] %s\n", i+1, m.Type, m.Scope, truncate(m.Content, 100))
				fmt.Printf("    ID: %s | Source: %s | Confidence: %.2f\n", m.ID, m.Source, m.Confidence)
			}

			if len(memories) == 0 {
				fmt.Println("No memories found.")
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&memType, "type", "", "filter by type")
	cmd.Flags().StringVar(&scope, "scope", "", "filter by scope")
	cmd.Flags().Uint64Var(&limit, "limit", 50, "max results")
	return cmd
}

func captureCmd() *cobra.Command {
	var (
		userMsg      string
		assistantMsg string
		sessionID    string
	)

	cmd := &cobra.Command{
		Use:   "capture",
		Short: "Extract memories from a conversation turn",
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := newLogger()
			ctx := context.Background()

			emb := newEmbedder(logger)
			st, err := newStore(logger)
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()

			if err := st.EnsureCollection(ctx); err != nil {
				return fmt.Errorf("ensuring collection: %w", err)
			}

			cap := capture.NewCapturer(cfg.Claude.APIKey, cfg.Claude.Model, logger)
			cls := classifier.NewClassifier(logger)

			memories, err := cap.Extract(ctx, userMsg, assistantMsg)
			if err != nil {
				return fmt.Errorf("extracting memories: %w", err)
			}

			logger.Info("extracted memories", "count", len(memories))

			stored := 0
			for _, cm := range memories {
				// Classify if not already typed
				if cm.Type == "" {
					cm.Type = cls.Classify(cm.Content)
				}

				vec, err := emb.Embed(ctx, cm.Content)
				if err != nil {
					logger.Error("embedding captured memory", "error", err)
					continue
				}

				// Dedup check
				dupes, err := st.FindDuplicates(ctx, vec, cfg.Memory.DedupThreshold)
				if err == nil && len(dupes) > 0 {
					logger.Info("skipping duplicate", "content", truncate(cm.Content, 60))
					continue
				}

				now := time.Now().UTC()
				mem := models.Memory{
					ID:           uuid.New().String(),
					Type:         cm.Type,
					Scope:        models.ScopePermanent,
					Visibility:   models.VisibilityShared,
					Content:      cm.Content,
					Confidence:   cm.Confidence,
					Source:       "inferred",
					Tags:         cm.Tags,
					CreatedAt:    now,
					UpdatedAt:    now,
					LastAccessed: now,
					Metadata: map[string]any{
						"session_id": sessionID,
					},
				}

				if err := st.Upsert(ctx, mem, vec); err != nil {
					logger.Error("storing captured memory", "error", err)
					continue
				}
				stored++
				fmt.Printf("Captured [%s]: %s\n", mem.Type, truncate(cm.Content, 100))
			}

			fmt.Printf("Captured %d memories from conversation\n", stored)
			return nil
		},
	}

	cmd.Flags().StringVar(&userMsg, "user", "", "user message")
	cmd.Flags().StringVar(&assistantMsg, "assistant", "", "assistant response")
	cmd.Flags().StringVar(&sessionID, "session-id", "", "session identifier")
	_ = cmd.MarkFlagRequired("user")
	_ = cmd.MarkFlagRequired("assistant")
	return cmd
}

func recallCmd() *cobra.Command {
	var (
		budget  int
		ctxJSON string
		project string
	)

	cmd := &cobra.Command{
		Use:   "recall [current message]",
		Short: "Recall relevant memories with multi-factor ranking",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := newLogger()
			ctx := context.Background()
			query := args[0]

			emb := newEmbedder(logger)
			st, err := newStore(logger)
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()

			vec, err := emb.Embed(ctx, query)
			if err != nil {
				return fmt.Errorf("embedding query: %w", err)
			}

			var filters *store.SearchFilters
			if project != "" {
				filters = &store.SearchFilters{Project: &project}
			}

			// Fetch more results than needed for re-ranking
			searchLimit := uint64(50)
			results, err := st.Search(ctx, vec, searchLimit, filters)
			if err != nil {
				return fmt.Errorf("searching: %w", err)
			}

			// Re-rank with multi-factor scoring
			recaller := recall.NewRecaller(recall.DefaultWeights(), logger)
			ranked := recaller.Rank(results, project)

			// Apply token budget
			var contents []string
			for _, r := range ranked {
				contents = append(contents, r.Memory.Content)
			}

			output, count := tokenizer.FormatMemoriesWithBudget(contents, budget)

			if ctxJSON != "" {
				// Output as JSON
				jsonResults := ranked
				if count < len(ranked) {
					jsonResults = ranked[:count]
				}
				data, _ := json.MarshalIndent(jsonResults, "", "  ")
				fmt.Println(string(data))
			} else {
				fmt.Printf("Recalled %d memories (budget: %d tokens):\n\n", count, budget)
				fmt.Println(output)
			}

			// Update access metadata for returned memories
			for i := 0; i < count && i < len(ranked); i++ {
				_ = st.UpdateAccessMetadata(ctx, ranked[i].Memory.ID)
			}

			return nil
		},
	}

	cmd.Flags().IntVar(&budget, "budget", 2000, "token budget")
	cmd.Flags().StringVar(&ctxJSON, "context", "", "output as JSON context")
	cmd.Flags().StringVar(&project, "project", "", "project context for scope boosting")
	return cmd
}

func statsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stats",
		Short: "Show memory collection statistics",
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := newLogger()
			ctx := context.Background()

			st, err := newStore(logger)
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()

			stats, err := st.Stats(ctx)
			if err != nil {
				return fmt.Errorf("getting stats: %w", err)
			}

			fmt.Printf("Total memories: %d\n\n", stats.TotalMemories)

			fmt.Println("By type:")
			for t, c := range stats.ByType {
				fmt.Printf("  %-12s %d\n", t, c)
			}

			fmt.Println("\nBy scope:")
			for s, c := range stats.ByScope {
				fmt.Printf("  %-12s %d\n", s, c)
			}

			return nil
		},
	}
}

func consolidateCmd() *cobra.Command {
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "consolidate",
		Short: "Run lifecycle management (TTL expiry, decay, consolidation)",
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := newLogger()
			ctx := context.Background()

			st, err := newStore(logger)
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()

			lm := lifecycle.NewManager(st, logger)
			report, err := lm.Run(ctx, dryRun)
			if err != nil {
				return fmt.Errorf("running lifecycle: %w", err)
			}

			fmt.Printf("Lifecycle report:\n")
			fmt.Printf("  Expired (TTL):  %d\n", report.Expired)
			fmt.Printf("  Decayed:        %d\n", report.Decayed)
			fmt.Printf("  Consolidated:   %d\n", report.Consolidated)
			if dryRun {
				fmt.Println("  (dry run — no changes applied)")
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview changes without applying")
	return cmd
}

func truncate(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}
