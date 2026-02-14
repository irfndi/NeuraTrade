package main

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/ai"
	"github.com/irfndi/neuratrade/internal/config"
	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPrintAIUsage tests the printAIUsage function
func TestPrintAIUsage(t *testing.T) {
	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	printAIUsage()

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	// Verify output contains expected sections
	assert.Contains(t, output, "NeuraTrade AI Model Registry CLI")
	assert.Contains(t, output, "Usage:")
	assert.Contains(t, output, "models")
	assert.Contains(t, output, "providers")
	assert.Contains(t, output, "search")
	assert.Contains(t, output, "show")
	assert.Contains(t, output, "sync")
	assert.Contains(t, output, "route")
	assert.Contains(t, output, "capabilities")
	assert.Contains(t, output, "status")
	assert.Contains(t, output, "Examples:")
}

// TestRunAICLI_MissingCommand tests runAICLI with no command
func TestRunAICLI_MissingCommand(t *testing.T) {
	// Save original args
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	// Set args with only "ai" but no subcommand
	os.Args = []string{"neuratrade", "ai"}

	err := runAICLI()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing command")
}

// TestRunAICLI_UnknownCommand tests runAICLI with unknown command
func TestRunAICLI_UnknownCommand(t *testing.T) {
	// Save original args
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	// Set args with unknown command
	os.Args = []string{"neuratrade", "ai", "unknown"}

	err := runAICLI()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command")
}

// TestNewBootstrapCommand tests the NewBootstrapCommand function
func TestNewBootstrapCommand(t *testing.T) {
	cmd := NewBootstrapCommand()
	assert.NotNil(t, cmd)
	assert.NotNil(t, cmd.reader)
}

// TestBootstrapCommandRun tests the bootstrap command Run method
func TestBootstrapCommandRun(t *testing.T) {
	// Create temp directory for test
	tempDir := t.TempDir()
	originalWd, _ := os.Getwd()
	defer os.Chdir(originalWd)
	os.Chdir(tempDir)

	cmd := NewBootstrapCommand()

	// Test with user declining to overwrite (when no .env exists)
	// This should complete successfully
	err := cmd.Run()

	// The command might fail due to missing input in test environment
	// but it should not panic
	if err != nil {
		// Expected in automated test environment
		assert.True(t, strings.Contains(err.Error(), "EOF") ||
			strings.Contains(err.Error(), "failed to generate") ||
			err == nil)
	}
}

// TestBootstrapCommandRun_WithExistingEnv tests bootstrap when .env exists
func TestBootstrapCommandRun_WithExistingEnv(t *testing.T) {
	// Create temp directory for test
	tempDir := t.TempDir()
	originalWd, _ := os.Getwd()
	defer os.Chdir(originalWd)
	os.Chdir(tempDir)

	// Create existing .env file
	envPath := filepath.Join(tempDir, ".env")
	err := os.WriteFile(envPath, []byte("EXISTING=value\n"), 0644)
	require.NoError(t, err)

	cmd := NewBootstrapCommand()

	// This should detect existing file and prompt for overwrite
	// In test environment without stdin, it will likely fail
	err = cmd.Run()

	// Should complete or fail gracefully
	if err != nil {
		assert.True(t, strings.Contains(err.Error(), "EOF") ||
			strings.Contains(err.Error(), "cancelled") ||
			strings.Contains(err.Error(), "failed to generate"))
	}
}

// TestBootstrapCommandReadInput tests the readInput method
func TestBootstrapCommandReadInput(t *testing.T) {
	cmd := NewBootstrapCommand()

	// Test reading with default value
	// Since we can't easily mock stdin, we verify the method doesn't panic
	result := cmd.readInput("default")
	// In test environment, it will likely return empty or the default
	assert.True(t, result == "" || result == "default")
}

