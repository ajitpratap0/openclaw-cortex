package store

import (
	"fmt"
	"strings"
)

// MinContentLen is the minimum number of non-whitespace characters required for
// a memory's content. Exported so that both the cmd layer and tests reference a
// single source of truth.
const MinContentLen = 10

// ErrContentTooShort is returned when a memory's trimmed content is shorter
// than MinContentLen.
type ErrContentTooShort struct {
	Actual  int
	Minimum int
}

func (e *ErrContentTooShort) Error() string {
	return fmt.Sprintf("content too short (%d chars, minimum %d); provide meaningful text",
		e.Actual, e.Minimum)
}

// ErrDedupThresholdRange is returned when a caller-supplied dedup threshold
// falls outside the valid half-open interval (0.0, 1.0].
type ErrDedupThresholdRange struct {
	Value float64
}

func (e *ErrDedupThresholdRange) Error() string {
	return fmt.Sprintf("dedup threshold %g out of range (0.0, 1.0]", e.Value)
}

// ValidateContentLength checks that content (after trimming whitespace) meets
// the MinContentLen requirement. Returns ErrContentTooShort when it does not.
func ValidateContentLength(content string) error {
	trimmed := strings.TrimSpace(content)
	if len(trimmed) < MinContentLen {
		return &ErrContentTooShort{Actual: len(trimmed), Minimum: MinContentLen}
	}
	return nil
}

// ValidateDedupThreshold checks that v is in the half-open interval (0.0, 1.0].
// Returns ErrDedupThresholdRange when the value is out of range.
func ValidateDedupThreshold(v float64) error {
	if v <= 0 || v > 1 {
		return &ErrDedupThresholdRange{Value: v}
	}
	return nil
}
