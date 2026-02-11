package logging

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"testing"

	zaplogrus "github.com/irfandi/celebrum-ai-go/internal/logging/zaplogrus"
	"github.com/stretchr/testify/assert"
)

// testLogger implements the Logger interface for testing
type testLogger struct {
	logger *slog.Logger
}

func (t *testLogger) WithService(serviceName string) *slog.Logger {
	return t.logger.With("service", serviceName)
}

func (t *testLogger) WithComponent(componentName string) *slog.Logger {
	return t.logger.With("component", componentName)
}

func (t *testLogger) WithOperation(operationName string) *slog.Logger {
	return t.logger.With("operation", operationName)
}

func (t *testLogger) WithRequestID(requestID string) *slog.Logger {
	return t.logger.With("request_id", requestID)
}

func (t *testLogger) WithUserID(userID string) *slog.Logger {
	return t.logger.With("user_id", userID)
}

func (t *testLogger) WithExchange(exchange string) *slog.Logger {
	return t.logger.With("exchange", exchange)
}

func (t *testLogger) WithSymbol(symbol string) *slog.Logger {
	return t.logger.With("symbol", symbol)
}

func (t *testLogger) WithError(err error) *slog.Logger {
	return t.logger.With("error", err)
}

func (t *testLogger) WithMetrics(metrics map[string]interface{}) *slog.Logger {
	attrs := make([]any, 0, len(metrics)*2)
	for k, v := range metrics {
		attrs = append(attrs, k, v)
	}
	return t.logger.With(attrs...)
}

func (t *testLogger) LogStartup(serviceName string, version string, port int) {
	t.logger.Info("Service starting",
		"service", serviceName,
		"version", version,
		"port", port,
		"event", "startup",
	)
}

func (t *testLogger) LogShutdown(serviceName string, reason string) {
	t.logger.Info("Service shutting down",
		"service", serviceName,
		"reason", reason,
		"event", "shutdown",
	)
}

func (t *testLogger) LogPerformanceMetrics(serviceName string, metrics map[string]interface{}) {
	attrs := make([]any, 0, len(metrics)*2+2)
	attrs = append(attrs, "service", serviceName, "event", "performance_metrics")
	for k, v := range metrics {
		attrs = append(attrs, k, v)
	}
	t.logger.Info("Performance metrics", attrs...)
}

func (t *testLogger) LogResourceStats(serviceName string, stats map[string]interface{}) {
	attrs := make([]any, 0, len(stats)*2+2)
	attrs = append(attrs, "service", serviceName, "event", "resource_stats")
	for k, v := range stats {
		attrs = append(attrs, k, v)
	}
	t.logger.Info("Resource statistics", attrs...)
}

func (t *testLogger) LogCacheOperation(operation string, key string, hit bool, duration int64) {
	t.logger.Info("Cache operation",
		"operation", operation,
		"key", key,
		"hit", hit,
		"duration_ms", duration,
		"event", "cache_operation",
	)
}

func (t *testLogger) LogDatabaseOperation(operation string, table string, duration int64, rowsAffected int64) {
	t.logger.Info("Database operation",
		"operation", operation,
		"table", table,
		"duration_ms", duration,
		"rows_affected", rowsAffected,
		"event", "database_operation",
	)
}

func (t *testLogger) LogAPIRequest(method string, path string, statusCode int, duration int64, userID string) {
	t.logger.Info("API request",
		"method", method,
		"path", path,
		"status_code", statusCode,
		"duration_ms", duration,
		"user_id", userID,
		"event", "api_request",
	)
}

func (t *testLogger) LogBusinessEvent(eventType string, details map[string]interface{}) {
	attrs := make([]any, 0, len(details)*2+4)
	attrs = append(attrs, "type", eventType, "event", "business_event")
	for k, v := range details {
		attrs = append(attrs, k, v)
	}
	t.logger.Info("Business event", attrs...)
}

func (t *testLogger) Logger() *slog.Logger {
	return t.logger
}

