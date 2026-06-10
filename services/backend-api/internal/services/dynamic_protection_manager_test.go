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
	price  decimal.Decimal
}

func (t *stubTicker) GetPrice() decimal.Decimal       { return t.price }
func (t *stubTicker) GetVolume() decimal.Decimal      { return decimal.Zero }
func (t *stubTicker) GetTimestamp() time.Time         { return time.Now().UTC() }
func (t *stubTicker) GetExchangeName() string         { return "bitget" }
func (t *stubTicker) GetSymbol() string               { return t.symbol }
func (t *stubTicker) GetBid() decimal.Decimal         { return t.price.Mul(decimal.NewFromFloat(0.999)) }
func (t *stubTicker) GetAsk() decimal.Decimal         { return t.price.Mul(decimal.NewFromFloat(1.001)) }
func (t *stubTicker) GetHigh() decimal.Decimal        { return t.price.Mul(decimal.NewFromFloat(1.01)) }
func (t *stubTicker) GetLow() decimal.Decimal         { return t.price.Mul(decimal.NewFromFloat(0.99)) }
func (t *stubTicker) GetPriceChange24h() float64      { return 0 }

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
		price:  decimal.NewFromFloat(price),
	}, nil
}

type stubProtectionSync struct {
	err error
}

func (s *stubProtectionSync) SyncPositionProtection(
	_ context.Context,
	_ string,
	_ ManagedOpenPosition,
	_ decimal.Decimal,
	_ decimal.Decimal,
) error {
	return s.err
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

func TestDynamicProtectionManager_ReconcileOpenPositions_SkipsDBUpdateWhenExchangeSyncFails(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "dynamic-protection-sync-fail.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store, err := NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)

	ctx := context.Background()
	entry := decimal.NewFromFloat(1.00)
	require.NoError(t, store.RecordOrderExecution(ctx, LifecycleExecutionRecord{
		OrderID:    "order-long-sync",
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
	}))

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
	manager.SetPositionProtectionSync(&stubProtectionSync{err: fmt.Errorf("exchange unavailable")})

	before, err := store.ListManagedOpenPositions(ctx, "chat-1", "bitget", 5)
	require.NoError(t, err)
	require.Len(t, before, 1)
	originalStop := before[0].StopLoss
	originalTake := before[0].TakeProfit

	summary, err := manager.ReconcileOpenPositions(ctx, "chat-1", "bitget")
	require.NoError(t, err)
	assert.Equal(t, 1, summary.PositionsEvaluated)
	assert.Equal(t, 0, summary.ProtectionsUpdated)
	assert.Equal(t, 1, summary.Errors)

	after, err := store.ListManagedOpenPositions(ctx, "chat-1", "bitget", 5)
	require.NoError(t, err)
	require.Len(t, after, 1)
	assert.True(t, after[0].StopLoss.Equal(originalStop))
	assert.True(t, after[0].TakeProfit.Equal(originalTake))
}

func TestDynamicProtectionManager_ReconcileOpenPositions_SkipsTinyProtectionAdjustments(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "dynamic-protection-min-adjustment.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store, err := NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)

	ctx := context.Background()
	entry := decimal.NewFromFloat(100.0)
	require.NoError(t, store.RecordOrderExecution(ctx, LifecycleExecutionRecord{
		OrderID:    "order-long-min-adjustment",
		ChatID:     "chat-1",
		Exchange:   "bitget",
		Symbol:     "ADA/USDT",
		Side:       "buy",
		OrderType:  "market",
		MarketType: "futures",
		Amount:     decimal.NewFromFloat(1),
		EntryPrice: entry,
		StopLoss:   decimal.NewFromFloat(100.20),
		TakeProfit: decimal.NewFromFloat(100.22),
		OpenedAt:   time.Now().UTC().Add(-2 * time.Minute),
	}))

	manager := NewDynamicProtectionManager(
		DynamicProtectionConfig{
			Enabled:               true,
			MaxPositions:          10,
			UpdateCooldown:        0,
			ProfitActivationPct:   0,
			BreakevenBufferPct:    0,
			TrailingStopPct:       0,
			TakeProfitDistancePct: 0,
			MinAdjustmentPct:      1.0,
		},
		store,
		&stubTickerSource{prices: map[string]float64{"ADA/USDT": 100.21}},
		nil,
	)

	summary, err := manager.ReconcileOpenPositions(ctx, "chat-1", "bitget")
	require.NoError(t, err)
	assert.Equal(t, 1, summary.PositionsEvaluated)
	assert.Equal(t, 0, summary.ProtectionsUpdated)
	assert.Equal(t, 0, summary.Errors)

	positions, err := store.ListManagedOpenPositions(ctx, "chat-1", "bitget", 5)
	require.NoError(t, err)
	require.Len(t, positions, 1)
	assert.True(t, positions[0].StopLoss.Equal(decimal.NewFromFloat(100.20)))
	assert.True(t, positions[0].TakeProfit.Equal(decimal.NewFromFloat(100.22)))
}

func TestDynamicProtectionManager_ReconcileOpenPositions_SkipsManualReconciliationPositions(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "dynamic-protection-skip-manual.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store, err := NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, store.RecordOrderExecution(ctx, LifecycleExecutionRecord{
		OrderID:    "manual-pos-1",
		ChatID:     "chat-1",
		Exchange:   "bitget",
		Symbol:     "ADA/USDT",
		Side:       "buy",
		OrderType:  "market",
		MarketType: "futures",
		Amount:     decimal.NewFromFloat(5),
		EntryPrice: decimal.NewFromFloat(1.00),
		Source:     "manual_reconciliation",
		OpenedAt:   time.Now().UTC().Add(-2 * time.Minute),
	}))

	manager := NewDynamicProtectionManager(
		DynamicProtectionConfig{
			Enabled:               true,
			MaxPositions:          10,
			UpdateCooldown:        0,
			ProfitActivationPct:   0,
			BreakevenBufferPct:    0,
			TrailingStopPct:       0,
			TakeProfitDistancePct: 0,
		},
		store,
		&stubTickerSource{prices: map[string]float64{"ADA/USDT": 1.03}},
		nil,
	)

	summary, err := manager.ReconcileOpenPositions(ctx, "chat-1", "bitget")
	require.NoError(t, err)
	assert.Equal(t, 0, summary.PositionsEvaluated)
	assert.Equal(t, 0, summary.ProtectionsUpdated)

	positions, err := store.ListManagedOpenPositions(ctx, "chat-1", "bitget", 10)
	require.NoError(t, err)
	require.Len(t, positions, 1)
	assert.True(t, positions[0].StopLoss.IsZero())
	assert.True(t, positions[0].TakeProfit.IsZero())
}
