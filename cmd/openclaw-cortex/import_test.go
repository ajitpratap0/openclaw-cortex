package main

import (
	"strings"
	"testing"
)

func TestNewJSONLScanner_HandlesLargeLine(t *testing.T) {
	// Verify the scanner can handle lines > 64KB (default limit).
	largeContent := strings.Repeat("a", 100*1024) // 100KB
	line := `{"id":"x","content":"` + largeContent + `","type":"fact","scope":"permanent","confidence":0.9}`
	scanner := newJSONLScanner(strings.NewReader(line + "\n"))
	if !scanner.Scan() {
		t.Fatalf("scanner failed on 100KB line: %v", scanner.Err())
	}
	if scanner.Err() != nil {
		t.Fatalf("unexpected error: %v", scanner.Err())
	}
}