// setupTestLogger creates a logger for testing
func setupTestLogger(level, env string) (*StandardLogger, *bytes.Buffer) {
	var buf bytes.Buffer
	// Create a basic slog logger with buffer output
	// Use TextHandler for development environment to match expected key=value format
	var handler slog.Handler
	if env == "development" {
		handler = slog.NewTextHandler(&buf, &slog.HandlerOptions{
			Level: getSlogLevel(level),
		})
	} else {
		handler = slog.NewJSONHandler(&buf, &slog.HandlerOptions{
			Level: getSlogLevel(level),
		})
	}
	logger := slog.New(handler)

	return &StandardLogger{
		logger: &testLogger{logger: logger},
	}, &buf
}

func TestNewStandardLogger_Basic(t *testing.T) {
	logger := NewStandardLogger("info", "development")

	assert.NotNil(t, logger)
	assert.NotNil(t, logger.Logger())
}

func TestNewStandardLogger_LogLevels(t *testing.T) {
	tests := []struct {
		levelStr string
		expected slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
		{"invalid", slog.LevelInfo}, // Should default to info
	}

	for _, tt := range tests {
		t.Run(tt.levelStr, func(t *testing.T) {
			level := getSlogLevel(tt.levelStr)
			assert.Equal(t, tt.expected, level)
		})
	}
}

func TestStandardLogger_WithService(t *testing.T) {
	logger, buf := setupTestLogger("info", "development")

	logger.WithService("new-service").Info("test message")

	logOutput := buf.String()
	assert.Contains(t, logOutput, "service=new-service")
	assert.Contains(t, logOutput, "test message")
}

func TestStandardLogger_WithComponent(t *testing.T) {
	logger, buf := setupTestLogger("info", "development")

	logger.WithComponent("database").Info("test message")

	logOutput := buf.String()
	assert.Contains(t, logOutput, "component=database")
	assert.Contains(t, logOutput, "test message")
}

func TestStandardLogger_WithOperation(t *testing.T) {
	logger, buf := setupTestLogger("info", "development")

	logger.WithOperation("fetch_symbols").Info("test message")

	logOutput := buf.String()
	assert.Contains(t, logOutput, "operation=fetch_symbols")
	assert.Contains(t, logOutput, "test message")
}

func TestStandardLogger_WithRequestID(t *testing.T) {
	logger, buf := setupTestLogger("info", "development")

	requestID := "req-123456"
	logger.WithRequestID(requestID).Info("test message")

	logOutput := buf.String()
	assert.Contains(t, logOutput, "request_id=req-123456")
	assert.Contains(t, logOutput, "test message")
}

func TestStandardLogger_WithUserID(t *testing.T) {
	logger, buf := setupTestLogger("info", "development")

	userID := "user-789"
	logger.WithUserID(userID).Info("test message")

	logOutput := buf.String()
	assert.Contains(t, logOutput, "user_id=user-789")
	assert.Contains(t, logOutput, "test message")
}

func TestStandardLogger_WithExchange(t *testing.T) {
	logger, buf := setupTestLogger("info", "development")

	logger.WithExchange("binance").Info("test message")

	logOutput := buf.String()
	assert.Contains(t, logOutput, "exchange=binance")
	assert.Contains(t, logOutput, "test message")
}

func TestStandardLogger_WithSymbol(t *testing.T) {
	logger, buf := setupTestLogger("info", "development")

	logger.WithSymbol("BTC/USD").Info("test message")

	logOutput := buf.String()
	assert.Contains(t, logOutput, "symbol=BTC/USD")
	assert.Contains(t, logOutput, "test message")
}

func TestStandardLogger_WithError(t *testing.T) {
	logger, buf := setupTestLogger("info", "development")

	testErr := assert.AnError
	logger.WithError(testErr).Error("test error message")

	logOutput := buf.String()
	assert.Contains(t, logOutput, "error=")
	assert.Contains(t, logOutput, "test error message")
}

func TestStandardLogger_WithMetrics(t *testing.T) {
	logger, buf := setupTestLogger("info", "development")

	metrics := map[string]interface{}{
		"duration_ms": 150,
		"status_code": 200,
		"bytes_sent":  1024,
	}

	logger.WithMetrics(metrics).Info("test message")

	logOutput := buf.String()
	assert.Contains(t, logOutput, "duration_ms=150")
	assert.Contains(t, logOutput, "status_code=200")
	assert.Contains(t, logOutput, "bytes_sent=1024")
	assert.Contains(t, logOutput, "test message")
}

