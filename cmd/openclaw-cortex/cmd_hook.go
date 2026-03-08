package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ajitpratap0/openclaw-cortex/internal/capture"
	"github.com/ajitpratap0/openclaw-cortex/internal/classifier"
	"github.com/ajitpratap0/openclaw-cortex/internal/hooks"
	"github.com/ajitpratap0/openclaw-cortex/internal/recall"
	"github.com/ajitpratap0/openclaw-cortex/pkg/tokenizer"
)

const hookTimeout = 30 * time.Second

// hookPreInput is the JSON input shape for `cortex hook pre`.
// It matches the Claude Code UserPromptSubmit hook stdin payload.
type hookPreInput struct {
	SessionID      string `json:"session_id"`
	HookEventName  string `json:"hook_event_name"`
	Prompt         string `json:"prompt"`          // the user's message
	Cwd            string `json:"cwd"`             // working directory (used as project if Project not set)
	TranscriptPath string `json:"transcript_path"` // for future use
	// Keep these as optional with omitempty for backward compat:
	Project     string `json:"project,omitempty"`      // override project name if provided
	TokenBudget int    `json:"token_budget,omitempty"` // override budget if provided
}

// hookPreOutput is the JSON output shape for `cortex hook pre`.
type hookPreOutput struct {
	Context     string `json:"context"`
	MemoryCount int    `json:"memory_count"`
	TokensUsed  int    `json:"tokens_used"`
}

// hookPostInput is the JSON input shape for `cortex hook post`.
// It matches the Claude Code Stop hook stdin payload.
type hookPostInput struct {
	SessionID            string `json:"session_id"`
	HookEventName        string `json:"hook_event_name"`
	LastAssistantMessage string `json:"last_assistant_message"` // Claude Code Stop event
	// Keep these as optional for backward compat:
	UserMessage      string `json:"user_message,omitempty"`
	AssistantMessage string `json:"assistant_message,omitempty"`
	Project          string `json:"project,omitempty"`
	TranscriptPath   string `json:"transcript_path,omitempty"`
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
	cmd.AddCommand(hookPreCmd(), hookPostCmd(), hookInstallCmd())
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
				_, _ = fmt.Fprintf(os.Stderr, "openclaw-cortex hook: services unavailable (Qdrant: %v), continuing without memory context\n", storeErr)
				writePreOutput(hookPreOutput{})
				return nil
			}
			defer func() { _ = st.Close() }()

			// Derive project: use explicit Project field if set, otherwise use the
			// last path segment of Cwd (matches the typical Claude Code project name).
			project := input.Project
			if project == "" && input.Cwd != "" {
				project = filepath.Base(input.Cwd)
			}

			recaller := recall.NewRecaller(recall.DefaultWeights(), logger)
			preTurnHook := hooks.NewPreTurnHook(emb, st, recaller, logger)

			if cfg.Claude.APIKey != "" {
				reasoner := recall.NewReasoner(cfg.Claude.APIKey, cfg.Claude.Model, logger)
				preTurnHook = preTurnHook.WithReasoner(reasoner, hooks.RerankConfig{
					ScoreSpreadThreshold: cfg.Recall.RerankScoreSpreadThreshold,
					LatencyBudgetMs:      cfg.Recall.RerankLatencyBudgetHooksMs,
				})
			}

			// Check pre-warm cache before doing a full recall.
			homeDir, _ := os.UserHomeDir()
			if cached := hooks.ReadRerankCache(homeDir, input.SessionID); len(cached) > 0 {
				logger.Debug("pre-turn hook: using pre-warmed ranked results", "session", input.SessionID)
				budget := input.TokenBudget
				if budget <= 0 {
					budget = 2000
				}
				var contents []string
				for i := range cached {
					contents = append(contents, cached[i].Memory.Content)
				}
				formatted, count := tokenizer.FormatMemoriesWithBudget(contents, budget)
				writePreOutput(hookPreOutput{
					Context:     formatted,
					MemoryCount: count,
					TokensUsed:  tokenizer.EstimateTokens(formatted),
				})
				return nil
			}

			out, execErr := preTurnHook.Execute(ctx, hooks.PreTurnInput{
				Message:     input.Prompt,
				Project:     project,
				TokenBudget: input.TokenBudget,
				SessionID:   input.SessionID,
			})
			if execErr != nil {
				logger.Error("hook pre: executing hook", "error", execErr)
				_, _ = fmt.Fprintf(os.Stderr, "openclaw-cortex hook: memory recall failed (%v), continuing without memory context\n", execErr)
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
				_, _ = fmt.Fprintf(os.Stderr, "openclaw-cortex hook: services unavailable (Qdrant: %v), skipping memory capture\n", storeErr)
				writePostOutput(hookPostOutput{Stored: false})
				return nil
			}
			defer func() { _ = st.Close() }()

			// Use LastAssistantMessage (Claude Code Stop event field) falling back to
			// the legacy AssistantMessage field for backward compatibility.
			assistantMsg := input.LastAssistantMessage
			if assistantMsg == "" {
				assistantMsg = input.AssistantMessage
			}

			// UserMessage is not available in the Claude Code Stop event payload.
			// Fall back to reading the last human message from the transcript file.
			userMsg := input.UserMessage
			if userMsg == "" {
				userMsg = lastHumanMessageFromTranscript(input.TranscriptPath)
				if userMsg == "" {
					logger.Warn("hook post: user message unavailable from Stop event and transcript, skipping capture",
						"transcript_path", input.TranscriptPath)
					writePostOutput(hookPostOutput{Stored: false})
					return nil
				}
			}

			cap := capture.NewCapturer(cfg.Claude.APIKey, cfg.Claude.Model, logger)
			cls := classifier.NewClassifier(logger)

			postHook := hooks.NewPostTurnHook(cap, cls, emb, st, logger, cfg.Memory.DedupThresholdHook).
				WithReinforcement(cfg.CaptureQuality.ReinforcementThreshold, cfg.CaptureQuality.ReinforcementConfidenceBoost)
			if cfg.Claude.APIKey != "" {
				cd := capture.NewConflictDetector(cfg.Claude.APIKey, cfg.Claude.Model, logger)
				postHook = postHook.WithConflictDetector(cd)
			}
			hook := postHook

			priorTurns := lastNTurnsFromTranscript(input.TranscriptPath, cfg.CaptureQuality.ContextWindowTurns)

			// XML-escaping of user/assistant content is handled inside
			// capture.ClaudeCapturer.Extract — do not bypass with a raw Capturer implementation.
			execErr := hook.Execute(ctx, hooks.PostTurnInput{
				UserMessage:      userMsg,
				AssistantMessage: assistantMsg,
				SessionID:        input.SessionID,
				Project:          input.Project,
				PriorTurns:       priorTurns,
			})
			if execErr != nil {
				logger.Error("hook post: executing hook", "error", execErr)
				_, _ = fmt.Fprintf(os.Stderr, "openclaw-cortex hook: memory capture failed (%v), skipping\n", execErr)
				writePostOutput(hookPostOutput{Stored: false})
				return nil
			}

			// Spawn background pre-warm: re-rank for next pre-turn hook call.
			if cfg.Claude.APIKey != "" && input.SessionID != "" {
				postHomeDir, _ := os.UserHomeDir()
				go func() {
					prewarmCtx, prewarmCancel := context.WithTimeout(context.Background(),
						time.Duration(cfg.Recall.RerankLatencyBudgetHooksMs*10)*time.Millisecond)
					defer prewarmCancel()
					vec, embedErr := emb.Embed(prewarmCtx, userMsg)
					if embedErr != nil {
						return
					}
					results, searchErr := st.Search(prewarmCtx, vec, 50, nil)
					if searchErr != nil {
						return
					}
					prewarmRecaller := recall.NewRecaller(recall.DefaultWeights(), logger)
					ranked := prewarmRecaller.Rank(results, input.Project)
					reasoner := recall.NewReasoner(cfg.Claude.APIKey, cfg.Claude.Model, logger)
					reranked, rerankErr := reasoner.ReRank(prewarmCtx, userMsg, ranked, 0)
					if rerankErr != nil {
						return
					}
					hooks.WriteRerankCache(postHomeDir, input.SessionID, reranked)
				}()
			}

			writePostOutput(hookPostOutput{Stored: true})
			return nil
		},
	}
}

