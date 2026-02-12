package services

import (
	"testing"
	"time"

	"github.com/irfandi/celebrum-ai-go/internal/database"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/irfandi/celebrum-ai-go/internal/logging"
	"github.com/irfandi/celebrum-ai-go/internal/models"
)

func TestSignalProcessor_ProcessSignal(t *testing.T) {
	// Setup mocks
	mockPool, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("an error '%s' was not expected when opening a stub database connection", err)
	}
	defer mockPool.Close()

	mockAggregator := &MockSignalAggregator{}
	mockScorer := &MockSignalQualityScorer{}
	// Explicitly define type to verify compilation
	var logger logging.Logger = logging.NewStandardLogger("info", "test")

	// Create SignalProcessor
	dbPool := database.NewMockDBPool(mockPool)
	sp := NewSignalProcessor(
		dbPool,
		logger,
		mockAggregator,
		mockScorer,
		nil, // technicalAnalysis
		nil, // notificationService
		nil, // collectorService
		nil, // circuitBreaker
	)

	// Test data
	marketData := models.MarketData{
		TradingPairID: 1,
		ExchangeID:    1,
		LastPrice:     decimal.NewFromFloat(50000),
		Volume24h:     decimal.NewFromFloat(1000),
		Timestamp:     time.Now(),
	}

	// Mock DB expectations
	// 1. getTradingPairSymbol
	mockPool.ExpectQuery("SELECT symbol FROM trading_pairs WHERE id = \\$1").
		WithArgs(1).
		WillReturnRows(pgxmock.NewRows([]string{"symbol"}).AddRow("BTC/USDT"))

	// 2. getExchangeName
	mockPool.ExpectQuery("SELECT name FROM exchanges WHERE id = \\$1").
		WithArgs(1).
		WillReturnRows(pgxmock.NewRows([]string{"name"}).AddRow("binance"))

	// 3. getTradingPairSymbol (called again inside generateArbitrageSignals)
	mockPool.ExpectQuery("SELECT symbol FROM trading_pairs WHERE id = \\$1").
		WithArgs(1).
		WillReturnRows(pgxmock.NewRows([]string{"symbol"}).AddRow("BTC/USDT"))

	// 4. getArbitrageOpportunities
	mockPool.ExpectQuery("SELECT .* FROM arbitrage_opportunities .*").
		WithArgs("BTC/USDT").
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "trading_pair_id", "buy_exchange_id", "sell_exchange_id",
			"buy_price", "sell_price", "profit_percentage", "detected_at", "expires_at",
		})) // Empty rows means no opportunities

	// 5. getTradingPairSymbol (called inside generateTechnicalSignals)
	mockPool.ExpectQuery("SELECT symbol FROM trading_pairs WHERE id = \\$1").
		WithArgs(1).
		WillReturnRows(pgxmock.NewRows([]string{"symbol"}).AddRow("BTC/USDT"))

	// Mock Aggregator expectations
	// Expect AggregateTechnicalSignals because we have no arbitrage opportunities
	mockAggregator.On("AggregateTechnicalSignals", mock.Anything, mock.Anything).
		Return([]*AggregatedSignal{
			{
				SignalType:      SignalTypeTechnical,
				Symbol:          "BTC/USDT",
				Confidence:      decimal.NewFromFloat(0.8),
				ProfitPotential: decimal.NewFromFloat(0.05), // 5%
				CreatedAt:       time.Now(),
			},
		}, nil)

	// Mock Scorer expectations
	mockScorer.On("AssessSignalQuality", mock.Anything, mock.Anything).
		Return(&SignalQualityMetrics{
			OverallScore:       decimal.NewFromFloat(0.85),
			ExchangeScore:      decimal.NewFromFloat(0.8),
			VolumeScore:        decimal.NewFromFloat(0.8),
			DataFreshnessScore: decimal.NewFromFloat(0.9),
		}, nil)

	// Execute
	result := sp.processSignal(marketData)

	// Assert
	assert.Nil(t, result.Error)
	assert.Equal(t, "BTC/USDT", result.Symbol)
	assert.True(t, result.Processed)
	assert.Equal(t, 0.85, result.QualityScore)

	// Verify mocks
	if err := mockPool.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expectations: %s", err)
	}
	mockAggregator.AssertExpectations(t)
	mockScorer.AssertExpectations(t)
}
