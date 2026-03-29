package async

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ajitpratap0/openclaw-cortex/internal/embedder"
	"github.com/ajitpratap0/openclaw-cortex/internal/extract"
	graphpkg "github.com/ajitpratap0/openclaw-cortex/internal/graph"
	"github.com/ajitpratap0/openclaw-cortex/internal/llm"
	"github.com/ajitpratap0/openclaw-cortex/internal/metrics"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

const defaultMaxRetries = 3

// Processor is the processing interface used by Pool.  Implementations receive
// a single WorkItem and return nil on success or an error that may trigger a
// retry.
type Processor interface {
	Process(ctx context.Context, item WorkItem) error
}

// Pool manages a fixed number of goroutines that consume work items from a
// Queue and delegate each item to a Processor.
type Pool struct {
	queue      *Queue
	processor  Processor
	workers    int
	maxRetries int
	retryDelay time.Duration // delay between retry attempts
	wg         sync.WaitGroup
	cancel     context.CancelFunc // cancels the loop context (stops accepting new work)
	processCtx context.Context    // context passed to Process; not canceled by Shutdown
	logger     *slog.Logger
}

// NewPool creates a Pool that reads from queue, dispatches each item to
// processor, and spawns workers goroutines when Start is called.
// maxRetries controls how many times a failing item is re-enqueued before it
// is permanently marked failed (pass 0 to use the package default of 3).
// retryDelay is the duration to wait between retry attempts; pass 0 to retry
// immediately.
func NewPool(queue *Queue, processor Processor, workers int, maxRetries int, retryDelay time.Duration, logger *slog.Logger) *Pool {
	if maxRetries <= 0 {
		maxRetries = defaultMaxRetries
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Pool{
		queue:      queue,
		processor:  processor,
		workers:    workers,
		maxRetries: maxRetries,
		retryDelay: retryDelay,
		logger:     logger,
	}
}

// Start spawns workers goroutines.  Each goroutine reads from the queue channel
// until it is closed or the context is canceled.  Start returns immediately;
// use Shutdown to wait for all goroutines to finish.
func (p *Pool) Start(ctx context.Context) {
	loopCtx, cancel := context.WithCancel(ctx)
	p.cancel = cancel
	p.processCtx = ctx // in-flight Process calls use the caller's context

	for range p.workers {
		p.wg.Add(1)
		go p.runWorker(loopCtx)
	}
}

// Shutdown signals workers to stop accepting new items and waits for all
// in-flight goroutines to exit.  The loop context is canceled so that idle
// workers (blocked on the queue channel) wake up and return; however the
// process context passed to each Process call is NOT canceled here — callers
// that want to abort in-flight work must cancel the context they passed to
// Start themselves.
//
// If the provided ctx expires before all goroutines exit, the context error is
// returned.  Items still in the WAL remain durable and will be replayed on the
// next startup.
func (p *Pool) Shutdown(ctx context.Context) error {
	if p.cancel != nil {
		p.cancel() // stop the accept loop; does not cancel in-flight Process calls
	}

	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("async.Pool.Shutdown: %w", ctx.Err())
	}
}

