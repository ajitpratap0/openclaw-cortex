package llm

import "strings"

// StripCodeFences removes markdown code fences (```json ... ```) that some
// models wrap around JSON responses. Returns the original string if no fences found.
func StripCodeFences(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	// Remove opening fence (```json or ```)
	idx := strings.Index(s, "\n")
	if idx < 0 {
		return s
	}
	s = s[idx+1:]
	// Remove closing fence
	if i := strings.LastIndex(s, "```"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
