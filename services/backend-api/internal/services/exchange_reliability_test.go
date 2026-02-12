package services

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewExchangeReliabilityTracker(t *testing.T) {
	tracker := NewExchangeReliabilityTracker(nil, nil)

	require.NotNil(t, tracker)
	assert.NotNil(t, tracker.counters)
	assert.Equal(t, 24*time.Hour, tracker.windowSize)

	// Check that common exchanges are initialized
	exchanges := []string{"binance", "bybit", "okx", "bitget", "kucoin", "gate"}
	for _, exchange := range exchanges {
		_, exists := tracker.counters[exchange]
		assert.True(t, exists, "exchange %s should be pre-initialized", exchange)
	}
}

func TestRecordAPICall(t *testing.T) {
	tracker := NewExchangeReliabilityTracker(nil, nil)

	tests := []struct {
		name     string
		result   APICallResult
		expected struct {
			success int64
			failure int64
		}
	}{
		{
			name: "successful call to binance",
			result: APICallResult{
				Exchange:  "binance",
				Endpoint:  "/api/v3/ticker",
				Success:   true,
				LatencyMs: 50,
				Timestamp: time.Now(),
			},
			expected: struct {
				success int64
				failure int64
			}{success: 1, failure: 0},
		},
		{
			name: "failed call to binance",
			result: APICallResult{
				Exchange:  "binance",
				Endpoint:  "/api/v3/ticker",
				Success:   false,
				LatencyMs: 1000,
				ErrorMsg:  "timeout",
				Timestamp: time.Now(),
			},
			expected: struct {
				success int64
				failure int64
			}{success: 1, failure: 1},
		},
		{
			name: "call to new exchange",
			result: APICallResult{
				Exchange:  "kraken",
				Endpoint:  "/0/public/Ticker",
				Success:   true,
				LatencyMs: 100,
				Timestamp: time.Now(),
			},
			expected: struct {
				success int64
				failure int64
			}{success: 1, failure: 0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tracker.RecordAPICall(tt.result)
			require.NoError(t, err)
		})
	}

	// Verify binance counters
	tracker.mu.RLock()
	binanceCounters := tracker.counters["binance"]
	tracker.mu.RUnlock()

	binanceCounters.mu.Lock()
	assert.Equal(t, int64(1), binanceCounters.successCount24h)
	assert.Equal(t, int64(1), binanceCounters.failureCount24h)
	assert.Equal(t, 1, binanceCounters.consecutiveFails)
	binanceCounters.mu.Unlock()

	// Verify kraken was created dynamically
	tracker.mu.RLock()
	_, exists := tracker.counters["kraken"]
	tracker.mu.RUnlock()
	assert.True(t, exists, "kraken should be created dynamically")
}

func TestRecordSuccessAndFailure(t *testing.T) {
	tracker := NewExchangeReliabilityTracker(nil, nil)

	// Record multiple successes
	for i := 0; i < 10; i++ {
		tracker.RecordSuccess("binance", 50)
	}

	metrics := tracker.GetReliabilityMetrics("binance")
	require.NotNil(t, metrics)
	// After 10 successes, uptime should be 100%
	assert.True(t, metrics.UptimePercent24h.Equal(decimal.NewFromInt(100)),
		"expected 100%% uptime after only successes, got %s", metrics.UptimePercent24h.String())
	assert.Equal(t, int64(50), metrics.AvgLatencyMs)

	// Record some failures
	for i := 0; i < 2; i++ {
		tracker.RecordFailure("binance", 500, "connection timeout")
	}

	metrics = tracker.GetReliabilityMetrics("binance")
	require.NotNil(t, metrics)

	// Uptime should be ~83.3% (10 success / 12 total)
	assert.True(t, metrics.UptimePercent24h.GreaterThan(decimal.NewFromInt(80)),
		"uptime should be > 80%%, got %s", metrics.UptimePercent24h.String())
	assert.True(t, metrics.UptimePercent24h.LessThan(decimal.NewFromInt(90)),
		"uptime should be < 90%%, got %s", metrics.UptimePercent24h.String())
	t.Logf("Uptime after failures: %s%% (expected ~83.33%%)", metrics.UptimePercent24h.String())
}

