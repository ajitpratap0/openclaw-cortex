// Package cursor provides HMAC-signed opaque pagination cursors.
// Cursors encode a SKIP offset and are tamper-evident via HMAC-SHA256.
package cursor

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
)

// Sign encodes skip as an 8-byte big-endian value, appends a 32-byte HMAC-SHA256
// over it, and returns the whole thing as a URL-safe base64 string.
func Sign(skip int64, secret []byte) string {
	// Allocate with capacity for both the 8-byte payload and 32-byte HMAC signature.
	buf := make([]byte, 8, 8+sha256.Size)
	binary.BigEndian.PutUint64(buf, uint64(skip))
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(buf)
	sig := mac.Sum(nil)
	return base64.RawURLEncoding.EncodeToString(append(buf, sig...))
}

// Verify decodes and validates a cursor produced by Sign.
// Returns (0, nil) for an empty cursor (first page).
// Returns an error if the cursor is malformed or the HMAC is invalid.
func Verify(encoded string, secret []byte) (int64, error) {
	if encoded == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return 0, fmt.Errorf("cursor: decode: %w", err)
	}
	const minLen = 8 + 32
	if len(raw) != minLen {
		return 0, fmt.Errorf("cursor: invalid length %d", len(raw))
	}
	payload := raw[:8]
	sig := raw[8:]

	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(payload)
	expected := mac.Sum(nil)
	if !hmac.Equal(sig, expected) {
		return 0, fmt.Errorf("cursor: invalid signature")
	}
	return int64(binary.BigEndian.Uint64(payload)), nil
}
