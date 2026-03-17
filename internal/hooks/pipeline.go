package hooks

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/ajitpratap0/openclaw-cortex/internal/capture"
	"github.com/ajitpratap0/openclaw-cortex/internal/classifier"
	"github.com/ajitpratap0/openclaw-cortex/internal/embedder"
	"github.com/ajitpratap0/openclaw-cortex/internal/metrics"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

// errSkipped is a sentinel returned by processSingleMemory when a memory is
// intentionally not stored (embed failure, dedup, reinforcement). It is
// never propagated beyond runMemoryPipeline.
var errSkipped = errors.New("memory skipped")

// pipelineDeps groups the dependencies required by runMemoryPipeline and
// processSingleMemory. It is intentionally not exported; callers use
// PostTurnHook which builds this struct before delegating to runMemoryPipeline.
type pipelineDeps struct {
	classifier             classifier.Classifier
	embedder               embedder.Embedder
	store                  store.Store
	conflictDetector       *capture.ConflictDetector
	dedupThreshold         float64
	reinforcementThreshold float64
	reinforcementBoost     float64
	project                string
}

// runMemoryPipeline processes captured memories concurrently using a
// semaphore-bounded goroutine pool backed by golang.org/x/sync/errgroup.
//
// Concurrency rules:
//   - concurrency <= 0  → clamped to 4 (default)
//   - concurrency > 16  → clamped to 16 (max)
//
// Per-memory errors (embed failures, store failures) are logged as warnings
// and the memory is skipped; they do not abort the pipeline.
// Hard errors (context.Canceled, context.DeadlineExceeded) are propagated
// upward so Execute can return them to the caller.
//
// Returns the count of successfully stored memories and any hard error.
func runMemoryPipeline(ctx context.Context, memories []models.CapturedMemory, concurrency int, deps pipelineDeps, logger *slog.Logger) (int, error) {
	if concurrency <= 0 {
		concurrency = 4
	}
	if concurrency > 16 {
		concurrency = 16
	}

	sem := make(chan struct{}, concurrency)
	eg, egCtx := errgroup.WithContext(ctx)
	var stored atomic.Int64

	for i := range memories {
		mem := memories[i]
		sem <- struct{}{}
		eg.Go(func() error {
			defer func() { <-sem }()
			processErr := processSingleMemory(egCtx, mem, deps, logger)
			if processErr == nil {
				stored.Add(1)
				return nil
			}
			if errors.Is(processErr, errSkipped) {
				return nil // soft skip: not an error
			}
			if errors.Is(processErr, context.Canceled) || errors.Is(processErr, context.DeadlineExceeded) {
				return processErr // propagate hard errors
			}
			logger.Warn("pipeline: skipping memory", "error", processErr)
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return int(stored.Load()), err
	}
	return int(stored.Load()), nil
}

// processSingleMemory runs the classify→embed→reinforce→dedup→conflict→store
// pipeline for one captured memory. Returns nil if the memory was stored or
// intentionally skipped (dedup, reinforcement). Returns a non-nil error only
// for hard failures (context cancellation or unrecoverable store errors).
func processSingleMemory(ctx context.Context, cm models.CapturedMemory, deps pipelineDeps, logger *slog.Logger) error {
	now := time.Now().UTC()

	// Classify — prefer the LLM-assigned type; fall back to heuristic classifier.
	memType := cm.Type
	if memType == "" {
		memType = deps.classifier.Classify(cm.Content)
	}

	// Embed the content.
	vec, embedErr := deps.embedder.Embed(ctx, cm.Content)
	if embedErr != nil {
		if errors.Is(embedErr, context.Canceled) || errors.Is(embedErr, context.DeadlineExceeded) {
			return embedErr
		}
		logger.Warn("post-turn embed failed, skipping memory", "error", embedErr)
		return errSkipped
	}

	// Reinforcement: boost confidence of near-duplicate existing memories
	// instead of storing a new one.
	if deps.reinforcementThreshold > 0 {
		nearDups, nearErr := deps.store.FindDuplicates(ctx, vec, deps.reinforcementThreshold)
		if nearErr == nil && len(nearDups) > 0 {
			top := nearDups[0]
			if top.Score < deps.dedupThreshold {
				if boostErr := deps.store.UpdateReinforcement(ctx, top.Memory.ID, deps.reinforcementBoost); boostErr != nil {
					logger.Warn("post-turn: reinforcement update failed", "id", top.Memory.ID, "error", boostErr)
				} else {
					logger.Info("post-turn: reinforced existing memory",
						"id", top.Memory.ID, "similarity", top.Score)
					return errSkipped
				}
			}
		}
	}

	// Dedup — skip if an exact duplicate already exists.
	dupes, dedupErr := deps.store.FindDuplicates(ctx, vec, deps.dedupThreshold)
	if dedupErr != nil {
		logger.Warn("post-turn dedup check failed, proceeding with store", "error", dedupErr)
	} else if len(dupes) > 0 {
		logger.Debug("post-turn skipping duplicate", "similar_to", dupes[0].Memory.ID)
		metrics.Inc(metrics.DedupSkipped)
		return errSkipped
	}

	// Contradiction detection — only when a ConflictDetector is configured.
	var conflictGroupID string
	if deps.conflictDetector != nil {
		candidates, searchErr := deps.store.Search(ctx, vec, conflictCandidateLimit, nil)
		if searchErr != nil {
			logger.Warn("post-turn conflict search failed", "error", searchErr)
		} else {
			mems := make([]models.Memory, len(candidates))
			for j := range candidates {
				mems[j] = candidates[j].Memory
			}
			contradicts, contradictedID, reason, _ := deps.conflictDetector.Detect(ctx, cm.Content, mems)
			if contradicts && contradictedID != "" {
				groupID := uuid.New().String()
				conflictGroupID = groupID
				logger.Info("conflict detected: tagging both memories",
					"new_content", cm.Content[:minLen(50, len(cm.Content))],
					"contradicted_id", contradictedID,
					"group_id", groupID,
					"reason", reason,
				)
				if tagErr := deps.store.UpdateConflictFields(ctx, contradictedID, groupID, "active"); tagErr != nil {
					logger.Warn("failed to tag contradicted memory", "id", contradictedID, "error", tagErr)
				}
			}
		}
	}

	// Store the new memory.
	conflictStatus := models.ConflictStatusNone
	if conflictGroupID != "" {
		conflictStatus = models.ConflictStatusActive
	}
	mem := models.Memory{
		ID:              uuid.New().String(),
		Type:            memType,
		Scope:           models.ScopeSession,
		Visibility:      models.VisibilityPrivate,
		Content:         cm.Content,
		Confidence:      cm.Confidence,
		Tags:            cm.Tags,
		Source:          "post-turn-hook",
		Project:         deps.project,
		CreatedAt:       now,
		UpdatedAt:       now,
		LastAccessed:    now,
		ConflictGroupID: conflictGroupID,
		ConflictStatus:  conflictStatus,
	}

	if upsertErr := deps.store.Upsert(ctx, mem, vec); upsertErr != nil {
		logger.Warn("post-turn store failed", "error", upsertErr)
		return errSkipped
	}
	metrics.Inc(metrics.CaptureTotal)
	metrics.Inc(metrics.StoreTotal)
	return nil
}
