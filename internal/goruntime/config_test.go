package goruntime

import (
	"testing"
)

func TestParseMemoryLimit(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
		hasError bool
	}{
		{"128Mi", 128 * 1024 * 1024, false},
		{"1Gi", 1024 * 1024 * 1024, false},
		{"512Ki", 512 * 1024, false},
		{"100", 100, false},
		{"", 0, true},
		{"invalid", 0, true},
		{"128Zi", 0, true}, // Invalid suffix
	}

	for _, test := range tests {
		result, err := ParseMemoryLimit(test.input)

		if test.hasError {
			if err == nil {
				t.Errorf("Expected error for input %s, but got none", test.input)
			}
		} else {
			if err != nil {
				t.Errorf("Unexpected error for input %s: %v", test.input, err)
			}
			if result != test.expected {
				t.Errorf("For input %s, expected %d but got %d", test.input, test.expected, result)
			}
		}
	}
}

func TestGetGOMEMLIMIT(t *testing.T) {
	// Test default case (should return "unset" when env var is not set)
	result := getGOMEMLIMIT()
	if result != "unset" {
		t.Errorf("Expected 'unset' when GOMEMLIMIT is not set, got %s", result)
	}
}
