package tests

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ajitpratap0/openclaw-cortex/internal/api"
	"github.com/ajitpratap0/openclaw-cortex/internal/sentry"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

func TestSentry_NoopWhenDSNEmpty(t *testing.T) {
	// Verify that all sentry functions are safe to call with no DSN configured.
	sentry.Init("", "test", "0.0.0")
	sentry.CaptureException(errors.New("test error"))
	finish := sentry.StartSpan(context.Background(), "test.op", "test description")
	finish()
	sentry.Flush(0)
	// If we get here without panic, the no-op behavior works.
}

func TestSentryMiddleware_PassthroughWhenNoDSN(t *testing.T) {
	// Build a minimal server and confirm a normal request still gets 200.
	st := store.NewMockStore()
	srv := api.NewServer(st, nil, &apiTestEmbedder{}, newTestLogger(t), "", "")
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 from healthz, got %d", rr.Code)
	}
}
