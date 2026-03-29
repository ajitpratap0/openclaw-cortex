package longmemeval

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// lmeSessionTurn is one turn from a LongMemEval session array.
type lmeSessionTurn struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// lmeRecord is one top-level record in the LongMemEval JSON array.
type lmeRecord struct {
	QuestionID   string             `json:"question_id"`
	Question     string             `json:"question"`
	Answer       string             `json:"answer"`
	QuestionType string             `json:"question_type"`
	Sessions     [][]lmeSessionTurn `json:"sessions"`
}

// normalizeQuestionType maps LongMemEval question_type strings to the canonical
// lowercase category values used by the existing QAPair.Category field.
func normalizeQuestionType(qt string) string {
	qt = strings.TrimSpace(qt)
	switch strings.ToLower(qt) {
	case "temporal", "temporal reasoning":
		return "temporal"
	case "multi-hop", "multi_hop", "multihop":
		return "multi-hop"
	case "knowledge-update", "knowledge_update", "knowledge update":
		return "knowledge-update"
	default:
		return strings.ToLower(qt)
	}
}

// LoadDataset parses a LongMemEval JSON file and returns QAPairs.
//
// LongMemEval file format:
//
//	[
//	  {
//	    "question_id": "...",
//	    "question": "...",
//	    "answer": "...",
//	    "question_type": "...",
//	    "sessions": [
//	      [ {"role": "user", "content": "..."}, {"role": "assistant", "content": "..."}, ... ],
//	      ...
//	    ]
//	  },
//	  ...
//	]
//
// Sessions are flattened into Facts: each turn becomes one MemoryFact with
// Content = "<role>: <content>". Returns an error if the file cannot be opened
// or if the JSON structure does not match the expected format.
func LoadDataset(path string) ([]QAPair, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is caller-supplied, not user-supplied via web.
	if err != nil {
		return nil, fmt.Errorf("longmemeval: open dataset %q: %w", path, err)
	}

	var records []lmeRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("longmemeval: parse dataset JSON: %w", err)
	}

	pairs := make([]QAPair, 0, len(records))
	for i := range records {
		rec := &records[i]

		id := rec.QuestionID
		if id == "" {
			id = fmt.Sprintf("lme-full-%d", i)
		}

		// Flatten all sessions into MemoryFact entries.
		var facts []MemoryFact
		for si := range rec.Sessions {
			for ti := range rec.Sessions[si] {
				turn := &rec.Sessions[si][ti]
				content := fmt.Sprintf("%s: %s", turn.Role, turn.Content)
				facts = append(facts, MemoryFact{Content: content})
			}
		}

		pairs = append(pairs, QAPair{
			ID:          id,
			Facts:       facts,
			Question:    rec.Question,
			GroundTruth: rec.Answer,
			Category:    normalizeQuestionType(rec.QuestionType),
		})
	}

	return pairs, nil
}
