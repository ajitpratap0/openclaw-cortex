package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
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
	)

	cmd := &cobra.Command{
		Use:   "capture",
		Short: "Extract memories from a conversation turn",
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := newLogger()
			ctx := context.Background()

			// Validate that the API key is present before making any API call.
			if cfg.Claude.APIKey == "" {
				slog.Error("ANTHROPIC_API_KEY is not set; cannot call Claude API")
				os.Exit(1)
			}

			emb := newEmbedder(logger)
			st, err := newStore(logger)
			if err != nil {
				return fmt.Errorf("capture: connecting to store: %w", err)
			}
			defer func() { _ = st.Close() }()

			if err := st.EnsureCollection(ctx); err != nil {
				return fmt.Errorf("capture: ensuring collection: %w", err)
			}

			cap := capture.NewCapturer(cfg.Claude.APIKey, cfg.Claude.Model, logger)
			cls := classifier.NewClassifier(logger)

			memories, err := cap.Extract(ctx, userMsg, assistantMsg)
			if err != nil {
				return fmt.Errorf("capture: extracting memories: %w", err)
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
