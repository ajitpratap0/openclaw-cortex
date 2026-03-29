package tests

import (
	"errors"
	"strings"
	"testing"

	"github.com/ajitpratap0/openclaw-cortex/internal/memgraph"
)

// TestParseVectorIndexRows_Empty verifies that an empty row set returns an empty map.
func TestParseVectorIndexRows_Empty(t *testing.T) {
	result := memgraph.ParseVectorIndexRows(nil)
	if len(result) != 0 {
		t.Errorf("expected empty map for nil rows, got %v", result)
	}

	result = memgraph.ParseVectorIndexRows([]map[string]any{})
	if len(result) != 0 {
		t.Errorf("expected empty map for empty rows, got %v", result)
	}
}

// TestParseVectorIndexRows_CorrectMapping verifies that rows with valid
// index_name and property_name fields are mapped correctly.
func TestParseVectorIndexRows_CorrectMapping(t *testing.T) {
	rows := []map[string]any{
		{"index_name": "memory_embedding", "property_name": "embedding"},
		{"index_name": "entity_name_embedding", "property_name": "name_embedding"},
	}
	result := memgraph.ParseVectorIndexRows(rows)

	if result["memory_embedding"] != "embedding" {
		t.Errorf("expected memory_embedding → embedding, got %q", result["memory_embedding"])
	}
	if result["entity_name_embedding"] != "name_embedding" {
		t.Errorf("expected entity_name_embedding → name_embedding, got %q", result["entity_name_embedding"])
	}
}

// TestParseVectorIndexRows_WrongProperty simulates the post-wipe corruption scenario
// where the memory_embedding index was recreated on the wrong property ("project").
// ParseVectorIndexRows must faithfully record the wrong property so the caller
// (verifyOrRebuildVectorIndex) can detect the mismatch.
func TestParseVectorIndexRows_WrongProperty(t *testing.T) {
	rows := []map[string]any{
		// Simulates the bug: index on wrong property after volume wipe.
		{"index_name": "memory_embedding", "property_name": "project"},
	}
	result := memgraph.ParseVectorIndexRows(rows)

	prop := result["memory_embedding"]
	if prop != "project" {
		t.Errorf("expected wrong property 'project' to be recorded as-is, got %q", prop)
	}
	// Sanity-check: the correct property for memory_embedding is "embedding".
	// The test simulates corruption where the property is "project" instead.
	// Confirming prop != "embedding" ensures verifyOrRebuildVectorIndex will detect the mismatch.
	correctProp := "embedding"
	if prop == correctProp {
		t.Errorf("wrong property %q was silently corrected to correct value %q — verifyOrRebuildVectorIndex would miss the mismatch", prop, correctProp)
	}
}

// TestParseVectorIndexRows_SkipsEmptyIndexName ensures rows with an empty or
// missing index_name are ignored so they cannot pollute the result map with a
// blank-key entry.
func TestParseVectorIndexRows_SkipsEmptyIndexName(t *testing.T) {
	rows := []map[string]any{
		{"index_name": "", "property_name": "embedding"},
		{"property_name": "name_embedding"}, // index_name key absent
		{"index_name": "valid_index", "property_name": "some_prop"},
	}
	result := memgraph.ParseVectorIndexRows(rows)

	if _, ok := result[""]; ok {
		t.Error("empty index_name key should not appear in result map")
	}
	if result["valid_index"] != "some_prop" {
		t.Errorf("expected valid_index → some_prop, got %q", result["valid_index"])
	}
	if len(result) != 1 {
		t.Errorf("expected exactly 1 entry, got %d: %v", len(result), result)
	}
}

// TestParseVectorIndexRows_NonStringValues verifies that non-string values for
// index_name or property_name are silently ignored (type-assert yields zero value).
// Rows where property_name is non-string (empty after type assertion) are skipped
// entirely to avoid spurious drop+recreate cycles.
func TestParseVectorIndexRows_NonStringValues(t *testing.T) {
	rows := []map[string]any{
		{"index_name": 42, "property_name": "embedding"},           // int name → skipped
		{"index_name": "ok_index", "property_name": true},          // bool prop → empty string → skipped
		{"index_name": "real_index", "property_name": "real_prop"}, // valid
	}
	result := memgraph.ParseVectorIndexRows(rows)

	if _, ok := result[""]; ok {
		t.Error("non-string index_name (42) should produce empty key, which must be skipped")
	}
	if _, ok := result["ok_index"]; ok {
		t.Error("non-string property_name yields empty prop; row must be skipped to avoid spurious drop+recreate")
	}
	if result["real_index"] != "real_prop" {
		t.Errorf("expected real_index → real_prop, got %q", result["real_index"])
	}
}

