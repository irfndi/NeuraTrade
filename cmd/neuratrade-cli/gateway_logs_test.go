package main

import (
	"testing"
)

func TestParseStatusArgs(t *testing.T) {
	result := parseStatusArgs([]string{})

	if result.ProjectRoot == "" {
		t.Error("ProjectRoot should not be empty")
	}
}

func TestParseLogsArgs(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected LogsOptions
	}{
		{
			name: "default options",
			args: []string{},
			expected: LogsOptions{
				Service: "",
				Follow:  false,
				Tail:    100,
			},
		},
		{
			name: "with service",
			args: []string{"--service", "backend"},
			expected: LogsOptions{
				Service: "backend",
				Follow:  false,
				Tail:    100,
			},
		},
		{
			name: "with follow flag",
			args: []string{"--follow"},
			expected: LogsOptions{
				Service: "",
				Follow:  true,
				Tail:    100,
			},
		},
		{
			name: "short follow flag",
			args: []string{"-f"},
			expected: LogsOptions{
				Service: "",
				Follow:  true,
				Tail:    100,
			},
		},
		{
			name: "with tail count",
			args: []string{"--tail", "50"},
			expected: LogsOptions{
				Service: "",
				Follow:  false,
				Tail:    50,
			},
		},
		{
			name: "short tail flag",
			args: []string{"-n", "200"},
			expected: LogsOptions{
				Service: "",
				Follow:  false,
				Tail:    200,
			},
		},
		{
			name: "combined options",
			args: []string{"--service", "ccxt", "--follow", "--tail", "25"},
			expected: LogsOptions{
				Service: "ccxt",
				Follow:  true,
				Tail:    25,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseLogsArgs(tt.args)

			if result.Service != tt.expected.Service {
				t.Errorf("Service: got %s, want %s", result.Service, tt.expected.Service)
			}
			if result.Follow != tt.expected.Follow {
				t.Errorf("Follow: got %v, want %v", result.Follow, tt.expected.Follow)
			}
			if result.Tail != tt.expected.Tail {
				t.Errorf("Tail: got %d, want %d", result.Tail, tt.expected.Tail)
			}
		})
	}
}
