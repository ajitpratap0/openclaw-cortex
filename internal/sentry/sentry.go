// Package sentry wraps the Sentry Go SDK.
// All functions are no-ops when DSN is empty so the package is safe to call
// unconditionally regardless of whether Sentry is configured.
package sentry

import (
	"context"
	"time"

	sentrygo "github.com/getsentry/sentry-go"
)

// initialised is set to true by Init when a non-empty DSN is provided.
// It guards all Sentry calls so they are no-ops when Sentry is not configured.
var initialised bool

// Init initialises the Sentry SDK. Does nothing if dsn is empty.
func Init(dsn, environment, release string) {
	if dsn == "" {
		return
	}
	_ = sentrygo.Init(sentrygo.ClientOptions{
		Dsn:              dsn,
		Environment:      environment,
		Release:          release,
		TracesSampleRate: 0.2,
	})
	initialised = true
}

// Flush waits up to timeout for buffered events to be sent.
func Flush(timeout time.Duration) {
	sentrygo.Flush(timeout)
}

// CaptureException sends err to Sentry. No-op if err is nil or Sentry is not configured.
func CaptureException(err error) {
	if err == nil {
		return
	}
	sentrygo.CaptureException(err)
}

// StartSpan starts a Sentry performance span. Returns a function that must be called to finish the span.
// If Sentry is not configured, returns a no-op finish function.
func StartSpan(op, description string) func() {
	if !initialised {
		return func() {}
	}
	span := sentrygo.StartSpan(context.Background(), op,
		sentrygo.WithDescription(description),
	)
	return func() { span.Finish() }
}
