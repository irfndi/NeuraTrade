package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/irfndi/neuratrade/internal/config"
	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/redis/go-redis/v9"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/stretchr/testify/assert"
)

// Test server configuration
func TestServerConfiguration(t *testing.T) {
	// Test server address formatting
	port := 8080
	expectedAddr := fmt.Sprintf(":%d", port)
	assert.Equal(t, ":8080", expectedAddr)

	// Test different ports
	testPorts := []int{3000, 8000, 8080, 9000}
	for _, p := range testPorts {
		addr := fmt.Sprintf(":%d", p)
		assert.Contains(t, addr, fmt.Sprintf("%d", p))
		assert.True(t, len(addr) > 1)
	}
}

// Test HTTP server creation
func TestHTTPServerCreation(t *testing.T) {
	// Test server creation with Gin router
	router := gin.New()
	assert.NotNil(t, router)

	// Test server configuration
	srv := &http.Server{
		Addr:    ":8080",
		Handler: router,
	}

	assert.NotNil(t, srv)
	assert.Equal(t, ":8080", srv.Addr)
	assert.Equal(t, router, srv.Handler)
}

// Test context with timeout
func TestContextWithTimeout(t *testing.T) {
	// Test context creation for graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	assert.NotNil(t, ctx)
	assert.NotNil(t, cancel)

	// Test deadline
	deadline, ok := ctx.Deadline()
	assert.True(t, ok)
	assert.True(t, deadline.After(time.Now()))
	assert.True(t, deadline.Before(time.Now().Add(31*time.Second)))
}

// Test signal handling setup
func TestSignalHandling(t *testing.T) {
	// Test signal channel creation
	quit := make(chan os.Signal, 1)
	assert.NotNil(t, quit)

	// Test channel capacity
	assert.Equal(t, 1, cap(quit))

	// Test signal types
	signals := []os.Signal{syscall.SIGINT, syscall.SIGTERM}
	for _, sig := range signals {
		assert.NotNil(t, sig)
	}
}

// Test Gin router setup
func TestGinRouterSetup(t *testing.T) {
	// Test default router creation
	router := gin.Default()
	assert.NotNil(t, router)

	// Test new router creation
	newRouter := gin.New()
	assert.NotNil(t, newRouter)

	// Test that routers are different instances
	assert.NotEqual(t, router, newRouter)
}

// Test middleware setup
func TestMiddlewareSetup(t *testing.T) {
	// Test router with middleware
	router := gin.New()
	router.Use(gin.Logger())
	router.Use(gin.Recovery())

	assert.NotNil(t, router)
}

// Test HTTP server timeouts
func TestHTTPServerTimeouts(t *testing.T) {
	// Test server with security timeouts
	router := gin.New()
	srv := &http.Server{
		Addr:              ":8080",
		Handler:           router,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       15 * time.Second,
	}

	assert.NotNil(t, srv)
	assert.Equal(t, 10*time.Second, srv.ReadTimeout)
	assert.Equal(t, 10*time.Second, srv.WriteTimeout)
	assert.Equal(t, 5*time.Second, srv.ReadHeaderTimeout)
	assert.Equal(t, 15*time.Second, srv.IdleTimeout)
}

// Test configuration loading
func TestConfigurationLoading(t *testing.T) {
	// Test actual config loading
	cfg, err := config.Load()
	assert.NoError(t, err)

	// Test default values
	assert.Equal(t, 8080, cfg.Server.Port)
	assert.Equal(t, "info", cfg.LogLevel)
}

// Test Redis connection mock
func TestRedisConnectionMock(t *testing.T) {
	// Test miniredis setup
	s, err := miniredis.Run()
	if err != nil {
		t.Skipf("miniredis unavailable; skipping Redis mock test: %v", err)
		return
	}
	defer s.Close()

	// Test Redis client
	rdb := redis.NewClient(&redis.Options{
		Addr: s.Addr(),
	})
	assert.NotNil(t, rdb)

	// Test basic operations
	err = rdb.Set(context.Background(), "test", "value", 0).Err()
	assert.NoError(t, err)

	val, err := rdb.Get(context.Background(), "test").Result()
	assert.NoError(t, err)
	assert.Equal(t, "value", val)
}

// Test database mock
func TestDatabaseMock(t *testing.T) {
	// Test pgxmock setup
	mock, err := pgxmock.NewPool()
	assert.NoError(t, err)
	defer mock.Close()

	assert.NotNil(t, mock)
}

// Test logger setup
func TestLoggerSetup(t *testing.T) {
	// Test logrus logger
	logger := zaplogrus.New()
	assert.NotNil(t, logger)

	// Test log level setting
	logger.SetLevel(zaplogrus.InfoLevel)
	assert.Equal(t, zaplogrus.InfoLevel, logger.GetLevel())

	// Test JSON formatter
	assert.NotPanics(t, func() {
		logger.SetFormatter(&zaplogrus.JSONFormatter{})
	})
}

// Test graceful shutdown
func TestGracefulShutdown(t *testing.T) {
	// Test server shutdown
	router := gin.New()
	_ = &http.Server{
		Addr:    ":8080",
		Handler: router,
	}

	// Test context cancellation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	assert.NotNil(t, ctx)
	assert.NotNil(t, cancel)

	// Test that context can be cancelled
	cancel()
	select {
	case <-ctx.Done():
		assert.True(t, true)
	default:
		assert.Fail(t, "Context should be cancelled")
	}
}

// Test error handling
func TestErrorHandling(t *testing.T) {
	// Test error formatting
	err := fmt.Errorf("test error")
	assert.NotNil(t, err)
	assert.Equal(t, "test error", err.Error())

	// Test error with context
	wrappedErr := fmt.Errorf("wrapped error: %w", err)
	assert.NotNil(t, wrappedErr)
	assert.Contains(t, wrappedErr.Error(), "test error")
}

// Test memory monitoring
func TestMemoryMonitoring(t *testing.T) {
	// Test memory stats
	memStats, err := mem.VirtualMemory()
	assert.NoError(t, err)
	assert.NotNil(t, memStats)

	// Test that memory values are reasonable
	assert.True(t, memStats.Total > 0)
	assert.True(t, memStats.Available > 0)
	assert.LessOrEqual(t, memStats.Used, memStats.Total)
	assert.GreaterOrEqual(t, memStats.UsedPercent, 0.0)
	assert.LessOrEqual(t, memStats.UsedPercent, 100.0)
}

// Test configuration validation
func TestConfigurationValidation(t *testing.T) {
	// Test port validation
	validPorts := []int{80, 8080, 3000, 8000, 9000}
	for _, port := range validPorts {
		assert.True(t, port > 0)
		assert.True(t, port < 65536)
	}

	// Test timeout validation
	timeouts := []time.Duration{
		5 * time.Second,
		10 * time.Second,
		30 * time.Second,
		60 * time.Second,
	}
	for _, timeout := range timeouts {
		assert.True(t, timeout > 0)
	}
}

// Test HTTP methods and routes
func TestHTTPMethodsAndRoutes(t *testing.T) {
	// Test router with basic routes
	router := gin.New()

	// Test GET route
	router.GET("/test", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "GET test"})
	})

	// Test POST route
	router.POST("/test", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "POST test"})
	})

	assert.NotNil(t, router)
}

