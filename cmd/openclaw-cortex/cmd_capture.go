package main

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/ajitpratap0/openclaw-cortex/internal/async"
	"github.com/ajitpratap0/openclaw-cortex/internal/capture"
	"github.com/ajitpratap0/openclaw-cortex/internal/classifier"
	"github.com/ajitpratap0/openclaw-cortex/internal/extract"
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
				return cmdErr("capture: connecting to store", err)
			}
			defer func() { _ = st.Close() }()

			if err = st.EnsureCollection(ctx); err != nil {
				return cmdErr("capture: ensuring collection", err)
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
				return cmdErr("capture: extracting memories", err)
			}

			logger.Info("extracted memories", "count", len(memories))

			stored := 0
			storedMems := make([]extract.StoredMemory, 0, len(memories))
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
				storedMems = append(storedMems, extract.StoredMemory{ID: mem.ID, Content: cm.Content})
				fmt.Printf("Captured [%s]: %s\n", mem.Type, truncate(cm.Content, 100))
			}

			if cfg.Async.Disabled || asyncQueue == nil {
				// Synchronous fallback (backward compat / disabled mode).
				gc := memgraph.NewGraphAdapter(st)
				res := extract.Run(ctx, extract.Deps{
					LLMClient:   llmClient,
					Model:       cfg.Claude.Model,
					Store:       st,
					GraphClient: gc,
					Logger:      logger,
				}, storedMems)
				logger.Info("post-store extraction", "entities", res.EntitiesExtracted, "facts", res.FactsExtracted)
			} else {
				// Fast path: enqueue each memory for async graph processing.
				for i := range storedMems {
					item := async.WorkItem{
						MemoryID:   storedMems[i].ID,
						Content:    storedMems[i].Content,
						SessionID:  sessionID,
						EnqueuedAt: time.Now().UTC(),
					}
					if enqErr := asyncQueue.Enqueue(item); enqErr != nil {
						logger.Warn("failed to enqueue graph work", "memory_id", storedMems[i].ID, "err", enqErr)
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
	_ = cmd.MarkFlagRequired("user")
	_ = cmd.MarkFlagRequired("assistant")
	return cmd
}
