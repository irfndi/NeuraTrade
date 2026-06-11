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
	"strconv"
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

// PR-6: readPIDFile treats a missing file as an error (the caller uses
// the error to decide whether to print 'NOT MANAGED' vs 'STALE PID').
// A non-existent PID reports alive=false without an error (the file
// was readable, but the process is gone). A current process's own
// PID reports alive=true. The PID is echoed back as a string for the
// status output.
func TestReadPIDFile(t *testing.T) {
	t.Run("missing file returns error", func(t *testing.T) {
		alive, pidStr, err := readPIDFile(filepath.Join(t.TempDir(), "nope.pid"))
		assert.Error(t, err)
		assert.False(t, alive)
		assert.Empty(t, pidStr)
	})
	t.Run("current process PID is alive", func(t *testing.T) {
		pidFile := filepath.Join(t.TempDir(), "self.pid")
		pid := os.Getpid()
		require.NoError(t, os.WriteFile(pidFile, []byte(strconv.Itoa(pid)), 0o600))
		alive, pidStr, err := readPIDFile(pidFile)
		assert.NoError(t, err)
		assert.True(t, alive, "current process PID %d must be alive", pid)
		assert.Equal(t, strconv.Itoa(pid), pidStr)
	})
	t.Run("garbage PID is invalid", func(t *testing.T) {
		pidFile := filepath.Join(t.TempDir(), "garbage.pid")
		require.NoError(t, os.WriteFile(pidFile, []byte("not-a-number"), 0o600))
		_, _, err := readPIDFile(pidFile)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid pid")
	})
	t.Run("stale PID reports not-alive", func(t *testing.T) {
		// Use a PID that's extremely unlikely to be alive (Unix PIDs
		// are typically capped well below 1<<30; 1<<31 is reserved
		// for kernel use and never assigned to a userspace process).
		pidFile := filepath.Join(t.TempDir(), "stale.pid")
		require.NoError(t, os.WriteFile(pidFile, []byte("2147483647"), 0o600))
		alive, pidStr, err := readPIDFile(pidFile)
		assert.NoError(t, err)
		assert.False(t, alive)
		assert.Equal(t, "2147483647", pidStr)
	})
}

// PR-6: tailFile returns the last n lines of a file in order. n<=0
// returns an empty slice. Missing file returns an error.
func TestTailFile(t *testing.T) {
	t.Run("returns last n lines in order", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "log.txt")
		content := "line1\nline2\nline3\nline4\nline5\n"
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
		lines, err := tailFile(path, 3)
		assert.NoError(t, err)
		assert.Equal(t, []string{"line3", "line4", "line5"}, lines)
	})
	t.Run("n=0 returns empty slice without error", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "log.txt")
		require.NoError(t, os.WriteFile(path, []byte("a\nb\n"), 0o600))
		lines, err := tailFile(path, 0)
		assert.NoError(t, err)
		assert.Empty(t, lines)
	})
	t.Run("n>=total returns all lines", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "log.txt")
		require.NoError(t, os.WriteFile(path, []byte("a\nb\n"), 0o600))
		lines, err := tailFile(path, 10)
		assert.NoError(t, err)
		assert.Equal(t, []string{"a", "b"}, lines)
	})
	t.Run("missing file returns error", func(t *testing.T) {
		_, err := tailFile(filepath.Join(t.TempDir(), "nope.txt"), 5)
		assert.Error(t, err)
	})
}