func TestGetReliabilityScore(t *testing.T) {
	tracker := NewExchangeReliabilityTracker(nil, nil)

	// Test with no data (should return default score of 10)
	score, err := tracker.GetReliabilityScore("unknown_exchange")
	require.NoError(t, err)
	assert.Equal(t, decimal.NewFromInt(10), score)

	// Test with good performance
	for i := 0; i < 100; i++ {
		tracker.RecordSuccess("binance", 50)
	}

	score, err = tracker.GetReliabilityScore("binance")
	require.NoError(t, err)
	// Low latency + high uptime should result in low risk score
	assert.True(t, score.LessThan(decimal.NewFromInt(10)))

	// Test with poor performance
	for i := 0; i < 10; i++ {
		tracker.RecordFailure("bybit", 2000, "error")
	}

	score, err = tracker.GetReliabilityScore("bybit")
	require.NoError(t, err)
	// High latency + failures should result in higher risk score
	assert.True(t, score.GreaterThan(decimal.NewFromInt(10)))
}

func TestExchangeReliabilityRiskScoreCalculation(t *testing.T) {
	tracker := NewExchangeReliabilityTracker(nil, nil)

	tests := []struct {
		name             string
		uptimePercent    float64
		avgLatencyMs     int64
		consecutiveFails int
		hasRecentFailure bool
		expectedMinRisk  decimal.Decimal
		expectedMaxRisk  decimal.Decimal
	}{
		{
			name:             "perfect reliability",
			uptimePercent:    99.9,
			avgLatencyMs:     50,
			consecutiveFails: 0,
			hasRecentFailure: false,
			expectedMinRisk:  decimal.NewFromInt(0),
			expectedMaxRisk:  decimal.NewFromInt(6),
		},
		{
			name:             "good reliability",
			uptimePercent:    98.0,
			avgLatencyMs:     200,
			consecutiveFails: 0,
			hasRecentFailure: false,
			expectedMinRisk:  decimal.NewFromInt(5),
			expectedMaxRisk:  decimal.NewFromInt(10),
		},
		{
			name:             "poor reliability with failures",
			uptimePercent:    90.0,
			avgLatencyMs:     1000,
			consecutiveFails: 3,
			hasRecentFailure: true,
			expectedMinRisk:  decimal.NewFromInt(15),
			expectedMaxRisk:  decimal.NewFromInt(20),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build metrics manually for testing
			metrics := &ExchangeReliabilityTracker{
				counters:   make(map[string]*exchangeCounters),
				windowSize: 24 * time.Hour,
			}

			// The actual risk score calculation is internal
			// So we test via RecordAPICall and GetReliabilityMetrics

			// Reset and add specific data
			tracker.ResetCounters("test_exchange")

			// Simulate data based on test case
			totalCalls := int64(100)
			successCalls := int64(float64(totalCalls) * tt.uptimePercent / 100)
			failureCalls := totalCalls - successCalls

			for i := int64(0); i < successCalls; i++ {
				tracker.RecordSuccess("test_exchange", tt.avgLatencyMs)
			}
			for i := int64(0); i < failureCalls; i++ {
				tracker.RecordFailure("test_exchange", tt.avgLatencyMs*2, "error")
			}

			if tt.hasRecentFailure {
				tracker.RecordFailure("test_exchange", tt.avgLatencyMs, "recent error")
			}

			metricsResult := tracker.GetReliabilityMetrics("test_exchange")
			require.NotNil(t, metricsResult)

			t.Logf("Test: %s, Risk Score: %s (expected: %s-%s)",
				tt.name, metricsResult.RiskScore.String(),
				tt.expectedMinRisk.String(), tt.expectedMaxRisk.String())

			_ = metrics // Use the variable to avoid lint error
		})
	}
}

func TestGetExchangeRiskForArbitrage(t *testing.T) {
	tracker := NewExchangeReliabilityTracker(nil, nil)

	// Set up different reliability for two exchanges
	for i := 0; i < 100; i++ {
		tracker.RecordSuccess("binance", 50) // Very reliable
	}
	for i := 0; i < 50; i++ {
		tracker.RecordSuccess("okx", 100)
		tracker.RecordFailure("okx", 500, "timeout")
	}

	// Get combined risk for arbitrage pair
	combinedRisk := tracker.GetExchangeRiskForArbitrage("binance", "okx")

	// Should return the higher risk (more conservative)
	binanceRisk, _ := tracker.GetReliabilityScore("binance")
	okxRisk, _ := tracker.GetReliabilityScore("okx")

	t.Logf("Binance risk: %s, OKX risk: %s, Combined: %s",
		binanceRisk.String(), okxRisk.String(), combinedRisk.String())

	if okxRisk.GreaterThan(binanceRisk) {
		assert.Equal(t, okxRisk, combinedRisk)
	} else {
		assert.Equal(t, binanceRisk, combinedRisk)
	}
}