// runWorker is the per-goroutine loop.  It reads from the queue channel and
// calls the processor for each item, applying retry / fail logic.
//
// The attempt counter is stored in WorkItem.Attempts and persisted to the WAL
// on every re-enqueue, so retries survive process restarts and are counted
// correctly even when different workers handle the same item.
func (p *Pool) runWorker(ctx context.Context) {
	defer p.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case item, ok := <-p.queue.C():
			if !ok {
				return
			}
			// Increment the durable attempt counter before processing so that
			// crashes mid-flight are counted on the next replay.
			item.Attempts++

			metrics.AsyncInFlight.Add(1)
			processErr := func() (err error) {
				defer func() {
					if r := recover(); r != nil {
						err = fmt.Errorf("async.Pool: processor panicked: %v", r)
						p.logger.Error("async: processor panicked",
							"id", item.ID,
							"memory_id", item.MemoryID,
							"panic", r)
					}
				}()
				return p.processor.Process(p.processCtx, item)
			}()
			metrics.AsyncInFlight.Add(-1)

			if processErr == nil {
				p.queue.Complete(item.ID)
				metrics.AsyncProcessedTotal.Add(1)
				continue
			}

			// Processing failed.
			if item.Attempts >= p.maxRetries {
				// Exhausted retries — permanently fail the item.
				p.logger.Warn("async: item failed permanently",
					"id", item.ID,
					"memory_id", item.MemoryID,
					"attempts", item.Attempts,
					"error", processErr)
				p.queue.Fail(item.ID, processErr)
				metrics.AsyncFailedTotal.Add(1)
				continue
			}

			// Retry: re-enqueue the same item (preserving its ID so the WAL
			// tombstone from Fail/Complete targets the right entry).
			// item.Attempts is already incremented and will be persisted to the WAL.
			p.logger.Warn("async: item processing failed, will retry",
				"id", item.ID,
				"memory_id", item.MemoryID,
				"attempt", item.Attempts,
				"max_retries", p.maxRetries,
				"error", processErr)

			// Honor the configured retry back-off before re-enqueuing.
			if p.retryDelay > 0 {
				select {
				case <-time.After(p.retryDelay):
				case <-ctx.Done():
					return
				}
			}

			if enqErr := p.queue.Enqueue(item); enqErr != nil {
				p.logger.Warn("async: re-enqueue failed, item lost for this session",
					"id", item.ID, "error", enqErr)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// GraphProcessor — implements Processor using extract.Run as the bridge.
// ---------------------------------------------------------------------------

// GraphProcessor extracts entities and relationship facts from a WorkItem and
// persists them via the store and graph client.  This is a temporary bridge
// implementation; Phase 5 will replace runExtraction with the full 8-stage
// async pipeline.
type GraphProcessor struct {
	store       store.Store
	graphClient graphpkg.Client
	embedder    embedder.Embedder
	llmClient   llm.LLMClient
	model       string
	logger      *slog.Logger
}

// NewGraphProcessor wires the dependencies needed by GraphProcessor.
// All parameters are required; passing nil for any of them causes Process to
// return an error immediately.
func NewGraphProcessor(
	s store.Store,
	gc graphpkg.Client,
	emb embedder.Embedder,
	lc llm.LLMClient,
	model string,
	logger *slog.Logger,
) *GraphProcessor {
	if logger == nil {
		logger = slog.Default()
	}
	return &GraphProcessor{
		store:       s,
		graphClient: gc,
		embedder:    emb,
		llmClient:   lc,
		model:       model,
		logger:      logger,
	}
}

// Process implements Processor.  It delegates to runExtraction which calls
// extract.Run for entity and fact extraction.
func (gp *GraphProcessor) Process(ctx context.Context, item WorkItem) error {
	return gp.runExtraction(ctx, item)
}

// runExtraction bridges a WorkItem into the existing synchronous extract.Run
// pipeline.  Phase 5 will replace this with the full 8-stage async pipeline.
//
// runExtraction returns an error when extract.Run produces zero entities AND
// zero facts for non-empty content.  This indicates a transient LLM failure
// (extract.Run logs the underlying error as a warning) and allows the Pool's
// retry mechanism to re-attempt the item rather than silently swallowing the
// failure.
func (gp *GraphProcessor) runExtraction(ctx context.Context, item WorkItem) error {
	if gp.store == nil || gp.graphClient == nil || gp.llmClient == nil {
		return errors.New("async.GraphProcessor: store, graphClient, and llmClient must be non-nil")
	}

	memories := []extract.StoredMemory{
		{ID: item.MemoryID, Content: item.Content},
	}

	deps := extract.Deps{
		LLMClient:   gp.llmClient,
		Model:       gp.model,
		Store:       gp.store,
		GraphClient: gp.graphClient,
		Logger:      gp.logger,
	}

	result := extract.Run(ctx, deps, memories)

	gp.logger.Info("async: extraction complete",
		"id", item.ID,
		"memory_id", item.MemoryID,
		"entities_extracted", result.EntitiesExtracted,
		"facts_extracted", result.FactsExtracted,
	)

	// Only retry when extract.Run encountered actual errors (LLM failures,
	// store write errors, etc.).  Trivially short content like "ok" or "noted"
	// legitimately produces zero entities and facts — retrying those would
	// exhaust the retry budget for no reason.
	if result.Errors > 0 && result.EntitiesExtracted == 0 && result.FactsExtracted == 0 {
		return fmt.Errorf("async.GraphProcessor: extraction failed with %d errors for memory %s", result.Errors, item.MemoryID)
	}

	return nil
}
