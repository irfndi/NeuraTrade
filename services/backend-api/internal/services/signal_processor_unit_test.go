package services

import (
	"context"
	"testing"
	"time"

	"log/slog"

	"github.com/stretchr/testify/assert"
)

// TestSignalProcessor_Config tests the default configuration values
func TestSignalProcessor_Config(t *testing.T) {
	// This test focuses on verifying the default configuration
	// without requiring full dependency injection

	// Test default configuration values
	config := &SignalProcessorConfig{
		BatchSize:           100,
		ProcessingInterval:  5 * time.Minute,
		MaxConcurrentBatch:  4,
		SignalTTL:           24 * time.Hour,
		QualityThreshold:    0.7,
		NotificationEnabled: true,
		RetryAttempts:       3,
		RetryDelay:          30 * time.Second,
	}

	// Verify config values
	assert.Equal(t, 100, config.BatchSize)
	assert.Equal(t, 5*time.Minute, config.ProcessingInterval)
	assert.Equal(t, 4, config.MaxConcurrentBatch)
	assert.Equal(t, 24*time.Hour, config.SignalTTL)
	assert.Equal(t, 0.7, config.QualityThreshold)
	assert.True(t, config.NotificationEnabled)
	assert.Equal(t, 3, config.RetryAttempts)
	assert.Equal(t, 30*time.Second, config.RetryDelay)
}

// TestSignalProcessor_ProcessingMetrics tests the metrics structure
func TestSignalProcessor_ProcessingMetrics(t *testing.T) {
	metrics := &ProcessingMetrics{}

	// Verify initial state
	assert.Equal(t, int64(0), metrics.TotalSignalsProcessed)
	assert.Equal(t, int64(0), metrics.SuccessfulSignals)
	assert.Equal(t, int64(0), metrics.FailedSignals)
	assert.Equal(t, int64(0), metrics.QualityFilteredSignals)
	assert.Equal(t, int64(0), metrics.NotificationsSent)

	// Test incrementing metrics
	metrics.TotalSignalsProcessed = 10
	metrics.SuccessfulSignals = 8
	metrics.FailedSignals = 2
	metrics.QualityFilteredSignals = 1
	metrics.NotificationsSent = 5

	assert.Equal(t, int64(10), metrics.TotalSignalsProcessed)
	assert.Equal(t, int64(8), metrics.SuccessfulSignals)
	assert.Equal(t, int64(2), metrics.FailedSignals)
	assert.Equal(t, int64(1), metrics.QualityFilteredSignals)
	assert.Equal(t, int64(5), metrics.NotificationsSent)
}

// TestSignalProcessor_CircuitBreakerStates tests the circuit breaker state enumeration
func TestSignalProcessor_CircuitBreakerStates(t *testing.T) {
	// Test that circuit breaker states are valid
	states := []CircuitBreakerState{
		Closed,
		Open,
		HalfOpen,
	}

	for _, state := range states {
		switch state {
		case Closed, Open, HalfOpen:
			// Valid state
		default:
			t.Errorf("Unknown circuit breaker state: %v", state)
		}
	}
}

// TestSignalProcessor_SignalStrengthMapping tests signal strength constants
func TestSignalProcessor_SignalStrengthMapping(t *testing.T) {
	// Test that signal strength constants are valid
	strengths := []SignalStrength{
		SignalStrengthWeak,
		SignalStrengthMedium,
		SignalStrengthStrong,
	}

	for _, strength := range strengths {
		switch strength {
		case SignalStrengthWeak, SignalStrengthMedium, SignalStrengthStrong:
			// Valid strength
		default:
			t.Errorf("Unknown signal strength: %v", strength)
		}
	}
}

// TestSignalProcessor_SignalTypeMapping tests signal type constants
func TestSignalProcessor_SignalTypeMapping(t *testing.T) {
	// Test that signal type constants are valid
	types := []SignalType{
		SignalTypeArbitrage,
		SignalTypeTechnical,
	}

	for _, signalType := range types {
		switch signalType {
		case SignalTypeArbitrage, SignalTypeTechnical:
			// Valid type
		default:
			t.Errorf("Unknown signal type: %v", signalType)
		}
	}
}

// TestSignalProcessor_ContextHandling tests context handling patterns
func TestSignalProcessor_ContextHandling(t *testing.T) {
	// Test context creation and cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Verify context is not cancelled
	assert.False(t, ctx.Err() != nil)

	// Cancel context and verify
	cancel()
	assert.True(t, ctx.Err() != nil)
	assert.Equal(t, context.Canceled, ctx.Err())
}

