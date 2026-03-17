package tests

import "context"

// mockLLMClient is an in-process llm.LLMClient for tests.
// Set Resp to control the return value; set Err to inject an error.
type mockLLMClient struct {
	Resp string
	Err  error
}

func (m *mockLLMClient) Complete(_ context.Context, _, _, _ string, _ int) (string, error) {
	return m.Resp, m.Err
}
