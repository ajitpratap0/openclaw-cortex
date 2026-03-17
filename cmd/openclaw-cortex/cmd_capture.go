package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/ajitpratap0/openclaw-cortex/internal/capture"
	"github.com/ajitpratap0/openclaw-cortex/internal/classifier"
	graphpkg "github.com/ajitpratap0/openclaw-cortex/internal/graph"
	"github.com/ajitpratap0/openclaw-cortex/internal/llm"
	"github.com/ajitpratap0/openclaw-cortex/internal/memgraph"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
)

func captureCmd() *cobra.Command {
	var (
		userMsg      string
		assistantMsg string
		sessionID    string
		scope        string
		userID       string
	)

	cmd := &cobra.Command{
		Use:   "capture",
		Short: "Extract memories from a conversation turn",
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := newLogger()
			ctx := cmd.Context()

			// Validate that LLM access is available (API key or gateway).
			if cfg.Claude.APIKey == "" && (cfg.Claude.GatewayURL == "" || cfg.Claude.GatewayToken == "") {
				return fmt.Errorf("capture: no LLM configured (set ANTHROPIC_API_KEY or claude.gateway_url + claude.gateway_token)")
			}

			// Validate memory scope.
			ms := models.MemoryScope(scope)
			if !ms.IsValid() {
				return fmt.Errorf("capture: invalid --scope %q: must be one of %s",
					scope, validScopesString())
			}

			emb := newEmbedder(logger)
			st, err := newMemgraphStore(ctx, logger)
			if err != nil {
				return fmt.Errorf("capture: connecting to store: %w", err)
			}
			defer func() { _ = st.Close() }()

			if err = st.EnsureCollection(ctx); err != nil {
				return fmt.Errorf("capture: ensuring collection: %w", err)
			}

			// Pre-capture quality filter: skip trivial exchanges.
			if !capture.ShouldCapture(userMsg, assistantMsg, cfg.CaptureQuality) {
				logger.Info("skipping low-quality conversation turn",
					"user_len", len(userMsg), "assistant_len", len(assistantMsg))
				fmt.Println("Skipped: conversation turn did not pass quality filter")
				return nil
			}

			llmClient := llm.NewClient(cfg.Claude)
			cap := capture.NewCapturer(llmClient, cfg.Claude.Model, logger)
			cls := classifier.NewClassifier(logger)

			memories, err := cap.Extract(ctx, userMsg, assistantMsg)
			if err != nil {
				return fmt.Errorf("capture: extracting memories: %w", err)
			}

			logger.Info("extracted memories", "count", len(memories))

			type storedMemory struct {
				id      string
				content string
			}

			stored := 0
			storedMems := make([]storedMemory, 0, len(memories))
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
					Scope:        ms,
					Visibility:   models.VisibilityShared,
					Content:      cm.Content,
					Confidence:   cm.Confidence,
					Source:       "inferred",
					Tags:         cm.Tags,
					UserID:       userID,
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
				storedMems = append(storedMems, storedMemory{id: mem.ID, content: cm.Content})
				fmt.Printf("Captured [%s]: %s\n", mem.Type, truncate(cm.Content, 100))
			}

			// The MemgraphStore implements graph.Client — use it directly for entity + fact writes.
			gc := memgraph.NewGraphAdapter(st)

			// Entity extraction (graceful — skipped if no LLM or on error).
			// entityNameToID maps lowercased entity names to their UUIDs so that
			// the fact extractor (which returns names) can resolve to UUIDs.
			var allEntityNames []string
			entityNameToID := make(map[string]string)
			if llmClient != nil {
				extractor := capture.NewEntityExtractor(llmClient, cfg.Claude.Model, logger)
				for i := range storedMems {
					entities, extractErr := extractor.Extract(ctx, storedMems[i].content)
					if extractErr != nil {
						logger.Warn("entity extraction failed, skipping", "error", extractErr)
						continue
					}
					for j := range entities {
						if upsertErr := st.UpsertEntity(ctx, entities[j]); upsertErr != nil {
							logger.Warn("upsert entity to store failed", "entity", entities[j].Name, "error", upsertErr)
							continue
						}
						if linkErr := st.LinkMemoryToEntity(ctx, entities[j].ID, storedMems[i].id); linkErr != nil {
							logger.Warn("link entity to memory failed", "entity", entities[j].Name, "error", linkErr)
						}
						allEntityNames = append(allEntityNames, entities[j].Name)
						entityNameToID[strings.ToLower(entities[j].Name)] = entities[j].ID
					}
				}
			}

			// Fact extraction + graph write (graceful — skipped on error).
			// The FactExtractor returns entity names in SourceEntityID/TargetEntityID;
			// we resolve them to UUIDs via entityNameToID before upserting to Memgraph.
			if llmClient != nil && len(allEntityNames) > 0 {
				factExtractor := graphpkg.NewFactExtractor(llmClient, cfg.Claude.Model, logger)
				for i := range storedMems {
					facts, factErr := factExtractor.Extract(ctx, storedMems[i].content, allEntityNames)
					if factErr != nil {
						logger.Warn("fact extraction failed, skipping", "error", factErr)
						continue
					}
					for j := range facts {
						// Resolve entity names to UUIDs.
						srcID, srcOK := entityNameToID[strings.ToLower(facts[j].SourceEntityID)]
						tgtID, tgtOK := entityNameToID[strings.ToLower(facts[j].TargetEntityID)]
						if !srcOK || !tgtOK {
							logger.Warn("fact references unknown entity, skipping",
								"source", facts[j].SourceEntityID, "target", facts[j].TargetEntityID,
								"source_resolved", srcOK, "target_resolved", tgtOK)
							continue
						}
						facts[j].SourceEntityID = srcID
						facts[j].TargetEntityID = tgtID

						if upsertErr := gc.UpsertFact(ctx, facts[j]); upsertErr != nil {
							logger.Warn("upsert fact failed", "fact_id", facts[j].ID, "error", upsertErr)
							continue
						}
						if linkErr := gc.AppendMemoryToFact(ctx, facts[j].ID, storedMems[i].id); linkErr != nil {
							logger.Warn("link fact to memory failed", "fact_id", facts[j].ID, "error", linkErr)
						}
					}
				}
			}

			fmt.Printf("Captured %d memories from conversation\n", stored)
			return nil
		},
	}

	cmd.Flags().StringVar(&userMsg, "user", "", "user message")
	cmd.Flags().StringVar(&assistantMsg, "assistant", "", "assistant response")
	cmd.Flags().StringVar(&sessionID, "session-id", "", "session identifier")
	cmd.Flags().StringVar(&scope, "scope", "permanent", "memory scope (permanent|project|session|ttl)")
	cmd.Flags().StringVar(&userID, "user-id", "", "user identifier for user-scoped memories")
	_ = cmd.MarkFlagRequired("user")
	_ = cmd.MarkFlagRequired("assistant")
	return cmd
}