// lastHumanMessageFromTranscript reads the transcript JSONL at path and
// returns the content of the last "human" role entry. Returns "" on any error.
func lastHumanMessageFromTranscript(path string) string {
	if path == "" {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()

	type transcriptEntry struct {
		Role    string `json:"role"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
	}

	var last string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry transcriptEntry
		if json.Unmarshal([]byte(line), &entry) != nil {
			continue
		}
		role := entry.Role
		content := entry.Message.Content
		if role == "" {
			role = entry.Message.Role
		}
		if strings.EqualFold(role, "human") || strings.EqualFold(role, "user") {
			last = content
		}
	}
	return last
}

// lastNTurnsFromTranscript reads the last n conversation turns from the JSONL transcript.
func lastNTurnsFromTranscript(path string, n int) []capture.ConversationTurn {
	if path == "" || n <= 0 {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()

	type entry struct {
		Role    string `json:"role"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
	}

	var all []capture.ConversationTurn
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var e entry
		if json.Unmarshal([]byte(line), &e) != nil {
			continue
		}
		role := e.Role
		if role == "" {
			role = e.Message.Role
		}
		content := e.Message.Content
		if strings.EqualFold(role, "human") || strings.EqualFold(role, "user") {
			all = append(all, capture.ConversationTurn{Role: "user", Content: content})
		} else if strings.EqualFold(role, "assistant") {
			all = append(all, capture.ConversationTurn{Role: "assistant", Content: content})
		}
	}
	if n > 0 && len(all) > n {
		all = all[len(all)-n:]
	}
	return all
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
	if _, err = os.Stdout.Write(enc); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "openclaw-cortex hook: failed to write pre-output: %v\n", err)
		return
	}
	if _, err = os.Stdout.WriteString("\n"); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "openclaw-cortex hook: failed to write pre-output newline: %v\n", err)
	}
}

// writePostOutput marshals the post-turn output to stdout.
// On marshal failure it falls back to a hard-coded false response.
func writePostOutput(out hookPostOutput) {
	enc, err := json.Marshal(out)
	if err != nil {
		_, _ = os.Stdout.WriteString(`{"stored":false}` + "\n")
		return
	}
	if _, err = os.Stdout.Write(enc); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "openclaw-cortex hook: failed to write post-output: %v\n", err)
		return
	}
	if _, err = os.Stdout.WriteString("\n"); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "openclaw-cortex hook: failed to write post-output newline: %v\n", err)
	}
}
