package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/eval/locomo"
)

// TestLoadDataset_LocOMO_FileNotFound verifies that LoadDataset returns an
// error when the specified file does not exist.
func TestLoadDataset_LocOMO_FileNotFound(t *testing.T) {
	_, err := locomo.LoadDataset("/nonexistent/path/locomo_dataset.json")
	require.Error(t, err, "expected error for missing file")
}

// TestLoadDataset_LoCoMO_MinimalValid writes a tiny valid LoCoMo JSON fixture
// to a temp directory and verifies that LoadDataset parses it correctly.
func TestLoadDataset_LoCoMO_MinimalValid(t *testing.T) {
	// Construct a minimal LoCoMo-format JSON with one conversation and two QA pairs.
	fixture := map[string]interface{}{
		"conv_0": map[string]interface{}{
			"session_1": []map[string]interface{}{
				{"human_turn": "I use Go for backend services.", "blenderbot_message": "Go is great for concurrency."},
				{"human_turn": "I started with Python though.", "blenderbot_message": "Python is great for scripting."},
			},
			"question_answer_pairs": []map[string]interface{}{
				{"question": "What language does the user use?", "answer": "Go", "category": "Single-hop"},
				{"question": "What did the user start with?", "answer": "Python", "category": "Temporal reasoning"},
			},
		},
	}

	data, err := json.Marshal(fixture)
	require.NoError(t, err)

	dir := t.TempDir()
	path := filepath.Join(dir, "locomo_test.json")
	require.NoError(t, os.WriteFile(path, data, 0o600))

	pairs, err := locomo.LoadDataset(path)
	require.NoError(t, err)
	require.Len(t, pairs, 2, "expected 2 QA pairs from the fixture")

	// First pair
	p0 := pairs[0]
	require.Equal(t, "What language does the user use?", p0.Question)
	require.Equal(t, "Go", p0.GroundTruth)
	require.Equal(t, "single-hop", p0.Category, "Single-hop should normalize to single-hop")
	require.Len(t, p0.Conversation, 2, "should have 2 turns from session_1")

	// Second pair
	p1 := pairs[1]
	require.Equal(t, "What did the user start with?", p1.Question)
	require.Equal(t, "Python", p1.GroundTruth)
	require.Equal(t, "temporal", p1.Category, "Temporal reasoning should normalize to temporal")

	// Both pairs share the same conversation turns (from the same conversation).
	require.Equal(t, p0.Conversation, p1.Conversation)
}

// TestLoadDataset_LoCoMO_CategoryNormalization verifies that all known LoCoMo
// category strings are normalized to their canonical lowercase forms.
func TestLoadDataset_LoCoMO_CategoryNormalization(t *testing.T) {
	cases := []struct {
		rawCategory  string
		wantCategory string
	}{
		{"Single-hop", "single-hop"},
		{"single-hop", "single-hop"},
		{"Multi-hop", "multi-hop"},
		{"multi-hop", "multi-hop"},
		{"Temporal reasoning", "temporal"},
		{"Temporal", "temporal"},
		{"temporal", "temporal"},
		{"Adversarial", "adversarial"},
		{"adversarial", "adversarial"},
	}

	dir := t.TempDir()

	for _, tc := range cases {
		tc := tc
		t.Run(tc.rawCategory, func(t *testing.T) {
			fixture := map[string]interface{}{
				"conv_0": map[string]interface{}{
					"session_1": []map[string]interface{}{
						{"human_turn": "fact", "blenderbot_message": "ok"},
					},
					"question_answer_pairs": []map[string]interface{}{
						{"question": "q?", "answer": "a", "category": tc.rawCategory},
					},
				},
			}
			data, err := json.Marshal(fixture)
			require.NoError(t, err)

			path := filepath.Join(dir, tc.rawCategory+".json")
			require.NoError(t, os.WriteFile(path, data, 0o600))

			pairs, err := locomo.LoadDataset(path)
			require.NoError(t, err)
			require.Len(t, pairs, 1)
			require.Equal(t, tc.wantCategory, pairs[0].Category)
		})
	}
}

// TestLoadDataset_LoCoMO_MultipleConversations verifies that QA pairs from
// multiple conversations are all returned and that each pair's ConvTurns
// contains only the turns from its own conversation.
func TestLoadDataset_LoCoMO_MultipleConversations(t *testing.T) {
	fixture := map[string]interface{}{
		"conv_0": map[string]interface{}{
			"session_1": []map[string]interface{}{
				{"human_turn": "Conv0 turn1", "blenderbot_message": "Conv0 resp1"},
			},
			"question_answer_pairs": []map[string]interface{}{
				{"question": "Conv0 Q1", "answer": "A0", "category": "single-hop"},
			},
		},
		"conv_1": map[string]interface{}{
			"session_1": []map[string]interface{}{
				{"human_turn": "Conv1 turn1", "blenderbot_message": "Conv1 resp1"},
				{"human_turn": "Conv1 turn2", "blenderbot_message": "Conv1 resp2"},
			},
			"question_answer_pairs": []map[string]interface{}{
				{"question": "Conv1 Q1", "answer": "A1", "category": "multi-hop"},
				{"question": "Conv1 Q2", "answer": "A2", "category": "temporal"},
			},
		},
	}

	data, err := json.Marshal(fixture)
	require.NoError(t, err)

	dir := t.TempDir()
	path := filepath.Join(dir, "multi_conv.json")
	require.NoError(t, os.WriteFile(path, data, 0o600))

	pairs, err := locomo.LoadDataset(path)
	require.NoError(t, err)
	// 1 pair from conv_0 + 2 from conv_1 = 3 total
	require.Len(t, pairs, 3)
}

// TestLoadDataset_LoCoMO_InvalidJSON verifies that LoadDataset returns an error
// when the file contains invalid JSON.
func TestLoadDataset_LoCoMO_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	require.NoError(t, os.WriteFile(path, []byte("{not valid json"), 0o600))

	_, err := locomo.LoadDataset(path)
	require.Error(t, err)
}

// TestLoadDataset_LoCoMO_NoQAPairs verifies that a conversation without a
// question_answer_pairs key is silently skipped (returns zero pairs).
func TestLoadDataset_LoCoMO_NoQAPairs(t *testing.T) {
	fixture := map[string]interface{}{
		"conv_0": map[string]interface{}{
			"session_1": []map[string]interface{}{
				{"human_turn": "hi", "blenderbot_message": "hello"},
			},
			// no question_answer_pairs key
		},
	}

	data, err := json.Marshal(fixture)
	require.NoError(t, err)

	dir := t.TempDir()
	path := filepath.Join(dir, "no_qa.json")
	require.NoError(t, os.WriteFile(path, data, 0o600))

	pairs, err := locomo.LoadDataset(path)
	require.NoError(t, err)
	require.Empty(t, pairs, "conversation without QA pairs should produce zero results")
}
