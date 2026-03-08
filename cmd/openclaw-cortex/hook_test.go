package main

import (
	"os"
	"path/filepath"
	"testing"
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
