package hooks

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"

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
	start := time.Now()
	finish := sentry.StartSpan(ctx, "hook.pre_turn", "PreTurnHook")
	defer finish()
	defer func() { metrics.RecallLatencyMs.Observe(float64(time.Since(start).Milliseconds())) }()
	metrics.RecallsTotal.Inc()

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
// dedupThreshold is the cosine similarity threshold above which a memory is considered a duplicate.
// A value of 0.95 is recommended for hook-captured memories to avoid false-positive dedup.
func NewPostTurnHook(cap capture.Capturer, cls classifier.Classifier, emb embedder.Embedder, st store.Store, logger *slog.Logger, dedupThreshold float64) *PostTurnHook {
	return &PostTurnHook{
		capturer:       cap,
		classifier:     cls,
		embedder:       emb,
		store:          st,
		logger:         logger,
		dedupThreshold: dedupThreshold,
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

	now := time.Now().UTC()
	stored := 0

	for i := range captured {
		cm := captured[i]
		// 2. Classify – prefer the LLM-assigned type; only run the classifier if empty.
		memType := cm.Type
		if memType == "" {
			memType = h.classifier.Classify(cm.Content)
		}

		// 3. Embed the content.
		vec, embedErr := h.embedder.Embed(ctx, cm.Content)
		if embedErr != nil {
			h.logger.Warn("post-turn embed failed, skipping memory", "error", embedErr)
			continue
		}

		// 4. Reinforcement: boost confidence of near-duplicate existing memories instead of storing new.
		if h.reinforcementThreshold > 0 {
			nearDups, nearErr := h.store.FindDuplicates(ctx, vec, h.reinforcementThreshold)
			if nearErr == nil && len(nearDups) > 0 {
				top := nearDups[0]
				if top.Score < h.dedupThreshold {
					// Near-duplicate (not exact): reinforce instead of store.
					if boostErr := h.store.UpdateReinforcement(ctx, top.Memory.ID, h.reinforcementBoost); boostErr != nil {
						h.logger.Warn("post-turn: reinforcement update failed", "id", top.Memory.ID, "error", boostErr)
					} else {
						h.logger.Info("post-turn: reinforced existing memory",
							"id", top.Memory.ID, "similarity", top.Score,
							"boost", h.reinforcementBoost)
						continue // Don't store new memory
					}
				}
			}
		}

		// 5. Dedup – skip if an exact duplicate already exists.
		dupes, dedupErr := h.store.FindDuplicates(ctx, vec, h.dedupThreshold)
		if dedupErr != nil {
			h.logger.Warn("post-turn dedup check failed, proceeding with store", "error", dedupErr)
		} else if len(dupes) > 0 {
			h.logger.Debug("post-turn skipping duplicate", "similar_to", dupes[0].Memory.ID)
			metrics.DedupSkippedTotal.Inc()
			continue
		}

		// 6. Contradiction detection — only when a ConflictDetector is configured.
		var conflictGroupID string
		if h.conflictDetector != nil {
			candidates, searchErr := h.store.Search(ctx, vec, conflictCandidateLimit, nil)
			if searchErr != nil {
				h.logger.Warn("post-turn conflict search failed", "error", searchErr)
			} else {
				mems := make([]models.Memory, len(candidates))
				for j := range candidates {
					mems[j] = candidates[j].Memory
				}
				contradicts, contradictedID, reason, _ := h.conflictDetector.Detect(ctx, cm.Content, mems)
				if contradicts && contradictedID != "" {
					groupID := uuid.New().String()
					conflictGroupID = groupID
					h.logger.Info("conflict detected: tagging both memories",
						"new_content", cm.Content[:minLen(50, len(cm.Content))],
						"contradicted_id", contradictedID,
						"group_id", groupID,
						"reason", reason,
					)
					if tagErr := h.store.UpdateConflictFields(ctx, contradictedID, groupID, "active"); tagErr != nil {
						h.logger.Warn("failed to tag contradicted memory", "id", contradictedID, "error", tagErr)
					}
				}
			}
		}

		// 7. Store the new memory.
		mem := models.Memory{
			ID:              uuid.New().String(),
			Type:            memType,
			Scope:           models.ScopeSession,
			Visibility:      models.VisibilityPrivate,
			Content:         cm.Content,
			Confidence:      cm.Confidence,
			Tags:            cm.Tags,
			Source:          "post-turn-hook",
			Project:         input.Project,
			CreatedAt:       now,
			UpdatedAt:       now,
			LastAccessed:    now,
			ConflictGroupID: conflictGroupID,
			ConflictStatus: func() models.ConflictStatus {
				if conflictGroupID != "" {
					return models.ConflictStatusActive
				}
				return models.ConflictStatusNone
			}(),
		}

		if upsertErr := h.store.Upsert(ctx, mem, vec); upsertErr != nil {
			h.logger.Warn("post-turn store failed", "error", upsertErr)
			continue
		}
		metrics.MemoriesStoredTotal.With(prometheus.Labels{"source": "hook"}).Inc()
		stored++
	}

	h.logger.Info("post-turn hook completed", "extracted", len(captured), "stored", stored)

	// Update memory_count gauge with the current total after writes.
	if stored > 0 {
		if stats, statsErr := h.store.Stats(ctx); statsErr == nil {
			metrics.MemoryCount.Set(float64(stats.TotalMemories))
		} else {
			h.logger.Warn("post-turn hook: Stats failed, memory_count gauge not updated", "error", statsErr)
		}
	}

	return nil
}
