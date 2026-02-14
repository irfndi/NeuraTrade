package services

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestShadowModeEngine_EnableDisable(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultShadowModeConfig()

	engine := NewShadowModeEngine(config, logger)

	assert.False(t, engine.IsEnabled())

	engine.Enable()
	assert.True(t, engine.IsEnabled())

	engine.Disable()
	assert.False(t, engine.IsEnabled())
}

func TestShadowModeEngine_ExecuteBuyTrade(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultShadowModeConfig()
	config.InitialCapital = decimal.NewFromInt(100000)

	engine := NewShadowModeEngine(config, logger)
	engine.Enable()

	trade, err := engine.ExecuteTrade(context.Background(), "BTC/USDT", "buy", decimal.NewFromInt(1), decimal.NewFromInt(50000))

	assert.NoError(t, err)
	assert.NotNil(t, trade)
	assert.Equal(t, "BTC/USDT", trade.Symbol)
	assert.Equal(t, "buy", trade.Side)

	portfolio := engine.GetPortfolio()
	assert.Equal(t, "49925", portfolio.Cash.String())
}

func TestShadowModeEngine_ExecuteSellTrade(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultShadowModeConfig()
	config.InitialCapital = decimal.NewFromInt(100000)

	engine := NewShadowModeEngine(config, logger)
	engine.Enable()

	_, err := engine.ExecuteTrade(context.Background(), "BTC/USDT", "buy", decimal.NewFromInt(1), decimal.NewFromInt(50000))
	assert.NoError(t, err)

	trade, err := engine.ExecuteTrade(context.Background(), "BTC/USDT", "sell", decimal.NewFromInt(1), decimal.NewFromInt(55000))

	assert.NoError(t, err)
	assert.NotNil(t, trade)
	assert.Equal(t, "sell", trade.Side)
}

func TestShadowModeEngine_InsufficientFunds(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultShadowModeConfig()
	config.InitialCapital = decimal.NewFromInt(40000)

	engine := NewShadowModeEngine(config, logger)
	engine.Enable()

	_, err := engine.ExecuteTrade(context.Background(), "BTC/USDT", "buy", decimal.NewFromInt(1), decimal.NewFromInt(50000))

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient funds")
}

func TestShadowModeEngine_UpdatePrices(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultShadowModeConfig()
	config.InitialCapital = decimal.NewFromInt(100000)

	engine := NewShadowModeEngine(config, logger)
	engine.Enable()

	engine.ExecuteTrade(context.Background(), "BTC/USDT", "buy", decimal.NewFromInt(1), decimal.NewFromInt(50000))

	prices := map[string]decimal.Decimal{
		"BTC/USDT": decimal.NewFromInt(55000),
	}
	engine.UpdatePrices(prices)

	portfolio := engine.GetPortfolio()
	assert.True(t, portfolio.UnrealizedPNL.GreaterThan(decimal.Zero))
}

func TestShadowModeEngine_GetTrades(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultShadowModeConfig()
	config.InitialCapital = decimal.NewFromInt(200000)

	engine := NewShadowModeEngine(config, logger)
	engine.Enable()

	engine.ExecuteTrade(context.Background(), "BTC/USDT", "buy", decimal.NewFromInt(1), decimal.NewFromInt(50000))
	engine.ExecuteTrade(context.Background(), "ETH/USDT", "buy", decimal.NewFromInt(10), decimal.NewFromInt(3000))

	trades := engine.GetTrades(10)
	assert.Equal(t, 2, len(trades))
}

func TestShadowModeEngine_Reset(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultShadowModeConfig()
	config.InitialCapital = decimal.NewFromInt(100000)

	engine := NewShadowModeEngine(config, logger)
	engine.Enable()

	engine.ExecuteTrade(context.Background(), "BTC/USDT", "buy", decimal.NewFromInt(1), decimal.NewFromInt(50000))

	engine.Reset()

	portfolio := engine.GetPortfolio()
	assert.Equal(t, decimal.NewFromInt(100000), portfolio.Cash)
	assert.Equal(t, 0, len(portfolio.Positions))
}

func TestShadowModeEngine_GetStats(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultShadowModeConfig()
	config.InitialCapital = decimal.NewFromInt(100000)

	engine := NewShadowModeEngine(config, logger)

	stats := engine.GetStats()

	assert.Equal(t, false, stats["enabled"])
	assert.Equal(t, "100000", stats["cash"])
}
