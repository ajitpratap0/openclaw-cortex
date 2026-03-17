// Package sentry wraps the Sentry Go SDK.
// All functions are no-ops when DSN is empty so the package is safe to call
// unconditionally regardless of whether Sentry is configured.
package sentry

import (
	"context"
	"sync/atomic"
	"time"

	sentrygo "github.com/getsentry/sentry-go"
)

// initialized is set to true by Init when a non-empty DSN is successfully configured.
// Atomic bool ensures safe reads from concurrent goroutines (HTTP handlers, hooks).
var initialized atomic.Bool

// Init initializes the Sentry SDK. Does nothing if dsn is empty.
// If the DSN is invalid, initialization is skipped silently.
func Init(dsn, environment, release string) {
	if dsn == "" {
		return
	}
	if err := sentrygo.Init(sentrygo.ClientOptions{
		Dsn:              dsn,
		Environment:      environment,
		Release:          release,
		TracesSampleRate: 0.2,
	}); err != nil {
		// Invalid DSN or other SDK error — do not mark as initialized.
		return
	}
	initialized.Store(true)
}

// Flush waits up to timeout for buffered events to be sent.
// No-op if Sentry is not configured.
func Flush(timeout time.Duration) {
	if !initialized.Load() {
		return
	}
	sentrygo.Flush(timeout)
}

// CaptureException sends err to Sentry. No-op if err is nil or Sentry is not configured.
func CaptureException(err error) {
	if err == nil {
		return
	}
	sentrygo.CaptureException(err)
}

// StartSpan starts a Sentry performance span attached to ctx.
// Returns a function that must be called to finish the span.
// If Sentry is not configured, returns a no-op finish function.
func StartSpan(ctx context.Context, op, description string) func() {
	if !initialized.Load() {
		return func() {}
	}
	span := sentrygo.StartSpan(ctx, op,
		sentrygo.WithDescription(description),
	)
	return func() { span.Finish() }
}
