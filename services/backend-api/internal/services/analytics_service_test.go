package services

import (
	"context"
	"testing"
	"time"

	"github.com/irfandi/celebrum-ai-go/internal/config"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
)

func TestAnalyticsService_CalculateCorrelationMatrix(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	assert.NoError(t, err)
	defer mockPool.Close()

	cfg := config.AnalyticsConfig{
		EnableCorrelation:    true,
		CorrelationWindow:    10,
		CorrelationMinPoints: 5,
	}
	service := NewAnalyticsServiceWithQuerier(mockPool, cfg)

	symbols := []string{"BTC/USDT", "ETH/USDT"}
	exchange := "binance"

	// Mock data for BTC/USDT
	mockPool.ExpectQuery("SELECT md.last_price, md.timestamp").
		WithArgs("BTC/USDT", "binance", 10).
		WillReturnRows(pgxmock.NewRows([]string{"last_price", "timestamp"}).
			AddRow(100.0, time.Now().Add(-9*time.Hour)).
			AddRow(102.0, time.Now().Add(-8*time.Hour)).
			AddRow(104.0, time.Now().Add(-7*time.Hour)).
			AddRow(103.0, time.Now().Add(-6*time.Hour)).
			AddRow(105.0, time.Now().Add(-5*time.Hour)))

	// Mock data for ETH/USDT
	mockPool.ExpectQuery("SELECT md.last_price, md.timestamp").
		WithArgs("ETH/USDT", "binance", 10).
		WillReturnRows(pgxmock.NewRows([]string{"last_price", "timestamp"}).
			AddRow(1000.0, time.Now().Add(-9*time.Hour)).
			AddRow(1010.0, time.Now().Add(-8*time.Hour)).
			AddRow(1020.0, time.Now().Add(-7*time.Hour)).
			AddRow(1015.0, time.Now().Add(-6*time.Hour)).
			AddRow(1030.0, time.Now().Add(-5*time.Hour)))

	matrix, err := service.CalculateCorrelationMatrix(context.Background(), exchange, symbols, 10)
	assert.NoError(t, err)
	assert.NotNil(t, matrix)
	assert.Equal(t, exchange, matrix.Exchange)
	assert.Equal(t, 2, len(matrix.Symbols))
	assert.Equal(t, 2, len(matrix.Matrix))

	// Check diagonal
	assert.InDelta(t, 1.0, matrix.Matrix[0][0], 0.0001)
	assert.InDelta(t, 1.0, matrix.Matrix[1][1], 0.0001)

	// Check correlation between BTC and ETH (they are positively correlated in this mock data)
	assert.Greater(t, matrix.Matrix[0][1], 0.0)
	assert.InDelta(t, matrix.Matrix[0][1], matrix.Matrix[1][0], 0.0001)
}

func TestAnalyticsService_DetectMarketRegime(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	assert.NoError(t, err)
	defer mockPool.Close()

	cfg := config.AnalyticsConfig{
		EnableRegimeDetection:   true,
		RegimeShortWindow:       5,
		RegimeLongWindow:        10,
		VolatilityHighThreshold: 0.03,
		VolatilityLowThreshold:  0.005,
	}
	service := NewAnalyticsServiceWithQuerier(mockPool, cfg)

	symbol := "BTC/USDT"
	exchange := "binance"

	// Mock data for a bullish trend
	rows := pgxmock.NewRows([]string{"last_price", "timestamp"})
	basePrice := 100.0
	for i := 0; i < 15; i++ {
		// Increasing price over time (T-14h to T)
		// Row 0 is latest (T), Row 14 is oldest (T-14h)
		// We want Row 0 to have HIGHEST price, Row 14 to have LOWEST
		rows.AddRow(basePrice+float64(15-i), time.Now().Add(time.Duration(-i)*time.Hour))
	}

	mockPool.ExpectQuery("SELECT md.last_price, md.timestamp").
		WithArgs(symbol, exchange, 10).
		WillReturnRows(rows)

	regime, err := service.DetectMarketRegime(context.Background(), exchange, symbol, 10)
	assert.NoError(t, err)
	assert.NotNil(t, regime)
	assert.Equal(t, "bullish", regime.Trend)
	assert.Greater(t, regime.Confidence, 0.0)
}

func TestAnalyticsService_ForecastSeries(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	assert.NoError(t, err)
	defer mockPool.Close()

	cfg := config.AnalyticsConfig{
		EnableForecasting: true,
		ForecastLookback:  10,
		ForecastHorizon:   3,
	}
	service := NewAnalyticsServiceWithQuerier(mockPool, cfg)

	symbol := "BTC/USDT"
	exchange := "binance"

	// Mock data for price series
	rows := pgxmock.NewRows([]string{"last_price", "timestamp"})
	now := time.Now()
	for i := 0; i < 15; i++ {
		rows.AddRow(100.0+float64(i), now.Add(time.Duration(-i)*time.Hour))
	}

	mockPool.ExpectQuery("SELECT md.last_price, md.timestamp").
		WithArgs(symbol, exchange, 10).
		WillReturnRows(rows)

	result, err := service.ForecastSeries(context.Background(), exchange, symbol, "price", 3, 10)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 3, len(result.Points))
	assert.Equal(t, "AR(1)+GARCH(1,1)", result.Model)
}
