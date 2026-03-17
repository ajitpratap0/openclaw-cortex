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

// CaptureQualityConfig controls capture extraction quality.
type CaptureQualityConfig struct {
	ContextWindowTurns           int      `mapstructure:"context_window_turns"`
	ReinforcementThreshold       float64  `mapstructure:"reinforcement_threshold"`
	ReinforcementConfidenceBoost float64  `mapstructure:"reinforcement_confidence_boost"`
	MinUserMessageLength         int      `mapstructure:"min_user_message_length"`
	MinAssistantMessageLength    int      `mapstructure:"min_assistant_message_length"`
	BlocklistPatterns            []string `mapstructure:"blocklist_patterns"`
}

// SentryConfig holds Sentry error tracking settings.
type SentryConfig struct {
	DSN         string `mapstructure:"dsn"`
	Environment string `mapstructure:"environment"`
}

// Config holds all configuration for cortex.
type Config struct {
	Memgraph         MemgraphConfig         `mapstructure:"memgraph"`
	Ollama           OllamaConfig           `mapstructure:"ollama"`
	Claude           ClaudeConfig           `mapstructure:"claude"`
	Memory           MemoryConfig           `mapstructure:"memory"`
	Logging          LoggingConfig          `mapstructure:"logging"`
	API              APIConfig              `mapstructure:"api"`
	Embedder         EmbedderConfig         `mapstructure:"embedder"`
	Recall           RecallConfig           `mapstructure:"recall"`
	CaptureQuality   CaptureQualityConfig   `mapstructure:"capture_quality"`
	EntityResolution EntityResolutionConfig `mapstructure:"entity_resolution"`
	FactExtraction   FactExtractionConfig   `mapstructure:"fact_extraction"`
	Sentry           SentryConfig           `mapstructure:"sentry"`
}

