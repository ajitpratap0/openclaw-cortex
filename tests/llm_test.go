package tests

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

// --- GatewayClient ---

func gatewayServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *llm.GatewayClient) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, llm.NewGatewayClient(srv.URL, "test-token")
}

func TestGatewayClient_Complete_Success(t *testing.T) {
	_, client := gatewayServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"content": "hello world"}},
			},
		})
	})
	got, err := client.Complete(context.Background(), "m", "sys", "usr", 100)
	require.NoError(t, err)
	assert.Equal(t, "hello world", got)
}

func TestGatewayClient_Complete_EmptyChoices(t *testing.T) {
	_, client := gatewayServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{}})
	})
	_, err := client.Complete(context.Background(), "m", "sys", "usr", 100)
	assert.Error(t, err)
}

func TestGatewayClient_Complete_APIError(t *testing.T) {
	_, client := gatewayServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{"message": "bad request"},
		})
	})
	_, err := client.Complete(context.Background(), "m", "sys", "usr", 100)
	assert.Error(t, err)
}

func TestGatewayClient_Complete_InvalidJSON(t *testing.T) {
	_, client := gatewayServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json"))
	})
	_, err := client.Complete(context.Background(), "m", "sys", "usr", 100)
	assert.Error(t, err)
}

func TestGatewayClient_Complete_ContextCancelled(t *testing.T) {
	_, client := gatewayServer(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		w.WriteHeader(http.StatusOK)
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := client.Complete(ctx, "m", "sys", "usr", 100)
	assert.Error(t, err)
}

func TestGatewayClient_Complete_ConnectionRefused(t *testing.T) {
	client := llm.NewGatewayClient("http://127.0.0.1:1", "tok")
	_, err := client.Complete(context.Background(), "m", "sys", "usr", 100)
	assert.Error(t, err)
}

// --- NewClient factory ---

func TestNewClient_GatewayConfig(t *testing.T) {
	cfg := config.ClaudeConfig{
		GatewayURL:   "http://localhost:9999",
		GatewayToken: "tok",
	}
	c := llm.NewClient(cfg)
	assert.NotNil(t, c, "gateway config should produce non-nil client")
}

func TestNewClient_APIKeyConfig(t *testing.T) {
	cfg := config.ClaudeConfig{APIKey: "sk-test"}
	c := llm.NewClient(cfg)
	assert.NotNil(t, c, "API key config should produce non-nil client")
}

func TestNewClient_EmptyConfig(t *testing.T) {
	c := llm.NewClient(config.ClaudeConfig{})
	assert.Nil(t, c, "empty config should produce nil client")
}

func TestNewClient_GatewayTakesPrecedence(t *testing.T) {
	cfg := config.ClaudeConfig{
		GatewayURL:   "http://localhost:9999",
		GatewayToken: "tok",
		APIKey:       "sk-test",
	}
	c := llm.NewClient(cfg)
	assert.NotNil(t, c)
}

// --- StripCodeFences ---

func TestStripCodeFences_NoFences(t *testing.T) {
	assert.Equal(t, `{"key":"val"}`, llm.StripCodeFences(`{"key":"val"}`))
}

func TestStripCodeFences_JSONFence(t *testing.T) {
	in := "```json\n{\"key\":\"val\"}\n```"
	assert.Equal(t, `{"key":"val"}`, llm.StripCodeFences(in))
}

func TestStripCodeFences_PlainFence(t *testing.T) {
	in := "```\nhello\n```"
	assert.Equal(t, "hello", llm.StripCodeFences(in))
}

func TestStripCodeFences_Empty(t *testing.T) {
	assert.Equal(t, "", llm.StripCodeFences(""))
}

func TestStripCodeFences_OnlyFenceNoNewline(t *testing.T) {
	in := "```json"
	assert.Equal(t, "```json", llm.StripCodeFences(in))
}
