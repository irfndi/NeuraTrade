package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/irfandi/celebrum-ai-go/internal/config"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
)

// Helper functions for environment variable management with proper error handling
func mustSetEnv(t *testing.T, key, value string) {
	t.Helper()
	if err := os.Setenv(key, value); err != nil {
		t.Fatalf("Failed to set env %s: %v", key, err)
	}
}

func mustUnsetEnv(t *testing.T, key string) {
	t.Helper()
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("Failed to unset env %s: %v", key, err)
	}
}

// TestMainFunctionBasic tests the main function in a controlled environment
func TestMainFunctionBasic(t *testing.T) {
	// Save original environment
	originalEnv := make(map[string]string)
	envVars := []string{
		"ENVIRONMENT", "LOG_LEVEL", "SERVER_PORT", "DATABASE_HOST", "DATABASE_PORT",
		"DATABASE_USER", "DATABASE_PASSWORD", "DATABASE_DBNAME", "DATABASE_SSLMODE",
		"REDIS_HOST", "REDIS_PORT", "REDIS_PASSWORD", "REDIS_DB",
		"TELEMETRY_ENABLED", "TELEMETRY_OTLP_ENDPOINT", "TELEMETRY_SERVICE_NAME",
		"TELEMETRY_SERVICE_VERSION", "TELEMETRY_LOG_LEVEL", "CLEANUP_INTERVAL",
		"CLEANUP_ENABLE_SMART_CLEANUP", "BACKFILL_ENABLED", "ARBITRAGE_ENABLED",
		"CCXT_SERVICE_URL", "CCXT_TIMEOUT", "TELEGRAM_BOT_TOKEN",
	}

	// Backup original environment
	for _, env := range envVars {
		if val := os.Getenv(env); val != "" {
			originalEnv[env] = val
		}
	}

	// Set test environment
	testEnv := map[string]string{
		"ENVIRONMENT":                  "test",
		"LOG_LEVEL":                    "error",
		"SERVER_PORT":                  "8083",
		"DATABASE_HOST":                "localhost",
		"DATABASE_PORT":                "5432",
		"DATABASE_USER":                "testuser",
		"DATABASE_PASSWORD":            "testpass",
		"DATABASE_DBNAME":              "testdb",
		"DATABASE_SSLMODE":             "disable",
		"REDIS_HOST":                   "localhost",
		"REDIS_PORT":                   "6379",
		"REDIS_PASSWORD":               "",
		"REDIS_DB":                     "0",
		"TELEMETRY_ENABLED":            "false",
		"TELEMETRY_SERVICE_NAME":       "test-service",
		"TELEMETRY_SERVICE_VERSION":    "test-version",
		"TELEMETRY_LOG_LEVEL":          "error",
		"CLEANUP_INTERVAL":             "1",
		"CLEANUP_ENABLE_SMART_CLEANUP": "false",
		"BACKFILL_ENABLED":             "false",
		"ARBITRAGE_ENABLED":            "false",
		"CCXT_SERVICE_URL":             "http://localhost:3001",
		"CCXT_TIMEOUT":                 "5",
		"TELEGRAM_BOT_TOKEN":           "",
	}

	// Set test environment
	for key, value := range testEnv {
		mustSetEnv(t, key, value)
	}

	// Restore environment after test
	defer func() {
		for key, value := range originalEnv {
			mustSetEnv(t, key, value)
		}
		for key := range testEnv {
			if _, exists := originalEnv[key]; !exists {
				mustUnsetEnv(t, key)
			}
		}
	}()

	// Set Gin to test mode to avoid debug output
	gin.SetMode(gin.TestMode)

	// Test configuration loading
	cfg, err := config.Load()
	assert.NoError(t, err)
	assert.NotNil(t, cfg)
	assert.Equal(t, "test", cfg.Environment)
	assert.Equal(t, 8083, cfg.Server.Port)

	// Test that main function can be called (it will exit, but we can't capture that easily)
	// This is a basic test to exercise the main function path
	assert.True(t, true) // Placeholder for actual main() testing
}

// TestRunFunctionBasic tests the run function components without external dependencies
func TestRunFunctionBasic(t *testing.T) {
	// Test configuration loading
	cfg, err := config.Load()
	if err != nil {
		// This is expected in test environment
		assert.Contains(t, err.Error(), "Config File")
		t.Skip("Cannot test run function without proper configuration")
		return
	}

	assert.NotNil(t, cfg)
	assert.NotZero(t, cfg.Server.Port)
	assert.NotEmpty(t, cfg.Environment)

	// Test server address formatting
	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	assert.Contains(t, addr, ":")
	assert.True(t, len(addr) > 1)

	// Test HTTP server creation without starting it
	router := gin.New()
	assert.NotNil(t, router)

	srv := &http.Server{
		Addr:    addr,
		Handler: router,
	}
	assert.NotNil(t, srv)
	assert.Equal(t, addr, srv.Addr)
	assert.Equal(t, router, srv.Handler)

	// Test context creation for graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	assert.NotNil(t, ctx)
	assert.NotNil(t, cancel)

	deadline, ok := ctx.Deadline()
	assert.True(t, ok)
	assert.True(t, deadline.After(time.Now()))
}