// MemgraphConfig holds Memgraph database connection settings.
type MemgraphConfig struct {
	URI      string `mapstructure:"uri"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	Database string `mapstructure:"database"`
}

// EntityResolutionConfig holds entity resolution parameters.
type EntityResolutionConfig struct {
	SimilarityThreshold float64 `mapstructure:"similarity_threshold"`
	MaxCandidates       int     `mapstructure:"max_candidates"`
}

// FactExtractionConfig holds fact extraction settings.
type FactExtractionConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

// RecallConfig holds re-ranking and latency budget settings for recall.
type RecallConfig struct {
	RerankScoreSpreadThreshold float64             `mapstructure:"rerank_score_spread_threshold"`
	RerankLatencyBudgetHooksMs int                 `mapstructure:"rerank_latency_budget_hooks_ms"`
	RerankLatencyBudgetCLIMs   int                 `mapstructure:"rerank_latency_budget_cli_ms"`
	GraphBudgetMs              int                 `mapstructure:"graph_budget_ms"`
	GraphBudgetCLIMs           int                 `mapstructure:"graph_budget_cli_ms"`
	Weights                    RecallWeightsConfig `mapstructure:"weights"`
}

// RecallWeightsConfig holds the scoring weights for the recall ranking formula.
type RecallWeightsConfig struct {
	Similarity    float64 `mapstructure:"similarity"`
	Recency       float64 `mapstructure:"recency"`
	Frequency     float64 `mapstructure:"frequency"`
	TypeBoost     float64 `mapstructure:"type_boost"`
	ScopeBoost    float64 `mapstructure:"scope_boost"`
	Confidence    float64 `mapstructure:"confidence"`
	Reinforcement float64 `mapstructure:"reinforcement"`
	TagAffinity   float64 `mapstructure:"tag_affinity"`
}

// APIConfig holds HTTP API server settings.
type APIConfig struct {
	ListenAddr   string `mapstructure:"listen_addr"`
	AuthToken    string `mapstructure:"auth_token"`
	CursorSecret string `mapstructure:"cursor_secret"`
}

// OllamaConfig holds Ollama embedding service settings.
type OllamaConfig struct {
	BaseURL string `mapstructure:"base_url"`
	Model   string `mapstructure:"model"`
}

// EmbedderConfig selects which embedding provider to use and holds provider-specific settings.
type EmbedderConfig struct {
	// Provider selects the embedding backend: "ollama" (default) or "openai".
	Provider string `mapstructure:"provider"`

	// OpenAIKey is the OpenAI API key. May also be supplied via OPENCLAW_CORTEX_OPENAI_API_KEY.
	OpenAIKey string `mapstructure:"openai_api_key"`

	// OpenAIModel is the OpenAI embedding model. Defaults to "text-embedding-3-small".
	OpenAIModel string `mapstructure:"openai_model"`

	// OpenAIDim is the number of dimensions requested from the OpenAI API.
	// Defaults to 768 to maintain compatibility with existing collections.
	OpenAIDim int `mapstructure:"openai_dimensions"`
}

// ClaudeConfig holds Anthropic Claude API settings.
type ClaudeConfig struct {
	APIKey       string `mapstructure:"api_key"`
	Model        string `mapstructure:"model"`
	GatewayURL   string `mapstructure:"gateway_url"`
	GatewayToken string `mapstructure:"gateway_token"`
}

// String returns a safe representation of ClaudeConfig with the API key masked.
func (c ClaudeConfig) String() string {
	masked := maskAPIKey(c.APIKey)
	return fmt.Sprintf("ClaudeConfig{APIKey:%s, Model:%s, GatewayURL:%s}", masked, c.Model, c.GatewayURL)
}

// String returns a safe representation of EmbedderConfig with the API key masked.
func (c EmbedderConfig) String() string {
	return fmt.Sprintf("EmbedderConfig{Provider:%s, OpenAIKey:%s, Model:%s, Dim:%d}",
		c.Provider, maskAPIKey(c.OpenAIKey), c.OpenAIModel, c.OpenAIDim)
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
	v.SetDefault("memgraph.uri", "bolt://localhost:7687")
	v.SetDefault("memgraph.username", "")
	v.SetDefault("memgraph.password", "")
	v.SetDefault("memgraph.database", "")

	v.SetDefault("ollama.base_url", "http://localhost:11434")
	v.SetDefault("ollama.model", "nomic-embed-text")

	v.SetDefault("embedder.provider", "ollama")
	v.SetDefault("embedder.openai_model", "text-embedding-3-small")
	v.SetDefault("embedder.openai_dimensions", 768)

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
	v.SetDefault("api.cursor_secret", "")

	v.SetDefault("recall.rerank_score_spread_threshold", 0.15)
	v.SetDefault("recall.rerank_latency_budget_hooks_ms", 100)
	v.SetDefault("recall.rerank_latency_budget_cli_ms", 3000)
	v.SetDefault("recall.graph_budget_ms", 50)
	v.SetDefault("recall.graph_budget_cli_ms", 500)

	v.SetDefault("recall.weights.similarity", 0.35)
	v.SetDefault("recall.weights.recency", 0.15)
	v.SetDefault("recall.weights.frequency", 0.10)
	v.SetDefault("recall.weights.type_boost", 0.10)
	v.SetDefault("recall.weights.scope_boost", 0.08)
	v.SetDefault("recall.weights.confidence", 0.10)
	v.SetDefault("recall.weights.reinforcement", 0.07)
	v.SetDefault("recall.weights.tag_affinity", 0.05)

	v.SetDefault("entity_resolution.similarity_threshold", 0.95)
	v.SetDefault("entity_resolution.max_candidates", 10)
	v.SetDefault("fact_extraction.enabled", true)

	v.SetDefault("capture_quality.context_window_turns", 3)
	v.SetDefault("capture_quality.reinforcement_threshold", 0.80)
	v.SetDefault("capture_quality.reinforcement_confidence_boost", 0.05)
	v.SetDefault("capture_quality.min_user_message_length", 20)
	v.SetDefault("capture_quality.min_assistant_message_length", 20)
	v.SetDefault("capture_quality.blocklist_patterns", []string{"HEARTBEAT_OK", "NO_REPLY"})

	v.SetDefault("sentry.dsn", "")
	v.SetDefault("sentry.environment", "production")
	_ = v.BindEnv("sentry.dsn", "SENTRY_DSN")
	_ = v.BindEnv("sentry.environment", "SENTRY_ENVIRONMENT")

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
	_ = v.BindEnv("memgraph.uri", "OPENCLAW_CORTEX_MEMGRAPH_URI")
	_ = v.BindEnv("memgraph.username", "OPENCLAW_CORTEX_MEMGRAPH_USERNAME")
	_ = v.BindEnv("memgraph.password", "OPENCLAW_CORTEX_MEMGRAPH_PASSWORD")
	_ = v.BindEnv("memgraph.database", "OPENCLAW_CORTEX_MEMGRAPH_DATABASE")
	_ = v.BindEnv("ollama.base_url", "OPENCLAW_CORTEX_OLLAMA_BASE_URL")
	_ = v.BindEnv("api.listen_addr", "OPENCLAW_CORTEX_API_LISTEN_ADDR")
	_ = v.BindEnv("api.auth_token", "OPENCLAW_CORTEX_API_AUTH_TOKEN")
	_ = v.BindEnv("api.cursor_secret", "OPENCLAW_CORTEX_API_CURSOR_SECRET")
	_ = v.BindEnv("embedder.provider", "OPENCLAW_CORTEX_EMBEDDER_PROVIDER")
	_ = v.BindEnv("embedder.openai_api_key", "OPENCLAW_CORTEX_OPENAI_API_KEY")
	_ = v.BindEnv("embedder.openai_model", "OPENCLAW_CORTEX_OPENAI_MODEL")
	_ = v.BindEnv("embedder.openai_dimensions", "OPENCLAW_CORTEX_OPENAI_DIMENSIONS")
	_ = v.BindEnv("claude.gateway_url", "OPENCLAW_GATEWAY_URL")
	_ = v.BindEnv("claude.gateway_token", "OPENCLAW_GATEWAY_TOKEN")

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
	if c.Memgraph.URI == "" {
		return fmt.Errorf("memgraph.uri must not be empty")
	}
	if c.Ollama.BaseURL == "" {
		return fmt.Errorf("ollama.base_url must not be empty")
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
	if c.Embedder.Provider == "openai" && c.Embedder.OpenAIKey == "" {
		return fmt.Errorf("embedder.openai_api_key must not be empty when provider is \"openai\"")
	}

	// Validate provider name.
	switch c.Embedder.Provider {
	case "ollama", "openai", "":
		// valid
	default:
		return fmt.Errorf("embedder.provider must be \"ollama\" or \"openai\", got %q", c.Embedder.Provider)
	}

	// Validate OpenAI-specific fields when openai provider is selected.
	if c.Embedder.Provider == "openai" {
		if c.Embedder.OpenAIDim <= 0 {
			return fmt.Errorf("embedder.openai_dimensions must be > 0 when provider is \"openai\"")
		}
		if int(c.Memory.VectorDimension) != c.Embedder.OpenAIDim {
			return fmt.Errorf("embedder.openai_dimensions (%d) must match memory.vector_dimension (%d)",
				c.Embedder.OpenAIDim, c.Memory.VectorDimension)
		}
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
