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
	"time"

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

// TestHealthLLMPing_GatewayContextTimeout verifies that an in-flight request is
// aborted when the context deadline fires — the health check applies a 5 s timeout
// via context.WithTimeout, and a slow gateway must not block indefinitely.
func TestHealthLLMPing_GatewayContextTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until the client cancels — simulates a gateway that never responds
		// within the health-check timeout window.
		<-r.Context().Done()
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	client := llm.NewGatewayClient(srv.URL, "tok", 0) // no http-level timeout; rely on context
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := client.Complete(ctx, "claude-haiku-4-5", "ping", "respond with ok", 5)
	require.Error(t, err, "timed-out context should return an error")
}

// TestNewClient_NoCredentials_ReturnsNil verifies that llm.NewClient returns nil
// when no credentials are configured. Note: the health command does not call
// llm.NewClient directly — it calls llm.NewGatewayClient / llm.NewAnthropicClient
// and handles the no-credentials case via an explicit default: branch. This test
// covers the factory's nil-return contract independently.
//
// AnthropicClient coverage note: AnthropicClient.Complete calls the Anthropic SDK
// directly (no HTTP interceptor is possible at test time), so its error path is
// covered by integration testing only. The gateway path is fully unit-tested above.
func TestNewClient_NoCredentials_ReturnsNil(t *testing.T) {
	client := llm.NewClient(config.ClaudeConfig{})
	assert.Nil(t, client, "no credentials should produce a nil client")
}

// TestHealthCmd_NoCredentials_DefaultBranch documents that the health command's
// default: branch (neither gateway nor API key configured) sets LLM status to
// false and records an error. This path is exercised by integration testing only
// — the Cobra command cannot be unit-tested without the full binary. Extracting
// the ping logic into a helper would enable unit coverage of all three branches.
func TestHealthCmd_NoCredentials_DefaultBranch(t *testing.T) {
	t.Skip("covered by integration test only; see cmd_health.go default: branch")
}
