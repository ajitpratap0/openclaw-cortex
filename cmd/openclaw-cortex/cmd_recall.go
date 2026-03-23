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
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
	"github.com/ajitpratap0/openclaw-cortex/pkg/tokenizer"
)

func recallCmd() *cobra.Command {
	var (
		budget           int
		ctxJSON          string
		format           string
		limit            int
		project          string
		memType          string
		memScope         string
		tagsFlag         string
		reason           bool
		reasonCandidates int
		graphDepth       int
		includeHistory   bool
	)

	cmd := &cobra.Command{
		Use:   "recall [current message]",
		Short: "Recall relevant memories with multi-factor ranking",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if format != "text" && format != "json" {
				return fmt.Errorf("recall: unknown --format %q; expected \"text\" or \"json\"", format)
			}
			if limit < 0 {
				return fmt.Errorf("recall: --limit must be non-negative, got %d", limit)
			}
			if limit > 10000 {
				return fmt.Errorf("recall: --limit %d exceeds maximum of 10000", limit)
			}

			logger := newLogger()
			ctx := cmd.Context()
			query := args[0]

			emb := newEmbedder(logger)
			st, err := newMemgraphStore(ctx, logger)
			if err != nil {
				return cmdErr("recall: connecting to store", err)
			}
			defer func() { _ = st.Close() }()

			vec, err := emb.Embed(ctx, query)
			if err != nil {
				return cmdErr("recall: embedding query", err)
			}

			filters, filterErr := buildSearchFilters("recall", memType, memScope, project, tagsFlag)
			if filterErr != nil {
				return filterErr
			}
			if includeHistory {
				if filters == nil {
					filters = &store.SearchFilters{}
				}
				filters.IncludeInvalidated = true
			}

			// Fetch more results than needed for re-ranking. When --limit is
			// set, use it as a floor so we always retrieve at least that many
			// candidates before post-ranking truncation.
			searchLimit := uint64(50)
			if limit > 0 && uint64(limit)*2 > searchLimit {
				searchLimit = uint64(limit) * 2
			}
			results, err := st.Search(ctx, vec, searchLimit, filters)
			if err != nil {
				return cmdErr("recall: searching store", err)
			}

			// Re-rank with multi-factor scoring using config-loaded weights.
			recaller := recall.NewRecaller(recallWeightsFromConfig(cfg.Recall.Weights), logger)

			// Wire graph client for graph-augmented recall — MemgraphStore implements graph.Client.
			gc := memgraph.NewGraphAdapter(st)
			recaller.SetGraphClient(gc, st, cfg.Recall.GraphBudgetCLIMs)
			recaller.SetGraphDepth(graphDepth)

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

			// Apply --limit cap before token-budget trimming so the result
			// count is deterministic when --limit is set. Note: --budget
			// applies after this cap for both modes — it formats text output
			// and also trims JSON results when count < len(ranked).
			if limit > 0 && len(ranked) > limit {
				ranked = ranked[:limit]
			}

			// Apply token budget
			var contents []string
			for i := range ranked {
				contents = append(contents, ranked[i].Memory.Content)
			}

			output, count := tokenizer.FormatMemoriesWithBudget(contents, budget)

			// JSON output mode is activated by either:
			//   --format json  (preferred; explicit, no sentinel hack)
			//   --context <any non-empty value>  (backward-compat sentinel; older
			//     eval harness versions pass this to trigger JSON mode)
			// Precedence: an explicit --format text always wins over the sentinel.
			// This lets callers opt out of the legacy behavior cleanly.
			// jsonMode relies on cmd.Flags().Changed("format") returning true only for
			// explicit CLI flags, not defaults or config-file values. If a viper binding
			// is added for --format in the future, this logic must be revisited.
			jsonMode := format == "json" || (ctxJSON != "" && !cmd.Flags().Changed("format"))
			if ctxJSON != "" && !jsonMode {
				logger.Warn("--context is set but --format text was explicitly requested; outputting text")
			}
			if jsonMode {
				// Output as JSON
				jsonResults := ranked
				if count < len(ranked) {
					jsonResults = ranked[:count]
				}
				out, err := json.MarshalIndent(jsonResults, "", "  ")
				if err != nil {
					return cmdErr("recall: marshaling JSON output", err)
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
	cmd.Flags().StringVar(&ctxJSON, "context", "", "output as JSON context; WARNING: activates JSON output mode unless --format text is explicitly set (backward-compat; prefer --format json)")
	cmd.Flags().StringVar(&format, "format", "text", "output format: text or json (json is preferred over --context sentinel)")
	cmd.Flags().IntVar(&limit, "limit", 0, "maximum number of results (0 = no cap, max 10000)")
	cmd.Flags().StringVar(&project, "project", "", "project context for scope boosting")
	cmd.Flags().StringVar(&memType, "type", "", "filter by memory type (rule|fact|episode|procedure|preference)")
	cmd.Flags().StringVar(&memScope, "scope", "", "filter by scope (permanent|project|session|ttl)")
	cmd.Flags().StringVar(&tagsFlag, "tags", "", "filter by tags (comma-separated)")
	cmd.Flags().BoolVar(&reason, "reason", false, "use Claude to re-rank results by genuine relevance (requires ANTHROPIC_API_KEY)")
	cmd.Flags().IntVar(&reasonCandidates, "reason-candidates", 10, "number of top candidates to pass to Claude for re-ranking")
	cmd.Flags().IntVar(&graphDepth, "graph-depth", 2, "graph traversal depth for graph-aware recall (1=direct entity facts only, 2=also traverse neighbor entities)")
	cmd.Flags().BoolVar(&includeHistory, "include-history", false, "include invalidated/superseded memories in results")
	return cmd
}
