package utils

import (
	"strings"
	"testing"
)

func TestNewUUIDv7(t *testing.T) {
	a, err := NewUUIDv7()
	if err != nil {
		t.Fatalf("NewUUIDv7 error: %v", err)
	}
	b, err := NewUUIDv7()
	if err != nil {
		t.Fatalf("NewUUIDv7 error: %v", err)
	}
	if a == b {
		t.Errorf("two UUIDs are identical: %q", a)
	}
	// UUIDv7 dạng 8-4-4-4-12, version nibble = 7.
	parts := strings.Split(a, "-")
	if len(parts) != 5 || len(parts[2]) != 4 || parts[2][0] != '7' {
		t.Errorf("UUID %q is not v7 format", a)
	}
}
