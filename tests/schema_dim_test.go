package tests

import (
	"strings"
	"testing"

	"github.com/ajitpratap0/openclaw-cortex/internal/memgraph"
)

// TestBuildVectorIndexDDL_UsesProvidedDimension verifies that the DDL generated
// by EnsureSchema injects the configured vector dimension, not a hardcoded 768.
func TestBuildVectorIndexDDL_UsesProvidedDimension(t *testing.T) {
	ddl := memgraph.BuildMemoryVectorIndexDDL(1024)
	if !strings.Contains(ddl, `"dimension": 1024`) {
		t.Errorf("expected dimension 1024 in DDL, got: %s", ddl)
	}
	if strings.Contains(ddl, `"dimension": 768`) {
		t.Error("DDL still contains hardcoded 768")
	}
}
