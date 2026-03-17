package llm

import (
	"context"
	"errors"
	"math/rand"
	"time"

	"github.com/sony/gobreaker"
)

// ResilientClient wraps any LLMClient with a circuit breaker, exponential
// backoff retry, and a worker pool concurrency cap.
type ResilientClient struct {
	inner   LLMClient
	breaker *gobreaker.CircuitBreaker
	sem     chan struct{}
	retries int
}

// NewResilientClient wraps inner with resilience controls.
//
//   - maxConcurrent:      maximum simultaneous in-flight LLM calls (worker pool size)
//   - cbFailureThreshold: consecutive failures before the circuit opens
//   - cbRecoverySeconds:  seconds the circuit stays open before allowing a probe
//   - maxRetries:         maximum additional attempts per call (0 = try once, no retry)
func NewResilientClient(
	inner LLMClient,
	maxConcurrent, cbFailureThreshold, cbRecoverySeconds, maxRetries int,
) *ResilientClient {
	settings := gobreaker.Settings{
		Name:        "llm-circuit-breaker",
		MaxRequests: 1, // half-open: allow exactly 1 probe request
		Interval:    0, // never reset counts in the closed state
		Timeout:     time.Duration(cbRecoverySeconds) * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= uint32(cbFailureThreshold)
		},
		OnStateChange: func(name string, from, to gobreaker.State) {
			// Extend with slog/metrics when available.
			_ = name
			_ = from
			_ = to
		},
	}
	return &ResilientClient{
		inner:   inner,
		breaker: gobreaker.NewCircuitBreaker(settings),
		sem:     make(chan struct{}, maxConcurrent),
		retries: maxRetries,
	}
}

// Complete satisfies the LLMClient interface.
// It acquires a worker pool slot, then delegates to the circuit breaker which
// wraps the retry loop.
func (r *ResilientClient) Complete(
	ctx context.Context,
	model, systemPrompt, userMessage string,
	maxTokens int,
) (string, error) {
	// Acquire worker pool slot or respect context cancellation.
	select {
	case r.sem <- struct{}{}:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	defer func() { <-r.sem }()

	raw, err := r.breaker.Execute(func() (interface{}, error) {
		return r.completeWithRetry(ctx, model, systemPrompt, userMessage, maxTokens)
	})
	if err != nil {
		if errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests) {
			return "", ErrCircuitOpen
		}
		return "", err
	}
	result, _ := raw.(string)
	return result, nil
}

// completeWithRetry executes the inner Complete call with exponential backoff
// retry. Non-retryable HTTP errors (400, 401, 403) cause an immediate return.
func (r *ResilientClient) completeWithRetry(
	ctx context.Context,
	model, systemPrompt, userMessage string,
	maxTokens int,
) (string, error) {
	var lastErr error
	for attempt := 0; attempt <= r.retries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 400ms, 800ms, 1600ms … with ±20% jitter.
			base := time.Duration(200*int(1<<attempt)) * time.Millisecond
			jitter := time.Duration(rand.Int63n(int64(base) / 5)) //nolint:gosec // jitter only
			select {
			case <-time.After(base + jitter):
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}

		result, err := r.inner.Complete(ctx, model, systemPrompt, userMessage, maxTokens)
		if err == nil {
			return result, nil
		}
		lastErr = err

		// Fast-fail on client errors that will never succeed on retry.
		var httpErr *HTTPError
		if errors.As(err, &httpErr) {
			switch httpErr.StatusCode {
			case 400, 401, 403:
				return "", err
			}
		}
		// 429, 5xx, and network errors fall through to retry.
	}
	return "", lastErr
}
