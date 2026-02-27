package tests

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/config"
)

func TestConfigDefaults(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENCLAW_CORTEX_QDRANT_HOST", "")

	cfg, err := config.Load()
	require.NoError(t, err)

	assert.Equal(t, "localhost", cfg.Qdrant.Host)
	assert.Equal(t, 6334, cfg.Qdrant.GRPCPort)
	assert.Equal(t, "cortex_memories", cfg.Qdrant.Collection)
	assert.Equal(t, "http://localhost:11434", cfg.Ollama.BaseURL)
	assert.Equal(t, "nomic-embed-text", cfg.Ollama.Model)
	assert.Equal(t, float64(0.92), cfg.Memory.DedupThreshold)
	assert.Equal(t, uint64(768), cfg.Memory.VectorDimension)
	assert.Greater(t, cfg.Memory.ChunkSize, 0)
}

func TestConfigEnvOverride(t *testing.T) {
	t.Setenv("OPENCLAW_CORTEX_QDRANT_HOST", "myqdrant.example.com")
	t.Setenv("ANTHROPIC_API_KEY", "test-key-12345")

	cfg, err := config.Load()
	require.NoError(t, err)

	assert.Equal(t, "myqdrant.example.com", cfg.Qdrant.Host)
	assert.Equal(t, "test-key-12345", cfg.Claude.APIKey)
}

func TestConfigValidationChunkOverlap(t *testing.T) {
	cfg := &config.Config{
		Qdrant: config.QdrantConfig{Host: "localhost", Collection: "test"},
		Ollama: config.OllamaConfig{BaseURL: "http://localhost:11434"},
		Memory: config.MemoryConfig{
			ChunkSize:       10,
			ChunkOverlap:    15,
			DedupThreshold:  0.9,
			VectorDimension: 768,
		},
	}
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "chunk_overlap")
}

func TestConfigClaudeStringMasksKey(t *testing.T) {
	cfg := config.ClaudeConfig{
		APIKey: "sk-ant-1234567890abcdef",
		Model:  "claude-haiku-4-5-20251001",
	}
	s := cfg.String()
	assert.Contains(t, s, "sk-a")
	assert.NotContains(t, s, "1234567890")
}

func TestConfigValidationDedupThreshold(t *testing.T) {
	cfg := &config.Config{
		Qdrant: config.QdrantConfig{Host: "localhost", Collection: "test"},
		Ollama: config.OllamaConfig{BaseURL: "http://localhost:11434"},
		Memory: config.MemoryConfig{
			ChunkSize:       512,
			ChunkOverlap:    64,
			DedupThreshold:  1.5,
			VectorDimension: 768,
		},
	}
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "dedup_threshold")
}

func TestConfigValidationMissingHost(t *testing.T) {
	cfg := &config.Config{
		Qdrant: config.QdrantConfig{Host: "", Collection: "test"},
		Ollama: config.OllamaConfig{BaseURL: "http://localhost:11434"},
		Memory: config.MemoryConfig{
			ChunkSize:       512,
			ChunkOverlap:    64,
			DedupThreshold:  0.9,
			VectorDimension: 768,
		},
	}
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "qdrant.host")
}

func TestConfigValidationMissingCollection(t *testing.T) {
	cfg := &config.Config{
		Qdrant: config.QdrantConfig{Host: "localhost", Collection: ""},
		Ollama: config.OllamaConfig{BaseURL: "http://localhost:11434"},
		Memory: config.MemoryConfig{
			ChunkSize:       512,
			ChunkOverlap:    64,
			DedupThreshold:  0.9,
			VectorDimension: 768,
		},
	}
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "qdrant.collection")
}

func TestConfigValidationMissingOllamaURL(t *testing.T) {
	cfg := &config.Config{
		Qdrant: config.QdrantConfig{Host: "localhost", Collection: "test"},
		Ollama: config.OllamaConfig{BaseURL: ""},
		Memory: config.MemoryConfig{
			ChunkSize:       512,
			ChunkOverlap:    64,
			DedupThreshold:  0.9,
			VectorDimension: 768,
		},
	}
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ollama.base_url")
}

func TestConfigValidationChunkSizeZero(t *testing.T) {
	cfg := &config.Config{
		Qdrant: config.QdrantConfig{Host: "localhost", Collection: "test"},
		Ollama: config.OllamaConfig{BaseURL: "http://localhost:11434"},
		Memory: config.MemoryConfig{
			ChunkSize:       0,
			ChunkOverlap:    0,
			DedupThreshold:  0.9,
			VectorDimension: 768,
		},
	}
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "chunk_size")
}

func TestConfigValidationVectorDimensionZero(t *testing.T) {
	cfg := &config.Config{
		Qdrant: config.QdrantConfig{Host: "localhost", Collection: "test"},
		Ollama: config.OllamaConfig{BaseURL: "http://localhost:11434"},
		Memory: config.MemoryConfig{
			ChunkSize:       512,
			ChunkOverlap:    64,
			DedupThreshold:  0.9,
			VectorDimension: 0,
		},
	}
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "vector_dimension")
}

func TestConfigValidationNegativeTTL(t *testing.T) {
	cfg := &config.Config{
		Qdrant: config.QdrantConfig{Host: "localhost", Collection: "test"},
		Ollama: config.OllamaConfig{BaseURL: "http://localhost:11434"},
		Memory: config.MemoryConfig{
			ChunkSize:       512,
			ChunkOverlap:    64,
			DedupThreshold:  0.9,
			VectorDimension: 768,
			DefaultTTLHours: -1,
		},
	}
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "default_ttl_hours")
}

func TestConfigValidationNegativeChunkOverlap(t *testing.T) {
	cfg := &config.Config{
		Qdrant: config.QdrantConfig{Host: "localhost", Collection: "test"},
		Ollama: config.OllamaConfig{BaseURL: "http://localhost:11434"},
		Memory: config.MemoryConfig{
			ChunkSize:       512,
			ChunkOverlap:    -1,
			DedupThreshold:  0.9,
			VectorDimension: 768,
		},
	}
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "chunk_overlap")
}

func TestConfigValidationValid(t *testing.T) {
	cfg := &config.Config{
		Qdrant: config.QdrantConfig{Host: "localhost", Collection: "cortex_memories"},
		Ollama: config.OllamaConfig{BaseURL: "http://localhost:11434"},
		Memory: config.MemoryConfig{
			ChunkSize:       512,
			ChunkOverlap:    64,
			DedupThreshold:  0.92,
			VectorDimension: 768,
			DefaultTTLHours: 720,
		},
	}
	err := cfg.Validate()
	assert.NoError(t, err)
}

func TestConfigClaudeStringShortKey(t *testing.T) {
	// Short keys (<=8 chars) should be masked as "***"
	cfg := config.ClaudeConfig{
		APIKey: "short",
		Model:  "claude-haiku-4-5-20251001",
	}
	s := cfg.String()
	assert.Contains(t, s, "***")
}