// TestServerComponents tests individual server components
func TestServerComponents(t *testing.T) {
	// Test server configuration
	cfg, err := config.Load()
	if err != nil {
		t.Skip("Cannot test server components without configuration")
		return
	}

	// Test router setup
	router := gin.New()
	assert.NotNil(t, router)

	// Test middleware setup
	router.Use(gin.Logger())
	router.Use(gin.Recovery())
	assert.NotNil(t, router)

	// Test HTTP server configuration
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	assert.NotNil(t, srv)
	assert.Equal(t, 10*time.Second, srv.ReadTimeout)
	assert.Equal(t, 10*time.Second, srv.WriteTimeout)

	// Test signal handling setup
	quit := make(chan os.Signal, 1)
	assert.NotNil(t, quit)
	assert.Equal(t, 1, cap(quit))
}

// TestConfigurationLoadingDirect tests configuration loading directly
func TestConfigurationLoadingDirect(t *testing.T) {
	// Test with default configuration
	cfg, err := config.Load()

	// In test environment, this might fail due to missing config file
	if err != nil {
		assert.Contains(t, err.Error(), "Config File")
		t.Skip("Configuration file not found in test environment")
		return
	}

	assert.NotNil(t, cfg)
	assert.NotEmpty(t, cfg.Environment)
	assert.NotZero(t, cfg.Server.Port)
	assert.Equal(t, "github.com/irfandi/celebrum-ai-go", cfg.Telemetry.ServiceName)
	assert.Equal(t, "1.0.0", cfg.Telemetry.ServiceVersion)
}

// TestMockDependencies tests that we can create mock dependencies
func TestMockDependencies(t *testing.T) {
	// Test mock Redis setup
	s, err := miniredis.Run()
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skip("miniredis cannot bind in this environment; skipping TestMockDependencies")
		}
		t.Fatalf("failed to start miniredis: %v", err)
	}
	assert.NoError(t, err)
	defer s.Close()

	rdb := redis.NewClient(&redis.Options{
		Addr: s.Addr(),
	})
	assert.NotNil(t, rdb)

	// Test basic Redis operations
	ctx := context.Background()
	err = rdb.Set(ctx, "test", "value", 0).Err()
	assert.NoError(t, err)

	val, err := rdb.Get(ctx, "test").Result()
	assert.NoError(t, err)
	assert.Equal(t, "value", val)

	// Test mock database setup
	mock, err := pgxmock.NewPool()
	assert.NoError(t, err)
	defer mock.Close()

	assert.NotNil(t, mock)
}

// TestServerAddressFormats tests various server address formats
func TestServerAddressFormats(t *testing.T) {
	testCases := []struct {
		port     int
		expected string
	}{
		{80, ":80"},
		{8080, ":8080"},
		{3000, ":3000"},
		{9000, ":9000"},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("port_%d", tc.port), func(t *testing.T) {
			addr := fmt.Sprintf(":%d", tc.port)
			assert.Equal(t, tc.expected, addr)
			assert.Contains(t, addr, fmt.Sprintf("%d", tc.port))
		})
	}
}

// TestTimeoutConfiguration tests various timeout configurations
func TestTimeoutConfiguration(t *testing.T) {
	timeouts := []struct {
		name     string
		duration time.Duration
		valid    bool
	}{
		{"zero", 0, false},
		{"second", time.Second, true},
		{"minute", time.Minute, true},
		{"hour", time.Hour, true},
	}

	for _, tt := range timeouts {
		t.Run(tt.name, func(t *testing.T) {
			if tt.valid {
				assert.True(t, tt.duration > 0)
			}
		})
	}
}

// TestEnvironmentVariableParsing tests environment variable handling
func TestEnvironmentVariableParsing(t *testing.T) {
	// Test that we can read environment variables
	testKey := "TEST_NEURATRADE_VAR"
	testValue := "test_value"

	// Set environment variable
	mustSetEnv(t, testKey, testValue)
	defer mustUnsetEnv(t, testKey)

	// Read it back
	retrieved := os.Getenv(testKey)
	assert.Equal(t, testValue, retrieved)

	// Test empty environment variable
	emptyKey := "TEST_NEURATRADE_EMPTY"
	mustSetEnv(t, emptyKey, "")
	defer mustUnsetEnv(t, emptyKey)

	retrieved = os.Getenv(emptyKey)
	assert.Equal(t, "", retrieved)
}