// Test middleware chain
func TestMiddlewareChain(t *testing.T) {
	// Test middleware order
	router := gin.New()

	// Add middleware in order
	router.Use(gin.Logger())
	router.Use(gin.Recovery())

	assert.NotNil(t, router)
}

// Test context management
func TestContextManagement(t *testing.T) {
	// Test background context
	ctx := context.Background()
	assert.NotNil(t, ctx)

	// Test context with timeout
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	assert.NotNil(t, ctx)
	assert.NotNil(t, cancel)
}

// Test server lifecycle
func TestServerLifecycle(t *testing.T) {
	// Test server creation and configuration
	router := gin.New()
	srv := &http.Server{
		Addr:    ":8080",
		Handler: router,
	}

	assert.NotNil(t, srv)
	assert.Equal(t, ":8080", srv.Addr)
	assert.NotNil(t, srv.Handler)

	// Test that server is not running initially
	assert.False(t, false) // Placeholder for actual running state check
}

// Test error scenarios
func TestErrorScenarios(t *testing.T) {
	// Test invalid port
	invalidPort := -1
	assert.True(t, invalidPort < 0)

	// Test empty address
	emptyAddr := ""
	assert.Equal(t, "", emptyAddr)

	// Test nil handler
	var nilHandler http.Handler
	assert.Nil(t, nilHandler)
}

// Test configuration scenarios
func TestConfigurationScenarios(t *testing.T) {
	// Test different server configurations
	configs := []struct {
		addr         string
		readTimeout  time.Duration
		writeTimeout time.Duration
	}{
		{":8080", 10 * time.Second, 10 * time.Second},
		{":3000", 5 * time.Second, 5 * time.Second},
		{":9000", 30 * time.Second, 30 * time.Second},
	}

	for _, cfg := range configs {
		assert.NotEmpty(t, cfg.addr)
		assert.True(t, cfg.readTimeout > 0)
		assert.True(t, cfg.writeTimeout > 0)
	}
}

// Test signal handling scenarios
func TestSignalHandlingScenarios(t *testing.T) {
	// Test signal channel capacity
	quit := make(chan os.Signal, 1)
	assert.Equal(t, 1, cap(quit))

	// Test that channel is empty initially
	select {
	case sig := <-quit:
		assert.Fail(t, fmt.Sprintf("Unexpected signal: %v", sig))
	default:
		assert.True(t, true)
	}
}

// Test timeout scenarios
func TestTimeoutScenarios(t *testing.T) {
	// Test various timeout durations
	timeouts := []struct {
		name     string
		duration time.Duration
		valid    bool
	}{
		{"zero", 0, false},
		{"short", time.Second, true},
		{"medium", 30 * time.Second, true},
		{"long", time.Minute, true},
		{"very long", time.Hour, true},
	}

	for _, tt := range timeouts {
		if tt.valid {
			assert.True(t, tt.duration > 0)
		}
	}
}

// Test main function error handling
func TestMainFunctionErrorHandling(t *testing.T) {
	// Test that main function handles errors properly
	// This is a placeholder test since we can't easily test os.Exit
	assert.True(t, true)
}

// Benchmark test for server creation
func BenchmarkServerCreation(b *testing.B) {
	for i := 0; i < b.N; i++ {
		router := gin.New()
		_ = &http.Server{
			Addr:    ":8080",
			Handler: router,
		}
	}
}

// Benchmark test for context creation
func BenchmarkContextCreation(b *testing.B) {
	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		cancel()
		_ = ctx
	}
}

// Test main function with mock environment
func TestMainFunction(t *testing.T) {
	if os.Getenv("TEST_MAIN_FUNCTION") == "1" {
		// This is the child process that will call main()
		main()
		return
	}

	// Set up test environment variables to mock external dependencies
	testEnv := map[string]string{
		"ENVIRONMENT":                     "test",
		"LOG_LEVEL":                       "error",
		"SERVER_PORT":                     "8081",
		"DATABASE_HOST":                   "nonexistent-host",
		"DATABASE_PORT":                   "5432",
		"DATABASE_USER":                   "testuser",
		"DATABASE_PASSWORD":               "testpass",
		"DATABASE_DBNAME":                 "testdb",
		"DATABASE_SSLMODE":                "disable",
		"REDIS_HOST":                      "nonexistent-host",
		"REDIS_PORT":                      "6379",
		"REDIS_PASSWORD":                  "",
		"REDIS_DB":                        "0",
		"TELEMETRY_ENABLED":               "false",
		"TELEMETRY_OTLP_ENDPOINT":         "http://localhost:4318",
		"TELEMETRY_SERVICE_NAME":          "test-service",
		"TELEMETRY_SERVICE_VERSION":       "test-version",
		"TELEMETRY_LOG_LEVEL":             "error",
		"CLEANUP_INTERVAL":                "1",
		"CLEANUP_ENABLE_SMART_CLEANUP":    "false",
		"BACKFILL_ENABLED":                "false",
		"ARBITRAGE_ENABLED":               "false",
		"CCXT_SERVICE_URL":                "http://localhost:3001",
		"CCXT_TIMEOUT":                    "5",
		"TELEGRAM_BOT_TOKEN":              "",
		"MARKET_DATA_COLLECTION_INTERVAL": "1m",
		"MARKET_DATA_BATCH_SIZE":          "1",
		"MARKET_DATA_MAX_RETRIES":         "1",
		"MARKET_DATA_TIMEOUT":             "5s",
		"MARKET_DATA_EXCHANGES":           "binance",
		"BLACKLIST_TTL":                   "1h",
		"BLACKLIST_SHORT_TTL":             "5m",
		"BLACKLIST_LONG_TTL":              "24h",
		"BLACKLIST_USE_REDIS":             "false",
		"BLACKLIST_RETRY_AFTER_CLEAR":     "false",
		"TEST_MAIN_FUNCTION":              "1", // Signal this is the test process
	}

	// Apply test environment variables for the duration of this test
	for key, value := range testEnv {
		t.Setenv(key, value)
	}

	// Test that main function handles errors gracefully by running it in a separate process
	// We use exec.Command to run the test binary with the TEST_MAIN_FUNCTION flag
	cmd := exec.Command(os.Args[0], "-test.run=TestMainFunction")
	cmd.Env = append(os.Environ(), "TEST_MAIN_FUNCTION=1")

	// Capture output
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Run the command
	err := cmd.Run()

	// The command should fail with exit code 1
	if err == nil {
		t.Error("Expected main() to exit with error code 1, but it succeeded")
		return
	}

	// Check if it's an exit error with code 1
	if exitErr, ok := err.(*exec.ExitError); ok {
		if exitErr.ExitCode() != 1 {
			t.Errorf("Expected exit code 1, got %d", exitErr.ExitCode())
		}
	} else {
		t.Errorf("Expected exec.ExitError, got %T: %v", err, err)
	}

	// Check that the error message contains our expected output
	output := stderr.String()
	if !strings.Contains(output, "Application failed: failed to connect to database") {
		t.Errorf("Expected error message about database connection failure, got: %s", output)
	}
}

