package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/ajitpratap0/openclaw-cortex/internal/async"
	"github.com/ajitpratap0/openclaw-cortex/internal/config"
	"github.com/ajitpratap0/openclaw-cortex/internal/llm"
	"github.com/ajitpratap0/openclaw-cortex/internal/memgraph"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/recall"
	"github.com/ajitpratap0/openclaw-cortex/internal/sentry"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

// asyncQueue is the global async work queue. Initialized in initAsyncQueue,
// shutdown in shutdownAsyncQueue. nil when cfg.Async.Disabled is true.
var asyncQueue async.Enqueuer

// cmdErr wraps an error with context and reports it to Sentry.
// Returns nil if err is nil.
func cmdErr(context string, err error) error {
	if err == nil {
		return nil
	}
	wrapped := fmt.Errorf("%s: %w", context, err)
	sentry.CaptureException(wrapped)
	return wrapped
}

// recallWeightsFromConfig converts the config weights struct into the recall
// package's Weights type, avoiding an 8-line struct literal duplicated across
// every command that creates a Recaller.
func recallWeightsFromConfig(c config.RecallWeightsConfig) recall.Weights {
	return recall.Weights{
		Similarity:     c.Similarity,
		Recency:        c.Recency,
		Frequency:      c.Frequency,
		TypeBoost:      c.TypeBoost,
		ScopeBoost:     c.ScopeBoost,
		Confidence:     c.Confidence,
		Reinforcement:  c.Reinforcement,
		TagAffinity:    c.TagAffinity,
		GraphProximity: c.GraphProximity,
	}
}

// buildSearchFilters constructs a SearchFilters from optional CLI flag values.
// Returns nil if all inputs are empty.
func buildSearchFilters(cmdName, memType, memScope, project, tagsFlag string) (*store.SearchFilters, error) {
	if memType == "" && memScope == "" && project == "" && tagsFlag == "" {
		return nil, nil
	}
	filters := &store.SearchFilters{}
	if memType != "" {
		mt := models.MemoryType(memType)
		if !mt.IsValid() {
			return nil, fmt.Errorf("%s: invalid type %q (want: %s)", cmdName, memType, validTypesString())
		}
		filters.Type = &mt
	}
	if memScope != "" {
		ms := models.MemoryScope(memScope)
		if !ms.IsValid() {
			return nil, fmt.Errorf("%s: invalid scope %q (want: %s)", cmdName, memScope, validScopesString())
		}
		filters.Scope = &ms
	}
	if project != "" {
		filters.Project = &project
	}
	if tagsFlag != "" {
		filters.Tags = parseTags(tagsFlag)
	}
	return filters, nil
}

// parseTags splits a comma-separated tags string into trimmed individual tags.
func parseTags(tagsStr string) []string {
	parts := strings.Split(tagsStr, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// initAsyncQueue creates and starts the async graph pipeline pool.
// Returns (nil, nil) when cfg.Async.Disabled is true.
// The caller is responsible for calling pool.Shutdown(ctx) when done.
func initAsyncQueue(ctx context.Context, c *config.Config, logger *slog.Logger) (*async.Pool, error) {
	if c.Async.Disabled {
		return nil, nil
	}

	// Resolve WAL path: explicit config or default under ~/.openclaw/
	walPath := c.Async.WALPath
	if walPath == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("initAsyncQueue: resolve home dir: %w", err)
		}
		walPath = filepath.Join(homeDir, ".openclaw", "graph_wal.jsonl")
	}

	// Ensure the parent directory exists.
	if err := os.MkdirAll(filepath.Dir(walPath), 0o700); err != nil {
		return nil, fmt.Errorf("initAsyncQueue: create WAL dir: %w", err)
	}

	q, err := async.NewQueue(walPath, c.Async.QueueCapacity, c.Async.WALCompactEvery)
	if err != nil {
		return nil, fmt.Errorf("initAsyncQueue: new queue: %w", err)
	}

	// Build the GraphProcessor dependencies from cfg.
	emb := newEmbedder(logger)

	st, stErr := newMemgraphStore(ctx, logger)
	if stErr != nil {
		return nil, fmt.Errorf("initAsyncQueue: connecting to store: %w", stErr)
	}

	gc := memgraph.NewGraphAdapter(st)
	lc := llm.NewClient(c.Claude)

	gp := async.NewGraphProcessor(st, gc, emb, lc, c.Claude.Model, logger)
	pool := async.NewPool(q, gp, c.Async.WorkerCount, c.Async.MaxRetries, logger)
	pool.Start(ctx)

	asyncQueue = q
	return pool, nil
}
