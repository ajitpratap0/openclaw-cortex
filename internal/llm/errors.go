package llm

import "fmt"

// HTTPError is returned when an HTTP gateway responds with a non-2xx status.
type HTTPError struct {
	StatusCode int
	Body       string
}

// Error implements the error interface.
func (e *HTTPError) Error() string {
	return fmt.Sprintf("gateway complete: unexpected status %d: %s", e.StatusCode, e.Body)
}

// ErrCircuitOpen is returned by ResilientClient when the circuit breaker is open.
var ErrCircuitOpen = fmt.Errorf("llm: circuit breaker open — too many recent failures")
