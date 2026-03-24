// Package health provides helpers shared between the health command and its tests.
package health

// LLMHealthOK reports whether the LLM health check should be considered OK.
//
// The LLM field in healthResult is a three-state *bool:
//   - nil  — check was skipped (--skip-llm-ping); counts as OK
//   - true — ping succeeded
//   - false — ping failed
func LLMHealthOK(v *bool) bool { return v == nil || *v }
