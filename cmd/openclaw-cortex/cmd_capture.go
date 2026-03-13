package main

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/ajitpratap0/openclaw-cortex/internal/capture"
	"github.com/ajitpratap0/openclaw-cortex/internal/classifier"
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

			// Validate that the API key is present before making any API call.
			if cfg.Claude.APIKey == "" {
				return fmt.Errorf("capture: ANTHROPIC_API_KEY environment variable is not set")
			}

			// Validate memory scope.
			ms := models.MemoryScope(scope)
			if !ms.IsValid() {
				return fmt.Errorf("capture: invalid --scope %q: must be one of %s",
					scope, validScopesString())
			}

			emb := newEmbedder(logger)
			st, err := newStore(logger)
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

			cap := capture.NewCapturer(cfg.Claude.APIKey, cfg.Claude.Model, logger)
			cls := classifier.NewClassifier(logger)

			memories, err := cap.Extract(ctx, userMsg, assistantMsg)
			if err != nil {
				return fmt.Errorf("capture: extracting memories: %w", err)
			}

			logger.Info("extracted memories", "count", len(memories))

			stored := 0
			storedIDs := make([]string, 0, len(memories))
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
				storedIDs = append(storedIDs, mem.ID)
				fmt.Printf("Captured [%s]: %s\n", mem.Type, truncate(cm.Content, 100))
			}

			// Entity extraction (graceful — skipped if no API key or on error)
			if cfg.Claude.APIKey != "" {
				extractor := capture.NewEntityExtractor(cfg.Claude.APIKey, cfg.Claude.Model, logger)
				for i := range storedIDs {
					// Use index to get corresponding memory content
					if i >= len(memories) {
						break
					}
					entities, extractErr := extractor.Extract(ctx, memories[i].Content)
					if extractErr != nil {
						logger.Warn("entity extraction failed, skipping", "error", extractErr)
						continue
					}
					for j := range entities {
						if upsertErr := st.UpsertEntity(ctx, entities[j]); upsertErr != nil {
							logger.Warn("upsert entity failed", "entity", entities[j].Name, "error", upsertErr)
							continue
						}
						if linkErr := st.LinkMemoryToEntity(ctx, entities[j].ID, storedIDs[i]); linkErr != nil {
							logger.Warn("link entity to memory failed", "entity", entities[j].Name, "error", linkErr)
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
	_ = cmd.MarkFlagRequired("user")
	_ = cmd.MarkFlagRequired("assistant")
	return cmd
}
