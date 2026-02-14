package services

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestMarketDataQualityFilter_CheckQuality_StaleData(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultMarketDataQualityConfig()
	config.StaleDataThresholdSeconds = 60

	filter := NewMarketDataQualityFilter(config, logger)

	// Test with stale data (2 minutes old)
	staleTime := time.Now().Add(-2 * time.Minute)
	result := filter.CheckQuality(
		context.Background(),
		"BTC/USDT",
		"binance",
		decimal.NewFromInt(50000),
		decimal.NewFromInt(1000),
		staleTime,
	)

	assert.Contains(t, result.Flags, QualityFlagStaleData)
	assert.True(t, result.AgeSeconds > 60)
}

func TestMarketDataQualityFilter_CheckQuality_PriceOutlier(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultMarketDataQualityConfig()
	config.PriceMoveThresholdPercent = decimal.NewFromFloat(0.1) // 10%

	filter := NewMarketDataQualityFilter(config, logger)

	now := time.Now()
	oldTime := now.Add(-30 * time.Second)

	// First, add a price point
	filter.CheckQuality(
		context.Background(),
		"BTC/USDT",
		"binance",
		decimal.NewFromInt(50000),
		decimal.NewFromInt(1000),
		oldTime,
	)

	// Now add a price that's 20% higher (should trigger outlier)
	result := filter.CheckQuality(
		context.Background(),
		"BTC/USDT",
		"binance",
		decimal.NewFromInt(60000), // 20% higher
		decimal.NewFromInt(1000),
		now,
	)

	assert.Contains(t, result.Flags, QualityFlagPriceOutlier)
	assert.NotNil(t, result.PriceChange)
}

func TestMarketDataQualityFilter_CheckQuality_ValidPrice(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultMarketDataQualityConfig()

	filter := NewMarketDataQualityFilter(config, logger)

	now := time.Now()
	oldTime := now.Add(-30 * time.Second)

	// First price
	filter.CheckQuality(
		context.Background(),
		"BTC/USDT",
		"binance",
		decimal.NewFromInt(50000),
		decimal.NewFromInt(1000),
		oldTime,
	)

	// Second price with small change (2%)
	result := filter.CheckQuality(
		context.Background(),
		"BTC/USDT",
		"binance",
		decimal.NewFromInt(51000), // 2% higher
		decimal.NewFromInt(1000),
		now,
	)

	assert.Contains(t, result.Flags, QualityFlagOK)
	assert.NotContains(t, result.Flags, QualityFlagPriceOutlier)
}

func TestMarketDataQualityFilter_CheckQuality_VolumeAnomaly(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultMarketDataQualityConfig()
	config.VolumeAnomalyThresholdPercent = decimal.NewFromFloat(2.0) // 2x average
	config.MinVolumeSamples = 3

	filter := NewMarketDataQualityFilter(config, logger)

	now := time.Now()

	// Add normal volume samples
	for i := 0; i < 3; i++ {
		filter.CheckQuality(
			context.Background(),
			"BTC/USDT",
			"binance",
			decimal.NewFromInt(50000),
			decimal.NewFromInt(1000),
			now.Add(time.Duration(-i)*time.Minute),
		)
	}

	// Now add a volume that's 5x the average
	result := filter.CheckQuality(
		context.Background(),
		"BTC/USDT",
		"binance",
		decimal.NewFromInt(50000),
		decimal.NewFromInt(5000), // 5x average
		now,
	)

	assert.Contains(t, result.Flags, QualityFlagVolumeAnomaly)
	assert.NotNil(t, result.VolumeRatio)
}

func TestMarketDataQualityFilter_CheckCrossExchange(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultMarketDataQualityConfig()
	config.CrossExchangeMaxDiffPercent = decimal.NewFromFloat(0.05) // 5%
	config.ReferenceExchange = "binance"

	filter := NewMarketDataQualityFilter(config, logger)

	now := time.Now()

	// Add price on reference exchange (binance)
	filter.CheckQuality(
		context.Background(),
		"BTC/USDT",
		"binance",
		decimal.NewFromInt(50000),
		decimal.NewFromInt(1000),
		now,
	)

	// Check price on another exchange with small diff (3%)
	result := filter.CheckCrossExchange(
		context.Background(),
		"BTC/USDT",
		"bybit",
		decimal.NewFromInt(51500), // 3% diff
	)

	assert.NotNil(t, result)
	assert.True(t, result.IsValid)
	assert.Equal(t, "0.03", result.DiffPercent.StringFixed(2))
}

func TestMarketDataQualityFilter_CheckCrossExchange_Invalid(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultMarketDataQualityConfig()
	config.CrossExchangeMaxDiffPercent = decimal.NewFromFloat(0.05) // 5%
	config.ReferenceExchange = "binance"

	filter := NewMarketDataQualityFilter(config, logger)

	now := time.Now()

	// Add price on reference exchange
	filter.CheckQuality(
		context.Background(),
		"BTC/USDT",
		"binance",
		decimal.NewFromInt(50000),
		decimal.NewFromInt(1000),
		now,
	)

	// Check price with large diff (10%)
	result := filter.CheckCrossExchange(
		context.Background(),
		"BTC/USDT",
		"bybit",
		decimal.NewFromInt(55000), // 10% diff
	)

	assert.NotNil(t, result)
	assert.False(t, result.IsValid)
}

func TestMarketDataQualityFilter_GetStats(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultMarketDataQualityConfig()

	filter := NewMarketDataQualityFilter(config, logger)

	// Add some data
	filter.CheckQuality(
		context.Background(),
		"BTC/USDT",
		"binance",
		decimal.NewFromInt(50000),
		decimal.NewFromInt(1000),
		time.Now(),
	)

	stats := filter.GetStats()

	assert.Equal(t, 1, stats["tracked_symbols"])
	assert.Equal(t, 1, stats["volume_stats"])
}

func TestMarketDataQualityFilter_Reset(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultMarketDataQualityConfig()

	filter := NewMarketDataQualityFilter(config, logger)

	// Add some data
	filter.CheckQuality(
		context.Background(),
		"BTC/USDT",
		"binance",
		decimal.NewFromInt(50000),
		decimal.NewFromInt(1000),
		time.Now(),
	)

	// Reset
	filter.Reset()

	stats := filter.GetStats()
	assert.Equal(t, 0, stats["tracked_symbols"])
	assert.Equal(t, 0, stats["volume_stats"])
}