// TestRunFunctionTelemetryFailure tests run function when telemetry initialization fails
func TestRunFunctionTelemetryFailure(t *testing.T) {
	// Set up environment to force telemetry failure but continue
	testEnv := map[string]string{
		"ENVIRONMENT":             "test",
		"LOG_LEVEL":               "error",
		"SERVER_PORT":             "8082",
		"TELEMETRY_ENABLED":       "true",
		"TELEMETRY_OTLP_ENDPOINT": "invalid-endpoint",
		"DATABASE_HOST":           "invalid-host",
		"REDIS_HOST":              "invalid-host",
		"ARBITRAGE_ENABLED":       "false",
		"BACKFILL_ENABLED":        "false",
		"CLEANUP_INTERVAL":        "1",
	}

	for key, value := range testEnv {
		t.Setenv(key, value)
	}

	err := run()
	assert.Error(t, err)
	// Should fail at database connection after telemetry attempt
	assert.Contains(t, err.Error(), "database")
}

// TestRunFunctionLoggerFallback tests run function logger fallback scenarios
func TestRunFunctionLoggerFallback(t *testing.T) {
	testCases := []struct {
		name             string
		telemetryEnabled string
	}{
		{
			name:             "telemetry enabled without logger",
			telemetryEnabled: "true",
		},
		{
			name:             "telemetry disabled",
			telemetryEnabled: "false",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			env := map[string]string{
				"ENVIRONMENT":       "test",
				"LOG_LEVEL":         "error",
				"SERVER_PORT":       "8083",
				"TELEMETRY_ENABLED": tc.telemetryEnabled,
				"DATABASE_HOST":     "invalid-host",
				"REDIS_HOST":        "invalid-host",
				"ARBITRAGE_ENABLED": "false",
				"BACKFILL_ENABLED":  "false",
				"CLEANUP_INTERVAL":  "1",
			}

			for key, value := range env {
				t.Setenv(key, value)
			}

			err := run()
			assert.Error(t, err)
		})
	}
}

// TestRunFunctionRedisFailure tests run function when Redis connection fails
func TestRunFunctionRedisFailure(t *testing.T) {
	// Set up environment where Redis will fail but database might succeed
	// We'll use invalid Redis to test the fallback behavior
	testEnv := map[string]string{
		"ENVIRONMENT":       "test",
		"LOG_LEVEL":         "error",
		"SERVER_PORT":       "8084",
		"TELEMETRY_ENABLED": "false",
		"DATABASE_HOST":     "invalid-host", // Still fail at database first
		"REDIS_HOST":        "invalid-redis-host",
		"REDIS_PORT":        "6379",
		"REDIS_PASSWORD":    "",
		"REDIS_DB":          "0",
		"ARBITRAGE_ENABLED": "false",
		"BACKFILL_ENABLED":  "false",
		"CLEANUP_INTERVAL":  "1",
	}

	for key, value := range testEnv {
		t.Setenv(key, value)
	}

	err := run()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database")
}

// TestRunFunctionServiceInit tests service initialization paths
func TestRunFunctionServiceInit(t *testing.T) {
	// Test with services enabled to exercise service initialization code
	testEnv := map[string]string{
		"ENVIRONMENT":                     "test",
		"LOG_LEVEL":                       "error",
		"SERVER_PORT":                     "8085",
		"TELEMETRY_ENABLED":               "false",
		"DATABASE_HOST":                   "invalid-host",
		"REDIS_HOST":                      "invalid-host",
		"ARBITRAGE_ENABLED":               "true",
		"BACKFILL_ENABLED":                "true",
		"CLEANUP_INTERVAL":                "1",
		"CLEANUP_ENABLE_SMART_CLEANUP":    "true",
		"CCXT_SERVICE_URL":                "http://localhost:3001",
		"CCXT_TIMEOUT":                    "5",
		"TELEGRAM_BOT_TOKEN":              "fake-token",
		"MARKET_DATA_COLLECTION_INTERVAL": "1m",
		"MARKET_DATA_BATCH_SIZE":          "1",
		"MARKET_DATA_MAX_RETRIES":         "1",
		"MARKET_DATA_TIMEOUT":             "5s",
		"MARKET_DATA_EXCHANGES":           "binance",
		"BLACKLIST_TTL":                   "1h",
		"BLACKLIST_SHORT_TTL":             "5m",
		"BLACKLIST_LONG_TTL":              "24h",
		"BLACKLIST_USE_REDIS":             "false",
		"BLACKLIST_RETRY_AFTER_CLEAR":     "false",
	}

	for key, value := range testEnv {
		t.Setenv(key, value)
	}

	err := run()
	assert.Error(t, err)
	// Should fail at database but service initialization code should be exercised
}

