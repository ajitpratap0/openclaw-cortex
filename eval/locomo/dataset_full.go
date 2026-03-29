package locomo

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// locomoSession is a single session turn from the LoCoMo JSON format.
// Each element in the session array is either a blenderbot_message or human_turn.
type locomoTurn struct {
	BlenderbotMessage string `json:"blenderbot_message"`
	HumanTurn         string `json:"human_turn"`
}

// locomoQAPair is one QA item from the LoCoMo JSON file.
type locomoQAPairRaw struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
	Category string `json:"category"`
}

// normalizeCategory maps LoCoMo dataset category strings to the canonical
// lowercase values used by the existing QAPair.Category field.
func normalizeCategory(raw string) string {
	switch strings.TrimSpace(raw) {
	case "Single-hop", "single-hop":
		return "single-hop"
	case "Multi-hop", "multi-hop":
		return "multi-hop"
	case "Temporal reasoning", "Temporal", "temporal":
		return "temporal"
	case "Adversarial", "adversarial":
		return "adversarial"
	default:
		return strings.ToLower(strings.TrimSpace(raw))
	}
}

// LoadDataset parses a LoCoMo JSON file and returns QAPairs.
//
// LoCoMo file format:
//
//	{
//	  "conv_0": {
//	    "session_1": [ {"blenderbot_message": "...", "human_turn": "..."}, ... ],
//	    "session_2": [ ... ],
//	    "question_answer_pairs": [ {"question":"...", "answer":"...", "category":"..."}, ... ]
//	  },
//	  "conv_1": { ... }
//	}
//
// Each QA pair in a conversation gets ConvTurns populated from ALL sessions of
// that conversation (for accumulate-mode ingestion).
//
// No network calls are made. Returns an error if the file cannot be opened or
// if the JSON structure does not match the expected format.
func LoadDataset(path string) ([]QAPair, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is caller-supplied, not user-supplied via web.
	if err != nil {
		return nil, fmt.Errorf("locomo: open dataset %q: %w", path, err)
	}

	// Top-level: map of conv_id → raw conversation object.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("locomo: parse dataset top-level JSON: %w", err)
	}

	var pairs []QAPair

	for convID, convRaw := range raw {
		// Parse the conversation as a map of string → json.RawMessage so we can
		// iterate over session_N keys and the question_answer_pairs key.
		var convFields map[string]json.RawMessage
		if err := json.Unmarshal(convRaw, &convFields); err != nil {
			return nil, fmt.Errorf("locomo: parse conversation %q: %w", convID, err)
		}

		// Collect all turns from every session in sorted key order.
		// Sessions are named "session_1", "session_2", etc.
		var sessionKeys []string
		for k := range convFields {
			if strings.HasPrefix(k, "session_") {
				sessionKeys = append(sessionKeys, k)
			}
		}
		sort.Strings(sessionKeys)
		var allTurns []ConvTurn
		for _, sessionKey := range sessionKeys {
			sessionRaw := convFields[sessionKey]
			var rawTurns []locomoTurn
			if err := json.Unmarshal(sessionRaw, &rawTurns); err != nil {
				return nil, fmt.Errorf("locomo: parse %s/%s: %w", convID, sessionKey, err)
			}
			for i := range rawTurns {
				t := &rawTurns[i]
				// Both fields may be present in the same turn object; we emit one
				// ConvTurn with User = human_turn, Assistant = blenderbot_message.
				if t.HumanTurn != "" || t.BlenderbotMessage != "" {
					allTurns = append(allTurns, ConvTurn{
						User:      t.HumanTurn,
						Assistant: t.BlenderbotMessage,
					})
				}
			}
		}

		// Parse the question_answer_pairs array.
		qaRaw, hasQA := convFields["question_answer_pairs"]
		if !hasQA {
			continue // skip conversations with no QA pairs
		}
		var qaPairs []locomoQAPairRaw
		if err := json.Unmarshal(qaRaw, &qaPairs); err != nil {
			return nil, fmt.Errorf("locomo: parse %s/question_answer_pairs: %w", convID, err)
		}

		for idx, qa := range qaPairs {
			pairs = append(pairs, QAPair{
				ID:           fmt.Sprintf("%s-q%d", convID, idx),
				Conversation: allTurns,
				Question:     qa.Question,
				GroundTruth:  qa.Answer,
				Category:     normalizeCategory(qa.Category),
			})
		}
	}

	return pairs, nil
}
