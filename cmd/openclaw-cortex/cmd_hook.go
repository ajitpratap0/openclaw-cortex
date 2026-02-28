package main

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/ajitpratap0/openclaw-cortex/internal/capture"
	"github.com/ajitpratap0/openclaw-cortex/internal/classifier"
	"github.com/ajitpratap0/openclaw-cortex/internal/hooks"
	"github.com/ajitpratap0/openclaw-cortex/internal/recall"
)

const hookTimeout = 30 * time.Second

// hookPreInput is the JSON input shape for `cortex hook pre`.
type hookPreInput struct {
	Message     string `json:"message"`
	Project     string `json:"project"`
	TokenBudget int    `json:"token_budget"`
}

// hookPreOutput is the JSON output shape for `cortex hook pre`.
type hookPreOutput struct {
	Context     string `json:"context"`
	MemoryCount int    `json:"memory_count"`
	TokensUsed  int    `json:"tokens_used"`
}

// hookPostInput is the JSON input shape for `cortex hook post`.
type hookPostInput struct {
	UserMessage      string `json:"user_message"`
	AssistantMessage string `json:"assistant_message"`
	SessionID        string `json:"session_id"`
	Project          string `json:"project"`
}

// hookPostOutput is the JSON output shape for `cortex hook post`.
type hookPostOutput struct {
	Stored bool `json:"stored"`
}

// hookCmd returns a cobra.Command that groups `hook pre` and `hook post`.
func hookCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hook",
		Short: "Claude Code hook integration (pre/post turn)",
	}
	cmd.AddCommand(hookPreCmd(), hookPostCmd())
	return cmd
}

// hookPreCmd implements `cortex hook pre`.
// It reads JSON from stdin, injects relevant memories, and writes JSON to stdout.
// On ANY error it exits 0 with an empty context so it never blocks Claude.
func hookPreCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pre",
		Short: "Pre-turn hook: inject relevant memories into Claude context",
		// SilenceErrors / SilenceUsage ensure errors do not print usage and do
		// not exit non-zero — graceful degradation is required by the spec.
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := newLogger()
			ctx, cancel := context.WithTimeout(cmd.Context(), hookTimeout)
			defer cancel()

			// Decode input from stdin.
			var input hookPreInput
			if decodeErr := json.NewDecoder(os.Stdin).Decode(&input); decodeErr != nil {
				logger.Error("hook pre: decoding stdin", "error", decodeErr)
				writePreOutput(hookPreOutput{})
				return nil
			}

			emb := newEmbedder(logger)
			st, storeErr := newStore(logger)
			if storeErr != nil {
				logger.Error("hook pre: connecting to store", "error", storeErr)
				writePreOutput(hookPreOutput{})
				return nil
			}
			defer func() { _ = st.Close() }()

			recaller := recall.NewRecaller(recall.DefaultWeights(), logger)
			hook := hooks.NewPreTurnHook(emb, st, recaller, logger)

			out, execErr := hook.Execute(ctx, hooks.PreTurnInput{
				Message:     input.Message,
				Project:     input.Project,
				TokenBudget: input.TokenBudget,
			})
			if execErr != nil {
				logger.Error("hook pre: executing hook", "error", execErr)
				writePreOutput(hookPreOutput{})
				return nil
			}

			writePreOutput(hookPreOutput{
				Context:     out.Context,
				MemoryCount: out.MemoryCount,
				TokensUsed:  out.TokensUsed,
			})
			return nil
		},
	}
}

// hookPostCmd implements `cortex hook post`.
// It reads JSON from stdin, captures memories from the turn, and writes JSON to stdout.
// On ANY error it exits 0 with `{"stored": false}` so it never blocks Claude.
func hookPostCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "post",
		Short:         "Post-turn hook: capture memories from a completed Claude turn",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := newLogger()
			ctx, cancel := context.WithTimeout(cmd.Context(), hookTimeout)
			defer cancel()

			// Decode input from stdin.
			var input hookPostInput
			if decodeErr := json.NewDecoder(os.Stdin).Decode(&input); decodeErr != nil {
				logger.Error("hook post: decoding stdin", "error", decodeErr)
				writePostOutput(hookPostOutput{Stored: false})
				return nil
			}

			if cfg.Claude.APIKey == "" {
				logger.Warn("hook post: ANTHROPIC_API_KEY not set, skipping capture")
				writePostOutput(hookPostOutput{Stored: false})
				return nil
			}

			emb := newEmbedder(logger)
			st, storeErr := newStore(logger)
			if storeErr != nil {
				logger.Error("hook post: connecting to store", "error", storeErr)
				writePostOutput(hookPostOutput{Stored: false})
				return nil
			}
			defer func() { _ = st.Close() }()

			cap := capture.NewCapturer(cfg.Claude.APIKey, cfg.Claude.Model, logger)
			cls := classifier.NewClassifier(logger)

			hook := hooks.NewPostTurnHook(cap, cls, emb, st, logger, cfg.Memory.DedupThreshold)

			// XML-escaping of user/assistant content is handled inside
			// capture.ClaudeCapturer.Extract — do not bypass with a raw Capturer implementation.
			execErr := hook.Execute(ctx, hooks.PostTurnInput{
				UserMessage:      input.UserMessage,
				AssistantMessage: input.AssistantMessage,
				SessionID:        input.SessionID,
				Project:          input.Project,
			})
			if execErr != nil {
				logger.Error("hook post: executing hook", "error", execErr)
				writePostOutput(hookPostOutput{Stored: false})
				return nil
			}

			writePostOutput(hookPostOutput{Stored: true})
			return nil
		},
	}
}

// writePreOutput marshals the pre-turn output to stdout.
// On marshal failure it falls back to a hard-coded zero-value response.
func writePreOutput(out hookPreOutput) {
	enc, err := json.Marshal(out)
	if err != nil {
		// Last-resort: write the zero-value response as a literal.
		_, _ = os.Stdout.WriteString(`{"context":"","memory_count":0,"tokens_used":0}` + "\n")
		return
	}
	_, err = os.Stdout.Write(enc)
	if err != nil {
		return
	}
	_, _ = os.Stdout.WriteString("\n")
}

// writePostOutput marshals the post-turn output to stdout.
// On marshal failure it falls back to a hard-coded false response.
func writePostOutput(out hookPostOutput) {
	enc, err := json.Marshal(out)
	if err != nil {
		_, _ = os.Stdout.WriteString(`{"stored":false}` + "\n")
		return
	}
	_, err = os.Stdout.Write(enc)
	if err != nil {
		return
	}
	_, _ = os.Stdout.WriteString("\n")
}
