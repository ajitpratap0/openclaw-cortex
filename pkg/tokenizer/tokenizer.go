package tokenizer

import (
	"strings"
)

// EstimateTokens provides a rough token count estimate.
// Uses the heuristic of ~4 characters per token for English text.
func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	// Count words and characters for a blended estimate
	words := len(strings.Fields(text))
	chars := len(text)

	// Heuristic: average of word-based and char-based estimates
	wordEstimate := int(float64(words) * 1.3) // ~1.3 tokens per word
	charEstimate := chars / 4                 // ~4 chars per token

	return (wordEstimate + charEstimate) / 2
}

// TruncateToTokenBudget truncates text to approximately fit within a token budget.
func TruncateToTokenBudget(text string, budget int) string {
	if budget <= 0 {
		return ""
	}

	tokens := EstimateTokens(text)
	if tokens <= budget {
		return text
	}

	// Approximate: 4 chars per token
	maxChars := budget * 4
	if maxChars >= len(text) {
		return text
	}

	// Truncate at word boundary
	truncated := text[:maxChars]
	lastSpace := strings.LastIndex(truncated, " ")
	if lastSpace > maxChars/2 {
		truncated = truncated[:lastSpace]
	}

	return truncated + "..."
}

// FormatMemoriesWithBudget formats multiple memory strings within a token budget.
// Returns the formatted string and the number of memories that fit.
func FormatMemoriesWithBudget(memories []string, budget int) (string, int) {
	if budget <= 0 || len(memories) == 0 {
		return "", 0
	}

	var builder strings.Builder
	count := 0
	usedTokens := 0

	for _, mem := range memories {
		memTokens := EstimateTokens(mem) + 2 // +2 for separator
		if usedTokens+memTokens > budget {
			break
		}
		if count > 0 {
			builder.WriteString("\n---\n")
		}
		builder.WriteString(mem)
		usedTokens += memTokens
		count++
	}

	return builder.String(), count
}