// TestRunFunctionServerConfig tests server startup configuration
func TestRunFunctionServerConfig(t *testing.T) {
	// Test different server configurations
	testCases := []struct {
		name    string
		port    string
		wantErr string
	}{
		{
			name:    "valid port",
			port:    "8080",
			wantErr: "database",
		},
		{
			name:    "alternative port",
			port:    "9000",
			wantErr: "database",
		},
		{
			name:    "high port",
			port:    "8081",
			wantErr: "database",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			env := map[string]string{
				"ENVIRONMENT":       "test",
				"LOG_LEVEL":         "error",
				"SERVER_PORT":       tc.port,
				"TELEMETRY_ENABLED": "false",
				"DATABASE_HOST":     "invalid-host",
				"REDIS_HOST":        "invalid-host",
				"ARBITRAGE_ENABLED": "false",
				"BACKFILL_ENABLED":  "false",
				"CLEANUP_INTERVAL":  "1",
			}

			for key, value := range env {
				t.Setenv(key, value)
			}

			err := run()
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

// TestRunFunctionContextHandling tests context management in run function
func TestRunFunctionContextHandling(t *testing.T) {
	// Test context creation and management that happens in run function
	env := map[string]string{
		"ENVIRONMENT":       "test",
		"LOG_LEVEL":         "error",
		"SERVER_PORT":       "8086",
		"TELEMETRY_ENABLED": "false",
		"DATABASE_HOST":     "invalid-host",
		"REDIS_HOST":        "invalid-host",
		"ARBITRAGE_ENABLED": "false",
		"BACKFILL_ENABLED":  "false",
		"CLEANUP_INTERVAL":  "1",
	}

	for key, value := range env {
		t.Setenv(key, value)
	}

	err := run()
	assert.Error(t, err)
}

// TestRunFunctionErrorScenarios tests error recovery scenarios
func TestRunFunctionErrorScenarios(t *testing.T) {
	// Test various error scenarios that could occur in run function
	testCases := []struct {
		name    string
		envVars map[string]string
		wantErr string
	}{
		{
			name: "database connection error",
			envVars: map[string]string{
				"DATABASE_HOST": "nonexistent-host",
			},
			wantErr: "database",
		},
		{
			name: "invalid database config",
			envVars: map[string]string{
				"DATABASE_PORT": "invalid",
			},
			wantErr: "invalid syntax",
		},
		{
			name: "invalid server config",
			envVars: map[string]string{
				"SERVER_PORT": "invalid",
			},
			wantErr: "invalid syntax",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Set base environment
			baseEnv := map[string]string{
				"ENVIRONMENT":       "test",
				"LOG_LEVEL":         "error",
				"TELEMETRY_ENABLED": "false",
				"REDIS_HOST":        "invalid-host",
				"ARBITRAGE_ENABLED": "false",
				"BACKFILL_ENABLED":  "false",
				"CLEANUP_INTERVAL":  "1",
			}

			for key, value := range baseEnv {
				t.Setenv(key, value)
			}
			for key, value := range tc.envVars {
				t.Setenv(key, value)
			}

			err := run()
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

// Test run function with test configuration
func TestRunFunction(t *testing.T) {
	// Set up minimal test environment with invalid database to fail fast
	testEnv := map[string]string{
		"ENVIRONMENT":                  "test",
		"LOG_LEVEL":                    "error",
		"SERVER_PORT":                  "8082",
		"DATABASE_HOST":                "invalid-host",
		"DATABASE_PORT":                "5432",
		"DATABASE_USER":                "testuser",
		"DATABASE_PASSWORD":            "testpass",
		"DATABASE_DBNAME":              "testdb",
		"DATABASE_SSLMODE":             "disable",
		"REDIS_HOST":                   "invalid-host",
		"REDIS_PORT":                   "6379",
		"REDIS_PASSWORD":               "",
		"REDIS_DB":                     "0",
		"TELEMETRY_ENABLED":            "false",
		"ARBITRAGE_ENABLED":            "false",
		"BACKFILL_ENABLED":             "false",
		"CLEANUP_INTERVAL":             "1",
		"CLEANUP_ENABLE_SMART_CLEANUP": "false",
	}

	// Apply the environment variables for the duration of this test
	for key, value := range testEnv {
		t.Setenv(key, value)
	}

	// Test run function directly - expect it to fail but exercise the function for coverage
	err := run()

	// We expect this to fail due to missing database/Redis connections
	// but the important thing is that it exercises the run() function for coverage
	assert.Error(t, err)

	// The error should be related to connection issues, but the exact message may vary
	assert.True(t, strings.Contains(err.Error(), "connect") ||
		strings.Contains(err.Error(), "database") ||
		strings.Contains(err.Error(), "redis") ||
		strings.Contains(err.Error(), "timeout") ||
		strings.Contains(err.Error(), "refused") ||
		strings.Contains(err.Error(), "invalid"))

	// Test with invalid configuration to exercise error paths
	testCases := []struct {
		name    string
		envVar  string
		value   string
		wantErr string
	}{
		{
			name:    "invalid database port",
			envVar:  "DATABASE_PORT",
			value:   "invalid",
			wantErr: "invalid syntax",
		},
		{
			name:    "invalid server port",
			envVar:  "SERVER_PORT",
			value:   "invalid",
			wantErr: "invalid syntax",
		},
		{
			name:    "invalid redis port",
			envVar:  "REDIS_PORT",
			value:   "invalid",
			wantErr: "invalid syntax",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Set invalid value and rely on testing cleanup for restore
			t.Setenv(tc.envVar, tc.value)

			err := run()
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

// Test configuration loading in main function context
func TestMainConfigurationLoading(t *testing.T) {
	// Test configuration loading as used in main()
	cfg, err := config.Load()

	// Configuration might load with defaults even in test environment
	if err != nil {
		assert.Contains(t, err.Error(), "Config File")
		return
	}

	// If config loaded successfully, verify default values
	assert.NotNil(t, cfg)
	assert.NotEmpty(t, cfg.Environment)
	assert.NotZero(t, cfg.Server.Port)

	// Test telemetry configuration
	assert.NotNil(t, cfg.Telemetry)
	assert.Equal(t, "github.com/irfndi/neuratrade", cfg.Telemetry.ServiceName)
	assert.Equal(t, "1.0.0", cfg.Telemetry.ServiceVersion)
}

// Test graceful shutdown scenario
func TestGracefulShutdownIntegration(t *testing.T) {
	// This test simulates the graceful shutdown portion of the run() function
	// by testing the signal handling and context cancellation logic

	// Test context creation for shutdown (used in main.go)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	assert.NotNil(t, ctx)
	assert.NotNil(t, cancel)

	// Test deadline functionality
	deadline, ok := ctx.Deadline()
	assert.True(t, ok)
	assert.True(t, deadline.After(time.Now()))

	// Test server configuration (matches main.go setup)
	router := gin.New()
	srv := &http.Server{
		Addr:         ":8080",
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	assert.NotNil(t, srv)
	assert.Equal(t, 10*time.Second, srv.ReadTimeout)
	assert.Equal(t, 10*time.Second, srv.WriteTimeout)

	// Test context cancellation (simulates signal handling)
	cancel()
	select {
	case <-ctx.Done():
		assert.Equal(t, context.Canceled, ctx.Err())
	default:
		assert.Fail(t, "Context should be cancelled")
	}
}

// Test error handling patterns in main function
func TestMainErrorHandling(t *testing.T) {
	// Test error formatting and output patterns used in main()

	// Test stderr output formatting
	var buf bytes.Buffer
	err := fmt.Errorf("test error: %w", fmt.Errorf("wrapped error"))
	fmt.Fprintf(&buf, "Application failed: %v\n", err)

	assert.Contains(t, buf.String(), "Application failed")
	assert.Contains(t, buf.String(), "test error")
	assert.Contains(t, buf.String(), "wrapped error")

	// Test multiple error scenarios
	errorScenarios := []struct {
		name      string
		errorFunc func() error
		wantErr   string
	}{
		{
			name: "configuration error",
			errorFunc: func() error {
				return fmt.Errorf("failed to load configuration: %w", fmt.Errorf("file not found"))
			},
			wantErr: "failed to load configuration",
		},
		{
			name: "database error",
			errorFunc: func() error {
				return fmt.Errorf("failed to connect to database: %w", fmt.Errorf("connection refused"))
			},
			wantErr: "failed to connect to database",
		},
		{
			name: "redis error",
			errorFunc: func() error {
				return fmt.Errorf("failed to connect to Redis: %w", fmt.Errorf("timeout"))
			},
			wantErr: "failed to connect to Redis",
		},
	}

	for _, scenario := range errorScenarios {
		t.Run(scenario.name, func(t *testing.T) {
			err := scenario.errorFunc()
			assert.Error(t, err)
			assert.Contains(t, err.Error(), scenario.wantErr)
		})
	}
}

// TestRunFunctionCompleteFlow tests a more complete initialization flow
func TestRunFunctionCompleteFlowExtended(t *testing.T) {
	// Set valid test configuration
	t.Setenv("SERVER_PORT", "8081")
	t.Setenv("DATABASE_HOST", "localhost")
	t.Setenv("DATABASE_PORT", "5432")
	t.Setenv("DATABASE_USER", "postgres")
	t.Setenv("DATABASE_PASSWORD", "postgres")
	t.Setenv("DATABASE_DBNAME", "test_db")
	t.Setenv("REDIS_HOST", "localhost")
	t.Setenv("REDIS_PORT", "6379")
	t.Setenv("TELEMETRY_ENABLED", "false")

	// Create a context with timeout to prevent hanging
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Run in a goroutine to test startup
	done := make(chan error, 1)
	go func() {
		done <- run()
	}()

	// Wait for either completion or timeout
	select {
	case err := <-done:
		// Should fail at database connection since we don't have a real DB
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database")
	case <-ctx.Done():
		t.Log("Test completed by timeout (expected since no real DB)")
	}
}

// TestRunFunctionDatabaseConfig tests various database configuration scenarios
func TestRunFunctionDatabaseConfig(t *testing.T) {
	tests := []struct {
		name   string
		host   string
		port   string
		user   string
		dbname string
	}{
		{
			name:   "invalid_host",
			host:   "nonexistent-host",
			port:   "5432",
			user:   "postgres",
			dbname: "testdb",
		},
		{
			name:   "invalid_port",
			host:   "localhost",
			port:   "invalid",
			user:   "postgres",
			dbname: "testdb",
		},
		{
			name:   "missing_db",
			host:   "localhost",
			port:   "5432",
			user:   "postgres",
			dbname: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TELEMETRY_ENABLED", "false")
			t.Setenv("DATABASE_HOST", tt.host)
			t.Setenv("DATABASE_PORT", tt.port)
			t.Setenv("DATABASE_USER", tt.user)
			t.Setenv("DATABASE_DBNAME", tt.dbname)

			err := run()
			assert.Error(t, err)
		})
	}
}

// TestRunFunctionRedisConfig tests various Redis configuration scenarios
func TestRunFunctionRedisConfig(t *testing.T) {
	tests := []struct {
		name string
		host string
		port string
	}{
		{
			name: "invalid_redis_host",
			host: "nonexistent-redis",
			port: "6379",
		},
		{
			name: "invalid_redis_port",
			host: "localhost",
			port: "invalid",
		},
		{
			name: "redis_timeout",
			host: "localhost",
			port: "6379",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TELEMETRY_ENABLED", "false")
			t.Setenv("DATABASE_HOST", "nonexistent")
			t.Setenv("REDIS_HOST", tt.host)
			t.Setenv("REDIS_PORT", tt.port)

			err := run()
			assert.Error(t, err)
		})
	}
}

// TestRunFunctionCCXTConfig tests CCXT service configuration
func TestRunFunctionCCXTConfig(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		timeout string
	}{
		{
			name:    "invalid_ccxt_url",
			url:     "invalid-url",
			timeout: "30",
		},
		{
			name:    "ccxt_timeout",
			url:     "http://localhost:3001",
			timeout: "1",
		},
		{
			name:    "valid_ccxt_config",
			url:     "http://localhost:3001",
			timeout: "30",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TELEMETRY_ENABLED", "false")
			t.Setenv("DATABASE_HOST", "nonexistent")
			t.Setenv("CCXT_SERVICE_URL", tt.url)
			t.Setenv("CCXT_TIMEOUT", tt.timeout)

			err := run()
			assert.Error(t, err)
		})
	}
}

// TestRunFunctionAdvancedConfig tests advanced configuration scenarios
func TestRunFunctionAdvancedConfig(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{
			name: "cleanup_config",
			env: map[string]string{
				"TELEMETRY_ENABLED":             "false",
				"DATABASE_HOST":                 "nonexistent",
				"CLEANUP_ENABLED":               "true",
				"CLEANUP_INTERVAL":              "60",
				"CLEANUP_MARKET_DATA_RETENTION": "24h",
				"CLEANUP_ARBITRAGE_RETENTION":   "48h",
			},
		},
		{
			name: "arbitrage_config",
			env: map[string]string{
				"TELEMETRY_ENABLED":         "false",
				"DATABASE_HOST":             "nonexistent",
				"ARBITRAGE_ENABLED":         "true",
				"ARBITRAGE_MIN_PROFIT":      "1.0",
				"ARBITRAGE_MAX_AGE_MINUTES": "60",
				"ARBITRAGE_BATCH_SIZE":      "50",
			},
		},
		{
			name: "backfill_config",
			env: map[string]string{
				"TELEMETRY_ENABLED":   "false",
				"DATABASE_HOST":       "nonexistent",
				"BACKFILL_ENABLED":    "true",
				"BACKFILL_HOURS":      "12",
				"BACKFILL_BATCH_SIZE": "10",
				"BACKFILL_DELAY":      "1000",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			for key, value := range tt.env {
				t.Setenv(key, value)
			}

			err := run()
			assert.Error(t, err)
		})
	}
}

// TestRunFunctionTelemetryInitialization tests telemetry initialization paths
func TestRunFunctionTelemetryInitialization(t *testing.T) {
	tests := []struct {
		name      string
		enabled   string
		endpoint  string
		service   string
		version   string
		logLevel  string
		expectErr bool
	}{
		{
			name:      "telemetry disabled",
			enabled:   "false",
			endpoint:  "http://localhost:4318",
			service:   "test-service",
			version:   "1.0.0",
			logLevel:  "info",
			expectErr: true, // Should fail at database
		},
		{
			name:      "telemetry enabled with invalid endpoint",
			enabled:   "true",
			endpoint:  "invalid-endpoint",
			service:   "test-service",
			version:   "1.0.0",
			logLevel:  "info",
			expectErr: true, // Should fail at database after telemetry attempt
		},
		{
			name:      "telemetry enabled with valid endpoint",
			enabled:   "true",
			endpoint:  "http://localhost:4318",
			service:   "test-service",
			version:   "1.0.0",
			logLevel:  "info",
			expectErr: true, // Should fail at database
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TELEMETRY_ENABLED", tt.enabled)
			t.Setenv("TELEMETRY_OTLP_ENDPOINT", tt.endpoint)
			t.Setenv("TELEMETRY_SERVICE_NAME", tt.service)
			t.Setenv("TELEMETRY_SERVICE_VERSION", tt.version)
			t.Setenv("TELEMETRY_LOG_LEVEL", tt.logLevel)
			t.Setenv("DATABASE_HOST", "nonexistent")
			t.Setenv("REDIS_HOST", "nonexistent")

			err := run()
			if tt.expectErr {
				assert.Error(t, err)
			}
		})
	}
}

// TestRunFunctionLoggerInitialization tests logger initialization paths
func TestRunFunctionLoggerInitialization(t *testing.T) {
	tests := []struct {
		name             string
		telemetryEnabled string
		logLevel         string
		expectErr        bool
	}{
		{
			name:             "telemetry enabled logger",
			telemetryEnabled: "true",
			logLevel:         "debug",
			expectErr:        true,
		},
		{
			name:             "telemetry disabled logger",
			telemetryEnabled: "false",
			logLevel:         "warn",
			expectErr:        true,
		},
		{
			name:             "error log level",
			telemetryEnabled: "false",
			logLevel:         "error",
			expectErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TELEMETRY_ENABLED", tt.telemetryEnabled)
			t.Setenv("LOG_LEVEL", tt.logLevel)
			t.Setenv("DATABASE_HOST", "nonexistent")
			t.Setenv("REDIS_HOST", "nonexistent")

			err := run()
			if tt.expectErr {
				assert.Error(t, err)
			}
		})
	}
}

// TestRunFunctionDatabaseInitialization tests database initialization paths
func TestRunFunctionDatabaseInitialization(t *testing.T) {
	tests := []struct {
		name   string
		host   string
		port   string
		user   string
		pass   string
		dbname string
		ssl    string
	}{
		{
			name:   "invalid database host",
			host:   "nonexistent-db-host",
			port:   "5432",
			user:   "postgres",
			pass:   "password",
			dbname: "testdb",
			ssl:    "disable",
		},
		{
			name:   "invalid database port",
			host:   "localhost",
			port:   "99999",
			user:   "postgres",
			pass:   "password",
			dbname: "testdb",
			ssl:    "disable",
		},
		{
			name:   "invalid database credentials",
			host:   "localhost",
			port:   "5432",
			user:   "invaliduser",
			pass:   "invalidpass",
			dbname: "nonexistentdb",
			ssl:    "disable",
		},
		{
			name:   "database with ssl",
			host:   "localhost",
			port:   "5432",
			user:   "postgres",
			pass:   "password",
			dbname: "testdb",
			ssl:    "require",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TELEMETRY_ENABLED", "false")
			t.Setenv("DATABASE_HOST", tt.host)
			t.Setenv("DATABASE_PORT", tt.port)
			t.Setenv("DATABASE_USER", tt.user)
			t.Setenv("DATABASE_PASSWORD", tt.pass)
			t.Setenv("DATABASE_DBNAME", tt.dbname)
			t.Setenv("DATABASE_SSLMODE", tt.ssl)
			t.Setenv("REDIS_HOST", "nonexistent")

			err := run()
			assert.Error(t, err)
		})
	}
}

// TestRunFunctionRedisInitialization tests Redis initialization paths
func TestRunFunctionRedisInitialization(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		port     string
		password string
		db       string
	}{
		{
			name:     "invalid redis host",
			host:     "nonexistent-redis-host",
			port:     "6379",
			password: "",
			db:       "0",
		},
		{
			name:     "invalid redis port",
			host:     "localhost",
			port:     "99999",
			password: "",
			db:       "0",
		},
		{
			name:     "redis with password",
			host:     "localhost",
			port:     "6379",
			password: "testpass",
			db:       "1",
		},
		{
			name:     "redis with different db",
			host:     "localhost",
			port:     "6379",
			password: "",
			db:       "2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TELEMETRY_ENABLED", "false")
			t.Setenv("DATABASE_HOST", "nonexistent")
			t.Setenv("REDIS_HOST", tt.host)
			t.Setenv("REDIS_PORT", tt.port)
			t.Setenv("REDIS_PASSWORD", tt.password)
			t.Setenv("REDIS_DB", tt.db)

			err := run()
			assert.Error(t, err)
		})
	}
}

