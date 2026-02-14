package services

import (
	"testing"

	"github.com/irfndi/neuratrade/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestMarketContext_WithRegime(t *testing.T) {
	regime := &models.MarketRegime{
		Symbol:          "BTC/USDT",
		Exchange:        "binance",
		Trend:           "bullish",
		Volatility:      "normal",
		TrendStrength:   0.03,
		VolatilityScore: 0.4,
		Confidence:      0.8,
		WindowSize:      60,
	}

	ctx := MarketContext{
		Symbol:       "BTC/USDT",
		CurrentPrice: 50000.0,
		Volatility:   0.4,
		Trend:        "bullish",
		Liquidity:    1000000,
		Regime:       regime,
	}

	assert.NotNil(t, ctx.Regime)
	assert.Equal(t, "bullish", ctx.Regime.Trend)
	assert.Equal(t, "normal", ctx.Regime.Volatility)
	assert.Greater(t, ctx.Regime.Confidence, 0.5)
}

func TestMarketContext_WithoutRegime(t *testing.T) {
	ctx := MarketContext{
		Symbol:       "ETH/USDT",
		CurrentPrice: 3000.0,
		Volatility:   0.5,
		Trend:        "neutral",
		Liquidity:    500000,
		Regime:       nil,
	}

	assert.Nil(t, ctx.Regime)
}

func TestTradingDecision_WithRegimeContext(t *testing.T) {
	decision := TradingDecision{
		ID:          "dec-123",
		Symbol:      "BTC/USDT",
		Action:      ActionOpenLong,
		Side:        SideLong,
		SizePercent: 10.0,
		EntryPrice:  50000.0,
		StopLoss:    49500.0,
		TakeProfit:  51000.0,
		Confidence:  0.85,
		Reasoning:   "Bullish regime, strong trend",
	}

	assert.Equal(t, ActionOpenLong, decision.Action)
	assert.Equal(t, SideLong, decision.Side)
	assert.Contains(t, decision.Reasoning, "Bullish")
}
