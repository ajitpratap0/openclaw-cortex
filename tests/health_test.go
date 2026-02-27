package tests

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/config"
)

// TestHealthConfig verifies that DedupThresholdHook config loads with the expected default value.
func TestHealthConfig(t *testing.T) {
	// Load config with defaults (no config file required).
	cfg, err := config.Load()
	require.NoError(t, err)

	// DedupThresholdHook should default to 0.95.
	assert.Equal(t, 0.95, cfg.Memory.DedupThresholdHook)
}

// TestHealthConfig_Validate_DedupThresholdHookOutOfRange verifies that an invalid
// DedupThresholdHook value is rejected by Validate.
func TestHealthConfig_Validate_DedupThresholdHookOutOfRange(t *testing.T) {
	cfg := &config.Config{
		Qdrant: config.QdrantConfig{
			Host:       "localhost",
			GRPCPort:   6334,
			HTTPPort:   6333,
			Collection: "test_col",
		},
		Ollama: config.OllamaConfig{
			BaseURL: "http://localhost:11434",
			Model:   "nomic-embed-text",
		},
		Memory: config.MemoryConfig{
			MemoryDir:          "/tmp",
			ChunkSize:          512,
			ChunkOverlap:       64,
			DedupThreshold:     0.92,
			DedupThresholdHook: 1.5, // invalid: > 1
			DefaultTTLHours:    720,
			VectorDimension:    768,
		},
	}

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dedup_threshold_hook")
}
