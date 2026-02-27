package tests

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ajitpratap0/openclaw-cortex/pkg/tokenizer"
)

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		minExpect int
		maxExpect int
	}{
		{"empty", "", 0, 0},
		{"single word", "hello", 1, 3},
		{"short sentence", "Go is a great programming language", 5, 15},
		{"longer text", strings.Repeat("word ", 100), 80, 200},
		// cl100k_base calibration cases
		{"pangram calibration", "The quick brown fox jumps over the lazy dog", 8, 15},
		{"code-like text", "func foo() { return nil }", 4, 15},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := tokenizer.EstimateTokens(tt.text)
			assert.GreaterOrEqual(t, tokens, tt.minExpect)
			assert.LessOrEqual(t, tokens, tt.maxExpect)
		})
	}
}

func TestEstimateTokensAdditional(t *testing.T) {
	t.Run("empty string returns zero", func(t *testing.T) {
		assert.Equal(t, 0, tokenizer.EstimateTokens(""))
	})

	t.Run("single word returns at least one", func(t *testing.T) {
		assert.GreaterOrEqual(t, tokenizer.EstimateTokens("hello"), 1)
	})

	t.Run("pangram 9-word calibration text", func(t *testing.T) {
		// "The quick brown fox jumps over the lazy dog" — 9 words, 43 chars
		// cl100k_base tokenizes this as ~9 tokens; heuristic should be in 8–15 range
		tokens := tokenizer.EstimateTokens("The quick brown fox jumps over the lazy dog")
		assert.GreaterOrEqual(t, tokens, 8)
		assert.LessOrEqual(t, tokens, 15)
	})

	t.Run("code-like text has reasonable estimate", func(t *testing.T) {
		tokens := tokenizer.EstimateTokens("func foo() { return nil }")
		assert.GreaterOrEqual(t, tokens, 4)
		assert.LessOrEqual(t, tokens, 20)
	})
}

func TestTruncateToTokenBudget(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		budget int
		check  func(t *testing.T, result string)
	}{
		{
			name:   "within budget",
			text:   "short text",
			budget: 100,
			check: func(t *testing.T, result string) {
				assert.Equal(t, "short text", result)
			},
		},
		{
			name:   "exceeds budget",
			text:   strings.Repeat("word ", 200),
			budget: 10,
			check: func(t *testing.T, result string) {
				assert.Less(t, len(result), len(strings.Repeat("word ", 200)))
				assert.True(t, strings.HasSuffix(result, "..."))
			},
		},
		{
			name:   "zero budget",
			text:   "some text",
			budget: 0,
			check: func(t *testing.T, result string) {
				assert.Equal(t, "", result)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tokenizer.TruncateToTokenBudget(tt.text, tt.budget)
			tt.check(t, result)
		})
	}
}

func TestFormatMemoriesWithBudget(t *testing.T) {
	memories := []string{
		"Memory one: Go is great",
		"Memory two: Testing is important",
		"Memory three: Qdrant is a vector database",
		"Memory four: Claude can extract memories",
	}

	t.Run("fits all", func(t *testing.T) {
		result, count := tokenizer.FormatMemoriesWithBudget(memories, 10000)
		assert.Equal(t, len(memories), count)
		assert.Contains(t, result, "Memory one")
		assert.Contains(t, result, "Memory four")
	})

	t.Run("partial fit", func(t *testing.T) {
		result, count := tokenizer.FormatMemoriesWithBudget(memories, 15)
		assert.Greater(t, count, 0)
		assert.Less(t, count, len(memories))
		assert.Contains(t, result, "Memory one")
	})

	t.Run("empty input", func(t *testing.T) {
		result, count := tokenizer.FormatMemoriesWithBudget(nil, 100)
		assert.Equal(t, 0, count)
		assert.Equal(t, "", result)
	})

	t.Run("zero budget", func(t *testing.T) {
		result, count := tokenizer.FormatMemoriesWithBudget(memories, 0)
		assert.Equal(t, 0, count)
		assert.Equal(t, "", result)
	})

	t.Run("respects budget — token count of result is within budget", func(t *testing.T) {
		budget := 20
		result, count := tokenizer.FormatMemoriesWithBudget(memories, budget)
		assert.Greater(t, count, 0)
		// The returned string should itself estimate to at most ~budget tokens.
		// Allow a small overage for the separator tokens that are counted separately.
		assert.LessOrEqual(t, tokenizer.EstimateTokens(result), budget+10)
	})
}

func TestTruncateToTokenBudgetSmallBudget(t *testing.T) {
	t.Run("budget 10 produces fewer tokens than original", func(t *testing.T) {
		original := strings.Repeat("word ", 100) // ~157 tokens with new heuristic
		budget := 10
		result := tokenizer.TruncateToTokenBudget(original, budget)
		assert.Less(t, len(result), len(original))
		assert.LessOrEqual(t, tokenizer.EstimateTokens(result), budget*3)
	})
}
