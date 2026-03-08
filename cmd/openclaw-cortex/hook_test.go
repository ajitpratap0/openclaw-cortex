package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLastHumanMessageFromTranscript(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.jsonl")
	_ = os.WriteFile(path, []byte(
		`{"role":"human","message":{"content":"q1"}}`+"\n"+
			`{"role":"assistant","message":{"content":"a1"}}`+"\n"+
			`{"role":"human","message":{"content":"q2"}}`+"\n",
	), 0o600)
	got := lastHumanMessageFromTranscript(path)
	if got != "q2" {
		t.Fatalf("want q2, got %q", got)
	}
	if lastHumanMessageFromTranscript("") != "" {
		t.Fatal("empty path should return empty")
	}
	if lastHumanMessageFromTranscript("/nonexistent/file") != "" {
		t.Fatal("missing file should return empty")
	}
}

func TestLastNTurnsFromTranscript(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	lines := `{"role":"human","message":{"content":"q1"}}` + "\n" +
		`{"role":"assistant","message":{"content":"a1"}}` + "\n" +
		`{"role":"human","message":{"content":"q2"}}` + "\n" +
		`{"role":"assistant","message":{"content":"a2"}}` + "\n" +
		`{"role":"human","message":{"content":"q3"}}` + "\n"
	require.NoError(t, os.WriteFile(path, []byte(lines), 0o600))

	turns := lastNTurnsFromTranscript(path, 2)
	require.Len(t, turns, 2)
	assert.Equal(t, "assistant", turns[0].Role)
	assert.Equal(t, "a2", turns[0].Content)
	assert.Equal(t, "user", turns[1].Role)
	assert.Equal(t, "q3", turns[1].Content)

	assert.Nil(t, lastNTurnsFromTranscript("", 3))
	assert.Nil(t, lastNTurnsFromTranscript(path, 0))
}
