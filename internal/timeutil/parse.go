// Package timeutil provides time-parsing helpers shared by CLI commands.
package timeutil

import (
	"fmt"
	"strings"
	"time"
)

// ParseDuration extends time.ParseDuration to support a "d" suffix for days.
func ParseDuration(s string) (time.Duration, error) {
	if strings.HasSuffix(s, "d") {
		daysStr := strings.TrimSuffix(s, "d")
		if daysStr == "" {
			return 0, fmt.Errorf("invalid duration %q: missing numeric value before 'd'", s)
		}
		days, err := time.ParseDuration(daysStr + "h")
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q: %w", s, err)
		}
		return days * 24, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: %w", s, err)
	}
	return d, nil
}

// ParseTimeFlag parses a time flag value that can be an ISO 8601 datetime
// (e.g. "2026-03-01" or "2026-03-01T15:00:00Z") or a relative duration
// subtracted from now (e.g. "7d", "24h", "30m" — same syntax as --valid-until).
// When endOfDay is true and the input is a date-only string (YYYY-MM-DD), the
// returned time is 23:59:59 UTC of that day instead of midnight.
// Use endOfDay=true for upper-bound filters like --valid-before so that the
// entire specified day is included in the result set.
// Note: the endOfDay adjustment applies only to date-only inputs; RFC3339 and
// relative-duration inputs (e.g. 7d) are returned as-is regardless of endOfDay.
// Returns an error prefixed with cmdName and flagName for clear CLI error messages.
func ParseTimeFlag(cmdName, flagName, s string, endOfDay bool) (time.Time, error) {
	// Try ISO 8601 date-only first (YYYY-MM-DD).
	if t, err := time.Parse("2006-01-02", s); err == nil {
		if endOfDay {
			// Advance to the last second of the day so the entire day is included.
			// RFC3339 has second precision; finer granularity would be silently
			// truncated when the value is stored or compared as a string.
			return t.Add(24*time.Hour - time.Second).UTC(), nil
		}
		return t.UTC(), nil
	}
	// Try full RFC3339.
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	// Try relative duration (subtract from now).
	dur, err := ParseDuration(s)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s: invalid %s %q: must be ISO 8601 date (2006-01-02), RFC3339, or relative duration (7d, 24h, 30m)", cmdName, flagName, s)
	}
	return time.Now().UTC().Add(-dur), nil
}