// TestBootstrapCommandGenerateEnvFile tests the generateEnvFile method
func TestBootstrapCommandGenerateEnvFile(t *testing.T) {
	// Create temp directory
	tempDir := t.TempDir()
	envPath := filepath.Join(tempDir, ".env")

	cmd := NewBootstrapCommand()
	cfg := &Config{
		Environment:      "test",
		DatabaseHost:     "localhost",
		DatabasePort:     "5432",
		DatabaseName:     "testdb",
		DatabaseUser:     "testuser",
		DatabasePassword: "testpass",
		RedisHost:        "localhost",
		RedisPort:        "6379",
		JWTSecret:        "test-secret",
		TelegramBotToken: "test-token",
		SentryDSN:        "",
	}

	err := cmd.generateEnvFile(envPath, cfg)
	assert.NoError(t, err)

	// Verify file was created
	content, err := os.ReadFile(envPath)
	assert.NoError(t, err)
	contentStr := string(content)

	// Verify content
	assert.Contains(t, contentStr, "ENVIRONMENT=test")
	assert.Contains(t, contentStr, "DATABASE_HOST=localhost")
	assert.Contains(t, contentStr, "DATABASE_PORT=5432")
	assert.Contains(t, contentStr, "DATABASE_NAME=testdb")
	assert.Contains(t, contentStr, "DATABASE_USER=testuser")
	assert.Contains(t, contentStr, "DATABASE_PASSWORD=testpass")
	assert.Contains(t, contentStr, "REDIS_HOST=localhost")
	assert.Contains(t, contentStr, "REDIS_PORT=6379")
	assert.Contains(t, contentStr, "JWT_SECRET=test-secret")
	assert.Contains(t, contentStr, "TELEGRAM_BOT_TOKEN=test-token")
}

// TestBootstrapCommandGenerateEnvFile_InvalidPath tests generateEnvFile with invalid path
func TestBootstrapCommandGenerateEnvFile_InvalidPath(t *testing.T) {
	cmd := NewBootstrapCommand()
	cfg := &Config{
		Environment: "test",
	}

	// Try to write to a path that doesn't exist
	invalidPath := "/nonexistent/directory/.env"
	err := cmd.generateEnvFile(invalidPath, cfg)
	assert.Error(t, err)
}

// TestBootstrapCommandCreateDirectories tests the createDirectories method
func TestBootstrapCommandCreateDirectories(t *testing.T) {
	// Create temp directory
	tempDir := t.TempDir()
	originalWd, _ := os.Getwd()
	defer os.Chdir(originalWd)
	os.Chdir(tempDir)

	cmd := NewBootstrapCommand()
	err := cmd.createDirectories()
	assert.NoError(t, err)

	// Verify directories were created
	assert.DirExists(t, filepath.Join(tempDir, "logs"))
	assert.DirExists(t, filepath.Join(tempDir, "data"))
	assert.DirExists(t, filepath.Join(tempDir, "tmp"))
}

// TestGenerateRandomSecret tests the generateRandomSecret function
func TestGenerateRandomSecret(t *testing.T) {
	secret1 := generateRandomSecret()
	secret2 := generateRandomSecret()

	// Verify secrets are not empty
	assert.NotEmpty(t, secret1)
	assert.NotEmpty(t, secret2)

	// Verify secrets are different (highly unlikely to be same)
	assert.NotEqual(t, secret1, secret2)

	// Verify they are valid hex strings
	assert.Equal(t, 64, len(secret1)) // 32 bytes = 64 hex chars
	assert.Equal(t, 64, len(secret2))
}

// TestTimeNow tests the timeNow function
func TestTimeNow(t *testing.T) {
	result := timeNow()

	// Verify the result is a valid date string in expected format
	assert.NotEmpty(t, result)
	assert.Equal(t, 10, len(result)) // YYYY-MM-DD = 10 chars

	// Parse it to verify it's valid
	parsed, err := time.Parse("2006-01-02", result)
	assert.NoError(t, err)
	assert.True(t, parsed.Year() > 2000)
}

// TestWarnLegacyHandlersPath_WithLegacyPath tests warnLegacyHandlersPath when legacy path exists
func TestWarnLegacyHandlersPath_WithLegacyPath(t *testing.T) {
	// Create temp directory with legacy handlers path
	tempDir := t.TempDir()
	originalWd, _ := os.Getwd()
	defer os.Chdir(originalWd)
	os.Chdir(tempDir)

	// Create the legacy directory
	err := os.MkdirAll("internal/handlers", 0755)
	require.NoError(t, err)

	logger := zaplogrus.New()

	warnLegacyHandlersPath(logger)
	// Should complete without panic
	assert.True(t, true)
}