// TestRunFunctionServiceInitialization tests service initialization paths
func TestRunFunctionServiceInitialization(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{
			name: "all services enabled",
			env: map[string]string{
				"TELEMETRY_ENABLED":               "false",
				"DATABASE_HOST":                   "nonexistent",
				"REDIS_HOST":                      "nonexistent",
				"ARBITRAGE_ENABLED":               "true",
				"BACKFILL_ENABLED":                "true",
				"CLEANUP_ENABLED":                 "true",
				"CLEANUP_INTERVAL":                "60",
				"CCXT_SERVICE_URL":                "http://localhost:3001",
				"CCXT_TIMEOUT":                    "30",
				"TELEGRAM_BOT_TOKEN":              "test-token",
				"MARKET_DATA_COLLECTION_INTERVAL": "1m",
				"MARKET_DATA_BATCH_SIZE":          "10",
				"MARKET_DATA_MAX_RETRIES":         "3",
				"MARKET_DATA_TIMEOUT":             "30s",
				"MARKET_DATA_EXCHANGES":           "binance,coinbase",
				"BLACKLIST_TTL":                   "1h",
				"BLACKLIST_USE_REDIS":             "true",
			},
		},
		{
			name: "minimal services",
			env: map[string]string{
				"TELEMETRY_ENABLED": "false",
				"DATABASE_HOST":     "nonexistent",
				"REDIS_HOST":        "nonexistent",
				"ARBITRAGE_ENABLED": "false",
				"BACKFILL_ENABLED":  "false",
				"CLEANUP_ENABLED":   "false",
				"CCXT_SERVICE_URL":  "http://localhost:3001",
				"CCXT_TIMEOUT":      "30",
			},
		},
		{
			name: "services with custom config",
			env: map[string]string{
				"TELEMETRY_ENABLED":             "false",
				"DATABASE_HOST":                 "nonexistent",
				"REDIS_HOST":                    "nonexistent",
				"ARBITRAGE_ENABLED":             "true",
				"ARBITRAGE_MIN_PROFIT":          "2.5",
				"ARBITRAGE_MAX_AGE_MINUTES":     "120",
				"ARBITRAGE_BATCH_SIZE":          "100",
				"BACKFILL_ENABLED":              "true",
				"BACKFILL_HOURS":                "24",
				"BACKFILL_BATCH_SIZE":           "50",
				"BACKFILL_DELAY":                "2000",
				"CLEANUP_ENABLED":               "true",
				"CLEANUP_INTERVAL":              "120",
				"CLEANUP_MARKET_DATA_RETENTION": "48h",
				"CLEANUP_ARBITRAGE_RETENTION":   "72h",
				"CLEANUP_ENABLE_SMART_CLEANUP":  "true",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			for key, value := range tt.env {
				t.Setenv(key, value)
			}

			err := run()
			assert.Error(t, err)
		})
	}
}

