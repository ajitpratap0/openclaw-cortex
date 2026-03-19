package tests

import (
	"strings"
	"testing"

	"github.com/ajitpratap0/openclaw-cortex/internal/memgraph"
)

// TestBuildSearchEntitiesCypher_UsesSearchAll verifies that the SearchEntities Cypher
// query uses text_search.search_all (not text_search.search) to avoid the "Unknown
// exception!" error thrown by current Memgraph versions.
func TestBuildSearchEntitiesCypher_UsesSearchAll(t *testing.T) {
	cypher := memgraph.BuildSearchEntitiesCypher()

	if !strings.Contains(cypher, "text_search.search_all") {
		t.Errorf("SearchEntities Cypher must use text_search.search_all, got:\n%s", cypher)
	}

	// Ensure the broken procedure name is not present.
	// We check for the bare ".search(" to avoid false-positives from ".search_all(".
	// Replace ".search_all" first, then check nothing remains.
	withoutSearchAll := strings.ReplaceAll(cypher, "text_search.search_all", "")
	if strings.Contains(withoutSearchAll, "text_search.search") {
		t.Errorf("SearchEntities Cypher must NOT use text_search.search (without _all), got:\n%s", cypher)
	}
}

// TestBuildSearchEntitiesCypher_WithBeforeWhere verifies that the SearchEntities
// Cypher query has an explicit WITH clause between YIELD and WHERE.
// Memgraph does not allow WHERE directly after YIELD; it requires WITH to bridge them.
func TestBuildSearchEntitiesCypher_WithBeforeWhere(t *testing.T) {
	cypher := memgraph.BuildSearchEntitiesCypher()

	yieldIdx := strings.Index(cypher, "YIELD")
	withIdx := strings.Index(cypher, "WITH node, score")
	whereIdx := strings.Index(cypher, "WHERE")

	if yieldIdx == -1 {
		t.Fatal("SearchEntities Cypher must contain YIELD")
	}
	if withIdx == -1 {
		t.Fatal("SearchEntities Cypher must contain 'WITH node, score' between YIELD and WHERE")
	}
	if whereIdx == -1 {
		t.Fatal("SearchEntities Cypher must contain WHERE clause")
	}

	// Order must be: YIELD ... WITH ... WHERE
	if !(yieldIdx < withIdx && withIdx < whereIdx) {
		t.Errorf(
			"clause order must be YIELD < WITH < WHERE; got positions YIELD=%d WITH=%d WHERE=%d in:\n%s",
			yieldIdx, withIdx, whereIdx, cypher,
		)
	}
}

// TestBuildSearchEntitiesCypher_ContainsEntityText verifies that the correct text index
// name ("entity_text") is referenced in the Cypher query.
func TestBuildSearchEntitiesCypher_ContainsEntityText(t *testing.T) {
	cypher := memgraph.BuildSearchEntitiesCypher()

	if !strings.Contains(cypher, `"entity_text"`) {
		t.Errorf("SearchEntities Cypher must reference the entity_text index, got:\n%s", cypher)
	}
}