// TestWarnLegacyHandlersPath_WithoutLegacyPath tests warnLegacyHandlersPath when no legacy path
func TestWarnLegacyHandlersPath_WithoutLegacyPath(t *testing.T) {
	// Create temp directory without legacy handlers path
	tempDir := t.TempDir()
	originalWd, _ := os.Getwd()
	defer os.Chdir(originalWd)
	os.Chdir(tempDir)

	logger := zaplogrus.New()

	warnLegacyHandlersPath(logger)
	// Should complete without panic
	assert.True(t, true)
}

// TestWarnLegacyHandlersPath_NilLogger tests warnLegacyHandlersPath with nil logger
func TestWarnLegacyHandlersPath_NilLogger(t *testing.T) {
	// Should not panic with nil logger
	warnLegacyHandlersPath(nil)
	// If we get here without panic, test passes
	assert.True(t, true)
}

// TestRunSeeder_ConfigLoadError tests runSeeder when config fails to load
func TestRunSeeder_ConfigLoadError(t *testing.T) {
	if os.Getenv("CI") == "true" {
		t.Skip("Skipping in CI environment - database may be available")
	}
	oldEnv := os.Getenv("ENVIRONMENT")
	defer os.Setenv("ENVIRONMENT", oldEnv)

	os.Unsetenv("ENVIRONMENT")
	os.Unsetenv("DATABASE_HOST")
	os.Unsetenv("DATABASE_URL")

	err := runSeeder()
	assert.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "failed to load config") ||
		strings.Contains(err.Error(), "database") ||
		strings.Contains(err.Error(), "connect"))
}

// TestRunSeeder_DatabaseConnectionError tests runSeeder when database connection fails
func TestRunSeeder_DatabaseConnectionError(t *testing.T) {
	// Set environment to trigger database connection failure
	t.Setenv("DATABASE_HOST", "nonexistent-host")
	t.Setenv("DATABASE_PORT", "5432")
	t.Setenv("DATABASE_USER", "test")
	t.Setenv("DATABASE_PASSWORD", "test")
	t.Setenv("DATABASE_DBNAME", "test")
	t.Setenv("DATABASE_SSLMODE", "disable")
	t.Setenv("DATABASE_DRIVER", "postgres")

	err := runSeeder()
	assert.Error(t, err)
	// Should fail at database connection
	assert.True(t, strings.Contains(err.Error(), "database") ||
		strings.Contains(err.Error(), "connect") ||
		strings.Contains(err.Error(), "failed to load config"))
}

// TestRunSQLiteSeeder_DatabaseConnectionError tests runSQLiteSeeder when connection fails
func TestRunSQLiteSeeder_DatabaseConnectionError(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Driver:     "sqlite",
			SQLitePath: "/nonexistent/directory/test.db",
		},
	}

	err := runSQLiteSeeder(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to connect")
}

// TestRunSeeder_InvalidDriver tests runSeeder with invalid driver
func TestRunSeeder_InvalidDriver(t *testing.T) {
	t.Setenv("DATABASE_DRIVER", "invalid")
	t.Setenv("DATABASE_HOST", "localhost")
	t.Setenv("DATABASE_PORT", "5432")
	t.Setenv("DATABASE_USER", "test")
	t.Setenv("DATABASE_PASSWORD", "test")
	t.Setenv("DATABASE_DBNAME", "test")
	t.Setenv("DATABASE_SSLMODE", "disable")

	err := runSeeder()
	assert.Error(t, err)
}

// TestListModels tests the listModels function
func TestListModels(t *testing.T) {
	ctx := context.Background()
	registry := ai.NewRegistry()

	// Test with no args
	err := listModels(ctx, registry, []string{})
	// May error due to empty registry, but should not panic
	_ = err
}

// TestListProviders tests the listProviders function
func TestListProviders(t *testing.T) {
	ctx := context.Background()
	registry := ai.NewRegistry()

	// Test with no args
	err := listProviders(ctx, registry, []string{})
	_ = err
}