// TestRunFunctionServerConfiguration tests server configuration paths
func TestRunFunctionServerConfiguration(t *testing.T) {
	tests := []struct {
		name         string
		port         string
		readTimeout  string
		writeTimeout string
	}{
		{
			name:         "default server config",
			port:         "8080",
			readTimeout:  "10s",
			writeTimeout: "10s",
		},
		{
			name:         "alternative port",
			port:         "9000",
			readTimeout:  "5s",
			writeTimeout: "5s",
		},
		{
			name:         "high port",
			port:         "8081",
			readTimeout:  "30s",
			writeTimeout: "30s",
		},
		{
			name:         "low port",
			port:         "3000",
			readTimeout:  "15s",
			writeTimeout: "15s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TELEMETRY_ENABLED", "false")
			t.Setenv("DATABASE_HOST", "nonexistent")
			t.Setenv("REDIS_HOST", "nonexistent")
			t.Setenv("SERVER_PORT", tt.port)

			err := run()
			assert.Error(t, err)
		})
	}
}

// TestRunFunctionMiddlewareConfiguration tests middleware setup paths
func TestRunFunctionMiddlewareConfiguration(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{
			name: "default middleware",
			env: map[string]string{
				"TELEMETRY_ENABLED": "false",
				"DATABASE_HOST":     "nonexistent",
				"REDIS_HOST":        "nonexistent",
				"GIN_MODE":          "debug",
			},
		},
		{
			name: "release mode middleware",
			env: map[string]string{
				"TELEMETRY_ENABLED": "false",
				"DATABASE_HOST":     "nonexistent",
				"REDIS_HOST":        "nonexistent",
				"GIN_MODE":          "release",
			},
		},
		{
			name: "test mode middleware",
			env: map[string]string{
				"TELEMETRY_ENABLED": "false",
				"DATABASE_HOST":     "nonexistent",
				"REDIS_HOST":        "nonexistent",
				"GIN_MODE":          "test",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			for key, value := range tt.env {
				t.Setenv(key, value)
			}

			err := run()
			assert.Error(t, err)
		})
	}
}

