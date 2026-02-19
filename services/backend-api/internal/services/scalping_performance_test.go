package services

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestScalpingPerformance_RecordWinningTrade(t *testing.T) {
	sp := NewScalpingPerformance()

	record := TradeRecord{
		Timestamp:  time.Now(),
		Symbol:     "BTC/USDT",
		Side:       "buy",
		Amount:     decimal.NewFromFloat(10),
		PnL:        decimal.NewFromFloat(50),
		Profitable: true,
	}

	sp.RecordTrade(record)

	perf := sp.GetPerformance()
	assert.Equal(t, 1, perf["total_trades"])
	assert.Equal(t, 1, perf["profitable_trades"])
	assert.Equal(t, 0, perf["losing_trades"])
	assert.Equal(t, 1.0, perf["win_rate"])
}

func TestScalpingPerformance_RecordLosingTrade(t *testing.T) {
	sp := NewScalpingPerformance()

	record := TradeRecord{
		Timestamp:  time.Now(),
		Symbol:     "ETH/USDT",
		Side:       "sell",
		Amount:     decimal.NewFromFloat(5),
		PnL:        decimal.NewFromFloat(-25),
		Profitable: false,
	}

	sp.RecordTrade(record)

	perf := sp.GetPerformance()
	assert.Equal(t, 1, perf["total_trades"])
	assert.Equal(t, 0, perf["profitable_trades"])
	assert.Equal(t, 1, perf["losing_trades"])
	assert.Equal(t, 0.0, perf["win_rate"])
}

func TestScalpingPerformance_ConsecutiveWins(t *testing.T) {
	sp := NewScalpingPerformance()

	for i := 0; i < 3; i++ {
		sp.RecordTrade(TradeRecord{
			Timestamp:  time.Now(),
			Symbol:     "BTC/USDT",
			PnL:        decimal.NewFromFloat(10),
			Profitable: true,
		})
	}

	perf := sp.GetPerformance()
	assert.Equal(t, 3, perf["consecutive_wins"])
	assert.Equal(t, 0, perf["consecutive_losses"])
}

func TestScalpingPerformance_ConsecutiveLosses(t *testing.T) {
	sp := NewScalpingPerformance()

	for i := 0; i < 4; i++ {
		sp.RecordTrade(TradeRecord{
			Timestamp:  time.Now(),
			Symbol:     "BTC/USDT",
			PnL:        decimal.NewFromFloat(-10),
			Profitable: false,
		})
	}

	perf := sp.GetPerformance()
	assert.Equal(t, 0, perf["consecutive_wins"])
	assert.Equal(t, 4, perf["consecutive_losses"])
}

func TestScalpingPerformance_GetAdjustedParameters_NoTrades(t *testing.T) {
	sp := NewScalpingPerformance()

	cfg := sp.GetAdjustedParameters()

	assert.Equal(t, DefaultScalpingConfig.MaxConcurrentPositions, cfg.MaxConcurrentPositions)
	assert.Equal(t, DefaultScalpingConfig.MinProfitPercent, cfg.MinProfitPercent)
}

func TestScalpingPerformance_GetAdjustedParameters_TightenOnLosses(t *testing.T) {
	sp := NewScalpingPerformance()

	// Need at least 10 trades before adjustment kicks in
	// First, record 10 losing trades to meet the threshold and get consecutive losses
	for i := 0; i < 10; i++ {
		sp.RecordTrade(TradeRecord{
			Timestamp:  time.Now(),
			Symbol:     "BTC/USDT",
			PnL:        decimal.NewFromFloat(-10),
			Profitable: false,
		})
	}

	// Now get adjusted parameters - should tighten (higher threshold = more conservative)
	// After 10 consecutive losses, MinProfitPercent should be multiplied by 1.5
	cfg := sp.GetAdjustedParameters()

	// Tightening means HIGHER profit threshold required (harder to trade)
	assert.Greater(t, cfg.MinProfitPercent, DefaultScalpingConfig.MinProfitPercent)
}

func TestScalpingPerformance_GetAdjustedParameters_LoosenOnWins(t *testing.T) {
	sp := NewScalpingPerformance()

	// Need 10+ trades, so record 10 winning trades
	for i := 0; i < 10; i++ {
		sp.RecordTrade(TradeRecord{
			Timestamp:  time.Now(),
			Symbol:     "BTC/USDT",
			PnL:        decimal.NewFromFloat(10),
			Profitable: true,
		})
	}

	// Now get adjusted parameters - should loosen (lower threshold = more aggressive)
	cfg := sp.GetAdjustedParameters()

	// Loosening means LOWER profit threshold (easier to trade)
	assert.Less(t, cfg.MinProfitPercent, DefaultScalpingConfig.MinProfitPercent)
}

func TestScalpingPerformance_LowWinRateTightens(t *testing.T) {
	sp := NewScalpingPerformance()

	// Need 20+ trades with low win rate for the win rate adjustment
	// Record 5 wins and 15 losses (25% win rate, less than 30% threshold)
	for i := 0; i < 5; i++ {
		sp.RecordTrade(TradeRecord{
			Timestamp:  time.Now(),
			PnL:        decimal.NewFromFloat(10),
			Profitable: true,
		})
	}
	for i := 0; i < 15; i++ {
		sp.RecordTrade(TradeRecord{
			Timestamp:  time.Now(),
			PnL:        decimal.NewFromFloat(-10),
			Profitable: false,
		})
	}

	// Now with low win rate and 20+ trades, should tighten dramatically
	cfg := sp.GetAdjustedParameters()

	// Low win rate = higher threshold required
	assert.Greater(t, cfg.MinProfitPercent, DefaultScalpingConfig.MinProfitPercent)
}

func TestScalpingPerformance_HighWinRateAggressive(t *testing.T) {
	sp := NewScalpingPerformance()

	// Need 20+ trades with high win rate
	// Record 14 wins and 6 losses (70% win rate)
	for i := 0; i < 14; i++ {
		sp.RecordTrade(TradeRecord{
			Timestamp:  time.Now(),
			PnL:        decimal.NewFromFloat(10),
			Profitable: true,
		})
	}
	for i := 0; i < 6; i++ {
		sp.RecordTrade(TradeRecord{
			Timestamp:  time.Now(),
			PnL:        decimal.NewFromFloat(-10),
			Profitable: false,
		})
	}

	// Now with high win rate and 20+ trades, should be more aggressive
	cfg := sp.GetAdjustedParameters()

	// High win rate = lower threshold (more aggressive)
	assert.Less(t, cfg.MinProfitPercent, DefaultScalpingConfig.MinProfitPercent)
}

func TestScalpingPerformance_HistoryLimit(t *testing.T) {
	sp := NewScalpingPerformance()

	// Record 150 trades (should keep last 100)
	for i := 0; i < 150; i++ {
		sp.RecordTrade(TradeRecord{
			Timestamp:  time.Now(),
			Symbol:     "BTC/USDT",
			PnL:        decimal.NewFromFloat(10),
			Profitable: true,
		})
	}

	perf := sp.GetPerformance()
	assert.Equal(t, 150, perf["total_trades"])
}

func TestRecordScalpingTrade(t *testing.T) {
	globalScalpingPerformance = NewScalpingPerformance()

	ctx := context.Background()
	RecordScalpingTrade(ctx, "BTC/USDT", "buy", decimal.NewFromFloat(1), decimal.NewFromFloat(50000), decimal.NewFromFloat(50100), true)

	perf := GetScalpingPerformance().GetPerformance()
	assert.Equal(t, 1, perf["total_trades"])
	assert.Equal(t, 1, perf["profitable_trades"])
}
