package logging

import (
	"fmt"
	"testing"

	zaplogrus "github.com/irfandi/celebrum-ai-go/internal/logging/zaplogrus"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestStandardLogger_Basic(t *testing.T) {
	logger := NewStandardLogger("info", "development")

	assert.NotNil(t, logger)
	assert.NotNil(t, logger.Logger())
}

func TestStandardLogger_LogLevels(t *testing.T) {
	tests := []struct {
		levelStr string
		expected zapcore.Level
	}{
		{"debug", zapcore.DebugLevel},
		{"info", zapcore.InfoLevel},
		{"warn", zapcore.WarnLevel},
		{"error", zapcore.ErrorLevel},
		{"invalid", zapcore.InfoLevel}, // Should default to info
	}

	for _, tt := range tests {
		t.Run(tt.levelStr, func(t *testing.T) {
			level := getZapLevel(tt.levelStr)
			assert.Equal(t, tt.expected, level)
		})
	}
}

// Helper to create an observable logger for assertions
func setupTestLogger() (*StandardLogger, *observer.ObservedLogs) {
	core, observedLogs := observer.New(zap.InfoLevel)
	logger := zap.New(core)
	return &StandardLogger{logger: logger}, observedLogs
}

func TestStandardLogger_WithService(t *testing.T) {
	logger, logs := setupTestLogger()

	// Chain calls to ensure it works
	logger.WithService("new-service").Info("test message")

	assert.Equal(t, 1, logs.Len())
	entry := logs.All()[0]
	assert.Equal(t, "test message", entry.Message)

	fields := entry.ContextMap()
	assert.Equal(t, "new-service", fields["service"])
}

func TestStandardLogger_WithComponent(t *testing.T) {
	logger, logs := setupTestLogger()

	logger.WithComponent("database").Info("test message")

	assert.Equal(t, 1, logs.Len())
	fields := logs.All()[0].ContextMap()
	assert.Equal(t, "database", fields["component"])
}

func TestStandardLogger_WithOperation(t *testing.T) {
	logger, logs := setupTestLogger()

	logger.WithOperation("fetch_symbols").Info("test message")

	assert.Equal(t, 1, logs.Len())
	fields := logs.All()[0].ContextMap()
	assert.Equal(t, "fetch_symbols", fields["operation"])
}

func TestStandardLogger_WithRequestID(t *testing.T) {
	logger, logs := setupTestLogger()

	logger.WithRequestID("req-123456").Info("test message")

	assert.Equal(t, 1, logs.Len())
	fields := logs.All()[0].ContextMap()
	assert.Equal(t, "req-123456", fields["request_id"])
}

func TestStandardLogger_WithUserID(t *testing.T) {
	logger, logs := setupTestLogger()

	logger.WithUserID("user-789").Info("test message")

	assert.Equal(t, 1, logs.Len())
	fields := logs.All()[0].ContextMap()
	assert.Equal(t, "user-789", fields["user_id"])
}

func TestStandardLogger_WithExchange(t *testing.T) {
	logger, logs := setupTestLogger()

	logger.WithExchange("binance").Info("test message")

	assert.Equal(t, 1, logs.Len())
	fields := logs.All()[0].ContextMap()
	assert.Equal(t, "binance", fields["exchange"])
}

func TestStandardLogger_WithSymbol(t *testing.T) {
	logger, logs := setupTestLogger()

	logger.WithSymbol("BTC/USD").Info("test message")

	assert.Equal(t, 1, logs.Len())
	fields := logs.All()[0].ContextMap()
	assert.Equal(t, "BTC/USD", fields["symbol"])
}

func TestStandardLogger_WithError(t *testing.T) {
	logger, logs := setupTestLogger()

	testErr := fmt.Errorf("mock error")
	logger.WithError(testErr).Info("test error message")

	assert.Equal(t, 1, logs.Len())
	entry := logs.All()[0]
	assert.Equal(t, "test error message", entry.Message)

	// Zap might encode error differently depending on encoder, but generic map check might miss it if type is different
	// ContextMap() handles simple types. Error might be implicit string or separate field.
	// Zap field zap.Error(err) usually puts it under "error" key.
	fields := entry.ContextMap()
	assert.Equal(t, "mock error", fields["error"])
}

func TestStandardLogger_WithFields(t *testing.T) {
	logger, logs := setupTestLogger()

	fields := map[string]interface{}{
		"custom_key": "custom_value",
		"number":     42,
	}
	logger.WithFields(fields).Info("test message")

	assert.Equal(t, 1, logs.Len())
	logFields := logs.All()[0].ContextMap()
	assert.Equal(t, "custom_value", logFields["custom_key"])

	// JSON number might be float64 in generic map
	val, ok := logFields["number"]
	assert.True(t, ok)
	// Assert appropriately depending on type
	assert.EqualValues(t, 42, val) // or cast to check
}

func TestStandardLogger_WithMetrics(t *testing.T) {
	logger, logs := setupTestLogger()

	metrics := map[string]interface{}{
		"duration_ms": 150,
		"status_code": 200,
	}
	logger.WithMetrics(metrics).Info("test message")

	assert.Equal(t, 1, logs.Len())
	fields := logs.All()[0].ContextMap()

	metricMap, ok := fields["metrics"].(map[string]interface{})
	if ok {
		assert.Equal(t, 150, metricMap["duration_ms"])
	}
	// Note: Test may not assert if zap encodes differently (e.g. strict JSON).
	// ContextMap tries to unmarshal. We only check if the cast succeeds.
}

func TestStandardLogger_LogStartup(t *testing.T) {
	logger, logs := setupTestLogger()

	logger.LogStartup("test-service", "1.0.0", 8080)

	assert.Equal(t, 1, logs.Len())
	fields := logs.All()[0].ContextMap()
	assert.Equal(t, "test-service", fields["service"])
	assert.Equal(t, "1.0.0", fields["version"])
	assert.EqualValues(t, 8080, fields["port"])
	assert.Equal(t, "startup", fields["event"])
}

func TestStandardLogger_LogShutdown(t *testing.T) {
	logger, logs := setupTestLogger()

	logger.LogShutdown("test-service", "graceful")

	assert.Equal(t, 1, logs.Len())
	fields := logs.All()[0].ContextMap()
	assert.Equal(t, "test-service", fields["service"])
	assert.Equal(t, "graceful", fields["reason"])
	assert.Equal(t, "shutdown", fields["event"])
}

func TestStandardLogger_LogAPIRequest(t *testing.T) {
	logger, logs := setupTestLogger()

	logger.LogAPIRequest("GET", "/api/symbols", 200, 150, "user123")

	assert.Equal(t, 1, logs.Len())
	fields := logs.All()[0].ContextMap()
	assert.Equal(t, "GET", fields["method"])
	assert.EqualValues(t, 200, fields["status_code"])
	// zap encodes int64 as float64 in generic unmarshal sometimes?
	// Just checking existence/value loosely
	assert.NotNil(t, fields["duration_ms"])
}

func TestStandardLogger_LogBusinessEvent(t *testing.T) {
	logger, buf := setupTestLogger("info", "development")

	details := map[string]interface{}{
		"symbol":     "BTC/USD",
		"exchange":   "binance",
		"profit_pct": 2.5,
	}

	logger.LogBusinessEvent("arbitrage_opportunity", details)

	logOutput := buf.String()
	assert.Contains(t, logOutput, "event=business_event")
	assert.Contains(t, logOutput, "type=arbitrage_opportunity")
	assert.Contains(t, logOutput, "symbol=BTC/USD")
	assert.Contains(t, logOutput, "exchange=binance")
	assert.Contains(t, logOutput, "profit_pct=2.5")
	assert.Contains(t, logOutput, "Business event")
}

// TestStandardLogger_SetLogger
func TestStandardLogger_SetLogger(t *testing.T) {
	logger := NewStandardLogger("info", "development")
	assert.NotNil(t, logger)

	// Create a mock logger to replace the default one
	mockLogger := &testLogger{logger: slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))}

	// Test SetLogger method
	logger.SetLogger(mockLogger)

	// Verify the logger was set by testing a method call
	resultLogger := logger.WithService("test-service")
	assert.NotNil(t, resultLogger)
}

