package tests

import (
	"context"
	"errors"

	"github.com/ajitpratap0/openclaw-cortex/eval/runner"
)

// stubHarnessClient is a runner.Client implementation for testing harness
// control-flow without a live openclaw-cortex binary or Memgraph instance.
//
// recallErrs controls per-call Recall behavior: index i maps to call i.
// A nil entry means success (recallResp is returned); a non-nil entry means
// that call returns the error. Calls beyond len(recallErrs) succeed.
type stubHarnessClient struct {
	resetErr   error
	storeErr   error
	recallErrs []error // indexed by call order; nil entry = success
	recallResp []string
	callIdx    int
}

var _ runner.Client = (*stubHarnessClient)(nil)

func (s *stubHarnessClient) Reset(_ context.Context) error { return s.resetErr }
func (s *stubHarnessClient) Store(_ context.Context, _ string) error { return s.storeErr }
func (s *stubHarnessClient) Recall(_ context.Context, _ string, _ int) ([]string, error) {
	idx := s.callIdx
	s.callIdx++
	if idx < len(s.recallErrs) && s.recallErrs[idx] != nil {
		return nil, s.recallErrs[idx]
	}
	return s.recallResp, nil
}

// recallErrors returns a slice of n non-nil errors for use in stubHarnessClient.recallErrs.
func recallErrors(n int) []error {
	errs := make([]error, n)
	for i := range errs {
		errs[i] = errors.New("stub: recall failed")
	}
	return errs
}