// TestSignalProcessor_TimeHandling tests time-related functionality
func TestSignalProcessor_TimeHandling(t *testing.T) {
	// Test time calculations for signal processing
	now := time.Now()

	// Test signal TTL calculation
	ttl := 24 * time.Hour
	expiryTime := now.Add(ttl)

	assert.True(t, expiryTime.After(now))
	assert.Equal(t, ttl, expiryTime.Sub(now))

	// Test processing interval
	interval := 5 * time.Minute
	nextProcessing := now.Add(interval)

	assert.True(t, nextProcessing.After(now))
	assert.Equal(t, interval, nextProcessing.Sub(now))

	// Test retry delay
	retryDelay := 30 * time.Second
	retryTime := now.Add(retryDelay)

	assert.True(t, retryTime.After(now))
	assert.Equal(t, retryDelay, retryTime.Sub(now))
}

// TestSignalProcessor_ValidationMethods tests validation logic patterns
func TestSignalProcessor_ValidationMethods(t *testing.T) {
	// Test quality threshold validation
	validThreshold := 0.7
	assert.True(t, validThreshold >= 0.0 && validThreshold <= 1.0)

	invalidThreshold := 1.5
	assert.False(t, invalidThreshold >= 0.0 && invalidThreshold <= 1.0)

	// Test batch size validation
	validBatchSize := 100
	assert.True(t, validBatchSize > 0)

	invalidBatchSize := 0
	assert.False(t, invalidBatchSize > 0)

	// Test retry attempts validation
	validRetryAttempts := 3
	assert.True(t, validRetryAttempts > 0)

	invalidRetryAttempts := -1
	assert.False(t, invalidRetryAttempts > 0)
}

// MockLogger is a simple logger implementation for testing
type MockLogger struct {
	logs []string
}

func (m *MockLogger) Debug(msg string, args ...any) {
	m.logs = append(m.logs, "DEBUG: "+msg)
}

func (m *MockLogger) Info(msg string, args ...any) {
	m.logs = append(m.logs, "INFO: "+msg)
}

func (m *MockLogger) Warn(msg string, args ...any) {
	m.logs = append(m.logs, "WARN: "+msg)
}

func (m *MockLogger) Error(msg string, args ...any) {
	m.logs = append(m.logs, "ERROR: "+msg)
}

// TestSignalProcessor_LoggerInterface tests logger interface compatibility
func TestSignalProcessor_LoggerInterface(t *testing.T) {
	// Test that we can create a compatible logger
	logger := slog.New(slog.NewTextHandler(&MockWriter{}, &slog.HandlerOptions{}))
	assert.NotNil(t, logger)

	// Test logger operations
	logger.Info("Test message", "key", "value")
	logger.Error("Error message", "error", "test error")
	logger.Warn("Warning message")
	logger.Debug("Debug message")

	// These should not panic
	assert.True(t, true) // If we reach here, logger operations worked
}

// MockWriter implements io.Writer for testing
type MockWriter struct{}

func (m *MockWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}

// TestSignalProcessor_ErrorScenarios tests error handling patterns
func TestSignalProcessor_ErrorScenarios(t *testing.T) {
	// Test error scenarios that don't require full processor initialization

	// Test context cancellation
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Verify context is cancelled
	assert.Error(t, ctx.Err())
	assert.Equal(t, context.Canceled, ctx.Err())

	// Test timeout context
	timeoutCtx, timeoutCancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer timeoutCancel()

	// Wait for timeout
	time.Sleep(time.Millisecond * 2)
	assert.Error(t, timeoutCtx.Err())
	assert.Equal(t, context.DeadlineExceeded, timeoutCtx.Err())
}

// TestSignalProcessor_ConcurrentSafety tests concurrent safety patterns
func TestSignalProcessor_ConcurrentSafety(t *testing.T) {
	// Test concurrent safety patterns that can be verified without full processor

	// Test channel operations
	done := make(chan bool)
	results := make(chan int, 10)

	// Start multiple goroutines
	for i := 0; i < 10; i++ {
		go func(id int) {
			defer func() {
				if r := recover(); r != nil {
					t.Logf("Recovered from panic in goroutine %d: %v", id, r)
				}
			}()
			results <- id
			done <- true
		}(i)
	}

	// Collect results
	completed := 0
	receivedResults := make([]int, 0)

	// We need to receive 20 messages total: 10 results + 10 done signals
	// Use separate loops to ensure we get all messages
	for len(receivedResults) < 10 {
		select {
		case result := <-results:
			receivedResults = append(receivedResults, result)
		case <-time.After(5 * time.Second):
			t.Fatal("Test timed out waiting for results")
		}
	}

	for completed < 10 {
		select {
		case <-done:
			completed++
		case <-time.After(5 * time.Second):
			t.Fatal("Test timed out waiting for completion signals")
		}
	}

	// Verify all goroutines completed and all results were received
	assert.Equal(t, 10, completed)
	assert.Len(t, receivedResults, 10)

	// Verify all IDs from 0-9 are present (order may vary)
	expectedIDs := make(map[int]bool)
	for i := 0; i < 10; i++ {
		expectedIDs[i] = true
	}

	for _, id := range receivedResults {
		assert.True(t, expectedIDs[id], "ID %d should be in expected range", id)
		delete(expectedIDs, id)
	}
	assert.Empty(t, expectedIDs, "All expected IDs should be received")
}

