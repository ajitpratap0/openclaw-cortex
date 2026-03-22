package tests

// Tests for the LLM ping behavior introduced in the health command fix (issue #97).
//
// The health command now makes a real API call to validate LLM credentials instead of
// only checking config-value presence. These tests verify the underlying LLM client
// behavior that the health check relies on.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/config"
	"github.com/ajitpratap0/openclaw-cortex/internal/llm"
)

// TestHealthLLMPing_GatewayOK verifies that a valid gateway token causes no error
// — the health check would mark LLM as OK.
func TestHealthLLMPing_GatewayOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer valid-token", r.Header.Get("Authorization"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"content": "ok"}},
			},
		})
	}))
	t.Cleanup(srv.Close)

	client := llm.NewGatewayClient(srv.URL, "valid-token", 5)
	ctx := context.Background()
	_, err := client.Complete(ctx, "claude-haiku-4-5", "ping", "respond with ok", 5)
	require.NoError(t, err, "valid gateway token should succeed — health check would mark LLM OK")
}

// TestHealthLLMPing_GatewayUnauthorized verifies that a 401 response causes an error
// — the health check would mark LLM as FAIL.
func TestHealthLLMPing_GatewayUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid token"}}`))
	}))
	t.Cleanup(srv.Close)

	client := llm.NewGatewayClient(srv.URL, "bad-token", 5)
	ctx := context.Background()
	_, err := client.Complete(ctx, "claude-haiku-4-5", "ping", "respond with ok", 5)
	require.Error(t, err, "invalid gateway token should return error — health check would mark LLM FAIL")

	var httpErr *llm.HTTPError
	require.ErrorAs(t, err, &httpErr, "error should be an HTTPError")
	assert.Equal(t, http.StatusUnauthorized, httpErr.StatusCode)
}

// TestHealthLLMPing_GatewayForbidden verifies that a 403 response causes an error.
func TestHealthLLMPing_GatewayForbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"message":"forbidden"}}`))
	}))
	t.Cleanup(srv.Close)

	client := llm.NewGatewayClient(srv.URL, "expired-token", 5)
	ctx := context.Background()
	_, err := client.Complete(ctx, "claude-haiku-4-5", "ping", "respond with ok", 5)
	require.Error(t, err, "forbidden gateway response should return error — health check would mark LLM FAIL")

	var httpErr *llm.HTTPError
	require.ErrorAs(t, err, &httpErr)
	assert.Equal(t, http.StatusForbidden, httpErr.StatusCode)
}

// TestHealthLLMPing_GatewayContextTimeout verifies that context cancellation is
// propagated correctly — the health check applies a 5s timeout via context.WithTimeout.
func TestHealthLLMPing_GatewayContextTimeout(t *testing.T) {
	// Pre-cancel the context to simulate a timeout firing before the server responds.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	client := llm.NewGatewayClient(srv.URL, "tok", 5)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before the call

	_, err := client.Complete(ctx, "claude-haiku-4-5", "ping", "respond with ok", 5)
	require.Error(t, err, "canceled context should return an error")
}

// TestHealthLLMPing_NoCredentials_NilClient verifies that llm.NewClient returns nil
// when no credentials are configured, which the health check handles as the "no API
// key or gateway configured" error case.
func TestHealthLLMPing_NoCredentials_NilClient(t *testing.T) {
	client := llm.NewClient(config.ClaudeConfig{})
	assert.Nil(t, client, "no credentials should produce a nil client — health check marks LLM FAIL")
}
