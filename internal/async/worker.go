package async

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

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
	wg         sync.WaitGroup
	cancel     context.CancelFunc
	logger     *slog.Logger
}

// NewPool creates a Pool that reads from queue, dispatches each item to
// processor, and spawns workers goroutines when Start is called.
// maxRetries controls how many times a failing item is re-enqueued before it
// is permanently marked failed (pass 0 to use the package default of 3).
func NewPool(queue *Queue, processor Processor, workers int, maxRetries int, logger *slog.Logger) *Pool {
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
		logger:     logger,
	}
}

// Start spawns workers goroutines.  Each goroutine reads from the queue channel
// until it is closed or the context is canceled.  Start returns immediately;
// use Shutdown to wait for all goroutines to finish.
func (p *Pool) Start(ctx context.Context) {
	workerCtx, cancel := context.WithCancel(ctx)
	p.cancel = cancel

	for range p.workers {
		p.wg.Add(1)
		go p.runWorker(workerCtx)
	}
}

// Shutdown cancels the worker context and waits for all goroutines to exit.
// If the provided ctx expires before all workers drain, the context error is
// returned and any remaining in-flight work is abandoned (items remain durable
// in the WAL for the next startup replay).
func (p *Pool) Shutdown(ctx context.Context) error {
	if p.cancel != nil {
		p.cancel()
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
func (p *Pool) runWorker(ctx context.Context) {
	defer p.wg.Done()

	// pending holds items that failed and need to be retried within this
	// worker's lifetime.  We store them in a local map keyed by ID so a
	// re-dequeue from the channel can increment the attempt counter.
	attempts := make(map[string]int)

	for {
		select {
		case <-ctx.Done():
			return
		case item, ok := <-p.queue.C():
			if !ok {
				return
			}
			attempts[item.ID]++
			currentAttempt := attempts[item.ID]

			metrics.AsyncInFlight.Add(1)
			processErr := p.processor.Process(ctx, item)
			metrics.AsyncInFlight.Add(-1)

			if processErr == nil {
				p.queue.Complete(item.ID)
				metrics.AsyncProcessedTotal.Add(1)
				delete(attempts, item.ID)
				continue
			}

			// Processing failed.
			if currentAttempt >= p.maxRetries {
				// Exhausted retries — permanently fail the item.
				p.logger.Warn("async: item failed permanently",
					"id", item.ID,
					"memory_id", item.MemoryID,
					"attempts", currentAttempt,
					"error", processErr)
				p.queue.Fail(item.ID, processErr)
				metrics.AsyncFailedTotal.Add(1)
				delete(attempts, item.ID)
				continue
			}

			// Retry: re-enqueue the same item (preserving its ID so the WAL
			// tombstone from Fail/Complete targets the right entry).
			p.logger.Warn("async: item processing failed, will retry",
				"id", item.ID,
				"memory_id", item.MemoryID,
				"attempt", currentAttempt,
				"max_retries", p.maxRetries,
				"error", processErr)

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

	return nil
}
