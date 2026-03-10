package services

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestNativeOrderExecutor_FormatTradeNotification_SuppressesZeroLeverageLabel(t *testing.T) {
	executor := NewNativeOrderExecutor(nil, "test-key", "test-secret")

	msg := executor.formatTradeNotification(TradeDetails{
		Exchange:          "bitget",
		Symbol:            "BTC/USDT",
		Side:              "buy",
		MarketType:        "futures",
		TradeType:         "scalping",
		Leverage:          0,
		EffectiveLeverage: 0,
		AmountUSDT:        decimal.NewFromInt(100),
	}, "paper-ord-1")

	assert.Contains(t, msg, "📍 Market: Futures")
	assert.NotContains(t, msg, "Futures (0x)")
	assert.NotContains(t, msg, "⚙️ Leverage:")
}
