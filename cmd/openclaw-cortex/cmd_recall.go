package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/ajitpratap0/openclaw-cortex/internal/llm"
	"github.com/ajitpratap0/openclaw-cortex/internal/memgraph"
	"github.com/ajitpratap0/openclaw-cortex/internal/recall"
	"github.com/ajitpratap0/openclaw-cortex/pkg/tokenizer"
)

func recallCmd() *cobra.Command {
	var (
		budget           int
		ctxJSON          string
		project          string
		memType          string
		memScope         string
		tagsFlag         string
		reason           bool
		reasonCandidates int
	)

	cmd := &cobra.Command{
		Use:   "recall [current message]",
		Short: "Recall relevant memories with multi-factor ranking",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := newLogger()
			ctx := cmd.Context()
			query := args[0]

			emb := newEmbedder(logger)
			st, err := newMemgraphStore(ctx, logger)
			if err != nil {
				return fmt.Errorf("recall: connecting to store: %w", err)
			}
			defer func() { _ = st.Close() }()

			vec, err := emb.Embed(ctx, query)
			if err != nil {
				return fmt.Errorf("recall: embedding query: %w", err)
			}

			filters, filterErr := buildSearchFilters("recall", memType, memScope, project, tagsFlag)
			if filterErr != nil {
				return filterErr
			}

			// Fetch more results than needed for re-ranking
			searchLimit := uint64(50)
			results, err := st.Search(ctx, vec, searchLimit, filters)
			if err != nil {
				return fmt.Errorf("recall: searching store: %w", err)
			}

			// Re-rank with multi-factor scoring using config-loaded weights.
			recaller := recall.NewRecaller(recallWeightsFromConfig(cfg.Recall.Weights), logger)

			// Wire graph client for graph-augmented recall — MemgraphStore implements graph.Client.
			gc := memgraph.NewGraphAdapter(st)
			recaller.SetGraphClient(gc, st, cfg.Recall.GraphBudgetCLIMs)

			ranked := recaller.RecallWithGraph(ctx, query, vec, results, project)

			// Optionally re-rank with Claude for genuine relevance.
			// Threshold-gated: also triggers automatically when top-4 scores are clustered.
			rerankThreshold := cfg.Recall.RerankScoreSpreadThreshold
			budgetMs := cfg.Recall.RerankLatencyBudgetCLIMs
			forceRerank := reason

			if cfg.Claude.APIKey != "" && (forceRerank || recaller.ShouldRerank(ranked, rerankThreshold)) {
				llmClient := llm.NewClient(cfg.Claude)
				reasoner := recall.NewReasoner(llmClient, cfg.Claude.Model, logger)
				rerankCtx, cancel := context.WithTimeout(ctx, time.Duration(budgetMs)*time.Millisecond)
				defer cancel()
				reranked, rerankErr := reasoner.ReRank(rerankCtx, query, ranked, reasonCandidates)
				if rerankErr != nil {
					logger.Warn("re-rank failed or timed out, using original order", "error", rerankErr)
				} else {
					ranked = reranked
					logger.Debug("re-ranked results", "threshold_triggered", !forceRerank)
				}
			} else if reason && cfg.Claude.APIKey == "" {
				logger.Warn("--reason requires ANTHROPIC_API_KEY; skipping re-rank")
			}

			// Apply token budget
			var contents []string
			for i := range ranked {
				contents = append(contents, ranked[i].Memory.Content)
			}

			output, count := tokenizer.FormatMemoriesWithBudget(contents, budget)

			if ctxJSON != "" {
				// Output as JSON
				jsonResults := ranked
				if count < len(ranked) {
					jsonResults = ranked[:count]
				}
				out, err := json.MarshalIndent(jsonResults, "", "  ")
				if err != nil {
					return fmt.Errorf("recall: marshaling JSON output: %w", err)
				}
				fmt.Println(string(out))
			} else {
				fmt.Printf("Recalled %d memories (budget: %d tokens):\n\n", count, budget)
				fmt.Println(output)
			}

			// Update access metadata for returned memories
			for i := 0; i < count && i < len(ranked); i++ {
				if updateErr := st.UpdateAccessMetadata(ctx, ranked[i].Memory.ID); updateErr != nil {
					logger.Warn("recall: UpdateAccessMetadata", "id", ranked[i].Memory.ID, "error", updateErr)
				}
			}

			return nil
		},
	}

	cmd.Flags().IntVar(&budget, "budget", 2000, "token budget")
	cmd.Flags().StringVar(&ctxJSON, "context", "", "output as JSON context")
	cmd.Flags().StringVar(&project, "project", "", "project context for scope boosting")
	cmd.Flags().StringVar(&memType, "type", "", "filter by memory type (rule|fact|episode|procedure|preference)")
	cmd.Flags().StringVar(&memScope, "scope", "", "filter by scope (permanent|project|session|ttl)")
	cmd.Flags().StringVar(&tagsFlag, "tags", "", "filter by tags (comma-separated)")
	cmd.Flags().BoolVar(&reason, "reason", false, "use Claude to re-rank results by genuine relevance (requires ANTHROPIC_API_KEY)")
	cmd.Flags().IntVar(&reasonCandidates, "reason-candidates", 10, "number of top candidates to pass to Claude for re-ranking")
	return cmd
}