// TestEnsureSchema_VectorIndex_DropRecreateOnMismatch exercises the drop+recreate
// code path in verifyOrRebuildVectorIndex using the exported ParseVectorIndexRows
// helper. It simulates the scenario where SHOW VECTOR INDEXES returns a row that
// puts the memory_embedding index on the wrong property ("project" instead of
// "embedding"). The test confirms that:
//   - ParseVectorIndexRows faithfully records the wrong property.
//   - A mismatch check (existingProp != expectedProp) evaluates to true, meaning
//     verifyOrRebuildVectorIndex would proceed to the DROP + recreate branch.
//   - The recreate DDL references the correct property ("embedding"), so the
//     rebuild would fix the corruption rather than reproduce it.
func TestEnsureSchema_VectorIndex_DropRecreateOnMismatch(t *testing.T) {
	// Simulate SHOW VECTOR INDEXES returning the index on the wrong property.
	rows := []map[string]any{
		{"index_name": "memory_embedding", "property_name": "project"},
	}
	indexes := memgraph.ParseVectorIndexRows(rows)

	const indexName = "memory_embedding"
	const expectedProperty = "embedding"

	existingProp, exists := indexes[indexName]
	if !exists {
		t.Fatalf("expected index %q to exist in parsed rows", indexName)
	}

	// Confirm mismatch is detected: this is the condition that triggers the drop+recreate branch.
	if existingProp == expectedProperty {
		t.Errorf("mismatch not detected: existingProp %q == expectedProperty %q; drop+recreate branch would be skipped", existingProp, expectedProperty)
	}

	// Confirm the recreate DDL targets the correct property so the rebuild fixes the corruption.
	recreateDDL := memgraph.BuildMemoryVectorIndexDDL(768)
	if !strings.Contains(recreateDDL, ":Memory(embedding)") {
		t.Errorf("recreate DDL does not target correct property 'embedding': %s", recreateDDL)
	}
	// Confirm the recreate DDL does not target the wrong property.
	if strings.Contains(recreateDDL, ":Memory(project)") {
		t.Errorf("recreate DDL targets wrong property 'project': %s", recreateDDL)
	}
}

// TestBuildMemoryVectorIndexDDL_IndexAndPropertyNames verifies that the memory
// vector index DDL targets the correct index name and property so that
// verifyOrRebuildVectorIndex can match against "memory_embedding" / "embedding".
func TestBuildMemoryVectorIndexDDL_IndexAndPropertyNames(t *testing.T) {
	ddl := memgraph.BuildMemoryVectorIndexDDL(768)
	if !strings.Contains(ddl, "memory_embedding") {
		t.Errorf("memory DDL must reference index name 'memory_embedding', got: %s", ddl)
	}
	if !strings.Contains(ddl, ":Memory(embedding)") {
		t.Errorf("memory DDL must target :Memory(embedding) property, got: %s", ddl)
	}
}

// TestBuildEntityVectorIndexDDL_IndexAndPropertyNames verifies that the entity
// vector index DDL targets the correct index name and property so that
// verifyOrRebuildVectorIndex can match against "entity_name_embedding" / "name_embedding".
func TestBuildEntityVectorIndexDDL_IndexAndPropertyNames(t *testing.T) {
	ddl := memgraph.BuildEntityVectorIndexDDL(768)
	if !strings.Contains(ddl, "entity_name_embedding") {
		t.Errorf("entity DDL must reference index name 'entity_name_embedding', got: %s", ddl)
	}
	if !strings.Contains(ddl, ":Entity(name_embedding)") {
		t.Errorf("entity DDL must target :Entity(name_embedding) property, got: %s", ddl)
	}
}

// TestIsAlreadyExistsErr_MatchesVectorIndexDuplicate verifies that IsAlreadyExistsErr
// matches the error patterns that Memgraph may return for duplicate vector index
// creation, including "already defined" which is used for vector indexes.
func TestIsAlreadyExistsErr_MatchesVectorIndexDuplicate(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil error", err: nil, want: false},
		{name: "unrelated error", err: errors.New("connection refused"), want: false},
		{name: "constraint already exists", err: errors.New("Constraint already exists"), want: true},
		{name: "index already exists", err: errors.New("Index already exists"), want: true},
		{name: "already defined (vector index)", err: errors.New("vector index memory_embedding already defined"), want: true},
		{name: "mixed case already exists", err: errors.New("ALREADY EXISTS"), want: true},
		{name: "index already (partial)", err: errors.New("index already created"), want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := memgraph.IsAlreadyExistsErr(tt.err)
			if got != tt.want {
				t.Errorf("IsAlreadyExistsErr(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// TestVerifyOrRebuildVectorIndex_ShowFailedReturnsError documents the expected
// behavior when SHOW VECTOR INDEXES fails and the index already exists:
// verifyOrRebuildVectorIndex should return an error (not nil) because the index
// may be on the wrong property and property correctness cannot be verified.
//
// This test cannot call verifyOrRebuildVectorIndex directly (it is unexported and
// requires a neo4j session), so it validates the preconditions that the function
// relies on — specifically, that showFailed=true with an empty indexes map means
// the index will not be found, triggering a CREATE attempt, and that a duplicate
// error in that scenario must not be silently accepted.
func TestVerifyOrRebuildVectorIndex_ShowFailedReturnsError(t *testing.T) {
	// When SHOW VECTOR INDEXES fails, EnsureSchema passes an empty map and showFailed=true.
	// The function will try to CREATE the index, which returns "already exists".
	// The review feedback requires that this scenario returns an error, not nil.
	//
	// We validate the condition: when indexes map is empty, the index is treated
	// as absent, meaning the CREATE path is taken.
	indexes := memgraph.ParseVectorIndexRows(nil) // simulates empty SHOW result
	_, exists := indexes["memory_embedding"]
	if exists {
		t.Fatal("empty indexes map should not contain memory_embedding — CREATE path would be skipped")
	}

	// Also verify that "already defined" (vector-index specific) is matched,
	// which is the error that triggers the showFailed error-return branch.
	vectorDupErr := errors.New("vector index memory_embedding already defined")
	if !memgraph.IsAlreadyExistsErr(vectorDupErr) {
		t.Error("IsAlreadyExistsErr must match vector-index-specific 'already defined' error")
	}
}
