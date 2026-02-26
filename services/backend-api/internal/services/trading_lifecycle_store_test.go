package services

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/database"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTradingLifecycleStore_RecordOrderAndClose(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store, err := NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)

	ctx := context.Background()
	entry := decimal.NewFromFloat(1.25)
	openedAt := time.Now().UTC().Add(-2 * time.Minute)

	err = store.RecordOrderExecution(ctx, LifecycleExecutionRecord{
		OrderID:    "ord-1",
		ChatID:     "chat-1",
		Exchange:   "bitget",
		Symbol:     "DOGE/USDT",
		Side:       "buy",
		OrderType:  "market",
		MarketType: "futures",
		Amount:     decimal.NewFromInt(10),
		EntryPrice: entry,
		Source:     "test",
		OpenedAt:   openedAt,
	})
	require.NoError(t, err)

	var orderStatus string
	var orderChatID string
	err = sqliteDB.QueryRow(ctx, `SELECT status, chat_id FROM trading_orders WHERE order_id = $1`, "ord-1").Scan(&orderStatus, &orderChatID)
	require.NoError(t, err)
	assert.Equal(t, "open", orderStatus)
	assert.Equal(t, "chat-1", orderChatID)

	closedAt := time.Now().UTC()
	err = store.RecordClosedOrder(ctx, LifecycleCloseRecord{
		OrderID:     "ord-1",
		ChatID:      "chat-1",
		Exchange:    "bitget",
		Symbol:      "DOGE/USDT",
		Side:        "buy",
		MarketType:  "futures",
		Filled:      decimal.NewFromInt(10),
		EntryPrice:  entry,
		ExitPrice:   decimal.NewFromFloat(1.35),
		RealizedPnL: decimal.NewFromFloat(1.0),
		Fees:        decimal.NewFromFloat(0.01),
		Source:      "reconciliation",
		ClosedAt:    closedAt,
	})
	require.NoError(t, err)

	var positionStatus string
	var realizedPnL decimal.Decimal
	err = sqliteDB.QueryRow(ctx, `SELECT status, realized_pnl FROM trading_positions WHERE order_id = $1`, "ord-1").Scan(&positionStatus, &realizedPnL)
	require.NoError(t, err)
	assert.Equal(t, "closed", positionStatus)
	assert.True(t, realizedPnL.Equal(decimal.NewFromFloat(1.0)))

	var journalCount int
	err = sqliteDB.QueryRow(ctx, `SELECT COUNT(1) FROM realized_pnl_journal WHERE order_id = $1`, "ord-1").Scan(&journalCount)
	require.NoError(t, err)
	assert.Equal(t, 1, journalCount)
}

func TestTradingLifecycleStore_GetRealizedPerformanceAndOpenOrders(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "lifecycle-performance.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store, err := NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)
	ctx := context.Background()

	now := time.Now().UTC()
	require.NoError(t, store.RecordOrderExecution(ctx, LifecycleExecutionRecord{
		OrderID:    "open-ord-1",
		ChatID:     "chat-1",
		Exchange:   "bitget",
		Symbol:     "ADA/USDT",
		Side:       "buy",
		OrderType:  "market",
		MarketType: "futures",
		Amount:     decimal.NewFromFloat(5),
		EntryPrice: decimal.NewFromFloat(1.0),
		OpenedAt:   now.Add(-10 * time.Minute),
	}))

	require.NoError(t, store.RecordClosedOrder(ctx, LifecycleCloseRecord{
		OrderID:     "closed-ord-win",
		ChatID:      "chat-1",
		Exchange:    "bitget",
		Symbol:      "ADA/USDT",
		Side:        "buy",
		MarketType:  "futures",
		Filled:      decimal.NewFromFloat(5),
		EntryPrice:  decimal.NewFromFloat(1.0),
		ExitPrice:   decimal.NewFromFloat(1.03),
		RealizedPnL: decimal.NewFromFloat(0.15),
		ClosedAt:    now.Add(-20 * time.Minute),
	}))
	require.NoError(t, store.RecordClosedOrder(ctx, LifecycleCloseRecord{
		OrderID:     "closed-ord-loss",
		ChatID:      "chat-1",
		Exchange:    "bitget",
		Symbol:      "DOGE/USDT",
		Side:        "buy",
		MarketType:  "futures",
		Filled:      decimal.NewFromFloat(8),
		EntryPrice:  decimal.NewFromFloat(0.2),
		ExitPrice:   decimal.NewFromFloat(0.19),
		RealizedPnL: decimal.NewFromFloat(-0.08),
		ClosedAt:    now.Add(-30 * time.Minute),
	}))
	// Different chat, excluded by filter.
	require.NoError(t, store.RecordClosedOrder(ctx, LifecycleCloseRecord{
		OrderID:     "closed-ord-other-chat",
		ChatID:      "chat-2",
		Exchange:    "bitget",
		Symbol:      "SOL/USDT",
		Side:        "buy",
		MarketType:  "futures",
		Filled:      decimal.NewFromFloat(1),
		EntryPrice:  decimal.NewFromFloat(100),
		ExitPrice:   decimal.NewFromFloat(101),
		RealizedPnL: decimal.NewFromFloat(1),
		ClosedAt:    now.Add(-15 * time.Minute),
	}))

	openOrders, err := store.CountOpenOrders(ctx, "chat-1", "bitget")
	require.NoError(t, err)
	assert.Equal(t, 1, openOrders)

	perf, err := store.GetRealizedPerformance(ctx, "chat-1", "bitget", now.Add(-24*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 2, perf.Trades)
	assert.Equal(t, 1, perf.Wins)
	assert.Equal(t, 1, perf.Losses)
	assert.True(t, perf.RealizedPnL.Round(6).Equal(decimal.NewFromFloat(0.07)))
	assert.True(t, perf.BestTrade.Round(6).Equal(decimal.NewFromFloat(0.15)))
	assert.True(t, perf.WorstTrade.Round(6).Equal(decimal.NewFromFloat(-0.08)))
}

func TestTradingLifecycleStore_EnsureSchema_UpgradesLegacyTables(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "lifecycle-legacy.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	ctx := context.Background()
	_, err = sqliteDB.Exec(ctx, `
		CREATE TABLE trading_orders (
			order_id TEXT PRIMARY KEY,
			position_id TEXT NOT NULL,
			exchange TEXT NOT NULL,
			symbol TEXT NOT NULL,
			side TEXT NOT NULL,
			type TEXT NOT NULL,
			amount NUMERIC NOT NULL,
			price NUMERIC NOT NULL,
			status TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL
		)
	`)
	require.NoError(t, err)
	_, err = sqliteDB.Exec(ctx, `
		CREATE TABLE trading_positions (
			position_id TEXT PRIMARY KEY,
			order_id TEXT NOT NULL,
			exchange TEXT NOT NULL,
			symbol TEXT NOT NULL,
			side TEXT NOT NULL,
			size NUMERIC NOT NULL,
			entry_price NUMERIC NOT NULL,
			status TEXT NOT NULL,
			opened_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL
		)
	`)
	require.NoError(t, err)

	store, err := NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)
	require.NotNil(t, store)

	var count int
	err = sqliteDB.QueryRow(ctx, `SELECT COUNT(*) FROM pragma_table_info('trading_orders') WHERE name = 'chat_id'`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	err = sqliteDB.QueryRow(ctx, `SELECT COUNT(*) FROM pragma_table_info('trading_positions') WHERE name = 'stop_loss'`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}
