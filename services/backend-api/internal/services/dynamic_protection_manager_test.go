package services

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubTicker struct {
	symbol string
	price  float64
}

func (t *stubTicker) GetPrice() float64       { return t.price }
func (t *stubTicker) GetVolume() float64      { return 0 }
func (t *stubTicker) GetTimestamp() time.Time { return time.Now().UTC() }
func (t *stubTicker) GetExchangeName() string { return "bitget" }
func (t *stubTicker) GetSymbol() string       { return t.symbol }
func (t *stubTicker) GetBid() float64         { return t.price * 0.999 }
func (t *stubTicker) GetAsk() float64         { return t.price * 1.001 }
func (t *stubTicker) GetHigh() float64        { return t.price * 1.01 }
func (t *stubTicker) GetLow() float64         { return t.price * 0.99 }

type stubTickerSource struct {
	prices map[string]float64
}

func (s *stubTickerSource) FetchSingleTicker(_ context.Context, _ string, symbol string) (ccxt.MarketPriceInterface, error) {
	price, ok := s.prices[symbol]
	if !ok {
		return nil, fmt.Errorf("ticker not found: %s", symbol)
	}
	return &stubTicker{
		symbol: symbol,
		price:  price,
	}, nil
}

func TestDynamicProtectionManager_ReconcileOpenPositions_TightensLongProtection(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "dynamic-protection-1.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store, err := NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)

	ctx := context.Background()
	entry := decimal.NewFromFloat(1.00)
	err = store.RecordOrderExecution(ctx, LifecycleExecutionRecord{
		OrderID:    "order-long-1",
		ChatID:     "chat-1",
		Exchange:   "bitget",
		Symbol:     "ADA/USDT",
		Side:       "buy",
		OrderType:  "market",
		MarketType: "futures",
		Amount:     decimal.NewFromFloat(10),
		EntryPrice: entry,
		StopLoss:   decimal.NewFromFloat(0.992),
		TakeProfit: decimal.NewFromFloat(1.012),
		OpenedAt:   time.Now().UTC().Add(-2 * time.Minute),
	})
	require.NoError(t, err)

	manager := NewDynamicProtectionManager(
		DynamicProtectionConfig{
			Enabled:               true,
			MaxPositions:          10,
			UpdateCooldown:        0,
			ProfitActivationPct:   0.20,
			BreakevenBufferPct:    0.05,
			TrailingStopPct:       0.40,
			TakeProfitDistancePct: 0.50,
		},
		store,
		&stubTickerSource{prices: map[string]float64{"ADA/USDT": 1.03}},
		nil,
	)

	summary, err := manager.ReconcileOpenPositions(ctx, "chat-1", "bitget")
	require.NoError(t, err)
	assert.Equal(t, 1, summary.PositionsEvaluated)
	assert.Equal(t, 1, summary.ProtectionsUpdated)
	assert.Equal(t, 0, summary.Errors)

	positions, err := store.ListManagedOpenPositions(ctx, "chat-1", "bitget", 5)
	require.NoError(t, err)
	require.Len(t, positions, 1)

	updated := positions[0]
	assert.True(t, updated.StopLoss.GreaterThan(entry), "stop loss should be tightened above entry")
	assert.True(t, updated.TakeProfit.GreaterThan(decimal.NewFromFloat(1.03)), "take profit should trail above current price")
	assert.True(t, updated.LastPrice.Equal(decimal.NewFromFloat(1.03)))
	assert.True(t, updated.UnrealizedPnL.GreaterThan(decimal.Zero))
}

func TestDynamicProtectionManager_ReconcileOpenPositions_RespectsCooldown(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "dynamic-protection-2.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store, err := NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)

	ctx := context.Background()
	entry := decimal.NewFromFloat(1.00)
	err = store.RecordOrderExecution(ctx, LifecycleExecutionRecord{
		OrderID:    "order-long-2",
		ChatID:     "chat-1",
		Exchange:   "bitget",
		Symbol:     "ADA/USDT",
		Side:       "buy",
		OrderType:  "market",
		MarketType: "futures",
		Amount:     decimal.NewFromFloat(10),
		EntryPrice: entry,
		StopLoss:   decimal.NewFromFloat(1.001),
		TakeProfit: decimal.NewFromFloat(1.02),
		OpenedAt:   time.Now().UTC(),
	})
	require.NoError(t, err)

	positions, err := store.ListManagedOpenPositions(ctx, "chat-1", "bitget", 5)
	require.NoError(t, err)
	require.Len(t, positions, 1)

	originalStop := positions[0].StopLoss
	originalTake := positions[0].TakeProfit

	manager := NewDynamicProtectionManager(
		DynamicProtectionConfig{
			Enabled:               true,
			MaxPositions:          10,
			UpdateCooldown:        5 * time.Minute,
			ProfitActivationPct:   0.10,
			BreakevenBufferPct:    0.05,
			TrailingStopPct:       0.40,
			TakeProfitDistancePct: 0.50,
		},
		store,
		&stubTickerSource{prices: map[string]float64{"ADA/USDT": 1.05}},
		nil,
	)

	summary, err := manager.ReconcileOpenPositions(ctx, "chat-1", "bitget")
	require.NoError(t, err)
	assert.Equal(t, 1, summary.PositionsEvaluated)
	assert.Equal(t, 0, summary.ProtectionsUpdated)

	after, err := store.ListManagedOpenPositions(ctx, "chat-1", "bitget", 5)
	require.NoError(t, err)
	require.Len(t, after, 1)
	assert.True(t, after[0].StopLoss.Equal(originalStop))
	assert.True(t, after[0].TakeProfit.Equal(originalTake))
}