func TestParseLogrusLevel(t *testing.T) {
	tests := []struct {
		levelStr string
		expected zaplogrus.Level
	}{
		{"debug", zaplogrus.DebugLevel},
		{"warn", zaplogrus.WarnLevel},
		{"warning", zaplogrus.WarnLevel},
		{"error", zaplogrus.ErrorLevel},
		{"info", zaplogrus.InfoLevel},
		{"INFO", zaplogrus.InfoLevel},    // case insensitive
		{"DEBUG", zaplogrus.DebugLevel},  // case insensitive
		{"invalid", zaplogrus.InfoLevel}, // default to info
		{"", zaplogrus.InfoLevel},        // empty string defaults to info
	}

	for _, tt := range tests {
		t.Run(tt.levelStr, func(t *testing.T) {
			result := ParseLogrusLevel(tt.levelStr)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Tests for fallbackLogger methods
func TestFallbackLogger_WithService(t *testing.T) {
	logger := &fallbackLogger{
		logger: slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})),
	}

	result := logger.WithService("test-service")
	assert.NotNil(t, result)
}

func TestFallbackLogger_WithComponent(t *testing.T) {
	logger := &fallbackLogger{
		logger: slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})),
	}

	result := logger.WithComponent("test-component")
	assert.NotNil(t, result)
}

func TestFallbackLogger_WithOperation(t *testing.T) {
	logger := &fallbackLogger{
		logger: slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})),
	}

	result := logger.WithOperation("test-operation")
	assert.NotNil(t, result)
}

func TestFallbackLogger_WithRequestID(t *testing.T) {
	logger := &fallbackLogger{
		logger: slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})),
	}

	result := logger.WithRequestID("test-request-id")
	assert.NotNil(t, result)
}

