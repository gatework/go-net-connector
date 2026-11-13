package nilcheck

import (
	"bytes"
	"io"
	"testing"
)

func TestIsNil(t *testing.T) {
	var typedNil *bytes.Buffer
	var nilMap map[string]int
	tests := []struct {
		name     string
		input    any
		expected bool
	}{
		{"untyped nil", nil, true},
		{"typed nil pointer", io.Reader(typedNil), true},
		{"nil map", nilMap, true},
		{"nil func", (func())(nil), true},
		{"pointer", &bytes.Buffer{}, false},
		{"struct", bytes.Buffer{}, false},
		{"empty string", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsNil(tt.input); got != tt.expected {
				t.Fatalf("IsNil(%T) = %t, want %t", tt.input, got, tt.expected)
			}
		})
	}
}
