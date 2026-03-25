package tests

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/ajitpratap0/openclaw-cortex/internal/extract"
	"github.com/ajitpratap0/openclaw-cortex/internal/graph"
	"github.com/ajitpratap0/openclaw-cortex/internal/store"
)

// mockSeqLLM returns a preset sequence of responses, one per call.
// If all responses are consumed, subsequent calls return an empty string.
type mockSeqLLM struct {
	responses []string
	idx       atomic.Int64
}

func (m *mockSeqLLM) Complete(_ context.Context, _, _, _ string, _ int) (string, error) {
	i := int(m.idx.Add(1)) - 1
	if i >= len(m.responses) {
		return "", nil
	}
	return m.responses[i], nil
}

// TestPostStoreExtract_NilLLM verifies that a nil LLMClient produces a zero Result.
func TestPostStoreExtract_NilLLM(t *testing.T) {
	t.Parallel()
	ms := store.NewMockStore()
	gc := graph.NewMockGraphClient()
	res := extract.Run(context.Background(), extract.Deps{
		LLMClient:   nil,
		Store:       ms,
		GraphClient: gc,
	}, []extract.StoredMemory{{ID: "m1", Content: "Alice works at Acme Corp"}})

	if res.EntitiesExtracted != 0 {
		t.Errorf("expected 0 entities, got %d", res.EntitiesExtracted)
	}
	if res.FactsExtracted != 0 {
		t.Errorf("expected 0 facts, got %d", res.FactsExtracted)
	}
}

// TestPostStoreExtract_Entities verifies that a valid entity JSON response causes
// UpsertEntity and LinkMemoryToEntity to be called and the count returned.
func TestPostStoreExtract_Entities(t *testing.T) {
	t.Parallel()

	// The entity extractor calls LLM once per memory; fact extractor needs entities
	// on first call — return empty fact list for the second call.
	entityJSON := `[{"name":"Alice","type":"person","aliases":[],"description":"A developer"}]`
	factJSON := `[]`

	llm := &mockSeqLLM{responses: []string{entityJSON, factJSON}}
	ms := store.NewMockStore()
	gc := graph.NewMockGraphClient()

	res := extract.Run(context.Background(), extract.Deps{
		LLMClient:   llm,
		Model:       "test-model",
		Store:       ms,
		GraphClient: gc,
	}, []extract.StoredMemory{{ID: "mem-1", Content: "Alice works at Acme Corp"}})

	if res.EntitiesExtracted != 1 {
		t.Errorf("expected 1 entity extracted, got %d", res.EntitiesExtracted)
	}

	// Verify the entity was actually stored and linked.
	ctx := context.Background()
	entities, err := ms.SearchEntities(ctx, "Alice", "", 10)
	if err != nil {
		t.Fatalf("search entities: %v", err)
	}
	if len(entities) != 1 {
		t.Fatalf("expected 1 entity in store, got %d", len(entities))
	}
	if entities[0].Name != "Alice" {
		t.Errorf("expected entity name 'Alice', got %q", entities[0].Name)
	}

	// Verify link was set.
	ent, err := ms.GetEntity(ctx, entities[0].ID)
	if err != nil {
		t.Fatalf("get entity: %v", err)
	}
	if len(ent.MemoryIDs) == 0 || ent.MemoryIDs[0] != "mem-1" {
		t.Errorf("expected entity linked to mem-1, got %v", ent.MemoryIDs)
	}
}

// TestPostStoreExtract_Facts verifies that when the LLM returns entities on the
// first call and facts on the second, facts end up in the graph client.
func TestPostStoreExtract_Facts(t *testing.T) {
	t.Parallel()

	entityJSON := `[
		{"name":"Alice","type":"person","aliases":[],"description":"A developer"},
		{"name":"Acme Corp","type":"project","aliases":[],"description":"A company"}
	]`
	// SourceEntityID / TargetEntityID must match names exactly.
	factJSON := `[{
		"source_entity_name": "Alice",
		"target_entity_name": "Acme Corp",
		"relation_type": "WORKS_AT",
		"fact": "Alice works at Acme Corp",
		"valid_at": null,
		"invalid_at": null
	}]`

	llm := &mockSeqLLM{responses: []string{entityJSON, factJSON}}
	ms := store.NewMockStore()
	gc := graph.NewMockGraphClient()

	res := extract.Run(context.Background(), extract.Deps{
		LLMClient:   llm,
		Model:       "test-model",
		Store:       ms,
		GraphClient: gc,
	}, []extract.StoredMemory{{ID: "mem-2", Content: "Alice works at Acme Corp"}})

	if res.EntitiesExtracted != 2 {
		t.Errorf("expected 2 entities extracted, got %d", res.EntitiesExtracted)
	}
	if res.FactsExtracted != 1 {
		t.Errorf("expected 1 fact extracted, got %d", res.FactsExtracted)
	}

	// Verify fact is in the graph client.
	facts, err := gc.SearchFacts(context.Background(), "", nil, 10)
	if err != nil {
		t.Fatalf("search facts: %v", err)
	}
	if len(facts) != 1 {
		t.Fatalf("expected 1 fact in graph, got %d", len(facts))
	}
	if facts[0].Fact != "Alice works at Acme Corp" {
		t.Errorf("unexpected fact text: %q", facts[0].Fact)
	}
	// Verify memory link on fact.
	if len(facts[0].SourceMemoryIDs) == 0 || facts[0].SourceMemoryIDs[0] != "mem-2" {
		t.Errorf("expected fact linked to mem-2, got %v", facts[0].SourceMemoryIDs)
	}
}

// TestPostStoreExtract_LLMError verifies that an LLM error produces a zero
// Result (graceful degradation) without panicking.
func TestPostStoreExtract_LLMError(t *testing.T) {
	t.Parallel()

	errLLM := &mockLLMClient{Err: errors.New("llm unavailable")}
	ms := store.NewMockStore()
	gc := graph.NewMockGraphClient()

	res := extract.Run(context.Background(), extract.Deps{
		LLMClient:   errLLM,
		Model:       "test-model",
		Store:       ms,
		GraphClient: gc,
	}, []extract.StoredMemory{{ID: "mem-3", Content: "Alice works at Acme Corp"}})

	if res.EntitiesExtracted != 0 {
		t.Errorf("expected 0 entities on LLM error, got %d", res.EntitiesExtracted)
	}
	if res.FactsExtracted != 0 {
		t.Errorf("expected 0 facts on LLM error, got %d", res.FactsExtracted)
	}
}