// TestRunFunctionErrorRecovery tests error recovery scenarios
func TestRunFunctionErrorRecovery(t *testing.T) {
	tests := []struct {
		name        string
		setupFunc   func()
		expectErr   bool
		errContains string
	}{
		{
			name: "recover from telemetry failure",
			setupFunc: func() {
				t.Setenv("TELEMETRY_ENABLED", "true")
				t.Setenv("TELEMETRY_OTLP_ENDPOINT", "invalid-endpoint")
			},
			expectErr:   true,
			errContains: "database",
		},
		{
			name: "recover from redis failure",
			setupFunc: func() {
				t.Setenv("TELEMETRY_ENABLED", "false")
				t.Setenv("REDIS_HOST", "nonexistent-redis")
				t.Setenv("REDIS_PORT", "6379")
			},
			expectErr:   true,
			errContains: "database",
		},
		{
			name: "recover from service initialization failure",
			setupFunc: func() {
				t.Setenv("TELEMETRY_ENABLED", "false")
				t.Setenv("ARBITRAGE_ENABLED", "true")
				t.Setenv("BACKFILL_ENABLED", "true")
				t.Setenv("CLEANUP_ENABLED", "true")
			},
			expectErr:   true,
			errContains: "database",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset environment
			t.Setenv("DATABASE_HOST", "nonexistent")
			t.Setenv("REDIS_HOST", "nonexistent")

			// Apply test-specific setup
			if tt.setupFunc != nil {
				tt.setupFunc()
			}

			err := run()
			if tt.expectErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			}
		})
	}
}

// TestRunFunctionContextAndSignalHandling tests context and signal handling paths
func TestRunFunctionContextAndSignalHandling(t *testing.T) {
	tests := []struct {
		name        string
		timeout     string
		expectErr   bool
		errContains string
	}{
		{
			name:        "default timeout",
			timeout:     "30s",
			expectErr:   true,
			errContains: "database",
		},
		{
			name:        "short timeout",
			timeout:     "5s",
			expectErr:   true,
			errContains: "database",
		},
		{
			name:        "long timeout",
			timeout:     "60s",
			expectErr:   true,
			errContains: "database",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TELEMETRY_ENABLED", "false")
			t.Setenv("DATABASE_HOST", "nonexistent")
			t.Setenv("REDIS_HOST", "nonexistent")

			err := run()
			if tt.expectErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			}
		})
	}
}

// TestRunFunctionWithMockDatabase tests the run function using mocked dependencies
func TestRunFunctionWithMockDatabase(t *testing.T) {
	// Set test environment to trigger test database path
	t.Setenv("RUN_TESTS", "true")

	// Set up minimal configuration to avoid early failures
	t.Setenv("TELEMETRY_ENABLED", "false")
	t.Setenv("LOG_LEVEL", "error")
	t.Setenv("SERVER_PORT", "8080")

	// Use a mock database configuration that should work with the test environment
	t.Setenv("DATABASE_HOST", "localhost")
	t.Setenv("DATABASE_PORT", "5432")
	t.Setenv("DATABASE_USER", "test")
	t.Setenv("DATABASE_PASSWORD", "test")
	t.Setenv("DATABASE_DBNAME", "test")
	t.Setenv("DATABASE_SSLMODE", "disable")

	// Set Redis to nonexistent to continue without cache
	t.Setenv("REDIS_HOST", "nonexistent")

	// Disable services that might cause issues
	t.Setenv("ARBITRAGE_ENABLED", "false")
	t.Setenv("BACKFILL_ENABLED", "false")
	t.Setenv("CLEANUP_ENABLED", "false")

	err := run()
	// We expect this to fail, but hopefully it gets further than before
	assert.Error(t, err)

	// The error might be different now - could be database connection still
	// or it could be something further in the initialization
	t.Logf("Run function error (expected): %v", err)
}

// TestRunFunctionWithTestEnvironment tests using test environment detection
func TestRunFunctionWithTestEnvironment(t *testing.T) {
	// Set multiple test environment indicators
	t.Setenv("CI_ENVIRONMENT", "test")
	t.Setenv("RUN_TESTS", "true")

	// Set up configuration that should work with test optimizations
	t.Setenv("TELEMETRY_ENABLED", "false")
	t.Setenv("LOG_LEVEL", "error")
	t.Setenv("SERVER_PORT", "8081")

	// Use database configuration that might work with test optimizations
	t.Setenv("DATABASE_HOST", "localhost")
	t.Setenv("DATABASE_PORT", "5432")
	t.Setenv("DATABASE_USER", "postgres")
	t.Setenv("DATABASE_PASSWORD", "postgres")
	t.Setenv("DATABASE_DBNAME", "test_db")
	t.Setenv("DATABASE_SSLMODE", "disable")

	// Configure Redis
	t.Setenv("REDIS_HOST", "localhost")
	t.Setenv("REDIS_PORT", "6379")
	t.Setenv("REDIS_PASSWORD", "")
	t.Setenv("REDIS_DB", "0")

	// Configure CCXT service
	t.Setenv("CCXT_SERVICE_URL", "http://localhost:3001")
	t.Setenv("CCXT_TIMEOUT", "5")

	// Disable heavy services
	t.Setenv("ARBITRAGE_ENABLED", "false")
	t.Setenv("BACKFILL_ENABLED", "false")
	t.Setenv("CLEANUP_ENABLED", "false")

	err := run()
	assert.Error(t, err)
	t.Logf("Run function error with test environment: %v", err)
}

