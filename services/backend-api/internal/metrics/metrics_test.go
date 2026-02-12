package metrics

import (
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/logging"
	"github.com/stretchr/testify/assert"
)

func TestNewMetricsCollector(t *testing.T) {
	logger := logging.NewStandardLogger("info", "development")
	collector := NewMetricsCollector(logger, "test-service")

	assert.NotNil(t, collector)
}

func TestMetricsCollector_RecordCounter(t *testing.T) {
	logger := logging.NewStandardLogger("debug", "development")
	collector := NewMetricsCollector(logger, "test-service")

	// Just test that it doesn't panic
	tags := map[string]string{"key": "value"}
	collector.RecordCounter("test_counter", 1.0, tags)

	// Test with nil tags
	collector.RecordCounter("test_counter_nil", 2.0, nil)
}

func TestMetricsCollector_RecordGauge(t *testing.T) {
	logger := logging.NewStandardLogger("debug", "development")
	collector := NewMetricsCollector(logger, "test-service")

	// Just test that it doesn't panic
	tags := map[string]string{"key": "value"}
	collector.RecordGauge("test_gauge", 10.5, "units", tags)

	// Test with zero value
	collector.RecordGauge("test_gauge_zero", 0.0, "units", nil)
}

func TestMetricsCollector_RecordTiming(t *testing.T) {
	logger := logging.NewStandardLogger("debug", "development")
	collector := NewMetricsCollector(logger, "test-service")

	// Just test that it doesn't panic
	tags := map[string]string{"operation": "test"}
	duration := 100 * time.Millisecond
	collector.RecordTiming("test_timing", duration, tags)

	// Test with zero duration
	collector.RecordTiming("test_timing_zero", 0, nil)
}

func TestMetricsCollector_RecordHistogram(t *testing.T) {
	logger := logging.NewStandardLogger("debug", "development")
	collector := NewMetricsCollector(logger, "test-service")

	// Just test that it doesn't panic
	tags := map[string]string{"bucket": "test"}
	collector.RecordHistogram("request_size", 1024, "bytes", tags)

	// Test with zero value
	collector.RecordHistogram("response_time", 0.0, "ms", nil)
}

func TestMetricsCollector_RecordBusinessMetric(t *testing.T) {
	logger := logging.NewStandardLogger("debug", "development")
	collector := NewMetricsCollector(logger, "test-service")

	// Just test that it doesn't panic
	tags := map[string]string{"exchange": "binance"}
	fields := map[string]interface{}{
		"buy_price":  100.50,
		"sell_price": 101.00,
		"volume":     1000,
	}

	collector.RecordBusinessMetric("arbitrage_opportunity", 0.5, "percent", tags, fields)

	// Test with nil fields
	collector.RecordBusinessMetric("simple_metric", 42.0, "units", nil, nil)
}

func TestMetricsCollector_RecordAPIRequestMetrics(t *testing.T) {
	logger := logging.NewStandardLogger("debug", "development")
	collector := NewMetricsCollector(logger, "test-service")

	// Just test that it doesn't panic
	duration := 150 * time.Millisecond
	collector.RecordAPIRequestMetrics("GET", "/api/symbols", 200, duration, "user123")

	// Test without user ID
	collector.RecordAPIRequestMetrics("POST", "/api/orders", 201, duration, "")
}

func TestMetricsCollector_RecordDatabaseMetrics(t *testing.T) {
	logger := logging.NewStandardLogger("debug", "development")
	collector := NewMetricsCollector(logger, "test-service")

	// Just test that it doesn't panic
	duration := 50 * time.Millisecond
	collector.RecordDatabaseMetrics("SELECT", "symbols", duration, 10, true)

	// Test failed operation
	collector.RecordDatabaseMetrics("INSERT", "orders", duration, -1, false)
}

