package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

const (
	// DefaultChunkSize is the default character count per text chunk for indexing.
	DefaultChunkSize = 512

	// DefaultChunkOverlap is the default character overlap between adjacent chunks.
	DefaultChunkOverlap = 64

	// DefaultDedupThreshold is the default cosine similarity threshold for deduplication.
	DefaultDedupThreshold = 0.92
)

// Config holds all configuration for cortex.
type Config struct {
	Qdrant  QdrantConfig  `mapstructure:"qdrant"`
	Ollama  OllamaConfig  `mapstructure:"ollama"`
	Claude  ClaudeConfig  `mapstructure:"claude"`
	Memory  MemoryConfig  `mapstructure:"memory"`
	Logging LoggingConfig `mapstructure:"logging"`
	API     APIConfig     `mapstructure:"api"`
}

// APIConfig holds HTTP API server settings.
type APIConfig struct {
	ListenAddr string `mapstructure:"listen_addr"`
	AuthToken  string `mapstructure:"auth_token"`
}

// QdrantConfig holds Qdrant vector database connection settings.
type QdrantConfig struct {
	Host       string `mapstructure:"host"`
	GRPCPort   int    `mapstructure:"grpc_port"`
	HTTPPort   int    `mapstructure:"http_port"`
	Collection string `mapstructure:"collection"`
	UseTLS     bool   `mapstructure:"use_tls"`
}

// OllamaConfig holds Ollama embedding service settings.
type OllamaConfig struct {
	BaseURL string `mapstructure:"base_url"`
	Model   string `mapstructure:"model"`
}

// ClaudeConfig holds Anthropic Claude API settings.
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

// MemoryConfig holds memory indexing and deduplication settings.
type MemoryConfig struct {
	MemoryDir          string  `mapstructure:"memory_dir"`
	ChunkSize          int     `mapstructure:"chunk_size"`
	ChunkOverlap       int     `mapstructure:"chunk_overlap"`
	DedupThreshold     float64 `mapstructure:"dedup_threshold"`
	DedupThresholdHook float64 `mapstructure:"dedup_threshold_hook"` // default 0.95
	DefaultTTLHours    int     `mapstructure:"default_ttl_hours"`
	VectorDimension    uint64  `mapstructure:"vector_dimension"`
}

// LoggingConfig holds structured logging settings.
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

	v.SetDefault("claude.model", "claude-haiku-4-5-20251001")

	v.SetDefault("memory.memory_dir", filepath.Join(homeDir(), ".openclaw", "workspace", "memory"))
	v.SetDefault("memory.chunk_size", DefaultChunkSize)
	v.SetDefault("memory.chunk_overlap", DefaultChunkOverlap)
	v.SetDefault("memory.dedup_threshold", DefaultDedupThreshold)
	v.SetDefault("memory.dedup_threshold_hook", 0.95)
	v.SetDefault("memory.default_ttl_hours", 720) // 30 days
	v.SetDefault("memory.vector_dimension", 768)

	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.format", "text")

	v.SetDefault("api.listen_addr", ":8080")
	v.SetDefault("api.auth_token", "")

	// Config file
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(filepath.Join(homeDir(), ".openclaw-cortex"))
	v.AddConfigPath(".")

	// Environment variables
	v.SetEnvPrefix("OPENCLAW_CORTEX")
	v.AutomaticEnv()

	// Map specific env vars
	_ = v.BindEnv("claude.api_key", "ANTHROPIC_API_KEY")
	_ = v.BindEnv("qdrant.host", "OPENCLAW_CORTEX_QDRANT_HOST")
	_ = v.BindEnv("qdrant.grpc_port", "OPENCLAW_CORTEX_QDRANT_GRPC_PORT")
	_ = v.BindEnv("ollama.base_url", "OPENCLAW_CORTEX_OLLAMA_BASE_URL")
	_ = v.BindEnv("api.listen_addr", "OPENCLAW_CORTEX_API_LISTEN_ADDR")
	_ = v.BindEnv("api.auth_token", "OPENCLAW_CORTEX_API_AUTH_TOKEN")

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

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}

	return &cfg, nil
}

// Validate checks that required configuration fields are set and consistent.
func (c *Config) Validate() error {
	if c.Qdrant.Host == "" {
		return fmt.Errorf("qdrant.host must not be empty")
	}
	if c.Ollama.BaseURL == "" {
		return fmt.Errorf("ollama.base_url must not be empty")
	}
	if c.Qdrant.Collection == "" {
		return fmt.Errorf("qdrant.collection must not be empty")
	}
	if c.Memory.ChunkSize <= 0 {
		return fmt.Errorf("memory.chunk_size must be greater than 0")
	}
	if c.Memory.ChunkOverlap < 0 {
		return fmt.Errorf("memory.chunk_overlap must be >= 0")
	}
	if c.Memory.ChunkOverlap >= c.Memory.ChunkSize {
		return fmt.Errorf("memory.chunk_overlap (%d) must be less than memory.chunk_size (%d)", c.Memory.ChunkOverlap, c.Memory.ChunkSize)
	}
	if c.Memory.DedupThreshold < 0 || c.Memory.DedupThreshold > 1 {
		return fmt.Errorf("memory.dedup_threshold must be between 0 and 1")
	}
	if c.Memory.DedupThresholdHook < 0 || c.Memory.DedupThresholdHook > 1 {
		return fmt.Errorf("memory.dedup_threshold_hook must be between 0 and 1")
	}
	if c.Memory.VectorDimension <= 0 {
		return fmt.Errorf("memory.vector_dimension must be greater than 0")
	}
	if c.Memory.DefaultTTLHours < 0 {
		return fmt.Errorf("memory.default_ttl_hours must be >= 0")
	}
	return nil
}

func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return home
}
