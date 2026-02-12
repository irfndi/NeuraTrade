package services

import (
	"context"
	"testing"
	"time"

	"github.com/irfandi/celebrum-ai-go/internal/config"
	"github.com/irfandi/celebrum-ai-go/internal/database"
	"github.com/irfandi/celebrum-ai-go/internal/logging"
	zaplogrus "github.com/irfandi/celebrum-ai-go/internal/logging/zaplogrus"
	"github.com/irfandi/celebrum-ai-go/internal/models"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSignalAggregationIntegration tests the complete signal processing pipeline
func TestSignalAggregationIntegration(t *testing.T) {
	// Create mock database pool
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mockPool.Close()
	dbPool := database.WrapLegacyDBPool(mockPool)

	// Setup test logger
	zapLogger := zaplogrus.New()
	zapLogger.SetLevel(zaplogrus.DebugLevel)
	serviceLogger := logging.NewStandardLogger("debug", "testing")

	// Setup configuration
	cfg := &config.Config{
		Arbitrage: config.ArbitrageConfig{
			Enabled: true,
		},
	}

	// Initialize services with mock dependencies

	// Circuit Breaker
	cbConfig := CircuitBreakerConfig{
		FailureThreshold: 5,
		ResetTimeout:     10 * time.Second,
	}
	circuitBreaker := NewCircuitBreaker("test-cb", cbConfig, zapLogger)

	// Quality Scorer
	qualityScorer := NewSignalQualityScorer(cfg, dbPool, zapLogger)

	// Tech Analysis - Use struct literal to avoid complex constructor dependencies
	taService := &TechnicalAnalysisService{
		config: cfg,
		logger: zapLogger,
	}

	// Signal Aggregator
	signalAggregator := NewSignalAggregator(cfg, dbPool, zapLogger)

	// Notification Service - Use struct literal
	notificationService := &NotificationService{
		// Safe to leave empty for tests as methods handle nil clients/empty URLs gracefully
	}

	// Collector Service
	collectorService := &CollectorService{} // Stub

	// Signal Processor
	signalProcessor := NewSignalProcessor(
		dbPool,
		serviceLogger,
		signalAggregator,
		qualityScorer,
		taService,
		notificationService,
		collectorService,
		circuitBreaker,
	)

	// Context
	ctx := context.Background()

	// Verify services are initialized
	assert.NotNil(t, signalProcessor, "Signal processor should be initialized")
	assert.NotNil(t, signalAggregator, "Signal aggregator should be initialized")
	assert.NotNil(t, qualityScorer, "Quality scorer should be initialized")
	assert.NotNil(t, notificationService, "Notification service should be initialized")
	assert.NotNil(t, circuitBreaker, "Circuit breaker should be initialized")
	assert.NotNil(t, zapLogger, "Logger should be initialized")

	// Test 1: Signal Processing Pipeline
	t.Run("SignalProcessingPipeline", func(t *testing.T) {
		// Since getActiveTradingPairs is stubbed to return empty, this should run without DB queries
		err := signalProcessor.processSignalBatch()
		assert.NoError(t, err, "Signal processing should not fail (even with empty data)")

		// Verify metrics were updated (even if empty run)
		metrics := signalProcessor.GetMetrics()
		assert.NotNil(t, metrics, "Metrics should be available")
		// LastProcessingTime might not be set if it returned early due to empty data.
		// Checking implementation: start time is recorded, but if empty, it returns early.
		// If it returns early (line 358 in processor), it doesn't update metrics last processing time?
		// Actually processSignalBatch updates lastRun (line 263).
	})

	// Test 2: Signal Quality Assessment
	t.Run("SignalQualityAssessment", func(t *testing.T) {
		// Create test signal quality input
		qualityInput := &SignalQualityInput{
			SignalType:      "technical",
			Symbol:          "BTC/USDT",
			Exchanges:       []string{"binance", "coinbase"},
			Volume:          decimal.NewFromFloat(50000),
			ProfitPotential: decimal.NewFromFloat(0.025),
			Confidence:      decimal.NewFromFloat(0.8),
			Timestamp:       time.Now(),
			MarketData: &MarketDataSnapshot{
				Price:          decimal.NewFromFloat(45000),
				Volume24h:      decimal.NewFromFloat(1000000),
				PriceChange24h: decimal.NewFromFloat(0.02),
				Volatility:     decimal.NewFromFloat(0.03),
				Spread:         decimal.NewFromFloat(0.001),
				OrderBookDepth: decimal.NewFromFloat(100000),
				LastTradeTime:  time.Now(),
			},
		}

		// Assess signal quality
		metrics, err := qualityScorer.AssessSignalQuality(ctx, qualityInput)
		require.NoError(t, err, "Quality assessment should not fail")
		require.NotNil(t, metrics, "Quality metrics should be returned")

		// Verify quality metrics
		assert.True(t, metrics.OverallScore.GreaterThan(decimal.Zero), "Overall score should be positive")
		assert.True(t, metrics.VolumeScore.GreaterThan(decimal.Zero), "Volume score should be positive")
		assert.True(t, metrics.ExchangeScore.GreaterThan(decimal.Zero), "Exchange score should be positive")
	})

	// Test 3: Signal Aggregation
	t.Run("SignalAggregation", func(t *testing.T) {
		// Create test arbitrage opportunities
		opportunities := []models.ArbitrageOpportunity{
			{
				ID:               "test-arb-1",
				TradingPairID:    1,
				BuyExchangeID:    1,
				SellExchangeID:   2,
				BuyPrice:         decimal.NewFromFloat(44900),
				SellPrice:        decimal.NewFromFloat(45100),
				ProfitPercentage: decimal.NewFromFloat(0.45),
				DetectedAt:       time.Now(),
				ExpiresAt:        time.Now().Add(5 * time.Minute),
				TradingPair:      &models.TradingPair{Symbol: "BTC/USDT"},
				BuyExchange:      &models.Exchange{Name: "binance"},
				SellExchange:     &models.Exchange{Name: "coinbase"},
			},
		}

		// Input for aggregation
		input := ArbitrageSignalInput{
			Opportunities: opportunities,
			MinVolume:     decimal.NewFromFloat(1000),
			BaseAmount:    decimal.NewFromFloat(20000),
		}

		// Aggregate signals
		signals, err := signalAggregator.AggregateArbitrageSignals(ctx, input)
		assert.NoError(t, err, "Signal aggregation should not return error for low profit (just filters)")
		assert.Empty(t, signals, "Should return empty signals for low profit")

		// Success Case
		opportunities[0].ProfitPercentage = decimal.NewFromFloat(1.5)
		input.Opportunities = opportunities
		input.MinVolume = decimal.NewFromFloat(100000) // Ensure high volume score

		signals, err = signalAggregator.AggregateArbitrageSignals(ctx, input)
		assert.NoError(t, err, "Signal aggregation should not fail")
		assert.NotEmpty(t, signals, "Should generate aggregated signals")

		// Verify signal properties
		for _, signal := range signals {
			assert.NotEmpty(t, signal.ID, "Signal should have ID")
			assert.NotEmpty(t, signal.Symbol, "Signal should have symbol")
			assert.True(t, signal.Confidence.GreaterThan(decimal.Zero), "Signal should have confidence")
			assert.NotEmpty(t, signal.Exchanges, "Signal should have exchanges")
		}
	})

	// Test 4: Circuit Breaker Integration
	t.Run("CircuitBreakerIntegration", func(t *testing.T) {
		// Test circuit breaker is closed initially
		assert.False(t, circuitBreaker.IsOpen(), "Circuit breaker should be closed initially")

		// Test successful execution
		err := circuitBreaker.Execute(ctx, func(ctx context.Context) error {
			return nil
		})
		assert.NoError(t, err, "Circuit breaker should allow successful execution")

		// Verify circuit breaker stats
		stats := circuitBreaker.GetStats()
		assert.NotNil(t, stats, "Circuit breaker stats should be available")
		assert.True(t, stats.TotalRequests > 0, "Should have recorded requests")
	})

	// Test 5: Error Handling and Recovery
	t.Run("ErrorHandlingAndRecovery", func(t *testing.T) {
		// Verify Error Rate handling logic in Processor
		// (Mocking specific errors would require mocking getActiveTradingPairs which is hardstubbed)

		// Instead test CircuitBreaker failure state
		// Trip the breaker
		for i := 0; i <= 5; i++ {
			_ = circuitBreaker.Execute(ctx, func(ctx context.Context) error {
				return assert.AnError
			})
		}

		assert.True(t, circuitBreaker.IsOpen(), "Circuit breaker should be open after failures")

		// Reset for other tests if needed (re-create or wait)
		// Since we use same circuitBreaker variable, we should be careful.
		// But this is the last test or we can check.
	})

	// Test 6: End-to-End Pipeline (Start/Stop)
	t.Run("EndToEndPipeline", func(t *testing.T) {
		// Initialize a fresh processor to avoid state issues from previous tests
		cleanProcessor := NewSignalProcessor(
			dbPool,
			serviceLogger,
			signalAggregator,
			qualityScorer,
			taService,
			notificationService,
			collectorService,
			circuitBreaker,
		)

		// Start the signal processor
		err := cleanProcessor.Start()
		assert.NoError(t, err, "Signal processor should start successfully")
		assert.True(t, cleanProcessor.IsRunning(), "Signal processor should be running")

		// Let it run for a short time
		time.Sleep(100 * time.Millisecond)

		// Stop the signal processor
		err = cleanProcessor.Stop()
		assert.NoError(t, err, "Signal processor should stop successfully")
		assert.False(t, cleanProcessor.IsRunning(), "Signal processor should be stopped")
	})
}
