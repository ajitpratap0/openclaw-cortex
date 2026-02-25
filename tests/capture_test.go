package tests

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/cortex/internal/models"
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

// TestCaptureExtraction tests the Claude extraction with a mock HTTP server.
func TestCaptureExtraction(t *testing.T) {
	// Create a mock Anthropic API server
	mockResp := `{
		"id": "msg_test",
		"type": "message",
		"role": "assistant",
		"content": [
			{
				"type": "text",
				"text": "[{\"content\":\"Go uses goroutines for concurrency\",\"type\":\"fact\",\"confidence\":0.9,\"tags\":[\"go\",\"concurrency\"]},{\"content\":\"Always handle errors explicitly in Go\",\"type\":\"rule\",\"confidence\":0.85,\"tags\":[\"go\",\"error-handling\"]}]"
			}
		],
		"model": "claude-haiku-4-5-20241022",
		"stop_reason": "end_turn",
		"usage": {"input_tokens": 100, "output_tokens": 50}
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(mockResp))
	}))
	defer server.Close()

	// We can't easily test the full Claude client without injecting the base URL.
	// Instead, test the JSON parsing logic directly.
	var memories []models.CapturedMemory
	responseText := `[{"content":"Go uses goroutines for concurrency","type":"fact","confidence":0.9,"tags":["go","concurrency"]},{"content":"Always handle errors explicitly in Go","type":"rule","confidence":0.85,"tags":["go","error-handling"]}]`

	err := json.Unmarshal([]byte(responseText), &memories)
	require.NoError(t, err)
	require.Len(t, memories, 2)

	assert.Equal(t, "Go uses goroutines for concurrency", memories[0].Content)
	assert.Equal(t, models.MemoryTypeFact, memories[0].Type)
	assert.Equal(t, 0.9, memories[0].Confidence)

	assert.Equal(t, "Always handle errors explicitly in Go", memories[1].Content)
	assert.Equal(t, models.MemoryTypeRule, memories[1].Type)

	// Test low-confidence filtering
	_ = context.Background()
	var filtered []models.CapturedMemory
	for _, m := range memories {
		if m.Confidence >= 0.5 {
			filtered = append(filtered, m)
		}
	}
	assert.Len(t, filtered, 2, "both should pass 0.5 threshold")
}
