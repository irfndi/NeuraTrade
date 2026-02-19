package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/urfave/cli/v2"
)

func TestMain(m *testing.M) {
	tmpHome, err := os.MkdirTemp("", "neuratrade-cli-test-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmpHome)

	os.Setenv("NEURATRADE_HOME", tmpHome)
	// Set environment variables for testing
	os.Setenv("NEURATRADE_API_BASE_URL", "http://localhost:8080")
	os.Unsetenv("NEURATRADE_API_KEY")
	exitCode := m.Run()
	os.Exit(exitCode)
}

func TestNewAPIClient(t *testing.T) {
	client := NewAPIClient("http://example.com", "test-key")

	assert.Equal(t, "http://example.com", client.BaseURL)
	assert.Equal(t, "test-key", client.APIKey)
	assert.NotNil(t, client.HTTPClient)
}

func TestGetBaseURL(t *testing.T) {
	// Test with environment variable set
	os.Setenv("NEURATRADE_API_BASE_URL", "http://test.com")
	url := getBaseURL()
	assert.Equal(t, "http://test.com", url)

	// Test with environment variable not set (should default)
	os.Unsetenv("NEURATRADE_API_BASE_URL")
	url = getBaseURL()
	assert.Equal(t, "http://localhost:8080", url)
}

func TestGetAPIKey(t *testing.T) {
	// Test with environment variable set
	os.Setenv("NEURATRADE_API_KEY", "test-api-key")
	key := getAPIKey()
	assert.Equal(t, "test-api-key", key)

	// Test with environment variable not set
	os.Unsetenv("NEURATRADE_API_KEY")
	key = getAPIKey()
	assert.Equal(t, "", key)
}

func TestGenerateRandomString(t *testing.T) {
	str := generateRandomString(8)
	assert.Len(t, str, 8)

	// Test with different length
	str = generateRandomString(12)
	assert.Len(t, str, 12)
}

func TestGenerateAuthCode(t *testing.T) {
	// Create a test server that simulates the API
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/telegram/generate-binding-code", r.URL.Path)
		assert.Equal(t, "POST", r.Method)

		var req GenerateAuthCodeRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		assert.NoError(t, err)
		// The actual implementation uses "demo-user-id" as placeholder
		assert.Equal(t, "demo-user-id", req.UserID)

		response := GenerateAuthCodeResponse{
			Success:   true,
			Message:   "Code generated successfully",
			UserID:    "demo-user-id",
			ExpiresAt: "2026-02-17T10:00:00Z",
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create a client pointing to our test server
	client := NewAPIClient(server.URL, "")

	// Test the client function directly
	response, err := client.GenerateAuthCode("demo-user-id")
	assert.NoError(t, err)
	assert.True(t, response.Success)
	assert.Equal(t, "demo-user-id", response.UserID)
	assert.Equal(t, "Code generated successfully", response.Message)
}

func TestGenerateAuthCodeFallback(t *testing.T) {
	// Create a test server that returns an error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "Internal Server Error")
	}))
	defer server.Close()

	// Temporarily set the base URL to our test server
	originalURL := os.Getenv("NEURATRADE_API_BASE_URL")
	os.Setenv("NEURATRADE_API_BASE_URL", server.URL)
	defer os.Setenv("NEURATRADE_API_BASE_URL", originalURL)

	// Create a context for the CLI command
	app := &cli.App{
		Name: "test",
		Commands: []*cli.Command{
			{
				Name:   "generate-auth-code",
				Action: generateAuthCode,
			},
		},
	}

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Run the command
	err := app.Run([]string{"test", "generate-auth-code"})
	assert.NoError(t, err)

	// Restore stdout
	w.Close()
	os.Stdout = oldStdout

	// Read the output
	var buf bytes.Buffer
	_, err = buf.ReadFrom(r)
	assert.NoError(t, err)
	output := buf.String()

	// Verify the output contains fallback message
	assert.Contains(t, output, "Warning: Could not reach API")
	assert.Contains(t, output, "Generating local auth code for demonstration purposes")
}

