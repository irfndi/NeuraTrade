package portfolio

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPositionApplyFill_AverageCostAndPartialClose(t *testing.T) {
	p := NewPosition("binance", "BTC/USDT")
	baseTime := time.Date(2026, time.January, 2, 10, 0, 0, 0, time.UTC)

	_, err := p.ApplyFill(Fill{
		Exchange:   "binance",
		Symbol:     "BTC/USDT",
		Side:       SideBuy,
		Quantity:   decimal.NewFromInt(1),
		Price:      decimal.NewFromInt(100),
		ExecutedAt: baseTime,
	})
	require.NoError(t, err)

	_, err = p.ApplyFill(Fill{
		Exchange:   "binance",
		Symbol:     "BTC/USDT",
		Side:       SideBuy,
		Quantity:   decimal.NewFromInt(1),
		Price:      decimal.NewFromInt(120),
		ExecutedAt: baseTime.Add(time.Minute),
	})
	require.NoError(t, err)
	assert.True(t, p.EntryPrice.Equal(decimal.NewFromInt(110)))

	res, err := p.ApplyFill(Fill{
		Exchange:   "binance",
		Symbol:     "BTC/USDT",
		Side:       SideSell,
		Quantity:   decimal.NewFromInt(1),
		Price:      decimal.NewFromInt(130),
		ExecutedAt: baseTime.Add(2 * time.Minute),
	})
	require.NoError(t, err)
	assert.False(t, res.Closed)
	assert.True(t, p.Quantity.Equal(decimal.NewFromInt(1)))
	assert.True(t, p.RealizedPnL.Equal(decimal.NewFromInt(20)))
	assert.True(t, p.EntryPrice.Equal(decimal.NewFromInt(110)))
	assert.True(t, p.UnrealizedPnL.Equal(decimal.NewFromInt(20)))
}

func TestPositionApplyFill_ShortAndReverse(t *testing.T) {
	p := NewPosition("bybit", "ETH/USDT")
	baseTime := time.Date(2026, time.January, 2, 11, 0, 0, 0, time.UTC)

	_, err := p.ApplyFill(Fill{
		Exchange:   "bybit",
		Symbol:     "ETH/USDT",
		Side:       SideSell,
		Quantity:   decimal.NewFromInt(2),
		Price:      decimal.NewFromInt(200),
		ExecutedAt: baseTime,
	})
	require.NoError(t, err)
	assert.Equal(t, PositionSideShort, p.Side())

	res, err := p.ApplyFill(Fill{
		Exchange:   "bybit",
		Symbol:     "ETH/USDT",
		Side:       SideBuy,
		Quantity:   decimal.NewFromInt(3),
		Price:      decimal.NewFromInt(180),
		ExecutedAt: baseTime.Add(time.Minute),
	})
	require.NoError(t, err)
	assert.True(t, res.Reversed)
	assert.True(t, p.Quantity.Equal(decimal.NewFromInt(1)))
	assert.True(t, p.EntryPrice.Equal(decimal.NewFromInt(180)))
	assert.True(t, p.RealizedPnL.Equal(decimal.NewFromInt(40)))
}

func TestPortfolioReconcile_SortsByExecutionTime(t *testing.T) {
	pf := NewPortfolio()
	baseTime := time.Now().UTC()

	fills := []Fill{
		{
			TradeID:    "2",
			Exchange:   "binance",
			Symbol:     "BTC/USDT",
			Side:       SideSell,
			Quantity:   decimal.NewFromInt(1),
			Price:      decimal.NewFromInt(120),
			ExecutedAt: baseTime.Add(2 * time.Minute),
		},
		{
			TradeID:    "1",
			Exchange:   "binance",
			Symbol:     "BTC/USDT",
			Side:       SideBuy,
			Quantity:   decimal.NewFromInt(2),
			Price:      decimal.NewFromInt(100),
			ExecutedAt: baseTime.Add(time.Minute),
		},
	}

	changes, err := pf.Reconcile(fills)
	require.NoError(t, err)
	assert.Len(t, changes, 2)

	pos, found := pf.GetPosition("binance", "BTC/USDT")
	require.True(t, found)
	assert.True(t, pos.Quantity.Equal(decimal.NewFromInt(1)))
	assert.True(t, pos.RealizedPnL.Equal(decimal.NewFromInt(20)))

	snapshot := pf.Snapshot()
	assert.True(t, snapshot.TotalExposure.Equal(decimal.NewFromInt(120)))
	assert.True(t, snapshot.TotalRealizedPnL.Equal(decimal.NewFromInt(20)))
}
