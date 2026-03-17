package hooks

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ajitpratap0/openclaw-cortex/internal/capture"
	"github.com/ajitpratap0/openclaw-cortex/internal/classifier"
	"github.com/ajitpratap0/openclaw-cortex/internal/embedder"
	"github.com/ajitpratap0/openclaw-cortex/internal/metrics"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/recall"
	"github.com/ajitpratap0/openclaw-cortex/internal/sentry"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
	"github.com/ajitpratap0/openclaw-cortex/pkg/tokenizer"
)

// RerankConfig holds re-ranking thresholds and latency budget for the hook.
type RerankConfig struct {
	ScoreSpreadThreshold float64
	LatencyBudgetMs      int
}

// preTurnSearchLimit is the maximum number of candidate memories retrieved during pre-turn search.
const preTurnSearchLimit = 50

// minLen returns the smaller of a and b.
func minLen(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// PreTurnHook retrieves relevant memories before an agent turn.
type PreTurnHook struct {
	embedder  embedder.Embedder
	store     store.Store
	recaller  *recall.Recaller
	reasoner  *recall.Reasoner // nil = disabled
	rerankCfg RerankConfig
	logger    *slog.Logger
}

// PreTurnInput contains the context for a pre-turn hook.
type PreTurnInput struct {
	Message     string `json:"message"`
	Project     string `json:"project"`
	TokenBudget int    `json:"token_budget"`
	SessionID   string `json:"session_id"`
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

// WithReasoner attaches an optional Reasoner for threshold-gated re-ranking.
// Must be called before the hook is used concurrently.
func (h *PreTurnHook) WithReasoner(r *recall.Reasoner, cfg RerankConfig) *PreTurnHook {
	h.reasoner = r
	h.rerankCfg = cfg
	return h
}

// Execute runs the pre-turn hook.
func (h *PreTurnHook) Execute(ctx context.Context, input PreTurnInput) (*PreTurnOutput, error) {
	finish := sentry.StartSpan(ctx, "hook.pre_turn", "PreTurnHook")
	defer finish()
	metrics.Inc(metrics.RecallTotal)

	if input.TokenBudget <= 0 {
		input.TokenBudget = 2000
	}

	// Embed the current message
	vec, err := h.embedder.Embed(ctx, input.Message)
	if err != nil {
		return nil, fmt.Errorf("embedding message: %w", err)
	}

	// Search for relevant memories — filter by project to prevent cross-project leakage.
	var filter *store.SearchFilters
	if input.Project != "" {
		filter = &store.SearchFilters{Project: &input.Project}
	}
	results, err := h.store.Search(ctx, vec, preTurnSearchLimit, filter)
	if err != nil {
		return nil, fmt.Errorf("searching memories: %w", err)
	}

	// Rank with multi-factor scoring
	ranked := h.recaller.Rank(results, input.Project, input.Message)

	// Optionally re-rank with Claude when scores are clustered.
	if h.reasoner != nil && h.recaller.ShouldRerank(ranked, h.rerankCfg.ScoreSpreadThreshold) {
		budget := time.Duration(h.rerankCfg.LatencyBudgetMs) * time.Millisecond
		rerankCtx, cancel := context.WithTimeout(ctx, budget)
		defer cancel()
		reranked, rerankErr := h.reasoner.ReRank(rerankCtx, input.Message, ranked, 0)
		if rerankErr != nil {
			h.logger.Warn("pre-turn hook: re-rank timed out or failed, using original order", "error", rerankErr)
		} else {
			ranked = reranked
		}
	}

	// Format within token budget
	var contents []string
	for i := range ranked {
		contents = append(contents, ranked[i].Memory.Content)
	}

	formatted, count := tokenizer.FormatMemoriesWithBudget(contents, input.TokenBudget)

	// Update access metadata
	for i := 0; i < count && i < len(ranked); i++ {
		if updateErr := h.store.UpdateAccessMetadata(ctx, ranked[i].Memory.ID); updateErr != nil {
			h.logger.Warn("PreTurnHook: UpdateAccessMetadata failed",
				"id", ranked[i].Memory.ID, "error", updateErr)
		}
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

// conflictCandidateLimit is the maximum number of similar memories passed to the
// ConflictDetector for contradiction analysis.
const conflictCandidateLimit = 5

// PostTurnHook captures memories from a completed agent turn.
type PostTurnHook struct {
	capturer               capture.Capturer
	classifier             classifier.Classifier
	embedder               embedder.Embedder
	store                  store.Store
	logger                 *slog.Logger
	dedupThreshold         float64
	conflictDetector       *capture.ConflictDetector // nil = disabled
	reinforcementThreshold float64                   // 0 = disabled
	reinforcementBoost     float64
	concurrency            int // number of goroutines for per-memory pipeline; 0 = default (4)
}

// PostTurnInput contains the conversation turn data.
type PostTurnInput struct {
	UserMessage      string                     `json:"user_message"`
	AssistantMessage string                     `json:"assistant_message"`
	SessionID        string                     `json:"session_id"`
	Project          string                     `json:"project"`
	PriorTurns       []capture.ConversationTurn `json:"prior_turns,omitempty"`
}

// NewPostTurnHook creates a post-turn hook handler.
// dedupThreshold is the cosine similarity threshold above which a memory is
// considered a duplicate. A value of 0.95 is recommended.
// concurrency controls the number of memories processed simultaneously in
// Execute; 0 falls back to the default of 4; values above 16 are clamped to 16.
func NewPostTurnHook(cap capture.Capturer, cls classifier.Classifier, emb embedder.Embedder, st store.Store, logger *slog.Logger, dedupThreshold float64, concurrency int) *PostTurnHook {
	return &PostTurnHook{
		capturer:       cap,
		classifier:     cls,
		embedder:       emb,
		store:          st,
		logger:         logger,
		dedupThreshold: dedupThreshold,
		concurrency:    concurrency,
	}
}

// WithConflictDetector configures an optional ConflictDetector.
// Must be called before the hook is used concurrently.
// When set, each new memory is checked against similar existing memories for
// contradictions before being stored. On any detector error the memory is stored
// as-is (graceful degradation).
func (h *PostTurnHook) WithConflictDetector(cd *capture.ConflictDetector) *PostTurnHook {
	h.conflictDetector = cd
	return h
}

// WithReinforcement configures confidence reinforcement for near-duplicate memories.
// threshold is the cosine similarity above which a near-duplicate triggers reinforcement.
// boost is added to the existing memory's confidence (capped at 1.0).
// Set threshold to 0 to disable reinforcement.
func (h *PostTurnHook) WithReinforcement(threshold, boost float64) *PostTurnHook {
	h.reinforcementThreshold = threshold
	h.reinforcementBoost = boost
	return h
}

// Execute runs the post-turn hook: extract → classify → embed → reinforce/dedup → store.
func (h *PostTurnHook) Execute(ctx context.Context, input PostTurnInput) error {
	finish := sentry.StartSpan(ctx, "hook.post_turn", "PostTurnHook")
	defer finish()
	h.logger.Info("post-turn hook starting",
		"session_id", input.SessionID,
		"project", input.Project,
		"user_msg_len", len(input.UserMessage),
		"assistant_msg_len", len(input.AssistantMessage),
	)

	// 1. Extract candidate memories from the conversation turn (with prior turns if available).
	captured, err := h.capturer.ExtractWithContext(ctx, input.UserMessage, input.AssistantMessage, input.PriorTurns)
	if err != nil {
		return fmt.Errorf("post-turn extract: %w", err)
	}
	if len(captured) == 0 {
		h.logger.Debug("post-turn hook: no memories extracted")
		return nil
	}

	deps := pipelineDeps{
		classifier:             h.classifier,
		embedder:               h.embedder,
		store:                  h.store,
		conflictDetector:       h.conflictDetector,
		dedupThreshold:         h.dedupThreshold,
		reinforcementThreshold: h.reinforcementThreshold,
		reinforcementBoost:     h.reinforcementBoost,
		project:                input.Project,
	}
	stored, pipelineErr := runMemoryPipeline(ctx, captured, h.concurrency, deps, h.logger)
	h.logger.Info("post-turn hook completed", "extracted", len(captured), "stored", stored)
	if pipelineErr != nil {
		return fmt.Errorf("post-turn pipeline: %w", pipelineErr)
	}
	return nil
}
