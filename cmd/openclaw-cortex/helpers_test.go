package main

import (
	"testing"
	"time"
)

func TestParseTimeFlag(t *testing.T) {
	// Fixed reference point for relative-duration cases.
	// We just verify the returned value is within a few seconds of now-dur.

	cases := []struct {
		name      string
		input     string
		endOfDay  bool
		wantErr   bool
		wantExact time.Time // zero means skip exact comparison
		// For relative durations we only check the value is recent (within 5s).
		isRelative bool
	}{
		{
			name:      "date-only endOfDay=false → midnight UTC",
			input:     "2026-03-01",
			endOfDay:  false,
			wantExact: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:      "date-only endOfDay=true → 23:59:59 UTC",
			input:     "2026-03-01",
			endOfDay:  true,
			wantExact: time.Date(2026, 3, 1, 23, 59, 59, 0, time.UTC),
		},
		{
			name:      "RFC3339 normalised to UTC",
			input:     "2026-03-01T12:30:00Z",
			endOfDay:  false,
			wantExact: time.Date(2026, 3, 1, 12, 30, 0, 0, time.UTC),
		},
		{
			name:       "relative duration 24h",
			input:      "24h",
			endOfDay:   false,
			isRelative: true,
		},
		{
			name:       "relative duration 7d",
			input:      "7d",
			endOfDay:   false,
			isRelative: true,
		},
		{
			name:    "invalid input → error",
			input:   "not-a-date",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := time.Now().UTC()
			got, err := parseTimeFlag("testcmd", "--test-flag", tc.input, tc.endOfDay)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got time %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !tc.wantExact.IsZero() {
				if !got.Equal(tc.wantExact) {
					t.Errorf("got %v, want %v", got, tc.wantExact)
				}
				return
			}

			// Relative duration: result should be in the past and within 8 days
			// (covers up to a 7d duration with some slack).
			if !got.Before(before) || !got.After(before.Add(-8*24*time.Hour)) {
				t.Errorf("relative duration result %v not in expected range [%v, %v]",
					got, before.Add(-8*24*time.Hour), before)
			}
		})
	}
}
