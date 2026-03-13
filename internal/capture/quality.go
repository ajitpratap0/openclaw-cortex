package capture

import (
	"strings"

	"github.com/ajitpratap0/openclaw-cortex/internal/config"
)

// ShouldCapture returns true if the conversation turn passes quality filters
// and is worth sending to Claude for memory extraction. It checks minimum
// message lengths and blocklist patterns (case-insensitive substring match).
func ShouldCapture(userMsg, assistantMsg string, cfg config.CaptureQualityConfig) bool {
	if len(strings.TrimSpace(userMsg)) < cfg.MinUserMessageLength {
		return false
	}
	if len(strings.TrimSpace(assistantMsg)) < cfg.MinAssistantMessageLength {
		return false
	}

	lowerUser := strings.ToLower(userMsg)
	lowerAssistant := strings.ToLower(assistantMsg)

	for _, pattern := range cfg.BlocklistPatterns {
		lp := strings.ToLower(pattern)
		if strings.Contains(lowerUser, lp) || strings.Contains(lowerAssistant, lp) {
			return false
		}
	}

	return true
}
