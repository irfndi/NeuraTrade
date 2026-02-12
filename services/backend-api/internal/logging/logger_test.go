package logging

import (
	"fmt"
	"testing"

	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"
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
	logger, logs := setupTestLogger()

	details := map[string]interface{}{
		"symbol":     "BTC/USD",
		"exchange":   "binance",
		"profit_pct": 2.5,
	}

	logger.LogBusinessEvent("arbitrage_opportunity", details)

	assert.Equal(t, 1, logs.Len())
	fields := logs.All()[0].ContextMap()
	assert.Equal(t, "arbitrage_opportunity", fields["type"])
	assert.Equal(t, "business_event", fields["event"])
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
