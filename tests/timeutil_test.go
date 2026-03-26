package tests

import (
	"testing"
	"time"

	"github.com/ajitpratap0/openclaw-cortex/internal/timeutil"
)

func TestParseDuration(t *testing.T) {
	cases := []struct {
		input   string
		wantErr bool
		wantDur time.Duration
	}{
		{"24h", false, 24 * time.Hour},
		{"30m", false, 30 * time.Minute},
		{"1d", false, 24 * time.Hour},
		{"7d", false, 7 * 24 * time.Hour},
		{"0d", false, 0},
		{"d", true, 0},
		{"invalid", true, 0},
		{"", true, 0},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, err := timeutil.ParseDuration(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseDuration(%q) expected error, got nil", tc.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseDuration(%q) unexpected error: %v", tc.input, err)
			}
			if got != tc.wantDur {
				t.Errorf("ParseDuration(%q) = %v; want %v", tc.input, got, tc.wantDur)
			}
		})
	}
}

func TestParseTimeFlag(t *testing.T) {
	// Anchor for relative-duration assertions.
	before := time.Now().UTC()

	cases := []struct {
		name       string
		input      string
		endOfDay   bool
		wantErr    bool
		exactTime  time.Time // non-zero → assert equality
		checkRange func(t time.Time) bool
	}{
		{
			name:      "date-only endOfDay=false gives midnight UTC",
			input:     "2026-03-01",
			endOfDay:  false,
			exactTime: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:      "date-only endOfDay=true gives 23:59:59 UTC",
			input:     "2026-03-01",
			endOfDay:  true,
			exactTime: time.Date(2026, 3, 1, 23, 59, 59, 0, time.UTC),
		},
		{
			name:      "RFC3339 UTC normalised",
			input:     "2026-03-01T15:00:00Z",
			endOfDay:  false,
			exactTime: time.Date(2026, 3, 1, 15, 0, 0, 0, time.UTC),
		},
		{
			name:      "RFC3339 non-UTC offset converted to UTC",
			input:     "2026-03-01T15:00:00+05:30",
			endOfDay:  false,
			exactTime: time.Date(2026, 3, 1, 9, 30, 0, 0, time.UTC),
		},
		{
			name:     "relative 24h",
			input:    "24h",
			endOfDay: false,
			checkRange: func(t time.Time) bool {
				lo := before.Add(-25 * time.Hour)
				hi := before.Add(-23 * time.Hour)
				return !t.Before(lo) && !t.After(hi)
			},
		},
		{
			name:     "relative 7d",
			input:    "7d",
			endOfDay: false,
			checkRange: func(t time.Time) bool {
				lo := before.Add(-8 * 24 * time.Hour)
				hi := before.Add(-6 * 24 * time.Hour)
				return !t.Before(lo) && !t.After(hi)
			},
		},
		{
			name:    "invalid input returns error",
			input:   "notadate",
			wantErr: true,
		},
		{
			name:    "negative relative duration returns error",
			input:   "-1h",
			wantErr: true,
		},
		{
			name:    "zero relative duration returns error",
			input:   "0h",
			wantErr: true,
		},
		{
			name:    "zero day relative duration returns error",
			input:   "0d",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := timeutil.ParseTimeFlag("test", "--flag", tc.input, tc.endOfDay)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseTimeFlag(%q, endOfDay=%v) expected error, got nil", tc.input, tc.endOfDay)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseTimeFlag(%q, endOfDay=%v) unexpected error: %v", tc.input, tc.endOfDay, err)
			}
			if got.Location() != time.UTC {
				t.Errorf("ParseTimeFlag(%q) result not UTC: %v", tc.input, got.Location())
			}
			if !tc.exactTime.IsZero() && !got.Equal(tc.exactTime) {
				t.Errorf("ParseTimeFlag(%q, endOfDay=%v) = %v; want %v", tc.input, tc.endOfDay, got, tc.exactTime)
			}
			if tc.checkRange != nil && !tc.checkRange(got) {
				t.Errorf("ParseTimeFlag(%q) = %v; outside expected range relative to %v", tc.input, got, before)
			}
		})
	}
}
