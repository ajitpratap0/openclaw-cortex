package tests

import (
	"testing"

	"github.com/ajitpratap0/openclaw-cortex/internal/models"
)

func TestConflictStatusIsValid(t *testing.T) {
	cases := []struct {
		status models.ConflictStatus
		want   bool
	}{
		{models.ConflictStatusNone, true},
		{models.ConflictStatusActive, true},
		{models.ConflictStatusResolved, true},
		{"bogus", false},
		{"", true}, // same as ConflictStatusNone
	}
	for _, tc := range cases {
		got := tc.status.IsValid()
		if got != tc.want {
			t.Errorf("ConflictStatus(%q).IsValid() = %v, want %v", tc.status, got, tc.want)
		}
	}
}

func TestMemoryScope_IsValid_EdgeCases(t *testing.T) {
	valid := []models.MemoryScope{
		models.ScopePermanent,
		models.ScopeProject,
		models.ScopeSession,
		models.ScopeTTL,
	}
	for _, s := range valid {
		if !s.IsValid() {
			t.Errorf("MemoryScope(%q).IsValid() = false, want true", s)
		}
	}
	if models.MemoryScope("unknown").IsValid() {
		t.Error("MemoryScope(\"unknown\").IsValid() = true, want false")
	}
}

func TestMemoryType_IsValid_EdgeCases(t *testing.T) {
	for _, mt := range models.ValidMemoryTypes {
		if !mt.IsValid() {
			t.Errorf("MemoryType(%q).IsValid() = false, want true", mt)
		}
	}
	if models.MemoryType("unknown").IsValid() {
		t.Error("MemoryType(\"unknown\").IsValid() = true, want false")
	}
}
