package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseAgentArgs(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected AgentOptions
	}{
		{
			name: "default options",
			args: []string{},
			expected: AgentOptions{
				SessionKey: "cli:agent",
				Debug:      false,
				ModelID:    "",
			},
		},
		{
			name: "debug flag",
			args: []string{"--debug"},
			expected: AgentOptions{
				SessionKey: "cli:agent",
				Debug:      true,
				ModelID:    "",
			},
		},
		{
			name: "short debug flag",
			args: []string{"-d"},
			expected: AgentOptions{
				SessionKey: "cli:agent",
				Debug:      true,
				ModelID:    "",
			},
		},
		{
			name: "model flag",
			args: []string{"--model", "gpt-4"},
			expected: AgentOptions{
				SessionKey: "cli:agent",
				Debug:      false,
				ModelID:    "gpt-4",
			},
		},
		{
			name: "session flag",
			args: []string{"--session", "test-session"},
			expected: AgentOptions{
				SessionKey: "test-session",
				Debug:      false,
				ModelID:    "",
			},
		},
		{
			name: "multiple flags",
			args: []string{"--debug", "--model", "gpt-4", "--session", "my-session"},
			expected: AgentOptions{
				SessionKey: "my-session",
				Debug:      true,
				ModelID:    "gpt-4",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseAgentArgs(tt.args)

			if result.Debug != tt.expected.Debug {
				t.Errorf("Debug: got %v, want %v", result.Debug, tt.expected.Debug)
			}
			if result.ModelID != tt.expected.ModelID {
				t.Errorf("ModelID: got %s, want %s", result.ModelID, tt.expected.ModelID)
			}
			if result.SessionKey != tt.expected.SessionKey {
				t.Errorf("SessionKey: got %s, want %s", result.SessionKey, tt.expected.SessionKey)
			}
		})
	}
}

func TestHandleAgentCommand(t *testing.T) {
	opts := AgentOptions{}

	tests := []struct {
		name    string
		input   string
		checkFn func(t *testing.T)
	}{
		{
			name:    "help command",
			input:   ":help",
			checkFn: func(t *testing.T) {},
		},
		{
			name:    "status command",
			input:   ":status",
			checkFn: func(t *testing.T) {},
		},
		{
			name:    "uppercase help",
			input:   ":HELP",
			checkFn: func(t *testing.T) {},
		},
		{
			name:    "clear command",
			input:   ":clear",
			checkFn: func(t *testing.T) {},
		},
		{
			name:    "unknown command",
			input:   ":unknown",
			checkFn: func(t *testing.T) {},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handleAgentCommand(tt.input, opts)
		})
	}
}

func TestBackendURLConstant(t *testing.T) {
	if BackendURL != "http://localhost:8080" {
		t.Errorf("BackendURL = %s, want http://localhost:8080", BackendURL)
	}
}

func TestAgentLogoConstant(t *testing.T) {
	if agentLogo != "🤖" {
		t.Errorf("agentLogo = %s, want 🤖", agentLogo)
	}
}

func TestSendMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ai/chat" {
			t.Errorf("Expected path /api/ai/chat, got %s", r.URL.Path)
		}

		contentType := r.Header.Get("Content-Type")
		if contentType != "application/json" {
			t.Errorf("Expected Content-Type application/json, got %s", contentType)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"response": "Test response"}`))
	}))
	defer server.Close()

	ctx := context.Background()
	_ = ctx
}
