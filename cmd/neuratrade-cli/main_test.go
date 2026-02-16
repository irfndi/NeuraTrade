package main

import (
	"strings"
	"testing"
)

func TestFormatVersion(t *testing.T) {
	tests := []struct {
		name      string
		version   string
		gitCommit string
		expected  string
	}{
		{
			name:      "with git commit",
			version:   "1.0.0",
			gitCommit: "abc123",
			expected:  "1.0.0 (git: abc123)",
		},
		{
			name:      "without git commit",
			version:   "1.0.0",
			gitCommit: "",
			expected:  "1.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version = tt.version
			gitCommit = tt.gitCommit

			result := formatVersion()

			if result != tt.expected {
				t.Errorf("formatVersion() = %s, want %s", result, tt.expected)
			}
		})
	}
}

func TestPrintVersion(t *testing.T) {
	version = "test-version"
	gitCommit = "test-commit"

	defer func() {
		version = "dev"
		gitCommit = ""
	}()

	printVersion()
}

func TestPrintHelp(t *testing.T) {
	printHelp()
}

func TestGetProjectRoot(t *testing.T) {
	result := getProjectRoot()

	if result == "" {
		t.Error("getProjectRoot should not return empty string")
	}

	if !strings.Contains(result, "NeuraTrade") {
		t.Logf("Warning: Project root may not be correct: %s", result)
	}
}

func TestLogo(t *testing.T) {
	if logo != "🚀" {
		t.Errorf("Logo should be 🚀, got %s", logo)
	}
}
