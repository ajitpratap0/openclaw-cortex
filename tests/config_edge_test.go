package tests

import (
	"strings"
	"testing"

	"github.com/ajitpratap0/openclaw-cortex/internal/config"
)

func TestEmbedderConfigString(t *testing.T) {
	t.Run("ollama provider", func(t *testing.T) {
		cfg := config.EmbedderConfig{Provider: "ollama"}
		s := cfg.String()
		if !strings.Contains(s, "ollama") {
			t.Errorf("String() %q does not contain provider name", s)
		}
	})

	t.Run("lmstudio provider", func(t *testing.T) {
		cfg := config.EmbedderConfig{
			Provider: "lmstudio",
			LMStudio: config.LMStudioConfig{
				URL:   "http://localhost:1234",
				Model: "nomic-embed-text-v1.5",
			},
		}
		s := cfg.String()
		if !strings.Contains(s, "lmstudio") {
			t.Errorf("String() %q does not contain provider name", s)
		}
	})

	t.Run("empty provider no panic", func(t *testing.T) {
		cfg := config.EmbedderConfig{}
		_ = cfg.String() // must not panic
	})
}

func TestClaudeConfigString(t *testing.T) {
	t.Run("api key masked", func(t *testing.T) {
		cfg := config.ClaudeConfig{
			APIKey:     "sk-ant-abcdefghijklmnopqrstuvwxyz1234",
			Model:      "claude-haiku-4-5",
			GatewayURL: "http://127.0.0.1:18789",
		}
		s := cfg.String()
		if strings.Contains(s, "sk-ant-abcdefghijklmnopqrstuvwxyz1234") {
			t.Error("full API key should not appear in ClaudeConfig.String() output")
		}
		if !strings.Contains(s, "****") {
			t.Error("masked key should contain '****'")
		}
	})

	t.Run("gateway URL not masked", func(t *testing.T) {
		gatewayURL := "http://127.0.0.1:18789"
		cfg := config.ClaudeConfig{
			APIKey:     "sk-ant-abcdefghijklmnopqrstuvwxyz1234",
			GatewayURL: gatewayURL,
		}
		s := cfg.String()
		if !strings.Contains(s, gatewayURL) {
			t.Errorf("gateway URL %q should not be masked in String() output", gatewayURL)
		}
	})

	t.Run("empty key no panic", func(t *testing.T) {
		cfg := config.ClaudeConfig{Model: "claude-haiku-4-5"}
		_ = cfg.String() // must not panic
	})
}
