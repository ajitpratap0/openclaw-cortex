package hooks

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/ajitpratap0/openclaw-cortex/internal/capture"
	"github.com/ajitpratap0/openclaw-cortex/internal/classifier"
	"github.com/ajitpratap0/openclaw-cortex/internal/embedder"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/recall"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
	"github.com/ajitpratap0/openclaw-cortex/pkg/tokenizer"
)

// preTurnSearchLimit is the maximum number of candidate memories retrieved during pre-turn search.
const preTurnSearchLimit = 50

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
	results, err := h.store.Search(ctx, vec, preTurnSearchLimit, nil)
	if err != nil {
		return nil, fmt.Errorf("searching memories: %w", err)
	}

	// Rank with multi-factor scoring
	ranked := h.recaller.Rank(results, input.Project)

	// Format within token budget
	var contents []string
	for i := range ranked {
		contents = append(contents, ranked[i].Memory.Content)
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
	} else {
		output.Memories = ranked
	}

	h.logger.Info("pre-turn hook executed", "memories_recalled", count, "tokens_used", output.TokensUsed)
	return output, nil
}

// dedupThreshold is intentionally stricter than the config default (0.92) to
// avoid false-positive dedup on automatically captured memories, where content
// overlap is more likely than with explicit user-stored memories.
const dedupThreshold = 0.95

// PostTurnHook captures memories from a completed agent turn.
type PostTurnHook struct {
	capturer   capture.Capturer
	classifier classifier.Classifier
	embedder   embedder.Embedder
	store      store.Store
	logger     *slog.Logger
}

// PostTurnInput contains the conversation turn data.
type PostTurnInput struct {
	UserMessage      string `json:"user_message"`
	AssistantMessage string `json:"assistant_message"`
	SessionID        string `json:"session_id"`
	Project          string `json:"project"`
}

// NewPostTurnHook creates a post-turn hook handler.
func NewPostTurnHook(cap capture.Capturer, cls classifier.Classifier, emb embedder.Embedder, st store.Store, logger *slog.Logger) *PostTurnHook {
	return &PostTurnHook{
		capturer:   cap,
		classifier: cls,
		embedder:   emb,
		store:      st,
		logger:     logger,
	}
}

// Execute runs the post-turn hook: extract → classify → embed → dedup → store.
func (h *PostTurnHook) Execute(ctx context.Context, input PostTurnInput) error {
	h.logger.Info("post-turn hook starting",
		"session_id", input.SessionID,
		"project", input.Project,
		"user_msg_len", len(input.UserMessage),
		"assistant_msg_len", len(input.AssistantMessage),
	)

	// 1. Extract candidate memories from the conversation turn.
	captured, err := h.capturer.Extract(ctx, input.UserMessage, input.AssistantMessage)
	if err != nil {
		return fmt.Errorf("post-turn extract: %w", err)
	}
	if len(captured) == 0 {
		h.logger.Debug("post-turn hook: no memories extracted")
		return nil
	}

	now := time.Now().UTC()
	stored := 0

	for _, cm := range captured {
		// 2. Classify – prefer the LLM-assigned type; only run the classifier if empty.
		memType := cm.Type
		if memType == "" {
			memType = h.classifier.Classify(cm.Content)
		}

		// 3. Embed the content.
		vec, err := h.embedder.Embed(ctx, cm.Content)
		if err != nil {
			h.logger.Warn("post-turn embed failed, skipping memory", "error", err)
			continue
		}

		// 4. Dedup – skip if a near-duplicate already exists.
		dupes, err := h.store.FindDuplicates(ctx, vec, dedupThreshold)
		if err != nil {
			h.logger.Warn("post-turn dedup check failed, proceeding with store", "error", err)
		} else if len(dupes) > 0 {
			h.logger.Debug("post-turn skipping duplicate", "similar_to", dupes[0].Memory.ID)
			continue
		}

		// 5. Store the new memory.
		mem := models.Memory{
			ID:         uuid.New().String(),
			Type:       memType,
			Scope:      models.ScopeSession,
			Visibility: models.VisibilityPrivate,
			Content:    cm.Content,
			Confidence: cm.Confidence,
			Tags:       cm.Tags,
			Source:     "post-turn-hook",
			Project:    input.Project,
			CreatedAt:    now,
			UpdatedAt:    now,
			LastAccessed: now,
		}

		if err := h.store.Upsert(ctx, mem, vec); err != nil {
			h.logger.Warn("post-turn store failed", "error", err)
			continue
		}
		stored++
	}

	h.logger.Info("post-turn hook completed", "extracted", len(captured), "stored", stored)
	return nil
}
