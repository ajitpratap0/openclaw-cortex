package llm

import (
	"github.com/ajitpratap0/openclaw-cortex/internal/config"
)

// NewClient returns the appropriate LLMClient based on cfg.
//
// Priority:
//  1. GatewayURL + GatewayToken set → GatewayClient (OpenAI-compatible gateway)
//  2. APIKey set → AnthropicClient (direct Anthropic SDK)
//  3. Neither set → nil (no LLM available; callers must guard against this)
func NewClient(cfg config.ClaudeConfig) LLMClient {
	if cfg.GatewayURL != "" && cfg.GatewayToken != "" {
		return NewGatewayClient(cfg.GatewayURL, cfg.GatewayToken)
	}
	if cfg.APIKey != "" {
		return NewAnthropicClient(cfg.APIKey)
	}
	return nil
}
