package embedder

import (
	"fmt"
	"log/slog"

	"github.com/ajitpratap0/openclaw-cortex/internal/config"
)

// New returns an Embedder implementation selected by embCfg.Provider.
//
// Supported providers:
//   - "" or "ollama" → OllamaEmbedder using ollaCfg
//   - "lmstudio"    → LMStudioEmbedder using embCfg.LMStudio
//
// Any other provider string is an error.
func New(ollaCfg config.OllamaConfig, embCfg config.EmbedderConfig, dimension int, logger *slog.Logger) (Embedder, error) {
	switch embCfg.Provider {
	case "", "ollama":
		return NewOllamaEmbedder(ollaCfg.BaseURL, ollaCfg.Model, dimension, logger), nil

	case "lmstudio":
		if embCfg.LMStudio.Model == "" {
			return nil, fmt.Errorf("embedder: lmstudio.model must not be empty when provider is \"lmstudio\"")
		}
		url := embCfg.LMStudio.URL
		if url == "" {
			url = "http://localhost:1234"
		}
		return NewLMStudioEmbedder(url, embCfg.LMStudio.Model), nil

	default:
		return nil, fmt.Errorf("embedder: unknown provider %q (supported: ollama, lmstudio)", embCfg.Provider)
	}
}