// PR-6: agentCommand returns a non-nil *cli.Command with both the
// 'run' and 'status' subcommands registered. This is a smoke test
// — the heavy lifting is in agentRunAction/agentStatusAction.
func TestAgentCommand(t *testing.T) {
	cmd := agentCommand()
	assert.NotNil(t, cmd)
	assert.Equal(t, "agent", cmd.Name)
	names := make([]string, 0, len(cmd.Subcommands))
	for _, sub := range cmd.Subcommands {
		names = append(names, sub.Name)
	}
	assert.ElementsMatch(t, []string{"run", "status"}, names)
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

func TestConfigInitUsesCurrentAIProviderDefaults(t *testing.T) {
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

	assert.Equal(t, "deepseek", aiConfig["provider"])
	assert.Equal(t, "deepseek-chat", aiConfig["model"])
	assert.Equal(t, "", aiConfig["base_url"])
	assert.Equal(t, "test-ai-key", aiConfig["api_key"])
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

	runtimeData, err := os.ReadFile(filepath.Join(configHome, runtimeConfigFileName))
	require.NoError(t, err)
	var runtimeCfg runtimeConfig
	require.NoError(t, json.Unmarshal(runtimeData, &runtimeCfg))
	assert.Equal(t, 8080, runtimeCfg.Server.Port)
	assert.Equal(t, filepath.Join(configHome, "data", "neuratrade.db"), runtimeCfg.Database.SQLitePath)
	assert.Equal(t, "deepseek", runtimeCfg.AI.Provider)
	assert.Equal(t, "10", runtimeCfg.AI.DailyBudget.String())
}

func TestConfigInitAddsRuntimeFileWhenLegacyConfigExists(t *testing.T) {
	home := t.TempDir()
	t.Setenv("NEURATRADE_HOME", home)
	require.NoError(t, os.WriteFile(filepath.Join(home, "config.json"), []byte(`{
		"server":{"host":"127.0.0.1","port":9090},
		"database":{"driver":"sqlite","sqlite_path":"/tmp/legacy-neuratrade.db"},
		"ccxt":{"service_url":"http://legacy-ccxt:3001","grpc_address":"127.0.0.1:51051"},
		"telegram":{"service_url":"http://legacy-telegram:3002","grpc_address":"127.0.0.1:51052","api_base_url":"http://legacy-api:9090"},
		"ai":{"provider":"legacy-ai","model":"legacy-model","base_url":"https://legacy-ai.example/v1"},
		"security":{"admin_api_key":"abcdefghijklmnopqrstuvwxyz123456"}
	}`), 0600))

	flags := flag.NewFlagSet("config-init", flag.ContinueOnError)
	flags.String("binance-key", "", "")
	flags.String("binance-secret", "", "")
	flags.String("telegram-token", "", "")
	flags.String("ai-key", "", "")
	flags.Bool("force", false, "")
	require.NoError(t, flags.Parse([]string{}))

	ctx := cli.NewContext(cli.NewApp(), flags, nil)
	require.NoError(t, configInit(ctx))
	runtimeData, err := os.ReadFile(filepath.Join(home, runtimeConfigFileName))
	require.NoError(t, err)
	var runtimeCfg runtimeConfig
	require.NoError(t, json.Unmarshal(runtimeData, &runtimeCfg))
	assert.Equal(t, 9090, runtimeCfg.Server.Port)
	assert.Equal(t, "/tmp/legacy-neuratrade.db", runtimeCfg.Database.SQLitePath)
	assert.Equal(t, "legacy-ai", runtimeCfg.AI.Provider)
	assert.Equal(t, "legacy-model", runtimeCfg.AI.Model)
	assert.Equal(t, "https://legacy-ai.example/v1", runtimeCfg.AI.BaseURL)
	assert.Equal(t, "http://legacy-ccxt:3001", runtimeCfg.CCXT.ServiceURL)
	assert.Equal(t, "127.0.0.1:51052", runtimeCfg.Telegram.GrpcAddress)
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

func TestDefaultCLIAIProviderConfigReturnsCurrentDefaults(t *testing.T) {
	defaults := defaultCLIAIProviderConfig()

	assert.Equal(t, "deepseek", defaults.Provider)
	assert.Equal(t, "deepseek-chat", defaults.Model)
	assert.Equal(t, "", defaults.BaseURL)
}

func TestNewCLIAppRegistersStableCommandSurface(t *testing.T) {
	app := newCLIApp()

	names := make([]string, 0, len(app.Commands))
	for _, cmd := range app.Commands {
		names = append(names, cmd.Name)
	}

	assert.Contains(t, names, "gateway")
	assert.Contains(t, names, "doctor")
	assert.Contains(t, names, "install")
	assert.Contains(t, names, "update")
	assert.Contains(t, names, "ops")
	assert.Contains(t, names, "backtest")
	assert.Contains(t, names, "autonomous")
	assert.Contains(t, names, "agent")
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
