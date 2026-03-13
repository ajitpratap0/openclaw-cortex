package tests

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ajitpratap0/openclaw-cortex/internal/capture"
	"github.com/ajitpratap0/openclaw-cortex/internal/config"
)

func defaultQualityCfg() config.CaptureQualityConfig {
	return config.CaptureQualityConfig{
		MinUserMessageLength:      20,
		MinAssistantMessageLength: 20,
		BlocklistPatterns:         []string{"HEARTBEAT_OK", "NO_REPLY"},
	}
}

func TestShouldCapture_ValidMessages(t *testing.T) {
	cfg := defaultQualityCfg()
	ok := capture.ShouldCapture(
		"Tell me about Go concurrency patterns",
		"Go uses goroutines and channels for concurrency",
		cfg,
	)
	assert.True(t, ok)
}

func TestShouldCapture_ShortUserMessage(t *testing.T) {
	cfg := defaultQualityCfg()
	ok := capture.ShouldCapture("hi", "This is a sufficiently long response from the assistant", cfg)
	assert.False(t, ok)
}

func TestShouldCapture_ShortAssistantMessage(t *testing.T) {
	cfg := defaultQualityCfg()
	ok := capture.ShouldCapture("Tell me about Go concurrency patterns", "ok", cfg)
	assert.False(t, ok)
}

func TestShouldCapture_BothShort(t *testing.T) {
	cfg := defaultQualityCfg()
	ok := capture.ShouldCapture("hey", "sure", cfg)
	assert.False(t, ok)
}

func TestShouldCapture_BlocklistMatchUser(t *testing.T) {
	cfg := defaultQualityCfg()
	ok := capture.ShouldCapture(
		"HEARTBEAT_OK checking status",
		"Everything is running fine in production",
		cfg,
	)
	assert.False(t, ok)
}

func TestShouldCapture_BlocklistMatchAssistant(t *testing.T) {
	cfg := defaultQualityCfg()
	ok := capture.ShouldCapture(
		"What is the system status?",
		"NO_REPLY from the backend service",
		cfg,
	)
	assert.False(t, ok)
}

func TestShouldCapture_BlocklistCaseInsensitive(t *testing.T) {
	cfg := defaultQualityCfg()
	ok := capture.ShouldCapture(
		"heartbeat_ok still running",
		"Everything is running fine in production",
		cfg,
	)
	assert.False(t, ok)

	ok = capture.ShouldCapture(
		"What is the system status?",
		"no_reply from the backend",
		cfg,
	)
	assert.False(t, ok)
}

func TestShouldCapture_EmptyBlocklist(t *testing.T) {
	cfg := config.CaptureQualityConfig{
		MinUserMessageLength:      20,
		MinAssistantMessageLength: 20,
		BlocklistPatterns:         nil,
	}
	ok := capture.ShouldCapture(
		"HEARTBEAT_OK checking status",
		"NO_REPLY from the backend service",
		cfg,
	)
	assert.True(t, ok)
}

func TestShouldCapture_WhitespaceOnlyMessages(t *testing.T) {
	cfg := defaultQualityCfg()
	ok := capture.ShouldCapture("   ", "   ", cfg)
	assert.False(t, ok)
}

func TestShouldCapture_ExactlyAtMinLength(t *testing.T) {
	cfg := config.CaptureQualityConfig{
		MinUserMessageLength:      5,
		MinAssistantMessageLength: 5,
		BlocklistPatterns:         nil,
	}
	// Exactly 5 chars — should pass
	ok := capture.ShouldCapture("hello", "world", cfg)
	assert.True(t, ok)

	// 4 chars — should fail
	ok = capture.ShouldCapture("hell", "world", cfg)
	assert.False(t, ok)
}

func TestShouldCapture_CustomBlocklistPattern(t *testing.T) {
	cfg := config.CaptureQualityConfig{
		MinUserMessageLength:      5,
		MinAssistantMessageLength: 5,
		BlocklistPatterns:         []string{"SKIP_ME"},
	}
	ok := capture.ShouldCapture(
		"This message contains SKIP_ME somewhere",
		"A valid assistant response here",
		cfg,
	)
	assert.False(t, ok)
}