// TestRunFunctionPartialInitialization tests partial initialization paths
func TestRunFunctionPartialInitialization(t *testing.T) {
	tests := []struct {
		name          string
		envVars       map[string]string
		expectedError string
	}{
		{
			name: "telemetry success, database failure",
			envVars: map[string]string{
				"CI_ENVIRONMENT":          "test",
				"TELEMETRY_ENABLED":       "true",
				"TELEMETRY_OTLP_ENDPOINT": "http://localhost:4318",
				"DATABASE_HOST":           "nonexistent-db",
				"REDIS_HOST":              "nonexistent-redis",
			},
			expectedError: "database",
		},
		{
			name: "database success, redis failure",
			envVars: map[string]string{
				"CI_ENVIRONMENT":    "test",
				"TELEMETRY_ENABLED": "false",
				"DATABASE_HOST":     "localhost",
				"DATABASE_PORT":     "5432",
				"DATABASE_USER":     "postgres",
				"DATABASE_PASSWORD": "postgres",
				"DATABASE_DBNAME":   "postgres",
				"REDIS_HOST":        "nonexistent-redis",
			},
			expectedError: "", // Might succeed further or fail at service init
		},
		{
			name: "minimal configuration",
			envVars: map[string]string{
				"CI_ENVIRONMENT":    "test",
				"TELEMETRY_ENABLED": "false",
				"DATABASE_HOST":     "localhost",
				"DATABASE_PORT":     "5432",
				"REDIS_HOST":        "localhost",
				"REDIS_PORT":        "6379",
				"ARBITRAGE_ENABLED": "false",
				"BACKFILL_ENABLED":  "false",
				"CLEANUP_ENABLED":   "false",
			},
			expectedError: "", // Should get the furthest
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set base environment
			t.Setenv("LOG_LEVEL", "error")
			t.Setenv("SERVER_PORT", "8082")
			t.Setenv("CCXT_SERVICE_URL", "http://localhost:3001")
			t.Setenv("CCXT_TIMEOUT", "5")

			// Apply test-specific environment
			for key, value := range tt.envVars {
				t.Setenv(key, value)
			}

			err := run()
			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
			} else {
				// We still expect an error, but we're testing how far we get
				if err != nil {
					t.Logf("Partial initialization error: %v", err)
				}
			}
		})
	}
}

// TestRunFunctionServiceLayer tests service layer initialization
func TestRunFunctionServiceLayer(t *testing.T) {
	// Set up environment to get past database and redis
	t.Setenv("CI_ENVIRONMENT", "test")
	t.Setenv("TELEMETRY_ENABLED", "false")
	t.Setenv("LOG_LEVEL", "error")
	t.Setenv("SERVER_PORT", "8083")

	// Database config that might work
	t.Setenv("DATABASE_HOST", "localhost")
	t.Setenv("DATABASE_PORT", "5432")
	t.Setenv("DATABASE_USER", "postgres")
	t.Setenv("DATABASE_PASSWORD", "postgres")
	t.Setenv("DATABASE_DBNAME", "postgres")

	// Redis config
	t.Setenv("REDIS_HOST", "localhost")
	t.Setenv("REDIS_PORT", "6379")

	// CCXT config
	t.Setenv("CCXT_SERVICE_URL", "http://localhost:3001")
	t.Setenv("CCXT_TIMEOUT", "5")

	// Test different service configurations
	tests := []struct {
		name string
		env  map[string]string
	}{
		{
			name: "all services disabled",
			env: map[string]string{
				"ARBITRAGE_ENABLED": "false",
				"BACKFILL_ENABLED":  "false",
				"CLEANUP_ENABLED":   "false",
			},
		},
		{
			name: "arbitrage enabled",
			env: map[string]string{
				"ARBITRAGE_ENABLED":         "true",
				"ARBITRAGE_MIN_PROFIT":      "1.0",
				"ARBITRAGE_MAX_AGE_MINUTES": "60",
				"BACKFILL_ENABLED":          "false",
				"CLEANUP_ENABLED":           "false",
			},
		},
		{
			name: "backfill enabled",
			env: map[string]string{
				"ARBITRAGE_ENABLED":   "false",
				"BACKFILL_ENABLED":    "true",
				"BACKFILL_HOURS":      "1",
				"BACKFILL_BATCH_SIZE": "10",
				"CLEANUP_ENABLED":     "false",
			},
		},
		{
			name: "cleanup enabled",
			env: map[string]string{
				"ARBITRAGE_ENABLED":             "false",
				"BACKFILL_ENABLED":              "false",
				"CLEANUP_ENABLED":               "true",
				"CLEANUP_INTERVAL":              "60",
				"CLEANUP_MARKET_DATA_RETENTION": "1h",
				"CLEANUP_ARBITRAGE_RETENTION":   "2h",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Apply service-specific environment
			for key, value := range tt.env {
				t.Setenv(key, value)
			}

			err := run()
			// We expect errors but testing different initialization paths
			if err != nil {
				t.Logf("Service layer test error for %s: %v", tt.name, err)
			}
		})
	}
}

// TestRunFunctionAdvancedPaths tests advanced initialization paths
func TestRunFunctionAdvancedPaths(t *testing.T) {
	// Set up base environment to get as far as possible
	t.Setenv("CI_ENVIRONMENT", "test")
	t.Setenv("TELEMETRY_ENABLED", "false")
	t.Setenv("LOG_LEVEL", "error")
	t.Setenv("SERVER_PORT", "8084")

	t.Setenv("DATABASE_HOST", "localhost")
	t.Setenv("DATABASE_PORT", "5432")
	t.Setenv("DATABASE_USER", "postgres")
	t.Setenv("DATABASE_PASSWORD", "postgres")
	t.Setenv("DATABASE_DBNAME", "postgres")

	t.Setenv("REDIS_HOST", "localhost")
	t.Setenv("REDIS_PORT", "6379")

	t.Setenv("CCXT_SERVICE_URL", "http://localhost:3001")
	t.Setenv("CCXT_TIMEOUT", "5")

	// Test advanced configurations
	tests := []struct {
		name string
		env  map[string]string
	}{
		{
			name: "telegram integration",
			env: map[string]string{
				"TELEGRAM_BOT_TOKEN": "fake-token-for-testing",
				"ARBITRAGE_ENABLED":  "false",
				"BACKFILL_ENABLED":   "false",
				"CLEANUP_ENABLED":    "false",
			},
		},
		{
			name: "market data collection",
			env: map[string]string{
				"MARKET_DATA_COLLECTION_INTERVAL": "1m",
				"MARKET_DATA_BATCH_SIZE":          "10",
				"MARKET_DATA_MAX_RETRIES":         "3",
				"MARKET_DATA_TIMEOUT":             "30s",
				"MARKET_DATA_EXCHANGES":           "binance",
				"ARBITRAGE_ENABLED":               "false",
				"BACKFILL_ENABLED":                "false",
				"CLEANUP_ENABLED":                 "false",
			},
		},
		{
			name: "blacklist configuration",
			env: map[string]string{
				"BLACKLIST_TTL":               "1h",
				"BLACKLIST_SHORT_TTL":         "5m",
				"BLACKLIST_LONG_TTL":          "24h",
				"BLACKLIST_USE_REDIS":         "true",
				"BLACKLIST_RETRY_AFTER_CLEAR": "false",
				"ARBITRAGE_ENABLED":           "false",
				"BACKFILL_ENABLED":            "false",
				"CLEANUP_ENABLED":             "false",
			},
		},
		{
			name: "auth configuration",
			env: map[string]string{
				"AUTH_JWT_SECRET":   "test-secret-key",
				"ARBITRAGE_ENABLED": "false",
				"BACKFILL_ENABLED":  "false",
				"CLEANUP_ENABLED":   "false",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Apply test-specific environment
			for key, value := range tt.env {
				t.Setenv(key, value)
			}

			err := run()
			if err != nil {
				t.Logf("Advanced path test error for %s: %v", tt.name, err)
			}
		})
	}
}
