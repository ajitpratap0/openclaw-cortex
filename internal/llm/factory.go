package llm

import "github.com/ajitpratap0/openclaw-cortex/internal/config"

// NewClient returns a ResilientClient wrapping the appropriate concrete LLMClient,
// or nil if no credentials are configured.
//
// Priority:
//  1. GatewayURL + GatewayToken set → GatewayClient (OpenAI-compatible gateway)
//  2. APIKey set → AnthropicClient (direct Anthropic SDK)
//  3. Neither set → nil (no LLM available; callers must guard against this)
func NewClient(cfg config.ClaudeConfig) LLMClient {
	var inner LLMClient
	if cfg.GatewayURL != "" && cfg.GatewayToken != "" {
		inner = NewGatewayClient(cfg.GatewayURL, cfg.GatewayToken)
	} else if cfg.APIKey != "" {
		inner = NewAnthropicClient(cfg.APIKey)
	}
	if inner == nil {
		return nil
	}

	// Apply resilience defaults if not configured.
	maxConcurrent := cfg.MaxConcurrentLLMCalls
	if maxConcurrent <= 0 {
		maxConcurrent = 4
	}
	cbThreshold := cfg.CBFailureThreshold
	if cbThreshold <= 0 {
		cbThreshold = 5
	}
	cbRecovery := cfg.CBRecoverySeconds
	if cbRecovery <= 0 {
		cbRecovery = 30
	}
	maxRetries := cfg.MaxRetries
	if maxRetries < 0 {
		maxRetries = 3
	}

	return NewResilientClient(inner, maxConcurrent, cbThreshold, cbRecovery, maxRetries)
}
