package services

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTradingLifecycleStore_RestartAndManualDriftSync(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "restart-drift-1.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store, err := NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)

	ctx := context.Background()
	order := ccxt.Order{
		ID:        "restart-ord-1",
		Symbol:    "ADA/USDT",
		Side:      "buy",
		Type:      "market",
		Amount:    decimal.NewFromFloat(10),
		Price:     decimal.NewFromFloat(1.01),
		CreatedAt: time.Now().UTC().Add(-5 * time.Minute),
	}
	require.NoError(t, store.SyncOpenOrder(ctx, "chat-1", "bitget", order))

	// Simulate restart by creating a new lifecycle store against the same DB.
	restartedStore, err := NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)
	order.Amount = decimal.NewFromFloat(12)
	order.Price = decimal.NewFromFloat(1.02)
	require.NoError(t, restartedStore.SyncOpenOrder(ctx, "chat-1", "bitget", order))

	var orderCount int
	var amount decimal.Decimal
	var price decimal.Decimal
	err = sqliteDB.QueryRow(ctx, `SELECT COUNT(*), amount, price FROM trading_orders WHERE order_id = $1`, "restart-ord-1").
		Scan(&orderCount, &amount, &price)
	require.NoError(t, err)
	assert.Equal(t, 1, orderCount)
	assert.True(t, amount.Equal(decimal.NewFromFloat(12)))
	assert.True(t, price.Equal(decimal.NewFromFloat(1.02)))

	require.NoError(t, restartedStore.RecordClosedOrder(ctx, LifecycleCloseRecord{
		OrderID:     "restart-ord-1",
		ChatID:      "chat-1",
		Exchange:    "bitget",
		Symbol:      "ADA/USDT",
		Side:        "buy",
		MarketType:  "futures",
		Filled:      decimal.NewFromFloat(12),
		EntryPrice:  decimal.NewFromFloat(1.02),
		ExitPrice:   decimal.NewFromFloat(1.00),
		RealizedPnL: decimal.NewFromFloat(-0.24),
		Fees:        decimal.NewFromFloat(0.01),
		Source:      "manual_reconciliation",
		ClosedAt:    time.Now().UTC(),
	}))

	// Replaying manual-drift reconciliation should update in-place, not duplicate.
	require.NoError(t, restartedStore.RecordClosedOrder(ctx, LifecycleCloseRecord{
		OrderID:     "restart-ord-1",
		ChatID:      "chat-1",
		Exchange:    "bitget",
		Symbol:      "ADA/USDT",
		Side:        "buy",
		MarketType:  "futures",
		Filled:      decimal.NewFromFloat(12),
		EntryPrice:  decimal.NewFromFloat(1.02),
		ExitPrice:   decimal.NewFromFloat(1.01),
		RealizedPnL: decimal.NewFromFloat(-0.12),
		Fees:        decimal.NewFromFloat(0.02),
		Source:      "manual_reconciliation",
		ClosedAt:    time.Now().UTC(),
	}))

	var journalRows int
	var realized decimal.Decimal
	err = sqliteDB.QueryRow(ctx, `SELECT COUNT(*), COALESCE(MAX(realized_pnl), 0) FROM realized_pnl_journal WHERE order_id = $1`, "restart-ord-1").
		Scan(&journalRows, &realized)
	require.NoError(t, err)
	assert.Equal(t, 1, journalRows)
	assert.True(t, realized.Equal(decimal.NewFromFloat(-0.12)))
}

func TestTradingLifecycleStore_ListManagedOpenPositions_ScopesByChatAndExchange(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "restart-drift-2.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store, err := NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)
	ctx := context.Background()

	require.NoError(t, store.RecordOrderExecution(ctx, LifecycleExecutionRecord{
		OrderID:    "chat1-bitget",
		ChatID:     "chat-1",
		Exchange:   "bitget",
		Symbol:     "ADA/USDT",
		Side:       "buy",
		OrderType:  "market",
		MarketType: "futures",
		Amount:     decimal.NewFromFloat(5),
		EntryPrice: decimal.NewFromFloat(1.0),
	}))
	require.NoError(t, store.RecordOrderExecution(ctx, LifecycleExecutionRecord{
		OrderID:    "chat2-bitget",
		ChatID:     "chat-2",
		Exchange:   "bitget",
		Symbol:     "ADA/USDT",
		Side:       "buy",
		OrderType:  "market",
		MarketType: "futures",
		Amount:     decimal.NewFromFloat(5),
		EntryPrice: decimal.NewFromFloat(1.0),
	}))
	require.NoError(t, store.RecordOrderExecution(ctx, LifecycleExecutionRecord{
		OrderID:    "chat1-binance",
		ChatID:     "chat-1",
		Exchange:   "binance",
		Symbol:     "ADA/USDT",
		Side:       "buy",
		OrderType:  "market",
		MarketType: "futures",
		Amount:     decimal.NewFromFloat(5),
		EntryPrice: decimal.NewFromFloat(1.0),
	}))

	positions, err := store.ListManagedOpenPositions(ctx, "chat-1", "bitget", 10)
	require.NoError(t, err)
	require.Len(t, positions, 1)
	assert.Equal(t, "chat-1", positions[0].ChatID)
	assert.Equal(t, "bitget", positions[0].Exchange)
	assert.Equal(t, "chat1-bitget", positions[0].OrderID)
}
