package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

// Config holds all configuration for cortex.
type Config struct {
	Qdrant  QdrantConfig  `mapstructure:"qdrant"`
	Ollama  OllamaConfig  `mapstructure:"ollama"`
	Claude  ClaudeConfig  `mapstructure:"claude"`
	Memory  MemoryConfig  `mapstructure:"memory"`
	Logging LoggingConfig `mapstructure:"logging"`
}

type QdrantConfig struct {
	Host       string `mapstructure:"host"`
	GRPCPort   int    `mapstructure:"grpc_port"`
	HTTPPort   int    `mapstructure:"http_port"`
	Collection string `mapstructure:"collection"`
	UseTLS     bool   `mapstructure:"use_tls"`
}

type OllamaConfig struct {
	BaseURL string `mapstructure:"base_url"`
	Model   string `mapstructure:"model"`
}

type ClaudeConfig struct {
	APIKey string `mapstructure:"api_key"`
	Model  string `mapstructure:"model"`
}

// String returns a safe representation of ClaudeConfig with the API key masked.
func (c ClaudeConfig) String() string {
	masked := maskAPIKey(c.APIKey)
	return fmt.Sprintf("ClaudeConfig{APIKey:%s, Model:%s}", masked, c.Model)
}

// maskAPIKey shows first 4 + last 4 chars, replacing the middle with asterisks.
func maskAPIKey(key string) string {
	const visible = 4
	if len(key) <= visible*2 {
		return "***"
	}
	return key[:visible] + "****" + key[len(key)-visible:]
}

type MemoryConfig struct {
	MemoryDir       string  `mapstructure:"memory_dir"`
	ChunkSize       int     `mapstructure:"chunk_size"`
	ChunkOverlap    int     `mapstructure:"chunk_overlap"`
	DedupThreshold  float64 `mapstructure:"dedup_threshold"`
	DefaultTTLHours int     `mapstructure:"default_ttl_hours"`
	VectorDimension uint64  `mapstructure:"vector_dimension"`
}

type LoggingConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

// Load reads configuration from file and environment variables.
func Load() (*Config, error) {
	v := viper.New()

	// Defaults
	v.SetDefault("qdrant.host", "localhost")
	v.SetDefault("qdrant.grpc_port", 6334)
	v.SetDefault("qdrant.http_port", 6333)
	v.SetDefault("qdrant.collection", "cortex_memories")
	v.SetDefault("qdrant.use_tls", false)

	v.SetDefault("ollama.base_url", "http://localhost:11434")
	v.SetDefault("ollama.model", "nomic-embed-text")

	v.SetDefault("claude.model", "claude-haiku-4-5-20241022")

	v.SetDefault("memory.memory_dir", filepath.Join(homeDir(), ".openclaw", "workspace", "memory"))
	v.SetDefault("memory.chunk_size", 512)
	v.SetDefault("memory.chunk_overlap", 64)
	v.SetDefault("memory.dedup_threshold", 0.92)
	v.SetDefault("memory.default_ttl_hours", 720) // 30 days
	v.SetDefault("memory.vector_dimension", 768)

	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.format", "text")

	// Config file
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(filepath.Join(homeDir(), ".cortex"))
	v.AddConfigPath(".")

	// Environment variables
	v.SetEnvPrefix("CORTEX")
	v.AutomaticEnv()

	// Map specific env vars
	_ = v.BindEnv("claude.api_key", "ANTHROPIC_API_KEY")
	_ = v.BindEnv("qdrant.host", "OPENCLAW_CORTEX_QDRANT_HOST")
	_ = v.BindEnv("qdrant.grpc_port", "OPENCLAW_CORTEX_QDRANT_GRPC_PORT")
	_ = v.BindEnv("ollama.base_url", "OPENCLAW_CORTEX_OLLAMA_BASE_URL")

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("reading config: %w", err)
		}
		// Config file not found is OK — use defaults + env vars
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshalling config: %w", err)
	}

	return &cfg, nil
}

func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return home
}
