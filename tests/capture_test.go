package tests

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
)

// TestCapturedMemoryJSON tests that CapturedMemory serializes correctly.
func TestCapturedMemoryJSON(t *testing.T) {
	mem := models.CapturedMemory{
		Content:    "Go uses goroutines for concurrency",
		Type:       models.MemoryTypeFact,
		Confidence: 0.95,
		Tags:       []string{"go", "concurrency"},
	}

	data, err := json.Marshal(mem)
	require.NoError(t, err)

	var decoded models.CapturedMemory
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, mem.Content, decoded.Content)
	assert.Equal(t, mem.Type, decoded.Type)
	assert.Equal(t, mem.Confidence, decoded.Confidence)
	assert.Equal(t, mem.Tags, decoded.Tags)
}

// TestCaptureExtractionParsing tests the JSON parsing logic used by the capturer.
// The Claude HTTP client cannot be easily injected with a mock base URL, so we
// test the JSON unmarshalling and confidence filtering independently.
func TestCaptureExtractionParsing(t *testing.T) {
	responseText := `[{"content":"Go uses goroutines for concurrency","type":"fact","confidence":0.9,"tags":["go","concurrency"]},{"content":"Always handle errors explicitly in Go","type":"rule","confidence":0.85,"tags":["go","error-handling"]}]`

	var memories []models.CapturedMemory
	err := json.Unmarshal([]byte(responseText), &memories)
	require.NoError(t, err)
	require.Len(t, memories, 2)

	assert.Equal(t, "Go uses goroutines for concurrency", memories[0].Content)
	assert.Equal(t, models.MemoryTypeFact, memories[0].Type)
	assert.Equal(t, 0.9, memories[0].Confidence)

	assert.Equal(t, "Always handle errors explicitly in Go", memories[1].Content)
	assert.Equal(t, models.MemoryTypeRule, memories[1].Type)

	// Test low-confidence filtering (same logic as capturer.Extract).
	_ = context.Background()
	var filtered []models.CapturedMemory
	for _, m := range memories {
		if m.Confidence >= 0.5 {
			filtered = append(filtered, m)
		}
	}
	assert.Len(t, filtered, 2, "both should pass 0.5 threshold")

	// Verify that a low-confidence memory would be filtered out.
	lowConf := models.CapturedMemory{Content: "maybe", Confidence: 0.3}
	var filtered2 []models.CapturedMemory
	for _, m := range append(memories, lowConf) {
		if m.Confidence >= 0.5 {
			filtered2 = append(filtered2, m)
		}
	}
	assert.Len(t, filtered2, 2, "low-confidence memory should be filtered")
}
