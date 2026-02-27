package recall

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
)

const (
	// defaultReasonerCandidates is how many top results are passed to Claude for re-ranking.
	defaultReasonerCandidates = 10

	// reasonerMaxTokens is the maximum tokens Claude can use for the re-ranking response.
	reasonerMaxTokens = 512
)

// Reasoner uses Claude to re-rank recall results by genuine relevance to the query.
// It addresses the "similarity ≠ relevance" problem: vector similarity finds
// related content, but Claude can reason about which memories are truly useful.
//
// On any API failure the Reasoner degrades gracefully and returns results in
// their original order so the caller always gets a usable response.
type Reasoner struct {
	client *anthropic.Client
	model  string
	logger *slog.Logger
}

// NewReasoner creates a Reasoner backed by the Anthropic Claude API.
func NewReasoner(apiKey, model string, logger *slog.Logger) *Reasoner {
	c := anthropic.NewClient(option.WithAPIKey(apiKey))
	return &Reasoner{
		client: &c,
		model:  model,
		logger: logger,
	}
}

// ReRank passes the top maxCandidates results to Claude and returns them
// reordered by relevance to query. Results beyond maxCandidates are appended
// unchanged after the re-ranked set.
//
// If maxCandidates <= 0, defaultReasonerCandidates is used.
// On any error (API failure, unexpected response) the original order is returned.
func (r *Reasoner) ReRank(ctx context.Context, query string, results []models.RecallResult, maxCandidates int) ([]models.RecallResult, error) {
	if len(results) == 0 {
		return results, nil
	}
	if maxCandidates <= 0 {
		maxCandidates = defaultReasonerCandidates
	}

	candidates := results
	var tail []models.RecallResult
	if len(results) > maxCandidates {
		candidates = results[:maxCandidates]
		tail = results[maxCandidates:]
	}

	// Build numbered memory list for the prompt.
	var sb strings.Builder
	for i, c := range candidates {
		fmt.Fprintf(&sb, "[%d] %s\n", i, reasonerXMLEscape(c.Memory.Content))
	}

	prompt := fmt.Sprintf(`You are a memory relevance ranker for an AI agent memory system.

Given the query and a numbered list of memory snippets, output a JSON array of the indices ordered from MOST to LEAST relevant to the query. Include every index exactly once.

Output ONLY a valid JSON array of integers, nothing else. Example: [2, 0, 3, 1]

<query>%s</query>

<memories>
%s</memories>`,
		reasonerXMLEscape(query),
		sb.String(),
	)

	resp, err := r.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(r.model),
		MaxTokens: reasonerMaxTokens,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})
	if err != nil {
		r.logger.Warn("reasoner: Claude API call failed, using original order", "error", err)
		return results, nil
	}

	// Extract the text block from the response.
	var responseText string
	for i := range resp.Content {
		if resp.Content[i].Type == "text" {
			responseText = strings.TrimSpace(resp.Content[i].Text)
			break
		}
	}
	if responseText == "" {
		r.logger.Warn("reasoner: empty response from Claude, using original order")
		return results, nil
	}

	// Parse the index ordering.
	var order []int
	if err := json.Unmarshal([]byte(responseText), &order); err != nil {
		r.logger.Warn("reasoner: could not parse Claude response, using original order",
			"response", responseText, "error", err)
		return results, nil
	}

	// Apply the ordering, guarding against out-of-range or duplicate indices.
	seen := make(map[int]bool, len(candidates))
	reranked := make([]models.RecallResult, 0, len(candidates))
	for _, idx := range order {
		if idx >= 0 && idx < len(candidates) && !seen[idx] {
			reranked = append(reranked, candidates[idx])
			seen[idx] = true
		}
	}
	// Append any candidates Claude omitted (shouldn't happen, but be safe).
	for i := range candidates {
		if !seen[i] {
			reranked = append(reranked, candidates[i])
		}
	}

	r.logger.Debug("reasoner: re-ranked results", "candidates", len(candidates), "order", order)
	return append(reranked, tail...), nil
}

// reasonerXMLEscape escapes characters that have special meaning in XML to
// prevent prompt injection when embedding user/memory content in XML tags.
func reasonerXMLEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}
