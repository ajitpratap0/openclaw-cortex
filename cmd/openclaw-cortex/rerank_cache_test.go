package main

import (
	"testing"

	"github.com/ajitpratap0/openclaw-cortex/internal/hooks"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
)

func TestRerankCache_WriteRead(t *testing.T) {
	dir := t.TempDir()
	results := []models.RecallResult{
		{Memory: models.Memory{ID: "a", Content: "hello"}, FinalScore: 0.9},
	}
	hooks.WriteRerankCache(dir, "sess-1", results)
	got := hooks.ReadRerankCache(dir, "sess-1")
	if len(got) != 1 || got[0].Memory.ID != "a" {
		t.Fatalf("expected cached result, got %v", got)
	}
	if hooks.ReadRerankCache(dir, "sess-unknown") != nil {
		t.Fatal("expected nil for unknown session")
	}
}
