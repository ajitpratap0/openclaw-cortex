package capture

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

// conflictDetectorMaxTokens is the maximum number of tokens Claude can use for
// the contradiction detection response.
const conflictDetectorMaxTokens = 512

// conflictPromptTemplate is the prompt used to ask Claude whether a new memory
// contradicts any of the candidate memories. All user-supplied content is injected
// via xmlEscape to prevent prompt injection.
const conflictPromptTemplate = `You are a memory contradiction detector for an AI agent memory system.

Determine whether the new memory contradicts any of the existing memories listed below.

A contradiction means the new memory asserts something that is directly incompatible with
an existing memory (e.g., "Python is slow" vs "Python is fast"). Minor expansions or
clarifications are NOT contradictions.

Return ONLY a JSON object with this exact schema:
{"contradicts": <bool>, "contradicted_id": "<id or empty string>", "reason": "<brief explanation>"}

<new_memory>%s</new_memory>

<existing_memories>
%s</existing_memories>`

// conflictResponse is the JSON schema Claude returns for contradiction detection.
type conflictResponse struct {
	Contradicts    bool   `json:"contradicts"`
	ContradictedID string `json:"contradicted_id"`
	Reason         string `json:"reason"`
}

// ConflictDetector uses Claude to detect when a new memory contradicts existing ones.
// On any API error or JSON parse failure the detector degrades gracefully and returns
// (false, "", "", nil) so that the caller can always proceed with storing the memory.
type ConflictDetector struct {
	client *anthropic.Client
	model  string
	logger *slog.Logger
}

// NewConflictDetector creates a ConflictDetector backed by the Anthropic Claude API.
func NewConflictDetector(apiKey, model string, logger *slog.Logger) *ConflictDetector {
	c := anthropic.NewClient(option.WithAPIKey(apiKey))
	return &ConflictDetector{
		client: &c,
		model:  model,
		logger: logger,
	}
}

// Detect returns (true, contradictedID, reason, nil) if newContent contradicts any
// candidate memory.  On any API error or parse failure it logs a warning and returns
// (false, "", "", nil) — the safe default is to store the memory anyway.
func (d *ConflictDetector) Detect(ctx context.Context, newContent string, candidates []models.Memory) (bool, string, string, error) {
	if len(candidates) == 0 {
		return false, "", "", nil
	}

	// Build the numbered list of existing memories for the prompt.
	var sb strings.Builder
	for i := range candidates {
		fmt.Fprintf(&sb, "[%s] %s\n", xmlEscape(candidates[i].ID), xmlEscape(candidates[i].Content))
	}

	prompt := fmt.Sprintf(conflictPromptTemplate, xmlEscape(newContent), sb.String())

	resp, err := d.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(d.model),
		MaxTokens: conflictDetectorMaxTokens,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
		System: []anthropic.TextBlockParam{
			{Text: "You are a precise contradiction detection system. Output only valid JSON."},
		},
	})
	if err != nil {
		d.logger.Warn("conflict_detector: Claude API call failed, skipping contradiction check", "error", err)
		return false, "", "", nil
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
		d.logger.Warn("conflict_detector: empty response from Claude, skipping contradiction check")
		return false, "", "", nil
	}

	d.logger.Debug("conflict_detector: Claude response", "response", responseText)

	var result conflictResponse
	if parseErr := json.Unmarshal([]byte(responseText), &result); parseErr != nil {
		d.logger.Warn("conflict_detector: could not parse Claude response, skipping contradiction check",
			"response", responseText, "error", parseErr)
		return false, "", "", nil
	}

	if result.Contradicts && result.ContradictedID != "" {
		// Validate the ID actually came from candidates
		valid := false
		for i := range candidates {
			if candidates[i].ID == result.ContradictedID {
				valid = true
				break
			}
		}
		if !valid {
			d.logger.Warn("conflict_detector: ContradictedID not found in candidates, ignoring",
				"contradicted_id", result.ContradictedID)
			return false, "", "", nil
		}
	}

	return result.Contradicts, result.ContradictedID, result.Reason, nil
}
