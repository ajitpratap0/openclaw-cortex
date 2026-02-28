package tests

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ajitpratap0/openclaw-cortex/internal/capture"
	"github.com/ajitpratap0/openclaw-cortex/internal/indexer"
)

// TestNewCapturer verifies the constructor doesn't panic and returns a non-nil capturer.
func TestNewCapturer(t *testing.T) {
	c := capture.NewCapturer("fake-api-key", "claude-haiku-4-5-20251001", slog.Default())
	assert.NotNil(t, c)
}

// TestNewSectionSummarizer verifies the constructor returns a non-nil summarizer.
func TestNewSectionSummarizer(t *testing.T) {
	s := indexer.NewSectionSummarizer("fake-api-key", "claude-haiku-4-5-20251001", slog.Default())
	assert.NotNil(t, s)
}
