package tests

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

// TestResetRequiresYesFlag verifies that `reset` without --yes exits non-zero
// and prints a message that explains how to confirm. This is the safety-critical
// guard that prevents accidental data loss.
func TestResetRequiresYesFlag(t *testing.T) {
	if !binExists() {
		t.Skip("binary not built; run: go build -o bin/openclaw-cortex ./cmd/openclaw-cortex")
	}

	// runCLI uses CombinedOutput, so out contains both stdout and stderr.
	// Cobra writes RunE errors to stderr, which is captured here.
	out, err := runCLI("reset")
	if err == nil {
		t.Fatal("reset without --yes should exit non-zero, but got nil error")
	}
	// The error message should guide the user toward the --yes flag.
	if !strings.Contains(out, "--yes") {
		t.Errorf("output does not mention --yes:\n%s", out)
	}
}

// TestResetMockStoreDeleteAllMemories verifies the ResettableStore contract:
// DeleteAllMemories removes all previously upserted memories. This validates
// the store behavior that cmd_reset.go depends on without requiring a live
// Memgraph instance or a running binary.
func TestResetMockStoreDeleteAllMemories(t *testing.T) {
	ctx := context.Background()
	ms := store.NewMockStore()

	// Seed a few memories.
	for _, content := range []string{"fact A", "fact B", "fact C"} {
		m := models.Memory{
			ID:           content, // reuse content as ID for simplicity
			Type:         models.MemoryTypeFact,
			Scope:        models.ScopePermanent,
			Visibility:   models.VisibilityShared,
			Content:      content,
			Confidence:   0.9,
			Source:       "test",
			CreatedAt:    time.Now().UTC(),
			UpdatedAt:    time.Now().UTC(),
			LastAccessed: time.Now().UTC(),
		}
		vec := testVector(0.1)
		if err := ms.Upsert(ctx, m, vec); err != nil {
			t.Fatalf("seed Upsert(%q): %v", content, err)
		}
	}

	// Verify seeding worked.
	memories, _, err := ms.List(ctx, nil, 100, "")
	if err != nil {
		t.Fatalf("List before delete: %v", err)
	}
	if len(memories) != 3 {
		t.Fatalf("want 3 memories before delete, got %d", len(memories))
	}

	// Exercise DeleteAllMemories via the ResettableStore interface, exactly
	// as cmd_reset.go does it.
	var rs store.ResettableStore = ms
	deleteErr := rs.DeleteAllMemories(ctx)
	if deleteErr != nil {
		t.Fatalf("DeleteAllMemories: %v", deleteErr)
	}

	// Store must be empty.
	memories, _, err = ms.List(ctx, nil, 100, "")
	if err != nil {
		t.Fatalf("List after delete: %v", err)
	}
	if len(memories) != 0 {
		t.Errorf("want 0 memories after DeleteAllMemories, got %d", len(memories))
	}
}
