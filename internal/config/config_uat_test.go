package config

import (
	"strings"
	"testing"
)

// validConfig returns a fully-valid Config for mutation testing.
func validCfg() *Config {
	return &Config{
		Qdrant: QdrantConfig{
			Host:       "localhost",
			GRPCPort:   6334,
			HTTPPort:   6333,
			Collection: "test_col",
		},
		Ollama: OllamaConfig{
			BaseURL: "http://localhost:11434",
			Model:   "nomic-embed-text",
		},
		Memory: MemoryConfig{
			MemoryDir:       "/tmp",
			ChunkSize:       512,
			ChunkOverlap:    64,
			DedupThreshold:  0.92,
			DefaultTTLHours: 720,
			VectorDimension: 768,
		},
	}
}

func TestUAT_Validate_ChunkOverlapNeg1(t *testing.T) {
	cfg := validCfg()
	cfg.Memory.ChunkOverlap = -1
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for ChunkOverlap = -1")
	}
	if !strings.Contains(err.Error(), "chunk_overlap") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUAT_Validate_ChunkSizeZero(t *testing.T) {
	cfg := validCfg()
	cfg.Memory.ChunkSize = 0
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for ChunkSize = 0")
	}
	if !strings.Contains(err.Error(), "chunk_size") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUAT_Validate_DedupThreshold1_5(t *testing.T) {
	cfg := validCfg()
	cfg.Memory.DedupThreshold = 1.5
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for DedupThreshold = 1.5")
	}
	if !strings.Contains(err.Error(), "dedup_threshold") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUAT_Validate_VectorDimensionZero(t *testing.T) {
	cfg := validCfg()
	cfg.Memory.VectorDimension = 0
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for VectorDimension = 0")
	}
	if !strings.Contains(err.Error(), "vector_dimension") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUAT_Validate_EmptyHost(t *testing.T) {
	cfg := validCfg()
	cfg.Qdrant.Host = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for empty qdrant.host")
	}
}

func TestUAT_Validate_EmptyCollection(t *testing.T) {
	cfg := validCfg()
	cfg.Qdrant.Collection = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for empty qdrant.collection")
	}
}

func TestUAT_Validate_EmptyOllamaBaseURL(t *testing.T) {
	cfg := validCfg()
	cfg.Ollama.BaseURL = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for empty ollama.base_url")
	}
}

func TestUAT_Validate_ChunkOverlapGteChunkSize(t *testing.T) {
	cfg := validCfg()
	cfg.Memory.ChunkOverlap = 512
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for ChunkOverlap >= ChunkSize")
	}
	if !strings.Contains(err.Error(), "chunk_overlap") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUAT_Validate_DedupThresholdNeg(t *testing.T) {
	cfg := validCfg()
	cfg.Memory.DedupThreshold = -0.1
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for DedupThreshold = -0.1")
	}
}

func TestUAT_Validate_NegativeTTL(t *testing.T) {
	cfg := validCfg()
	cfg.Memory.DefaultTTLHours = -1
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for DefaultTTLHours = -1")
	}
}

func TestUAT_Validate_ValidConfigPasses(t *testing.T) {
	cfg := validCfg()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid config should pass, got: %v", err)
	}
}