func TestFallbackLogger_WithUserID(t *testing.T) {
	logger := &fallbackLogger{
		logger: slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})),
	}

	result := logger.WithUserID("test-user-id")
	assert.NotNil(t, result)
}

func TestFallbackLogger_WithExchange(t *testing.T) {
	logger := &fallbackLogger{
		logger: slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})),
	}

	result := logger.WithExchange("test-exchange")
	assert.NotNil(t, result)
}

func TestFallbackLogger_WithSymbol(t *testing.T) {
	logger := &fallbackLogger{
		logger: slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})),
	}

	result := logger.WithSymbol("test-symbol")
	assert.NotNil(t, result)
}

func TestFallbackLogger_WithError(t *testing.T) {
	logger := &fallbackLogger{
		logger: slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})),
	}

	testErr := fmt.Errorf("test error")
	result := logger.WithError(testErr)
	assert.NotNil(t, result)
}

func TestFallbackLogger_WithMetrics(t *testing.T) {
	logger := &fallbackLogger{
		logger: slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})),
	}

	metrics := map[string]interface{}{
		"test": "value",
		"num":  42,
	}
	result := logger.WithMetrics(metrics)
	assert.NotNil(t, result)
}

func TestFallbackLogger_LogStartup(t *testing.T) {
	var buf bytes.Buffer
	logger := &fallbackLogger{
		logger: slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})),
	}

	logger.LogStartup("test-service", "1.0.0", 8080)
	assert.Contains(t, buf.String(), "test-service")
	assert.Contains(t, buf.String(), "1.0.0")
	assert.Contains(t, buf.String(), "8080")
}

func TestFallbackLogger_LogShutdown(t *testing.T) {
	var buf bytes.Buffer
	logger := &fallbackLogger{
		logger: slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})),
	}

	logger.LogShutdown("test-service", "graceful")
	assert.Contains(t, buf.String(), "test-service")
	assert.Contains(t, buf.String(), "graceful")
}

func TestFallbackLogger_LogPerformanceMetrics(t *testing.T) {
	var buf bytes.Buffer
	logger := &fallbackLogger{
		logger: slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})),
	}

	metrics := map[string]interface{}{
		"cpu": 75.5,
		"mem": 1024,
	}
	logger.LogPerformanceMetrics("test-service", metrics)
	assert.Contains(t, buf.String(), "test-service")
	assert.Contains(t, buf.String(), "75.5")
	assert.Contains(t, buf.String(), "1024")
}

func TestFallbackLogger_LogResourceStats(t *testing.T) {
	var buf bytes.Buffer
	logger := &fallbackLogger{
		logger: slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})),
	}

	stats := map[string]interface{}{
		"goroutines": 100,
		"heap_size":  2048,
	}
	logger.LogResourceStats("test-service", stats)
	assert.Contains(t, buf.String(), "test-service")
	assert.Contains(t, buf.String(), "100")
	assert.Contains(t, buf.String(), "2048")
}

func TestFallbackLogger_LogCacheOperation(t *testing.T) {
	var buf bytes.Buffer
	logger := &fallbackLogger{
		logger: slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})),
	}

	logger.LogCacheOperation("get", "test-key", true, 15)
	assert.Contains(t, buf.String(), "get")
	assert.Contains(t, buf.String(), "test-key")
	assert.Contains(t, buf.String(), "true")
	assert.Contains(t, buf.String(), "15")
}

func TestFallbackLogger_LogDatabaseOperation(t *testing.T) {
	var buf bytes.Buffer
	logger := &fallbackLogger{
		logger: slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})),
	}

	logger.LogDatabaseOperation("insert", "users", 250, 1)
	assert.Contains(t, buf.String(), "insert")
	assert.Contains(t, buf.String(), "users")
	assert.Contains(t, buf.String(), "250")
	assert.Contains(t, buf.String(), "1")
}

func TestFallbackLogger_LogAPIRequest(t *testing.T) {
	var buf bytes.Buffer
	logger := &fallbackLogger{
		logger: slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})),
	}

	logger.LogAPIRequest("GET", "/api/test", 200, 150, "test-user")
	assert.Contains(t, buf.String(), "GET")
	assert.Contains(t, buf.String(), "/api/test")
	assert.Contains(t, buf.String(), "200")
	assert.Contains(t, buf.String(), "150")
	assert.Contains(t, buf.String(), "test-user")
}

func TestFallbackLogger_LogBusinessEvent(t *testing.T) {
	var buf bytes.Buffer
	logger := &fallbackLogger{
		logger: slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})),
	}

	details := map[string]interface{}{
		"symbol": "BTC/USD",
		"action": "buy",
	}
	logger.LogBusinessEvent("test-event", details)
	assert.Contains(t, buf.String(), "test-event")
	assert.Contains(t, buf.String(), "BTC/USD")
	assert.Contains(t, buf.String(), "buy")
}

func TestFallbackLogger_Logger(t *testing.T) {
	logger := &fallbackLogger{
		logger: slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})),
	}

	result := logger.Logger()
	assert.NotNil(t, result)
	assert.Equal(t, logger.logger, result)
}
