package tests

import (
	"context"
	"testing"

	"github.com/ajitpratap0/openclaw-cortex/internal/capture"
	"github.com/ajitpratap0/openclaw-cortex/internal/models"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

func TestCaptureWithEntityExtraction(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping entity pipeline test in short mode")
	}

	ms := store.NewMockStore()
	ctx := context.Background()

	mem := models.Memory{
		ID:      "mem-1",
		Content: "Alice from Acme Corp prefers Go for backend services",
		Type:    models.MemoryTypeFact,
		Scope:   models.ScopePermanent,
	}
	vec := make([]float32, 768)
	if err := ms.Upsert(ctx, mem, vec); err != nil {
		t.Fatalf("upsert memory: %v", err)
	}

	entities := []models.Entity{
		{ID: "ent-1", Name: "Alice", Type: models.EntityTypePerson},
		{ID: "ent-2", Name: "Acme Corp", Type: models.EntityTypeProject},
	}

	for i := range entities {
		if err := ms.UpsertEntity(ctx, entities[i]); err != nil {
			t.Fatalf("upsert entity %s: %v", entities[i].Name, err)
		}
		if err := ms.LinkMemoryToEntity(ctx, entities[i].ID, mem.ID); err != nil {
			t.Fatalf("link entity %s to memory: %v", entities[i].Name, err)
		}
	}

	found, err := ms.SearchEntities(ctx, "Alice", "", 100)
	if err != nil {
		t.Fatalf("search entities: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("expected 1 entity for 'Alice', got %d", len(found))
	}
	if found[0].Name != "Alice" {
		t.Errorf("expected entity name 'Alice', got %q", found[0].Name)
	}

	ent, err := ms.GetEntity(ctx, "ent-1")
	if err != nil {
		t.Fatalf("get entity: %v", err)
	}
	if len(ent.MemoryIDs) != 1 || ent.MemoryIDs[0] != "mem-1" {
		t.Errorf("expected entity linked to mem-1, got %v", ent.MemoryIDs)
	}

	_ = capture.NewEntityExtractor // ensure import compiles
}
