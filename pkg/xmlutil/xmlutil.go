// Package xmlutil provides XML escaping utilities for prompt injection prevention.
package xmlutil

import (
	"encoding/xml"
	"strings"
	"unicode/utf8"
)

// Escape replaces characters with special meaning in XML to prevent
// prompt injection when embedding user content in XML-delimited templates.
// Invalid UTF-8 sequences are replaced with the Unicode replacement character
// (U+FFFD) before escaping so that xml.EscapeText never fails.
func Escape(s string) string {
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "\uFFFD")
	}
	var buf strings.Builder
	_ = xml.EscapeText(&buf, []byte(s)) // cannot fail with valid UTF-8
	return buf.String()
}
