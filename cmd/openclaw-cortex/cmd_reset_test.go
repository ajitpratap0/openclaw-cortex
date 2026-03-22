package main

import (
	"context"
	"io"
	"strings"
	"testing"
)

// TestResetRequiresYesFlagUnit verifies the --yes guard at the Cobra command
// level without requiring a pre-built binary. This test always runs (no
// testing.Short() skip) because it is safety-critical: without --yes the
// command must exit non-zero to prevent accidental data loss.
func TestResetRequiresYesFlagUnit(t *testing.T) {
	cmd := resetCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	err := cmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("reset without --yes should return an error, got nil")
	}
	if !strings.Contains(err.Error(), "--yes") {
		t.Errorf("error does not mention --yes: %v", err)
	}
}
