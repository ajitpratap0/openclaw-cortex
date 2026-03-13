package tests

import (
	"context"
	"fmt"
	"testing"

	"github.com/ajitpratap0/openclaw-cortex/internal/graph"
)

func TestRRFMerge(t *testing.T) {
	lists := [][]graph.FactResult{
		{{ID: "a", Score: 0.9}, {ID: "b", Score: 0.8}},
		{{ID: "b", Score: 0.95}, {ID: "c", Score: 0.7}},
		{{ID: "a", Score: 0.85}, {ID: "c", Score: 0.6}},
	}

	merged := graph.RRFMerge(lists, 10)

	if len(merged) != 3 {
		t.Fatalf("expected 3 merged results, got %d", len(merged))
	}

	// Verify deduplication
	seen := map[string]bool{}
	for i := range merged {
		if seen[merged[i].ID] {
			t.Errorf("duplicate ID in merged results: %s", merged[i].ID)
		}
		seen[merged[i].ID] = true
	}

	// Verify all IDs present
	if !seen["a"] || !seen["b"] || !seen["c"] {
		t.Errorf("expected all IDs present, got %v", seen)
	}

	// "a" and "b" appear in 2 lists each with rank 1; "c" appears in 2 lists with rank 2
	// "a": 1/(1+60) + 1/(1+60) = 2/61
	// "b": 1/(2+60) + 1/(1+60) = 1/62 + 1/61
	// "c": 1/(2+60) + 1/(2+60) = 2/62
	// So a > b > c
	if merged[0].ID != "a" {
		t.Errorf("expected 'a' first (highest RRF), got %s", merged[0].ID)
	}
	if merged[2].ID != "c" {
		t.Errorf("expected 'c' last (lowest RRF), got %s", merged[2].ID)
	}
}

func TestRRFMerge_EmptyLists(t *testing.T) {
	merged := graph.RRFMerge(nil, 10)
	if len(merged) != 0 {
		t.Errorf("expected empty, got %d", len(merged))
	}
}

func TestRRFMerge_SingleList(t *testing.T) {
	lists := [][]graph.FactResult{
		{{ID: "a", Score: 0.9}, {ID: "b", Score: 0.8}},
	}
	merged := graph.RRFMerge(lists, 1)
	if len(merged) != 1 {
		t.Fatalf("expected 1 with limit, got %d", len(merged))
	}
}

func TestRRFMerge_ScoresPositive(t *testing.T) {
	lists := [][]graph.FactResult{
		{{ID: "a"}, {ID: "b"}},
	}
	merged := graph.RRFMerge(lists, 10)
	for i := range merged {
		if merged[i].Score <= 0 {
			t.Errorf("expected positive score, got %f for %s", merged[i].Score, merged[i].ID)
		}
	}
}

func TestRRFMerge_LimitZero(t *testing.T) {
	lists := [][]graph.FactResult{
		{{ID: "a"}, {ID: "b"}},
	}
	merged := graph.RRFMerge(lists, 0)
	if len(merged) != 2 {
		t.Errorf("expected 2 results with limit=0 (no limit), got %d", len(merged))
	}
}

func TestHybridSearch_ParallelExecution(t *testing.T) {
	ctx := context.Background()
	fn1 := func(_ context.Context, _ int) ([]graph.FactResult, error) {
		return []graph.FactResult{{ID: "a", Fact: "fact-a"}}, nil
	}
	fn2 := func(_ context.Context, _ int) ([]graph.FactResult, error) {
		return []graph.FactResult{{ID: "b", Fact: "fact-b"}}, nil
	}

	results, err := graph.HybridSearch(ctx, []graph.SearchFunc{fn1, fn2}, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
}

func TestHybridSearch_GracefulDegradation(t *testing.T) {
	ctx := context.Background()
	fn1 := func(_ context.Context, _ int) ([]graph.FactResult, error) {
		return []graph.FactResult{{ID: "a", Fact: "fact-a"}}, nil
	}
	fn2 := func(_ context.Context, _ int) ([]graph.FactResult, error) {
		return nil, fmt.Errorf("search method failed")
	}

	results, err := graph.HybridSearch(ctx, []graph.SearchFunc{fn1, fn2}, 10)
	if err != nil {
		t.Fatalf("expected graceful degradation, got error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result from surviving method, got %d", len(results))
	}
	if results[0].ID != "a" {
		t.Errorf("expected result from fn1, got %s", results[0].ID)
	}
}

func TestHybridSearch_AllFail(t *testing.T) {
	ctx := context.Background()
	fn1 := func(_ context.Context, _ int) ([]graph.FactResult, error) {
		return nil, fmt.Errorf("fail 1")
	}
	fn2 := func(_ context.Context, _ int) ([]graph.FactResult, error) {
		return nil, fmt.Errorf("fail 2")
	}

	_, err := graph.HybridSearch(ctx, []graph.SearchFunc{fn1, fn2}, 10)
	if err == nil {
		t.Fatal("expected error when all methods fail")
	}
}

func TestHybridSearch_EmptyFuncs(t *testing.T) {
	ctx := context.Background()
	results, err := graph.HybridSearch(ctx, nil, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected empty results, got %d", len(results))
	}
}