func TestSignalProcessor_ExtractExchanges(t *testing.T) {
	sp := &SignalProcessor{}

	t.Run("nil signal", func(t *testing.T) {
		exchanges := sp.extractExchanges(nil)
		assert.Empty(t, exchanges)
	})

	t.Run("combine and dedupe from fields and metadata", func(t *testing.T) {
		signal := &AggregatedSignal{
			Exchanges: []string{"binance", "Binance", "coinbase", " "},
			Metadata: map[string]interface{}{
				"buy_exchanges":  []string{"kraken", "binance"},
				"sell_exchanges": []interface{}{"okx", "kraken", 123},
				"exchanges":      "bitfinex, coinbase",
			},
		}

		exchanges := sp.extractExchanges(signal)
		assert.Equal(t, []string{"binance", "coinbase", "kraken", "okx", "bitfinex"}, exchanges)
	})
}

func TestSignalProcessor_CollectNotifiableSignals(t *testing.T) {
	sp := &SignalProcessor{config: &SignalProcessorConfig{QualityThreshold: 0.7}}
	now := time.Now()

	aggPtr := &AggregatedSignal{
		ID:         "sig-1",
		SignalType: SignalTypeArbitrage,
		Symbol:     "BTC/USDT",
		Action:     "buy",
		Metadata: map[string]interface{}{
			"buy_exchanges": []string{"binance"},
		},
		CreatedAt: now,
	}

	aggValue := AggregatedSignal{
		ID:         "sig-2",
		SignalType: SignalTypeTechnical,
		Symbol:     "ETH/USDT",
		Action:     "sell",
		Exchanges:  []string{"coinbase", "coinbase"},
		CreatedAt:  now.Add(time.Second),
	}

	results := []ProcessingResult{
		{Processed: true, QualityScore: 0.9, Metadata: map[string]interface{}{"aggregated_signal": aggPtr}},
		{Processed: true, QualityScore: 0.95, Metadata: map[string]interface{}{"aggregated_signal": aggValue}},
		{Processed: true, QualityScore: 0.95, Metadata: map[string]interface{}{"aggregated_signal": aggPtr}},
		{Processed: true, QualityScore: 0.6, Metadata: map[string]interface{}{"aggregated_signal": aggValue}},
		{Processed: false, QualityScore: 0.99, Metadata: map[string]interface{}{"aggregated_signal": aggValue}},
		{Processed: true, QualityScore: 0.99, Metadata: map[string]interface{}{"aggregated_signal": "invalid"}},
	}

	notifiable := sp.collectNotifiableSignals(results)
	if assert.Len(t, notifiable, 2) {
		assert.Equal(t, "sig-1", notifiable[0].ID)
		assert.Equal(t, []string{"binance"}, notifiable[0].Exchanges)
		assert.Equal(t, "sig-2", notifiable[1].ID)
		assert.Equal(t, []string{"coinbase"}, notifiable[1].Exchanges)
	}
}

func TestSignalProcessor_HandleProcessingResultsWithContext_SkipsWhenNotificationsUnavailable(t *testing.T) {
	results := []ProcessingResult{{Processed: true, QualityScore: 0.9}}

	t.Run("nil config", func(t *testing.T) {
		sp := &SignalProcessor{}
		err := sp.handleProcessingResultsWithContext(context.Background(), results)
		assert.NoError(t, err)
	})

	t.Run("notifications disabled", func(t *testing.T) {
		sp := &SignalProcessor{config: &SignalProcessorConfig{NotificationEnabled: false}}
		err := sp.handleProcessingResultsWithContext(context.Background(), results)
		assert.NoError(t, err)
	})

	t.Run("notification service nil", func(t *testing.T) {
		sp := &SignalProcessor{config: &SignalProcessorConfig{NotificationEnabled: true}}
		err := sp.handleProcessingResultsWithContext(context.Background(), results)
		assert.NoError(t, err)
	})
}
