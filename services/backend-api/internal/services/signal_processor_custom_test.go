package services

import (
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/database"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/irfndi/neuratrade/internal/logging"
	"github.com/irfndi/neuratrade/internal/models"
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
		WithArgs("BTC/USDT", pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "trading_pair_id", "buy_exchange_id", "sell_exchange_id",
			"buy_price", "sell_price", "profit_percentage", "detected_at", "expires_at",
		})) // Empty rows means no opportunities

	// 5. getTradingPairSymbol (called inside generateTechnicalSignals)
	mockPool.ExpectQuery("SELECT symbol FROM trading_pairs WHERE id = \\$1").
		WithArgs(1).
		WillReturnRows(pgxmock.NewRows([]string{"symbol"}).AddRow("BTC/USDT"))

	// 6. getExchangeName (called inside generateTechnicalSignals)
	mockPool.ExpectQuery("SELECT name FROM exchanges WHERE id = \\$1").
		WithArgs(1).
		WillReturnRows(pgxmock.NewRows([]string{"name"}).AddRow("binance"))

	// 7. OHLCV candles query (called inside generateTechnicalSignals)
	ohlcvRows := pgxmock.NewRows([]string{"open", "high", "low", "close", "volume", "timestamp"})
	for i := 0; i < 60; i++ {
		price := 50000.0 + float64(i)*10.0
		ohlcvRows.AddRow(price, price+50, price-50, price+20, 1000.0, time.Now().Add(-time.Duration(60-i)*time.Minute))
	}
	mockPool.ExpectQuery("SELECT open, high, low, close, volume, timestamp").
		WithArgs(1, 1).
		WillReturnRows(ohlcvRows)

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

func TestSignalProcessor_OHLCVBoundaryCases(t *testing.T) {
	tests := []struct {
		name       string
		arbitrage  bool
		assertions func(t *testing.T, sp *SignalProcessor, marketData models.MarketData)
	}{
		{
			name:      "generate technical signals requires fifty candles",
			arbitrage: false,
			assertions: func(t *testing.T, sp *SignalProcessor, marketData models.MarketData) {
				_, err := sp.generateTechnicalSignals(marketData)
				require.Error(t, err)
				assert.Contains(t, err.Error(), "need 50, got 49")
			},
		},
		{
			name:      "process signal allows arbitrage when ohlcv history is short",
			arbitrage: true,
			assertions: func(t *testing.T, sp *SignalProcessor, marketData models.MarketData) {
				result := sp.processSignal(marketData)
				assert.NoError(t, result.Error)
				assert.True(t, result.Processed)
				assert.Equal(t, SignalTypeArbitrage, result.SignalType)
				assert.Equal(t, 0.9, result.QualityScore)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockPool, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mockPool.Close()

			var aggregator SignalAggregatorInterface
			var scorer SignalQualityScorerInterface
			if tt.arbitrage {
				mockAggregator := &MockSignalAggregator{}
				mockScorer := &MockSignalQualityScorer{}
				aggregator = mockAggregator
				scorer = mockScorer
				mockAggregator.On("AggregateArbitrageSignals", mock.Anything, mock.Anything).
					Return([]*AggregatedSignal{{
						SignalType:      SignalTypeArbitrage,
						Symbol:          "BTC/USDT",
						Confidence:      decimal.NewFromFloat(0.85),
						ProfitPotential: decimal.NewFromFloat(0.05),
						CreatedAt:       time.Now(),
					}}, nil)
				mockScorer.On("AssessSignalQuality", mock.Anything, mock.Anything).
					Return(&SignalQualityMetrics{OverallScore: decimal.NewFromFloat(0.9)}, nil)
				defer mockAggregator.AssertExpectations(t)
				defer mockScorer.AssertExpectations(t)
			}

			dbPool := database.NewMockDBPool(mockPool)
			var logger logging.Logger = logging.NewStandardLogger("info", "test")
			sp := NewSignalProcessor(dbPool, logger, aggregator, scorer, nil, nil, nil, nil)
			marketData := models.MarketData{
				TradingPairID: 1,
				ExchangeID:    1,
				LastPrice:     decimal.NewFromFloat(50000),
				Volume24h:     decimal.NewFromFloat(1000),
				Timestamp:     time.Now(),
			}

			mockPool.ExpectQuery("SELECT symbol FROM trading_pairs WHERE id = \\$1").
				WithArgs(1).
				WillReturnRows(pgxmock.NewRows([]string{"symbol"}).AddRow("BTC/USDT"))
			mockPool.ExpectQuery("SELECT name FROM exchanges WHERE id = \\$1").
				WithArgs(1).
				WillReturnRows(pgxmock.NewRows([]string{"name"}).AddRow("binance"))

			if tt.arbitrage {
				mockPool.ExpectQuery("SELECT symbol FROM trading_pairs WHERE id = \\$1").
					WithArgs(1).
					WillReturnRows(pgxmock.NewRows([]string{"symbol"}).AddRow("BTC/USDT"))
				arbRows := pgxmock.NewRows([]string{
					"id", "trading_pair_id", "buy_exchange_id", "sell_exchange_id",
					"buy_price", "sell_price", "profit_percentage", "detected_at", "expires_at",
				}).AddRow(
					"arb-1", 1, 1, 2,
					decimal.NewFromFloat(49900), decimal.NewFromFloat(50150), decimal.NewFromFloat(0.8),
					time.Now(), time.Now().Add(time.Minute),
				)
				mockPool.ExpectQuery("SELECT .* FROM arbitrage_opportunities .*").
					WithArgs("BTC/USDT", pgxmock.AnyArg()).
					WillReturnRows(arbRows)
				mockPool.ExpectQuery("SELECT symbol FROM trading_pairs WHERE id = \\$1").
					WithArgs(1).
					WillReturnRows(pgxmock.NewRows([]string{"symbol"}).AddRow("BTC/USDT"))
				mockPool.ExpectQuery("SELECT name FROM exchanges WHERE id = \\$1").
					WithArgs(1).
					WillReturnRows(pgxmock.NewRows([]string{"name"}).AddRow("binance"))
			}

			ohlcvRows := pgxmock.NewRows([]string{"open", "high", "low", "close", "volume", "timestamp"})
			for i := 0; i < 49; i++ {
				price := 50000.0 + float64(i)*10.0
				ohlcvRows.AddRow(price, price+50, price-50, price+20, 1000.0, time.Now().Add(-time.Duration(49-i)*time.Minute))
			}
			mockPool.ExpectQuery("SELECT open, high, low, close, volume, timestamp").
				WithArgs(1, 1).
				WillReturnRows(ohlcvRows)

			tt.assertions(t, sp, marketData)
			assert.NoError(t, mockPool.ExpectationsWereMet())
		})
	}
}
