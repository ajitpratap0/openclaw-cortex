// Package dmr provides the DMR (Deep Memory Retrieval) benchmark harness.
//
// DMR is described in the Zep paper "Deep Memory Retrieval" (arXiv 2501.13956).
// It evaluates multi-hop memory retrieval by measuring whether a system can
// answer questions that require chaining across 1–5 memory hops.
package dmr

import (
	"encoding/json"
	"fmt"
	"os"
)

// ConvTurn represents a single turn in a DMR conversation.
type ConvTurn struct {
	Speaker string
	Content string
}

// QAPair represents one DMR evaluation item.
type QAPair struct {
	ID           string
	Conversation []ConvTurn // turns to ingest before querying
	Question     string
	Answer       string
	Category     string // "1-hop" | "2-hop" | "3-hop" | "4-hop" | "5-hop"
}

// dmrTurnRaw is one turn from the DMR JSON format.
type dmrTurnRaw struct {
	Speaker string `json:"speaker"`
	Content string `json:"content"`
	// Alternative field names used by some DMR dataset variants.
	Role string `json:"role"`
	Text string `json:"text"`
}

// dmrRecordRaw is one record from the DMR JSON array.
type dmrRecordRaw struct {
	ID           string       `json:"id"`
	Conversation []dmrTurnRaw `json:"conversation"`
	Question     string       `json:"question"`
	Answer       string       `json:"answer"`
	HopCount     int          `json:"hop_count"`
	// Alternative field names.
	Turns   []dmrTurnRaw `json:"turns"`
	Query   string       `json:"query"`
	ExpectedAnswer string `json:"expected_answer"`
}

// hopCategory converts a numeric hop count to the canonical category string.
func hopCategory(hopCount int) string {
	switch hopCount {
	case 1:
		return "1-hop"
	case 2:
		return "2-hop"
	case 3:
		return "3-hop"
	case 4:
		return "4-hop"
	case 5:
		return "5-hop"
	default:
		if hopCount <= 0 {
			return "1-hop"
		}
		return fmt.Sprintf("%d-hop", hopCount)
	}
}

// LoadDataset loads a DMR JSON file and returns QAPairs.
//
// DMR file format:
//
//	[
//	  {
//	    "id": "...",
//	    "conversation": [
//	      {"speaker": "human", "content": "..."},
//	      {"speaker": "ai",    "content": "..."},
//	      ...
//	    ],
//	    "question":  "...",
//	    "answer":    "...",
//	    "hop_count": 3
//	  },
//	  ...
//	]
//
// hop_count is mapped to a category string: 1 → "1-hop", … 5 → "5-hop".
// No network calls are made. Returns an error if the file cannot be opened or
// if the JSON structure does not match the expected format.
func LoadDataset(path string) ([]QAPair, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is caller-supplied, not user-supplied via web.
	if err != nil {
		return nil, fmt.Errorf("dmr: open dataset %q: %w", path, err)
	}

	var records []dmrRecordRaw
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("dmr: parse dataset JSON: %w", err)
	}

	pairs := make([]QAPair, 0, len(records))
	for i := range records {
		rec := &records[i]

		id := rec.ID
		if id == "" {
			id = fmt.Sprintf("dmr-%d", i)
		}

		// Prefer rec.Question over rec.Query; prefer rec.Answer over rec.ExpectedAnswer.
		question := rec.Question
		if question == "" {
			question = rec.Query
		}
		answer := rec.Answer
		if answer == "" {
			answer = rec.ExpectedAnswer
		}

		// Prefer rec.Conversation over rec.Turns.
		rawTurns := rec.Conversation
		if len(rawTurns) == 0 {
			rawTurns = rec.Turns
		}

		turns := make([]ConvTurn, 0, len(rawTurns))
		for j := range rawTurns {
			t := &rawTurns[j]
			speaker := t.Speaker
			if speaker == "" {
				speaker = t.Role
			}
			content := t.Content
			if content == "" {
				content = t.Text
			}
			if speaker != "" || content != "" {
				turns = append(turns, ConvTurn{
					Speaker: speaker,
					Content: content,
				})
			}
		}

		pairs = append(pairs, QAPair{
			ID:           id,
			Conversation: turns,
			Question:     question,
			Answer:       answer,
			Category:     hopCategory(rec.HopCount),
		})
	}

	return pairs, nil
}
