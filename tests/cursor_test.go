package tests

import (
	"testing"

	"github.com/ajitpratap0/openclaw-cortex/pkg/cursor"
)

var testSecret = []byte("test-secret-32-bytes-long-padding")

func TestCursor_RoundTrip(t *testing.T) {
	signed := cursor.Sign(42, testSecret)
	skip, err := cursor.Verify(signed, testSecret)
	if err != nil {
		t.Fatalf("verify error: %v", err)
	}
	if skip != 42 {
		t.Fatalf("expected 42, got %d", skip)
	}
}

func TestCursor_TamperedReturnsError(t *testing.T) {
	signed := cursor.Sign(10, testSecret)
	tampered := signed[:len(signed)-4] + "XXXX"
	_, err := cursor.Verify(tampered, testSecret)
	if err == nil {
		t.Fatal("expected error for tampered cursor, got nil")
	}
}

func TestCursor_WrongSecretReturnsError(t *testing.T) {
	signed := cursor.Sign(10, testSecret)
	_, err := cursor.Verify(signed, []byte("different-secret-here-padding-xx"))
	if err == nil {
		t.Fatal("expected error for wrong secret, got nil")
	}
}

func TestCursor_EmptyStringReturnsZero(t *testing.T) {
	skip, err := cursor.Verify("", testSecret)
	if err != nil {
		t.Fatalf("unexpected error for empty cursor: %v", err)
	}
	if skip != 0 {
		t.Fatalf("expected 0 for empty cursor, got %d", skip)
	}
}
