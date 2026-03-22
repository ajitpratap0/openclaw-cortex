package tests

import (
	"strings"
	"testing"
)

// TestResetBinaryRequiresYesFlag verifies that `reset` without --yes exits non-zero
// and prints a message that explains how to confirm. This is the safety-critical
// guard that prevents accidental data loss.
func TestResetBinaryRequiresYesFlag(t *testing.T) {
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