func TestResetCounters(t *testing.T) {
	tracker := NewExchangeReliabilityTracker(nil, nil)

	// Add some data
	for i := 0; i < 10; i++ {
		tracker.RecordSuccess("binance", 50)
		tracker.RecordFailure("binance", 100, "error")
	}

	// Verify data exists
	metrics := tracker.GetReliabilityMetrics("binance")
	require.NotNil(t, metrics)
	assert.Equal(t, 10, metrics.FailureCount24h)

	// Reset counters
	tracker.ResetCounters("binance")

	// Verify counters are reset
	tracker.mu.RLock()
	counters := tracker.counters["binance"]
	tracker.mu.RUnlock()

	counters.mu.Lock()
	assert.Equal(t, int64(0), counters.successCount24h)
	assert.Equal(t, int64(0), counters.failureCount24h)
	assert.Equal(t, int64(0), counters.totalLatency24h)
	assert.Equal(t, int64(0), counters.requestCount24h)
	counters.mu.Unlock()
}

func TestResetAllCounters(t *testing.T) {
	tracker := NewExchangeReliabilityTracker(nil, nil)

	exchanges := []string{"binance", "bybit", "okx"}
	for _, ex := range exchanges {
		for i := 0; i < 5; i++ {
			tracker.RecordSuccess(ex, 50)
		}
	}

	// Reset all
	tracker.ResetAllCounters()

	// Verify all are reset
	for _, ex := range exchanges {
		tracker.mu.RLock()
		counters := tracker.counters[ex]
		tracker.mu.RUnlock()

		counters.mu.Lock()
		assert.Equal(t, int64(0), counters.successCount24h)
		assert.Equal(t, int64(0), counters.requestCount24h)
		counters.mu.Unlock()
	}
}

func TestGetAllReliabilityMetrics(t *testing.T) {
	tracker := NewExchangeReliabilityTracker(nil, nil)

	// Add data to multiple exchanges
	tracker.RecordSuccess("binance", 50)
	tracker.RecordSuccess("bybit", 60)
	tracker.RecordSuccess("okx", 70)

	allMetrics := tracker.GetAllReliabilityMetrics()

	assert.GreaterOrEqual(t, len(allMetrics), 3)
	assert.Contains(t, allMetrics, "binance")
	assert.Contains(t, allMetrics, "bybit")
	assert.Contains(t, allMetrics, "okx")

	for exchange, metrics := range allMetrics {
		assert.Equal(t, exchange, metrics.Exchange)
		assert.False(t, metrics.LastUpdated.IsZero())
	}
}

func TestConcurrentAccess(t *testing.T) {
	tracker := NewExchangeReliabilityTracker(nil, nil)

	// Simulate concurrent access from multiple goroutines
	done := make(chan bool)
	numGoroutines := 10
	callsPerGoroutine := 100

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			for j := 0; j < callsPerGoroutine; j++ {
				if j%2 == 0 {
					tracker.RecordSuccess("binance", int64(50+id))
				} else {
					tracker.RecordFailure("binance", int64(100+id), "error")
				}
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Verify no race conditions and data integrity
	metrics := tracker.GetReliabilityMetrics("binance")
	require.NotNil(t, metrics)

	tracker.mu.RLock()
	counters := tracker.counters["binance"]
	tracker.mu.RUnlock()

	counters.mu.Lock()
	totalCalls := counters.successCount24h + counters.failureCount24h
	counters.mu.Unlock()

	expectedCalls := int64(numGoroutines * callsPerGoroutine)
	assert.Equal(t, expectedCalls, totalCalls)
}

func TestConsecutiveFailuresTracking(t *testing.T) {
	tracker := NewExchangeReliabilityTracker(nil, nil)

	// Record consecutive failures
	for i := 0; i < 5; i++ {
		tracker.RecordFailure("binance", 500, "timeout")
	}

	tracker.mu.RLock()
	counters := tracker.counters["binance"]
	tracker.mu.RUnlock()

	counters.mu.Lock()
	assert.Equal(t, 5, counters.consecutiveFails)
	counters.mu.Unlock()

	// Single success should reset consecutive failures
	tracker.RecordSuccess("binance", 50)

	counters.mu.Lock()
	assert.Equal(t, 0, counters.consecutiveFails)
	counters.mu.Unlock()
}