func TestBindOperator(t *testing.T) {
	// Create a test server that simulates the API
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/telegram/bind-operator", r.URL.Path)
		assert.Equal(t, "POST", r.Method)

		var req BindOperatorRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		assert.NoError(t, err)
		assert.Equal(t, "ABC123", req.AuthCode)
		assert.Equal(t, "demo-telegram-user-id", req.TelegramUserID)

		response := BindOperatorResponse{
			Success:      true,
			Message:      "Operator profile bound successfully",
			OperatorName: "Test Operator",
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create a client pointing to our test server
	client := NewAPIClient(server.URL, "")

	// Test the client function directly
	request := &BindOperatorRequest{
		ChatID:           "test-chat-id",
		TelegramUserID:   "demo-telegram-user-id",
		TelegramUsername: "demo_user",
		AuthCode:         "ABC123",
	}

	response, err := client.BindOperatorProfile(request)
	assert.NoError(t, err)
	assert.True(t, response.Success)
	assert.Equal(t, "Test Operator", response.OperatorName)
	assert.Equal(t, "Operator profile bound successfully", response.Message)
}

func TestListAIModels(t *testing.T) {
	// Create a test server that simulates the API
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/ai/models", r.URL.Path)
		assert.Equal(t, "GET", r.Method)

		response := AIModelsResponse{
			Models: []AIModel{
				{
					ID:             "gpt-4-turbo",
					DisplayName:    "GPT-4 Turbo",
					Provider:       "openai",
					Cost:           "0.01",
					SupportsTools:  true,
					SupportsVision: true,
				},
				{
					ID:             "claude-3-opus",
					DisplayName:    "Claude 3 Opus",
					Provider:       "anthropic",
					Cost:           "0.015",
					SupportsTools:  true,
					SupportsVision: false,
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Temporarily set the base URL to our test server
	originalURL := os.Getenv("NEURATRADE_API_BASE_URL")
	os.Setenv("NEURATRADE_API_BASE_URL", server.URL)
	defer os.Setenv("NEURATRADE_API_BASE_URL", originalURL)

	// Create a context for the CLI command
	app := &cli.App{
		Name: "test",
		Commands: []*cli.Command{
			{
				Name:   "models",
				Action: listAIModels,
			},
		},
	}

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Run the command
	err := app.Run([]string{"test", "models"})
	assert.NoError(t, err)

	// Restore stdout
	w.Close()
	os.Stdout = oldStdout

	// Read the output
	var buf bytes.Buffer
	_, err = buf.ReadFrom(r)
	assert.NoError(t, err)
	output := buf.String()

	// Verify the output contains expected content
	assert.Contains(t, output, "Available AI Models")
	assert.Contains(t, output, "gpt-4-turbo (openai): tools, vision")
	assert.Contains(t, output, "claude-3-opus (anthropic): tools")
}

func TestBuildPrompt(t *testing.T) {
	// Create a context for the CLI command
	app := &cli.App{
		Name: "test",
		Commands: []*cli.Command{
			{
				Name:   "build",
				Action: buildPrompt,
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "skill", Required: true},
					&cli.StringFlag{Name: "context"},
				},
			},
		},
	}

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Run the command
	err := app.Run([]string{"test", "build", "--skill", "trading-advice", "--context", "BTC is at $45000"})
	assert.NoError(t, err)

	// Restore stdout
	w.Close()
	os.Stdout = oldStdout

	// Read the output
	var buf bytes.Buffer
	_, err = buf.ReadFrom(r)
	assert.NoError(t, err)
	output := buf.String()

	// Verify the output contains expected content
	assert.Contains(t, output, "Building prompt for skill: trading-advice")
	assert.Contains(t, output, "With context: BTC is at $45000")
	assert.Contains(t, output, "You are an expert trading assistant. Skill: trading-advice. Context: BTC is at $45000")
}

func TestStatusCommand(t *testing.T) {
	// Create a context for the CLI command
	app := &cli.App{
		Name: "test",
		Commands: []*cli.Command{
			{
				Name:   "status",
				Action: status,
			},
		},
	}

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Run the command
	err := app.Run([]string{"test", "status"})
	assert.NoError(t, err)

	// Restore stdout
	w.Close()
	os.Stdout = oldStdout

	// Read the output
	var buf bytes.Buffer
	_, err = buf.ReadFrom(r)
	assert.NoError(t, err)
	output := buf.String()

	// Verify the output contains expected content
	assert.Contains(t, output, "NeuraTrade System Status")
	assert.Contains(t, output, "Version: dev")
	// Note: Status depends on backend availability, so we just verify the command runs
	assert.True(t, len(output) > 0, "Status command should produce output")
}

func TestHealthCommand(t *testing.T) {
	// Create a context for the CLI command
	app := &cli.App{
		Name: "test",
		Commands: []*cli.Command{
			{
				Name:   "health",
				Action: health,
			},
		},
	}

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Run the command
	err := app.Run([]string{"test", "health"})
	assert.NoError(t, err)

	// Restore stdout
	w.Close()
	os.Stdout = oldStdout

	// Read the output
	var buf bytes.Buffer
	_, err = buf.ReadFrom(r)
	assert.NoError(t, err)
	output := buf.String()

	// Verify the output contains expected content
	assert.Contains(t, output, "Health Check Results")
	// Note: Health status depends on backend availability, so we just verify the command runs
	assert.True(t, len(output) > 0, "Health command should produce output")
}

func TestMakeRequest(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/test-endpoint", r.URL.Path)
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "test-key", r.Header.Get("X-API-Key"))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"message": "success"}`)
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "test-key")

	// Override the HTTP client to use our test server
	client.HTTPClient = &http.Client{}

	// Test the makeRequest function
	resp, err := client.makeRequest("GET", "/test-endpoint", nil)
	assert.NoError(t, err)
	assert.True(t, strings.Contains(string(resp), "success"))
}
