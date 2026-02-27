package tokenizer

import (
	"strings"
)

// EstimateTokens returns an approximate token count for the given text.
// Heuristic: calibrated to cl100k_base tokenization used by Claude and GPT-4.
// Accuracy: ±15% for typical English prose; may undercount for code or non-ASCII.
func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	chars := len(text)
	words := len(strings.Fields(text))

	// cl100k_base produces roughly 1 token per 3.5–4 chars for English.
	// Short words (articles, prepositions) are usually 1 token each,
	// longer words may be split into 2–3 tokens.
	// Use max(words * 1.25, chars / 3.5) with a 10% safety margin.
	byWord := float64(words) * 1.25
	byChar := float64(chars) / 3.5

	estimate := byWord
	if byChar > estimate {
		estimate = byChar
	}

	// Add 10% safety margin to avoid underestimating (which risks exceeding budget).
	return int(estimate * 1.1)
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

	// Approximate: 3.5 chars per token (calibrated to cl100k_base)
	maxChars := int(float64(budget) * 3.5)
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