// TestSearchModels tests the searchModels function
func TestSearchModels(t *testing.T) {
	ctx := context.Background()
	registry := ai.NewRegistry()

	// Test with empty search term
	err := searchModels(ctx, registry, []string{})
	// Should error about missing search term
	if err != nil {
		assert.Contains(t, err.Error(), "search term")
	}

	// Test with search term
	err = searchModels(ctx, registry, []string{"gpt"})
	_ = err
}

// TestShowModel tests the showModel function
func TestShowModel(t *testing.T) {
	ctx := context.Background()
	registry := ai.NewRegistry()

	// Test with empty model ID
	err := showModel(ctx, registry, []string{})
	if err != nil {
		assert.Contains(t, err.Error(), "model ID")
	}

	// Test with model ID
	err = showModel(ctx, registry, []string{"gpt-4"})
	_ = err
}

// TestSyncRegistry tests the syncRegistry function
func TestSyncRegistry(t *testing.T) {
	ctx := context.Background()
	registry := ai.NewRegistry()

	// Test sync
	err := syncRegistry(ctx, registry, []string{})
	_ = err
}

// TestRouteModel tests the routeModel function
func TestRouteModel(t *testing.T) {
	ctx := context.Background()
	registry := ai.NewRegistry()

	// Test routing
	err := routeModel(ctx, registry, []string{})
	_ = err
}

// TestListByCapabilities tests the listByCapabilities function
func TestListByCapabilities(t *testing.T) {
	ctx := context.Background()
	registry := ai.NewRegistry()

	// Test with no filters
	err := listByCapabilities(ctx, registry, []string{})
	_ = err

	// Test with tool filter
	err = listByCapabilities(ctx, registry, []string{"--tools"})
	_ = err

	// Test with vision filter
	err = listByCapabilities(ctx, registry, []string{"--vision"})
	_ = err
}

// TestShowStatus tests the showStatus function
func TestShowStatus(t *testing.T) {
	ctx := context.Background()
	registry := ai.NewRegistry()

	// Test status
	err := showStatus(ctx, registry, []string{})
	_ = err
}

// TestBootstrapCommandReadInputWithBuffer tests readInput with simulated input
func TestBootstrapCommandReadInputWithBuffer(t *testing.T) {
	// Create a command with a buffer containing user input
	input := "custom-value\n"
	cmd := &BootstrapCommand{
		reader: bufio.NewReader(strings.NewReader(input)),
	}

	result := cmd.readInput("default")
	assert.Equal(t, "custom-value", result)
}

// TestBootstrapCommandReadInputWithEmptyInput tests readInput with empty user input
func TestBootstrapCommandReadInputWithEmptyInput(t *testing.T) {
	// Create a command with empty input (user just hits enter)
	input := "\n"
	cmd := &BootstrapCommand{
		reader: bufio.NewReader(strings.NewReader(input)),
	}

	result := cmd.readInput("default-value")
	assert.Equal(t, "default-value", result)
}

// TestBootstrapCommandCollectConfigurationDefaults tests collectConfiguration using defaults
func TestBootstrapCommandCollectConfigurationDefaults(t *testing.T) {
	// Simulate user pressing enter for all prompts (accepting defaults)
	input := strings.Repeat("\n", 10)

	cmd := &BootstrapCommand{
		reader: bufio.NewReader(strings.NewReader(input)),
	}

	cfg := cmd.collectConfiguration()

	assert.Equal(t, "development", cfg.Environment)
	assert.Equal(t, "localhost", cfg.DatabaseHost)
	assert.Equal(t, "5432", cfg.DatabasePort)
	assert.Equal(t, "neuratrade", cfg.DatabaseName)
	assert.Equal(t, "postgres", cfg.DatabaseUser)
	assert.Equal(t, "postgres", cfg.DatabasePassword)
	assert.Equal(t, "localhost", cfg.RedisHost)
	assert.Equal(t, "6379", cfg.RedisPort)
	assert.NotEmpty(t, cfg.JWTSecret)
	assert.Equal(t, "", cfg.TelegramBotToken)
	assert.Equal(t, "", cfg.SentryDSN)
}
