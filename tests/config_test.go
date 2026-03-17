package tests

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/config"
)

func TestConfigDefaults(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENCLAW_CORTEX_MEMGRAPH_URI", "")

	cfg, err := config.Load()
	require.NoError(t, err)

	assert.Equal(t, "bolt://localhost:7687", cfg.Memgraph.URI)
	assert.Equal(t, "http://localhost:11434", cfg.Ollama.BaseURL)
	assert.Equal(t, "nomic-embed-text", cfg.Ollama.Model)
	assert.Equal(t, float64(0.92), cfg.Memory.DedupThreshold)
	assert.Equal(t, uint64(768), cfg.Memory.VectorDimension)
	assert.Greater(t, cfg.Memory.ChunkSize, 0)
}

func TestConfigEnvOverride(t *testing.T) {
	t.Setenv("OPENCLAW_CORTEX_MEMGRAPH_URI", "bolt://myhost:7687")
	t.Setenv("ANTHROPIC_API_KEY", "test-key-12345")

	cfg, err := config.Load()
	require.NoError(t, err)

	assert.Equal(t, "bolt://myhost:7687", cfg.Memgraph.URI)
	assert.Equal(t, "test-key-12345", cfg.Claude.APIKey)
}

func TestConfigValidationChunkOverlap(t *testing.T) {
	cfg := &config.Config{
		Memgraph: config.MemgraphConfig{URI: "bolt://localhost:7687"},
		Ollama:   config.OllamaConfig{BaseURL: "http://localhost:11434"},
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
		Memgraph: config.MemgraphConfig{URI: "bolt://localhost:7687"},
		Ollama:   config.OllamaConfig{BaseURL: "http://localhost:11434"},
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
		Memgraph: config.MemgraphConfig{URI: ""},
		Ollama:   config.OllamaConfig{BaseURL: "http://localhost:11434"},
		Memory: config.MemoryConfig{
			ChunkSize:       512,
			ChunkOverlap:    64,
			DedupThreshold:  0.9,
			VectorDimension: 768,
		},
	}
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "memgraph.uri")
}

func TestConfigValidationMissingOllamaURL(t *testing.T) {
	cfg := &config.Config{
		Memgraph: config.MemgraphConfig{URI: "bolt://localhost:7687"},
		Ollama:   config.OllamaConfig{BaseURL: ""},
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
		Memgraph: config.MemgraphConfig{URI: "bolt://localhost:7687"},
		Ollama:   config.OllamaConfig{BaseURL: "http://localhost:11434"},
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
		Memgraph: config.MemgraphConfig{URI: "bolt://localhost:7687"},
		Ollama:   config.OllamaConfig{BaseURL: "http://localhost:11434"},
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
		Memgraph: config.MemgraphConfig{URI: "bolt://localhost:7687"},
		Ollama:   config.OllamaConfig{BaseURL: "http://localhost:11434"},
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
		Memgraph: config.MemgraphConfig{URI: "bolt://localhost:7687"},
		Ollama:   config.OllamaConfig{BaseURL: "http://localhost:11434"},
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
		Memgraph: config.MemgraphConfig{URI: "bolt://localhost:7687"},
		Ollama:   config.OllamaConfig{BaseURL: "http://localhost:11434"},
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

func TestAPIConfig_RateLimitDefaults(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("config.Load() failed (likely missing Memgraph in CI): %v", err)
	}
	if cfg.API.RateLimitRPS != 100.0 {
		t.Errorf("expected RateLimitRPS=100.0, got %v", cfg.API.RateLimitRPS)
	}
	if cfg.API.RateLimitBurst != 20 {
		t.Errorf("expected RateLimitBurst=20, got %d", cfg.API.RateLimitBurst)
	}
}

func validBaseConfig() config.Config {
	return config.Config{
		Memgraph: config.MemgraphConfig{URI: "bolt://localhost:7687"},
		Ollama:   config.OllamaConfig{BaseURL: "http://localhost:11434"},
		Memory: config.MemoryConfig{
			ChunkSize:       512,
			ChunkOverlap:    64,
			DedupThreshold:  0.92,
			VectorDimension: 768,
			DefaultTTLHours: 720,
		},
	}
}

func TestConfig_Validate_UnknownProvider(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Embedder.Provider = "gemini"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for unknown provider 'gemini'")
	}
}

func TestConfig_Validate_LMStudioMissingModel(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Embedder.Provider = "lmstudio"
	cfg.Embedder.LMStudio.Model = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for lmstudio with empty model")
	}
}

func TestConfig_Validate_LMStudio_Valid(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Embedder.Provider = "lmstudio"
	cfg.Embedder.LMStudio.Model = "nomic-embed-text-v1.5"
	cfg.Embedder.LMStudio.URL = "http://localhost:1234"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid lmstudio config, got: %v", err)
	}
}
