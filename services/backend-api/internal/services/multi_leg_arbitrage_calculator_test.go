package services

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestMultiLegArbitrageCalculator_FindTriangularOpportunities(t *testing.T) {
	calc := NewMultiLegArbitrageCalculator(nil, decimal.NewFromFloat(0.001))

	// Mock tickers that form a profitable triangle
	// BTC/USDT: Ask 50000
	// ETH/BTC: Ask 0.05
	// ETH/USDT: Bid 2600
	// path: USDT -> BTC -> ETH -> USDT
	// 100 USDT -> 100/50000 BTC = 0.002 BTC
	// 0.002 BTC -> 0.002/0.05 ETH = 0.04 ETH
	// 0.04 ETH -> 0.04 * 2600 USDT = 104 USDT
	// Profit: ~4% (before fees)

	tickers := []TickerData{
		{Symbol: "BTC/USDT", Bid: decimal.NewFromInt(49000), Ask: decimal.NewFromInt(50000)},
		{Symbol: "ETH/BTC", Bid: decimal.NewFromFloat(0.04), Ask: decimal.NewFromFloat(0.05)},
		{Symbol: "ETH/USDT", Bid: decimal.NewFromInt(2600), Ask: decimal.NewFromInt(2700)},
	}

	opportunities, err := calc.FindTriangularOpportunities(context.Background(), "binance", tickers)
	assert.NoError(t, err)
	assert.NotEmpty(t, opportunities)

	// Verify the most profitable one
	foundProfit := false
	for _, opp := range opportunities {
		if opp.ProfitPercentage.GreaterThan(decimal.NewFromInt(1)) {
			foundProfit = true
			assert.Equal(t, 3, len(opp.Legs))
			break
		}
	}
	assert.True(t, foundProfit, "Should find at least one profitable opportunity")
}

func TestMultiLegArbitrageCalculator_CalculateTriangularOpp_Negative(t *testing.T) {
	calc := NewMultiLegArbitrageCalculator(nil, decimal.NewFromFloat(0.001))

	// Mock tickers that are NOT profitable
	tickers := []TickerData{
		{Symbol: "BTC/USDT", Bid: decimal.NewFromInt(50000), Ask: decimal.NewFromInt(51000)},
		{Symbol: "ETH/BTC", Bid: decimal.NewFromFloat(0.04), Ask: decimal.NewFromFloat(0.05)},
		{Symbol: "ETH/USDT", Bid: decimal.NewFromInt(2000), Ask: decimal.NewFromInt(2100)},
	}

	tickerMap := make(map[string]TickerData)
	for _, t := range tickers {
		tickerMap[t.Symbol] = t
	}

	opp, err := calc.calculateTriangularOpp(context.Background(), "binance", "USDT", "BTC", "ETH", tickerMap)
	assert.NoError(t, err)
	assert.True(t, opp.ProfitPercentage.LessThan(decimal.Zero), "Should be negative profit")
}
