package hooks

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/ajitpratap0/openclaw-cortex/internal/embedder"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/recall"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
	"github.com/ajitpratap0/openclaw-cortex/pkg/tokenizer"
)

// PreTurnHook retrieves relevant memories before an agent turn.
type PreTurnHook struct {
	embedder embedder.Embedder
	store    store.Store
	recaller *recall.Recaller
	logger   *slog.Logger
}

// PreTurnInput contains the context for a pre-turn hook.
type PreTurnInput struct {
	Message     string `json:"message"`
	Project     string `json:"project"`
	TokenBudget int    `json:"token_budget"`
}

// PreTurnOutput contains the memories to inject into context.
type PreTurnOutput struct {
	Memories    []models.RecallResult `json:"memories"`
	TokensUsed  int                   `json:"tokens_used"`
	MemoryCount int                   `json:"memory_count"`
	Context     string                `json:"context"`
}

// NewPreTurnHook creates a pre-turn hook handler.
func NewPreTurnHook(emb embedder.Embedder, st store.Store, recaller *recall.Recaller, logger *slog.Logger) *PreTurnHook {
	return &PreTurnHook{
		embedder: emb,
		store:    st,
		recaller: recaller,
		logger:   logger,
	}
}

// Execute runs the pre-turn hook.
func (h *PreTurnHook) Execute(ctx context.Context, input PreTurnInput) (*PreTurnOutput, error) {
	if input.TokenBudget <= 0 {
		input.TokenBudget = 2000
	}

	// Embed the current message
	vec, err := h.embedder.Embed(ctx, input.Message)
	if err != nil {
		return nil, fmt.Errorf("embedding message: %w", err)
	}

	// Search for relevant memories
	results, err := h.store.Search(ctx, vec, 50, nil)
	if err != nil {
		return nil, fmt.Errorf("searching memories: %w", err)
	}

	// Rank with multi-factor scoring
	ranked := h.recaller.Rank(results, input.Project)

	// Format within token budget
	var contents []string
	for _, r := range ranked {
		contents = append(contents, r.Memory.Content)
	}

	formatted, count := tokenizer.FormatMemoriesWithBudget(contents, input.TokenBudget)

	// Update access metadata
	for i := 0; i < count && i < len(ranked); i++ {
		_ = h.store.UpdateAccessMetadata(ctx, ranked[i].Memory.ID)
	}

	output := &PreTurnOutput{
		MemoryCount: count,
		TokensUsed:  tokenizer.EstimateTokens(formatted),
		Context:     formatted,
	}
	if count <= len(ranked) {
		output.Memories = ranked[:count]
	}

	h.logger.Info("pre-turn hook executed", "memories_recalled", count, "tokens_used", output.TokensUsed)
	return output, nil
}

// PostTurnHook captures memories from a completed agent turn.
type PostTurnHook struct {
	logger *slog.Logger
}

// PostTurnInput contains the conversation turn data.
type PostTurnInput struct {
	UserMessage      string `json:"user_message"`
	AssistantMessage string `json:"assistant_message"`
	SessionID        string `json:"session_id"`
	Project          string `json:"project"`
}

// NewPostTurnHook creates a post-turn hook handler.
func NewPostTurnHook(logger *slog.Logger) *PostTurnHook {
	return &PostTurnHook{logger: logger}
}

// Execute runs the post-turn hook (delegates to capture pipeline).
func (h *PostTurnHook) Execute(_ context.Context, input PostTurnInput) error {
	// In production, this would call the capture pipeline.
	// The actual capture logic lives in internal/capture and is invoked via CLI.
	h.logger.Info("post-turn hook",
		"session_id", input.SessionID,
		"project", input.Project,
		"user_msg_len", len(input.UserMessage),
		"assistant_msg_len", len(input.AssistantMessage),
	)
	return nil
}
