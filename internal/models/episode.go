package models

import "time"

// Episode captures the provenance of a conversation turn — which memories and
// facts were extracted from it.  Inspired by Graphiti's episodic layer.
type Episode struct {
	UUID         string    `json:"uuid"`
	SessionID    string    `json:"session_id"`
	UserMsg      string    `json:"user_msg"`
	AssistantMsg string    `json:"assistant_msg"`
	CapturedAt   time.Time `json:"captured_at"`
	// MemoryIDs are the Memory UUIDs derived from this episode via blob extraction.
	MemoryIDs []string `json:"memory_ids,omitempty"`
	// FactIDs are the Fact IDs (RELATES_TO edge IDs) extracted from this episode.
	FactIDs []string `json:"fact_ids,omitempty"`
}