func TestMetricsCollector_RecordCacheMetrics(t *testing.T) {
	logger := logging.NewStandardLogger("debug", "development")
	collector := NewMetricsCollector(logger, "test-service")

	// Just test that it doesn't panic
	duration := 5 * time.Millisecond
	collector.RecordCacheMetrics("GET", "symbols:binance", true, duration)

	// Test cache miss
	collector.RecordCacheMetrics("GET", "symbols:coinbase", false, duration)
}

func TestMetricsCollector_RecordExchangeMetrics(t *testing.T) {
	logger := logging.NewStandardLogger("debug", "development")
	collector := NewMetricsCollector(logger, "test-service")

	// Just test that it doesn't panic
	duration := 200 * time.Millisecond
	collector.RecordExchangeMetrics("binance", "fetch_symbols", "BTCUSDT", duration, true)

	// Test failed operation without symbol
	collector.RecordExchangeMetrics("coinbase", "fetch_orderbook", "", duration, false)
}

func TestMetricsCollector_RecordArbitrageMetrics(t *testing.T) {
	logger := logging.NewStandardLogger("debug", "development")
	collector := NewMetricsCollector(logger, "test-service")

	// Just test that it doesn't panic
	collector.RecordArbitrageMetrics("BTCUSDT", 1.5, "binance", "coinbase")
}

// Edge case tests

func TestMetricsCollector_EmptyTags(t *testing.T) {
	logger := logging.NewStandardLogger("debug", "development")
	collector := NewMetricsCollector(logger, "test-service")

	// Just test that it doesn't panic
	collector.RecordCounter("test_counter", 1.0, nil)
	collector.RecordGauge("test_gauge", 50.0, "units", nil)
}

func TestMetricsCollector_ZeroValues(t *testing.T) {
	logger := logging.NewStandardLogger("debug", "development")
	collector := NewMetricsCollector(logger, "test-service")

	// Just test that it doesn't panic
	collector.RecordTiming("zero_timing", 0, map[string]string{"test": "zero"})
	collector.RecordGauge("negative_gauge", -25.5, "units", map[string]string{"type": "delta"})
}

func TestMetricsCollector_LargeValues(t *testing.T) {
	logger := logging.NewStandardLogger("debug", "development")
	collector := NewMetricsCollector(logger, "test-service")

	// Just test that it doesn't panic
	collector.RecordCounter("large_counter", 1000000.0, map[string]string{"scale": "large"})
	collector.RecordGauge("large_gauge", 999999.99, "bytes", map[string]string{"size": "huge"})
}

func TestMetricsCollector_SpecialCharacters(t *testing.T) {
	logger := logging.NewStandardLogger("debug", "development")
	collector := NewMetricsCollector(logger, "test-service")

	// Just test that it doesn't panic
	tags := map[string]string{
		"symbol":   "BTC/USDT",
		"exchange": "binance-us",
		"type":     "spot_trading",
	}
	collector.RecordCounter("special_chars", 1.0, tags)
}

func TestMetricsCollector_RecordSystemMetrics(t *testing.T) {
	logger := logging.NewStandardLogger("debug", "development")
	collector := NewMetricsCollector(logger, "test-service")

	// Test with normal values
	collector.RecordSystemMetrics(1024, 100, 75.5)

	// Test with zero values
	collector.RecordSystemMetrics(0, 0, 0.0)

	// Test with high values
	collector.RecordSystemMetrics(8192, 1000, 99.9)
}

func TestMetricsCollector_RecordNotificationMetrics(t *testing.T) {
	logger := logging.NewStandardLogger("debug", "development")
	collector := NewMetricsCollector(logger, "test-service")

	// Test successful notification
	collector.RecordNotificationMetrics("telegram", "user123", true)

	// Test failed notification
	collector.RecordNotificationMetrics("email", "user456", false)

	// Test with empty user ID
	collector.RecordNotificationMetrics("webhook", "", true)

	// Test with different notification types
	collector.RecordNotificationMetrics("sms", "user789", false)
}
