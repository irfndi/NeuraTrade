package main

import (
	"testing"
)

func TestTruncate(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{
			name:     "string shorter than max length",
			input:    "short",
			maxLen:   10,
			expected: "short",
		},
		{
			name:     "string equal to max length",
			input:    "exactly",
			maxLen:   7,
			expected: "exactly",
		},
		{
			name:     "string longer than max length",
			input:    "this is a very long string",
			maxLen:   10,
			expected: "this is...",
		},
		{
			name:     "empty string",
			input:    "",
			maxLen:   5,
			expected: "",
		},
		{
			name:     "max length of 3 returns full string with ellipsis",
			input:    "hello",
			maxLen:   3,
			expected: "...",
		},
		{
			name:     "max length of 4 truncates correctly",
			input:    "testing",
			maxLen:   4,
			expected: "t...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncate(tt.input, tt.maxLen)
			if result != tt.expected {
				t.Errorf("truncate(%q, %d) = %q; want %q", tt.input, tt.maxLen, result, tt.expected)
			}
		})
	}
}

func TestTruncateEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{
			name:     "unicode string truncates by bytes",
			input:    "こんにちは",
			maxLen:   5,
			expected: "\xe3\x81...",
		},
		{
			name:     "ascii string truncates correctly",
			input:    "Hello",
			maxLen:   6,
			expected: "Hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncate(tt.input, tt.maxLen)
			if tt.maxLen >= 3 && result != tt.expected {
				t.Errorf("truncate(%q, %d) = %q; want %q", tt.input, tt.maxLen, result, tt.expected)
			}
		})
	}
}