func TestStandardLogger_LogStartup(t *testing.T) {
	logger, buf := setupTestLogger("info", "development")

	logger.LogStartup("test-service", "1.0.0", 8080)

	logOutput := buf.String()
	assert.Contains(t, logOutput, "service=test-service")
	assert.Contains(t, logOutput, "version=1.0.0")
	assert.Contains(t, logOutput, "port=8080")
	assert.Contains(t, logOutput, "event=startup")
	assert.Contains(t, logOutput, "Service starting")
}

func TestStandardLogger_LogShutdown(t *testing.T) {
	logger, buf := setupTestLogger("info", "development")

	logger.LogShutdown("test-service", "graceful")

	logOutput := buf.String()
	assert.Contains(t, logOutput, "service=test-service")
	assert.Contains(t, logOutput, "reason=graceful")
	assert.Contains(t, logOutput, "event=shutdown")
	assert.Contains(t, logOutput, "Service shutting down")
}

func TestStandardLogger_LogPerformanceMetrics(t *testing.T) {
	logger, buf := setupTestLogger("debug", "development")

	metrics := map[string]interface{}{
		"cpu_usage":    75.5,
		"memory_usage": 1024,
	}

	logger.LogPerformanceMetrics("test-service", metrics)

	logOutput := buf.String()
	assert.Contains(t, logOutput, "service=test-service")
	assert.Contains(t, logOutput, "event=performance_metrics")
	assert.Contains(t, logOutput, "Performance metrics")
}

func TestStandardLogger_LogResourceStats(t *testing.T) {
	logger, buf := setupTestLogger("info", "development")

	stats := map[string]interface{}{
		"goroutines": 100,
		"heap_size":  2048,
	}

	logger.LogResourceStats("test-service", stats)

	logOutput := buf.String()
	assert.Contains(t, logOutput, "service=test-service")
	assert.Contains(t, logOutput, "event=resource_stats")
	assert.Contains(t, logOutput, "Resource statistics")
}

func TestStandardLogger_LogCacheOperation(t *testing.T) {
	logger, buf := setupTestLogger("debug", "development")

	logger.LogCacheOperation("get", "symbols:binance", true, 15)

	logOutput := buf.String()
	assert.Contains(t, logOutput, "event=cache_operation")
	assert.Contains(t, logOutput, "operation=get")
	assert.Contains(t, logOutput, "key=symbols:binance")
	assert.Contains(t, logOutput, "hit=true")
	assert.Contains(t, logOutput, "duration_ms=15")
	assert.Contains(t, logOutput, "Cache operation")
}

func TestStandardLogger_LogDatabaseOperation(t *testing.T) {
	logger, buf := setupTestLogger("debug", "development")

	logger.LogDatabaseOperation("insert", "users", 250, 1)

	logOutput := buf.String()
	assert.Contains(t, logOutput, "event=database_operation")
	assert.Contains(t, logOutput, "operation=insert")
	assert.Contains(t, logOutput, "table=users")
	assert.Contains(t, logOutput, "duration_ms=250")
	assert.Contains(t, logOutput, "rows_affected=1")
	assert.Contains(t, logOutput, "Database operation")
}

func TestStandardLogger_LogAPIRequest(t *testing.T) {
	logger, buf := setupTestLogger("info", "development")

	logger.LogAPIRequest("GET", "/api/symbols", 200, 150, "user123")

	logOutput := buf.String()
	assert.Contains(t, logOutput, "event=api_request")
	assert.Contains(t, logOutput, "method=GET")
	assert.Contains(t, logOutput, "path=/api/symbols")
	assert.Contains(t, logOutput, "status_code=200")
	assert.Contains(t, logOutput, "duration_ms=150")
	assert.Contains(t, logOutput, "user_id=user123")
	assert.Contains(t, logOutput, "API request")
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
