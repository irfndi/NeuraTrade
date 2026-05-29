package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestConfigInitUsesCurrentZAIProviderDefaults(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("NEURATRADE_HOME", filepath.Join(home, ".neuratrade"))

	flags := flag.NewFlagSet("config-init", flag.ContinueOnError)
	flags.String("binance-key", "", "")
	flags.String("binance-secret", "", "")
	flags.String("telegram-token", "", "")
	flags.String("ai-key", "test-ai-key", "")
	flags.Bool("force", false, "")
	require.NoError(t, flags.Parse([]string{}))

	ctx := cli.NewContext(cli.NewApp(), flags, nil)
	require.NoError(t, configInit(ctx))

	configPath := filepath.Join(home, ".neuratrade", "config.json")
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)

	var config map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &config))
	require.IsType(t, map[string]interface{}{}, config["ai"])
	aiConfig := config["ai"].(map[string]interface{})
	require.IsType(t, map[string]interface{}{}, config["ccxt"])
	ccxtConfig := config["ccxt"].(map[string]interface{})
	require.IsType(t, map[string]interface{}{}, config["security"])
	securityConfig := config["security"].(map[string]interface{})

	assert.Equal(t, "zhipu", aiConfig["provider"])
	assert.Equal(t, "glm-5-turbo", aiConfig["model"])
	assert.Equal(t, "https://api.z.ai/api/paas/v4", aiConfig["base_url"])
	assert.Equal(t, "test-ai-key", aiConfig["api_key"])
	assert.Len(t, ccxtConfig["admin_api_key"], 32)
	assert.Len(t, securityConfig["jwt_secret"], 64)
	assert.Len(t, securityConfig["admin_api_key"], 32)
	assert.NotEqual(t, "change-me-in-production-use-random-32-chars", securityConfig["jwt_secret"])
	assert.NotEqual(t, "change-me-in-production", ccxtConfig["admin_api_key"])
	assert.NotEqual(t, ccxtConfig["admin_api_key"], securityConfig["jwt_secret"])
	assert.NotEqual(t, ccxtConfig["admin_api_key"], securityConfig["admin_api_key"])
	assert.NotEqual(t, securityConfig["jwt_secret"], securityConfig["admin_api_key"])
}

func TestConfigInitRespectsNeuratradeHome(t *testing.T) {
	home := t.TempDir()
	configHome := filepath.Join(home, "custom-neuratrade-home")
	t.Setenv("HOME", filepath.Join(home, "default-home"))
	t.Setenv("NEURATRADE_HOME", configHome)

	flags := flag.NewFlagSet("config-init", flag.ContinueOnError)
	flags.String("binance-key", "", "")
	flags.String("binance-secret", "", "")
	flags.String("telegram-token", "", "")
	flags.String("ai-key", "", "")
	flags.Bool("force", false, "")
	require.NoError(t, flags.Parse([]string{}))

	ctx := cli.NewContext(cli.NewApp(), flags, nil)
	require.NoError(t, configInit(ctx))

	configPath := filepath.Join(configHome, "config.json")
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	require.NoFileExists(t, filepath.Join(home, "default-home", ".neuratrade", "config.json"))

	var config map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &config))
	databaseConfig, ok := config["database"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, filepath.Join(configHome, "data", "neuratrade.db"), databaseConfig["sqlite_path"])
}

func TestConfigStatusAndShowRespectNeuratradeHome(t *testing.T) {
	home := t.TempDir()
	configHome := filepath.Join(home, "custom-neuratrade-home")
	t.Setenv("HOME", filepath.Join(home, "default-home"))
	t.Setenv("NEURATRADE_HOME", configHome)
	require.NoError(t, os.MkdirAll(configHome, 0700))
	require.NoError(t, os.WriteFile(
		filepath.Join(configHome, "config.json"),
		[]byte(`{"services":{"ccxt":{},"telegram":{}},"ai":{"api_key":"secret"},"security":{}}`),
		0600,
	))

	ctx := cli.NewContext(cli.NewApp(), flag.NewFlagSet("config", flag.ContinueOnError), nil)
	require.NoError(t, configStatus(ctx))
	require.NoError(t, configShow(ctx))
	require.NoFileExists(t, filepath.Join(home, "default-home", ".neuratrade", "config.json"))
}

func TestDefaultCLIAIProviderConfigReturnsCurrentZAIDefaults(t *testing.T) {
	defaults := defaultCLIAIProviderConfig()

	assert.Equal(t, "zhipu", defaults.Provider)
	assert.Equal(t, "glm-5-turbo", defaults.Model)
	assert.Equal(t, "https://api.z.ai/api/paas/v4", defaults.BaseURL)
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

		response := struct {
			Models []AIModel `json:"models"`
		}{
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/health", r.URL.Path)
		assert.Equal(t, http.MethodGet, r.Method)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"healthy","services":{"backend":"healthy"},"timestamp":"2026-02-26T00:00:00Z"}`))
	}))
	defer server.Close()

	originalURL := os.Getenv("NEURATRADE_API_BASE_URL")
	os.Setenv("NEURATRADE_API_BASE_URL", server.URL)
	defer os.Setenv("NEURATRADE_API_BASE_URL", originalURL)

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
