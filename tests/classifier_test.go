package tests

import (
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ajitpratap0/cortex/internal/classifier"
	"github.com/ajitpratap0/cortex/internal/models"
)

func TestClassifier_ClassifyRules(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	cls := classifier.NewClassifier(logger)

	tests := []struct {
		name     string
		content  string
		expected models.MemoryType
	}{
		{
			name:     "must constraint",
			content:  "You must always validate input before processing",
			expected: models.MemoryTypeRule,
		},
		{
			name:     "never constraint",
			content:  "Never expose API keys in client-side code",
			expected: models.MemoryTypeRule,
		},
		{
			name:     "required keyword",
			content:  "Authentication is required for all API endpoints",
			expected: models.MemoryTypeRule,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cls.Classify(tt.content)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestClassifier_ClassifyProcedures(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	cls := classifier.NewClassifier(logger)

	tests := []struct {
		name     string
		content  string
		expected models.MemoryType
	}{
		{
			name:     "step by step",
			content:  "Step 1: Install dependencies. Step 2: Configure the database.",
			expected: models.MemoryTypeProcedure,
		},
		{
			name:     "how to",
			content:  "How to deploy the application to production",
			expected: models.MemoryTypeProcedure,
		},
		{
			name:     "run command",
			content:  "Run the test suite with: go test ./...",
			expected: models.MemoryTypeProcedure,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cls.Classify(tt.content)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestClassifier_ClassifyPreferences(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	cls := classifier.NewClassifier(logger)

	tests := []struct {
		name     string
		content  string
		expected models.MemoryType
	}{
		{
			name:     "prefer keyword",
			content:  "I prefer to use Go over Python for systems programming",
			expected: models.MemoryTypePreference,
		},
		{
			name:     "favorite",
			content:  "My favorite editor is Neovim",
			expected: models.MemoryTypePreference,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cls.Classify(tt.content)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestClassifier_ClassifyEpisodes(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	cls := classifier.NewClassifier(logger)

	tests := []struct {
		name     string
		content  string
		expected models.MemoryType
	}{
		{
			name:     "yesterday event",
			content:  "Yesterday we discovered a race condition in the scheduler",
			expected: models.MemoryTypeEpisode,
		},
		{
			name:     "incident",
			content:  "The incident occurred when the cache expired during peak load",
			expected: models.MemoryTypeEpisode,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cls.Classify(tt.content)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestClassifier_ClassifyFacts(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	cls := classifier.NewClassifier(logger)

	// Plain declarative statements default to fact
	result := cls.Classify("Go uses goroutines for concurrent programming")
	assert.Equal(t, models.MemoryTypeFact, result)

	result = cls.Classify("The API returns JSON responses")
	assert.Equal(t, models.MemoryTypeFact, result)
}
