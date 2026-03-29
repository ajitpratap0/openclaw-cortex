package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/ajitpratap0/openclaw-cortex/internal/async"
	"github.com/ajitpratap0/openclaw-cortex/internal/extract"
	"github.com/ajitpratap0/openclaw-cortex/internal/llm"
	"github.com/ajitpratap0/openclaw-cortex/internal/memgraph"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
	"github.com/ajitpratap0/openclaw-cortex/internal/timeutil"
)

func storeCmd() *cobra.Command {
	var (
		memType         string
		scope           string
		tags            string
		project         string
		confidence      float64
		ttlHours        int
		supersedesID    string
		validUntil      string
		extractEntities bool
		skipDedup       bool
		dedupThreshold  float64
	)

	cmd := &cobra.Command{
		Use:   "store [memory text]",
		Short: "Store a new memory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := newLogger()
			ctx := cmd.Context()
			content := args[0]

			// Validate minimum content length.
			if err := store.ValidateContentLength(content); err != nil {
				return fmt.Errorf("store: %w", err)
			}

			// Validate memory type.
			mt := models.MemoryType(memType)
			if !mt.IsValid() {
				return fmt.Errorf("store: invalid --type %q: must be one of %s",
					memType, validTypesString())
			}

			// Validate memory scope.
			ms := models.MemoryScope(scope)
			if !ms.IsValid() {
				return fmt.Errorf("store: invalid --scope %q: must be one of %s",
					scope, validScopesString())
			}

			emb := newEmbedder(logger)
			st, err := newMemgraphStore(ctx, logger)
			if err != nil {
				return cmdErr("store: connecting to store", err)
			}
			defer func() { _ = st.Close() }()

			if err = st.EnsureCollection(ctx); err != nil {
				return cmdErr("store: ensuring collection", err)
			}

			vec, err := emb.Embed(ctx, content)
			if err != nil {
				return cmdErr("store: embedding content", err)
			}

			// Store-time dedup: check for near-identical memories (similarity > 0.92).
			// Bypassed when --skip-dedup is set.
			if !skipDedup {
				// Resolve effective dedup threshold: flag overrides config default.
				effectiveThreshold := cfg.Memory.DedupThreshold
				if cmd.Flags().Changed("dedup-threshold") {
					if err := store.ValidateDedupThreshold(dedupThreshold); err != nil {
						return fmt.Errorf("store: --dedup-threshold: %w", err)
					}
					effectiveThreshold = dedupThreshold
				}

				dedupRes, dedupErr := store.CheckAndHandleDuplicate(ctx, st, vec, content, effectiveThreshold)
				if dedupErr != nil {
					// Dedup is an optimisation, not a correctness gate — fail open
					// so a transient Memgraph hiccup does not block all stores.
					logger.Warn("store: dedup check failed, proceeding without dedup", "error", dedupErr)
				} else {
					switch {
					case dedupRes.IsDuplicate:
						fmt.Printf("duplicate detected: memory %s already covers this content (skipped)\n", dedupRes.ExistingID)
						return nil
					case dedupRes.IsUpdated:
						fmt.Printf("duplicate detected: updated existing memory %s with richer content (note: --tags/--confidence/--scope flags were not applied; use --skip-dedup to replace fully)\n", dedupRes.ExistingID)
						return nil
					}
				}
			}

			now := time.Now().UTC()
			var tagList []string
			if tags != "" {
				tagList = parseTags(tags)
			}

			mem := models.Memory{
				ID:           uuid.New().String(),
				Type:         mt,
				Scope:        ms,
				Visibility:   models.VisibilityShared,
				Content:      content,
				Confidence:   confidence,
				Source:       "explicit",
				Tags:         tagList,
				Project:      project,
				CreatedAt:    now,
				UpdatedAt:    now,
				LastAccessed: now,
				SupersedesID: supersedesID,
			}

			if ttlHours > 0 {
				mem.TTLSeconds = int64(ttlHours) * 3600
				mem.Scope = models.ScopeSession // TTL memories are session-scoped by convention
			}

			if validUntil != "" {
				dur, parseErr := timeutil.ParseDuration(validUntil)
				if parseErr != nil {
					return fmt.Errorf("store: invalid --valid-until %q: %w", validUntil, parseErr)
				}
				mem.ValidUntil = now.Add(dur)
			}

			if err := st.Upsert(ctx, mem, vec); err != nil {
				return cmdErr("store: upserting memory", err)
			}

			fmt.Printf("Stored memory %s [%s/%s]\n", mem.ID, mem.Type, mem.Scope)

			if extractEntities {
				if cfg.Async.Disabled || asyncQueue == nil {
					// Synchronous fallback (backward compat / disabled mode).
					llmClient := llm.NewClient(cfg.Claude)
					if llmClient == nil {
						fmt.Println("  Entity extraction skipped: no LLM configured (set ANTHROPIC_API_KEY or gateway)")
					} else {
						gc := memgraph.NewGraphAdapter(st)
						gc.SetEmbedder(emb)
						res := extract.Run(ctx, extract.Deps{
							LLMClient:   llmClient,
							Model:       cfg.Claude.Model,
							Store:       st,
							GraphClient: gc,
							Logger:      logger,
						}, []extract.StoredMemory{{ID: mem.ID, Content: content}})
						if res.EntitiesExtracted > 0 || res.FactsExtracted > 0 {
							fmt.Printf("  Extracted %d entities, %d facts\n", res.EntitiesExtracted, res.FactsExtracted)
						} else {
							fmt.Println("  No entities or facts extracted")
						}
					}
				} else {
					// Fast path: enqueue for async graph processing.
					item := async.WorkItem{
						MemoryID:   mem.ID,
						Content:    content,
						Project:    project,
						EnqueuedAt: time.Now().UTC(),
					}
					if enqErr := asyncQueue.Enqueue(item); enqErr != nil {
						logger.Warn("failed to enqueue graph work", "memory_id", mem.ID, "err", enqErr)
					}
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&memType, "type", "fact", "memory type (rule|fact|episode|procedure|preference)")
	cmd.Flags().StringVar(&scope, "scope", "permanent", "memory scope")
	cmd.Flags().StringVar(&tags, "tags", "", "comma-separated tags")
	cmd.Flags().StringVar(&project, "project", "", "project name")
	cmd.Flags().Float64Var(&confidence, "confidence", 0.9, "confidence score")
	cmd.Flags().IntVar(&ttlHours, "ttl", 0, "time-to-live in hours (0 = permanent)")
	cmd.Flags().StringVar(&supersedesID, "supersedes", "", "ID of memory this one replaces")
	cmd.Flags().StringVar(&validUntil, "valid-until", "", "validity duration from now (e.g. 24h, 7d)")
	cmd.Flags().BoolVar(&extractEntities, "extract-entities", false, "extract entities and facts from content (requires LLM)")
	cmd.Flags().BoolVar(&skipDedup, "skip-dedup", false, "bypass store-time dedup check (always store as new memory)")
	cmd.Flags().Float64Var(&dedupThreshold, "dedup-threshold", 0, "override cosine similarity dedup threshold for this call (range (0.0, 1.0]; omit to use config default)")
	return cmd
}

func validTypesString() string {
	types := make([]string, len(models.ValidMemoryTypes))
	for i, t := range models.ValidMemoryTypes {
		types[i] = string(t)
	}
	return strings.Join(types, "|")
}

func validScopesString() string {
	scopes := make([]string, len(models.ValidMemoryScopes))
	for i, s := range models.ValidMemoryScopes {
		scopes[i] = string(s)
	}
	return strings.Join(scopes, "|")
}
