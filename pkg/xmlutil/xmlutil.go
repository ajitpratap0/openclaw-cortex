// Package xmlutil provides XML escaping utilities for prompt injection prevention.
package xmlutil

import (
	"encoding/xml"
	"strings"
)

// Escape replaces characters with special meaning in XML to prevent
// prompt injection when embedding user content in XML-delimited templates.
func Escape(s string) string {
	var buf strings.Builder
	if err := xml.EscapeText(&buf, []byte(s)); err != nil {
		// EscapeText only fails on invalid UTF-8; return original on error.
		return s
	}
	return buf.String()
}
