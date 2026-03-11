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

func TestTradingLifecycleStore_GetRealizedReturnSeries(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "lifecycle-returns.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store, err := NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)

	ctx := context.Background()
	now := time.Now().UTC()
	require.NoError(t, store.RecordClosedOrder(ctx, LifecycleCloseRecord{
		OrderID:     "ret-1",
		ChatID:      "chat-1",
		Exchange:    "bitget",
		Symbol:      "BTC/USDT",
		Side:        "buy",
		MarketType:  "futures",
		Filled:      decimal.NewFromFloat(0.01),
		EntryPrice:  decimal.NewFromFloat(50000),
		ExitPrice:   decimal.NewFromFloat(50500),
		RealizedPnL: decimal.NewFromFloat(5),
		ClosedAt:    now.Add(-20 * time.Minute),
	}))
	require.NoError(t, store.RecordClosedOrder(ctx, LifecycleCloseRecord{
		OrderID:     "ret-2",
		ChatID:      "chat-1",
		Exchange:    "bitget",
		Symbol:      "ETH/USDT",
		Side:        "buy",
		MarketType:  "futures",
		Filled:      decimal.NewFromFloat(0.5),
		EntryPrice:  decimal.NewFromFloat(2000),
		ExitPrice:   decimal.NewFromFloat(1980),
		RealizedPnL: decimal.NewFromFloat(-10),
		ClosedAt:    now.Add(-10 * time.Minute),
	}))

	returns, err := store.GetRealizedReturnSeries(ctx, "chat-1", "bitget", now.Add(-24*time.Hour))
	require.NoError(t, err)
	require.Len(t, returns, 2)
	assert.True(t, returns[0].Round(6).Equal(decimal.NewFromFloat(0.01)))
	assert.True(t, returns[1].Round(6).Equal(decimal.NewFromFloat(-0.01)))
}

func TestTradingLifecycleStore_GetRealizedReturnSeries_FeeAdjustedAndGross(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "lifecycle-returns-fees.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store, err := NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)

	ctx := context.Background()
	now := time.Now().UTC()
	require.NoError(t, store.RecordClosedOrder(ctx, LifecycleCloseRecord{
		OrderID:     "ret-fee-1",
		ChatID:      "chat-1",
		Exchange:    "bitget",
		Symbol:      "BTC/USDT",
		Side:        "buy",
		MarketType:  "futures",
		Filled:      decimal.NewFromFloat(0.01),
		EntryPrice:  decimal.NewFromFloat(50000),
		ExitPrice:   decimal.NewFromFloat(50500),
		RealizedPnL: decimal.NewFromFloat(5),
		Fees:        decimal.NewFromFloat(-1),
		ClosedAt:    now.Add(-5 * time.Minute),
	}))

	netReturns, err := store.GetRealizedReturnSeries(ctx, "chat-1", "bitget", now.Add(-24*time.Hour))
	require.NoError(t, err)
	require.Len(t, netReturns, 1)
	assert.True(t, netReturns[0].Round(6).Equal(decimal.NewFromFloat(0.008)))

	grossReturns, err := store.GetGrossRealizedReturnSeries(ctx, "chat-1", "bitget", now.Add(-24*time.Hour))
	require.NoError(t, err)
	require.Len(t, grossReturns, 1)
	assert.True(t, grossReturns[0].Round(6).Equal(decimal.NewFromFloat(0.01)))
}

func TestTradingLifecycleStore_RecordClosedOrder_ClosesSyncRowInPlace(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "lifecycle-sync-close.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store, err := NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)
	ctx := context.Background()

	position := ccxt.Position{
		Symbol:        "UNI/USDT",
		Side:          "long",
		Size:          decimal.NewFromFloat(10),
		EntryPrice:    decimal.NewFromFloat(4.10),
		MarkPrice:     decimal.NewFromFloat(4.00),
		UnrealizedPnl: decimal.NewFromFloat(-1.0),
		Timestamp:     ccxt.UnixTimestamp(time.Now().UTC()),
	}
	require.NoError(t, store.SyncPosition(ctx, "chat-1", "bitget", position))

	require.NoError(t, store.RecordClosedOrder(ctx, LifecycleCloseRecord{
		OrderID:     "sync-bitget-uni-usdt-long",
		ChatID:      "chat-1",
		Exchange:    "bitget",
		Symbol:      "UNI/USDT",
		Side:        "buy",
		MarketType:  "futures",
		Filled:      decimal.NewFromFloat(10),
		EntryPrice:  decimal.NewFromFloat(4.10),
		ExitPrice:   decimal.NewFromFloat(4.00),
		RealizedPnL: decimal.Zero,
		Source:      "startup_drift_repair_exchange_missing",
		ClosedAt:    time.Now().UTC(),
	}))

	var status string
	err = sqliteDB.QueryRow(ctx, `
		SELECT LOWER(status)
		FROM trading_positions
		WHERE position_id = 'sync-bitget-uni-usdt-long'
	`).Scan(&status)
	require.NoError(t, err)
	assert.Equal(t, "closed", status)

	var dupCount int
	err = sqliteDB.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM trading_positions
		WHERE position_id LIKE 'pos-sync-bitget-uni-usdt-long%'
	`).Scan(&dupCount)
	require.NoError(t, err)
	assert.Equal(t, 0, dupCount)

	// Replaying the same close event should remain idempotent.
	require.NoError(t, store.RecordClosedOrder(ctx, LifecycleCloseRecord{
		OrderID:     "sync-bitget-uni-usdt-long",
		ChatID:      "chat-1",
		Exchange:    "bitget",
		Symbol:      "UNI/USDT",
		Side:        "buy",
		MarketType:  "futures",
		Filled:      decimal.NewFromFloat(10),
		EntryPrice:  decimal.NewFromFloat(4.10),
		ExitPrice:   decimal.NewFromFloat(4.01),
		RealizedPnL: decimal.Zero,
		Source:      "startup_drift_repair_exchange_missing",
		ClosedAt:    time.Now().UTC(),
	}))

	var rowCount int
	err = sqliteDB.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM trading_positions
		WHERE position_id = 'sync-bitget-uni-usdt-long'
	`).Scan(&rowCount)
	require.NoError(t, err)
	assert.Equal(t, 1, rowCount)
}

func TestTradingLifecycleStore_SyncPosition_AssignsDefaultProtectionForNewSyncRows(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "lifecycle-sync-default-protection.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store, err := NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)
	ctx := context.Background()

	require.NoError(t, store.SyncPosition(ctx, "chat-1", "bitget", ccxt.Position{
		Symbol:        "ADA/USDT",
		Side:          "long",
		Size:          decimal.NewFromFloat(5),
		EntryPrice:    decimal.NewFromFloat(1.00),
		MarkPrice:     decimal.NewFromFloat(1.02),
		UnrealizedPnl: decimal.NewFromFloat(0.10),
		Timestamp:     ccxt.UnixTimestamp(time.Now().UTC()),
	}))

	var stopLoss decimal.Decimal
	var takeProfit decimal.Decimal
	err = sqliteDB.QueryRow(ctx, `
		SELECT stop_loss, take_profit
		FROM trading_positions
		WHERE position_id = 'sync-bitget-ada-usdt-long'
	`).Scan(&stopLoss, &takeProfit)
	require.NoError(t, err)
	assert.True(t, stopLoss.GreaterThan(decimal.Zero))
	assert.True(t, takeProfit.GreaterThan(decimal.Zero))
	assert.True(t, stopLoss.LessThan(decimal.NewFromFloat(1.00)))
	assert.True(t, takeProfit.GreaterThan(decimal.NewFromFloat(1.00)))
}

func TestTradingLifecycleStore_SyncPosition_PreservesExistingProtection(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "lifecycle-sync-preserve-protection.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store, err := NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)
	ctx := context.Background()

	require.NoError(t, store.SyncPosition(ctx, "chat-1", "bitget", ccxt.Position{
		Symbol:        "ADA/USDT",
		Side:          "long",
		Size:          decimal.NewFromFloat(5),
		EntryPrice:    decimal.NewFromFloat(1.00),
		MarkPrice:     decimal.NewFromFloat(1.02),
		UnrealizedPnl: decimal.NewFromFloat(0.10),
		Timestamp:     ccxt.UnixTimestamp(time.Now().UTC()),
	}))

	require.NoError(t, store.UpdatePositionProtection(
		ctx,
		"sync-bitget-ada-usdt-long",
		decimal.NewFromFloat(0.97),
		decimal.NewFromFloat(1.04),
		decimal.NewFromFloat(1.02),
		decimal.NewFromFloat(0.10),
		time.Now().UTC(),
	))

	require.NoError(t, store.SyncPosition(ctx, "chat-1", "bitget", ccxt.Position{
		Symbol:        "ADA/USDT",
		Side:          "long",
		Size:          decimal.NewFromFloat(5),
		EntryPrice:    decimal.NewFromFloat(1.00),
		MarkPrice:     decimal.NewFromFloat(1.03),
		UnrealizedPnl: decimal.NewFromFloat(0.15),
		Timestamp:     ccxt.UnixTimestamp(time.Now().UTC()),
	}))

	var stopLoss decimal.Decimal
	var takeProfit decimal.Decimal
	err = sqliteDB.QueryRow(ctx, `
		SELECT stop_loss, take_profit
		FROM trading_positions
		WHERE position_id = 'sync-bitget-ada-usdt-long'
	`).Scan(&stopLoss, &takeProfit)
	require.NoError(t, err)
	assert.True(t, stopLoss.Equal(decimal.NewFromFloat(0.97)))
	assert.True(t, takeProfit.Equal(decimal.NewFromFloat(1.04)))
}

func TestTradingLifecycleStore_SyncPosition_PreservesAutonomousSource(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "lifecycle-sync-preserve-source.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store, err := NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)
	ctx := context.Background()

	require.NoError(t, store.RecordOrderExecution(ctx, LifecycleExecutionRecord{
		OrderID:    "ord-autonomous-src",
		ChatID:     "chat-1",
		Exchange:   "bitget",
		Symbol:     "ADA/USDT",
		Side:       "buy",
		OrderType:  "market",
		MarketType: "futures",
		Amount:     decimal.NewFromFloat(5),
		EntryPrice: decimal.NewFromFloat(1.0),
		Source:     "autonomous",
	}))

	require.NoError(t, store.SyncPosition(ctx, "chat-1", "bitget", ccxt.Position{
		Symbol:        "ADA/USDT",
		Side:          "long",
		Size:          decimal.NewFromFloat(5),
		EntryPrice:    decimal.NewFromFloat(1.0),
		MarkPrice:     decimal.NewFromFloat(1.01),
		UnrealizedPnl: decimal.NewFromFloat(0.05),
		Timestamp:     ccxt.UnixTimestamp(time.Now().UTC()),
	}))

	var source string
	err = sqliteDB.QueryRow(ctx, `
		SELECT source
		FROM trading_positions
		WHERE order_id = 'ord-autonomous-src'
		  AND LOWER(status) = 'open'
	`).Scan(&source)
	require.NoError(t, err)
	assert.Equal(t, "autonomous", source)
}

func TestTradingLifecycleStore_RecordClosedOrder_PrefersOpenSyncRowOverLegacyOrderMapping(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "lifecycle-sync-ordermap.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store, err := NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)
	ctx := context.Background()

	syncID := "sync-bitget-ada-usdt-long"
	require.NoError(t, store.SyncPosition(ctx, "chat-1", "bitget", ccxt.Position{
		Symbol:        "ADA/USDT",
		Side:          "long",
		Size:          decimal.NewFromFloat(8),
		EntryPrice:    decimal.NewFromFloat(0.30),
		MarkPrice:     decimal.NewFromFloat(0.29),
		UnrealizedPnl: decimal.NewFromFloat(-0.08),
		Timestamp:     ccxt.UnixTimestamp(time.Now().UTC()),
	}))

	// Simulate legacy/incorrect mapping row from an earlier bad close path.
	_, err = sqliteDB.Exec(ctx, `
		INSERT INTO trading_orders (
			order_id, position_id, chat_id, exchange, symbol, side, type, market_type,
			amount, price, filled_amount, status, source, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,'market','futures',$7,$8,$9,'closed',$10,$11,$12)
		ON CONFLICT(order_id) DO UPDATE SET
			position_id = EXCLUDED.position_id,
			status = EXCLUDED.status,
			source = EXCLUDED.source,
			updated_at = EXCLUDED.updated_at
	`,
		syncID,
		"pos-"+syncID,
		"chat-1",
		"bitget",
		"ADA/USDT",
		"buy",
		decimal.NewFromFloat(8),
		decimal.NewFromFloat(0.30),
		decimal.NewFromFloat(8),
		"adaptive_time_stop_close_exchange_missing",
		time.Now().UTC(),
		time.Now().UTC(),
	)
	require.NoError(t, err)

	require.NoError(t, store.RecordClosedOrder(ctx, LifecycleCloseRecord{
		OrderID:     syncID,
		ChatID:      "chat-1",
		Exchange:    "bitget",
		Symbol:      "ADA/USDT",
		Side:        "buy",
		MarketType:  "futures",
		Filled:      decimal.NewFromFloat(8),
		EntryPrice:  decimal.NewFromFloat(0.30),
		ExitPrice:   decimal.NewFromFloat(0.29),
		RealizedPnL: decimal.Zero,
		Source:      "state_drift_deadlock_clear",
		ClosedAt:    time.Now().UTC(),
	}))

	var syncStatus string
	err = sqliteDB.QueryRow(ctx, `
		SELECT LOWER(status)
		FROM trading_positions
		WHERE position_id = $1
	`, syncID).Scan(&syncStatus)
	require.NoError(t, err)
	assert.Equal(t, "closed", syncStatus)

	var openSync int
	err = sqliteDB.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM trading_positions
		WHERE position_id = $1
		  AND LOWER(status) = 'open'
	`, syncID).Scan(&openSync)
	require.NoError(t, err)
	assert.Equal(t, 0, openSync)

	var openPosSync int
	err = sqliteDB.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM trading_positions
		WHERE position_id = $1
		  AND LOWER(status) = 'open'
	`, "pos-"+syncID).Scan(&openPosSync)
	require.NoError(t, err)
	assert.Equal(t, 0, openPosSync)
}

func TestTradingLifecycleStore_RepairMissingSyncPositions_ClosesMissingRows(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "lifecycle-sync-repair.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store, err := NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)
	ctx := context.Background()

	require.NoError(t, store.SyncPosition(ctx, "chat-1", "bitget", ccxt.Position{
		Symbol:        "ADA/USDT",
		Side:          "long",
		Size:          decimal.NewFromFloat(20),
		EntryPrice:    decimal.NewFromFloat(0.30),
		MarkPrice:     decimal.NewFromFloat(0.29),
		UnrealizedPnl: decimal.NewFromFloat(-0.2),
		Timestamp:     ccxt.UnixTimestamp(time.Now().UTC()),
	}))
	require.NoError(t, store.SyncPosition(ctx, "chat-1", "bitget", ccxt.Position{
		Symbol:        "UNI/USDT",
		Side:          "short",
		Size:          decimal.NewFromFloat(5),
		EntryPrice:    decimal.NewFromFloat(4.00),
		MarkPrice:     decimal.NewFromFloat(3.98),
		UnrealizedPnl: decimal.NewFromFloat(0.1),
		Timestamp:     ccxt.UnixTimestamp(time.Now().UTC()),
	}))

	closed, err := store.RepairMissingSyncPositions(ctx, "chat-1", "bitget", []ccxt.Position{}, "startup_drift_repair_exchange_missing")
	require.NoError(t, err)
	assert.Equal(t, 2, closed)

	var openRows int
	err = sqliteDB.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM trading_positions
		WHERE LOWER(status) = 'open'
		  AND chat_id = 'chat-1'
		  AND exchange = 'bitget'
	`).Scan(&openRows)
	require.NoError(t, err)
	assert.Equal(t, 0, openRows)
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

func TestTradingLifecycleStore_ReconcileExchangeSnapshot_ClosesStaleState(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "lifecycle-reconcile-fresh.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store, err := NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)
	ctx := context.Background()

	openedAt := time.Now().UTC().Add(-15 * time.Minute)
	require.NoError(t, store.RecordOrderExecution(ctx, LifecycleExecutionRecord{
		OrderID:    "ord-keep",
		ChatID:     "chat-1",
		Exchange:   "bitget",
		Symbol:     "BTC/USDT",
		Side:       "buy",
		OrderType:  "market",
		MarketType: "futures",
		Amount:     decimal.NewFromFloat(0.02),
		EntryPrice: decimal.NewFromFloat(50000),
		OpenedAt:   openedAt,
	}))
	require.NoError(t, store.RecordOrderExecution(ctx, LifecycleExecutionRecord{
		OrderID:    "ord-stale",
		ChatID:     "chat-1",
		Exchange:   "bitget",
		Symbol:     "ETH/USDT",
		Side:       "buy",
		OrderType:  "market",
		MarketType: "futures",
		Amount:     decimal.NewFromFloat(0.5),
		EntryPrice: decimal.NewFromFloat(3000),
		OpenedAt:   openedAt,
	}))

	summary, err := store.ReconcileExchangeSnapshot(ctx, "chat-1", "bitget", LifecycleExchangeSnapshot{
		OpenOrders: []ccxt.Order{
			{
				ID:        "ord-keep",
				Symbol:    "BTC/USDT",
				Side:      "buy",
				Type:      "market",
				Amount:    decimal.NewFromFloat(0.02),
				Price:     decimal.NewFromFloat(50000),
				CreatedAt: time.Now().UTC().Add(-2 * time.Minute),
			},
		},
		Positions: []ccxt.Position{
			{
				Symbol:        "BTC/USDT",
				Side:          "long",
				Size:          decimal.NewFromFloat(0.02),
				EntryPrice:    decimal.NewFromFloat(50000),
				MarkPrice:     decimal.NewFromFloat(50100),
				UnrealizedPnl: decimal.NewFromFloat(2),
				Timestamp:     ccxt.UnixTimestamp(time.Now().UTC()),
			},
		},
		OrdersFresh:    true,
		PositionsFresh: true,
	}, "bootstrap_reconciliation")
	require.NoError(t, err)
	assert.Equal(t, 1, summary.OrdersSynced)
	assert.Equal(t, 1, summary.PositionsSynced)
	assert.Equal(t, 1, summary.OrdersCancelled)
	assert.Equal(t, 1, summary.PositionsClosed)

	var staleOrderStatus string
	err = sqliteDB.QueryRow(ctx, `SELECT LOWER(status) FROM trading_orders WHERE order_id = $1`, "ord-stale").Scan(&staleOrderStatus)
	require.NoError(t, err)
	assert.Equal(t, "closed", staleOrderStatus)

	var stalePosStatus string
	err = sqliteDB.QueryRow(ctx, `SELECT LOWER(status) FROM trading_positions WHERE order_id = $1`, "ord-stale").Scan(&stalePosStatus)
	require.NoError(t, err)
	assert.Equal(t, "closed", stalePosStatus)

	var journalRows int
	err = sqliteDB.QueryRow(ctx, `SELECT COUNT(*) FROM realized_pnl_journal WHERE order_id = $1`, "ord-stale").Scan(&journalRows)
	require.NoError(t, err)
	assert.Equal(t, 1, journalRows)
}

func TestTradingLifecycleStore_ReconcileExchangeSnapshot_DoesNotCloseOnStaleFetch(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "lifecycle-reconcile-stale.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store, err := NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)
	ctx := context.Background()

	require.NoError(t, store.RecordOrderExecution(ctx, LifecycleExecutionRecord{
		OrderID:    "ord-open",
		ChatID:     "chat-1",
		Exchange:   "bitget",
		Symbol:     "SOL/USDT",
		Side:       "buy",
		OrderType:  "market",
		MarketType: "futures",
		Amount:     decimal.NewFromFloat(1),
		EntryPrice: decimal.NewFromFloat(100),
		OpenedAt:   time.Now().UTC().Add(-20 * time.Minute),
	}))

	summary, err := store.ReconcileExchangeSnapshot(ctx, "chat-1", "bitget", LifecycleExchangeSnapshot{
		OrdersFresh:    false,
		PositionsFresh: false,
	}, "bootstrap_reconciliation")
	require.NoError(t, err)
	assert.Equal(t, 0, summary.OrdersCancelled)
	assert.Equal(t, 0, summary.PositionsClosed)

	var orderStatus string
	var posStatus string
	err = sqliteDB.QueryRow(ctx, `SELECT LOWER(status) FROM trading_orders WHERE order_id = $1`, "ord-open").Scan(&orderStatus)
	require.NoError(t, err)
	assert.Equal(t, "open", orderStatus)
	err = sqliteDB.QueryRow(ctx, `SELECT LOWER(status) FROM trading_positions WHERE order_id = $1`, "ord-open").Scan(&posStatus)
	require.NoError(t, err)
	assert.Equal(t, "open", posStatus)
}

func TestTradingLifecycleStore_ReconcileExchangeSnapshot_DoesNotCloseWhenPositionsSnapshotIsStale(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "lifecycle-reconcile-stale-positions.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store, err := NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)
	ctx := context.Background()

	require.NoError(t, store.RecordOrderExecution(ctx, LifecycleExecutionRecord{
		OrderID:    "ord-open-stale-pos",
		ChatID:     "chat-1",
		Exchange:   "bitget",
		Symbol:     "SOL/USDT",
		Side:       "buy",
		OrderType:  "market",
		MarketType: "futures",
		Amount:     decimal.NewFromFloat(1),
		EntryPrice: decimal.NewFromFloat(100),
		OpenedAt:   time.Now().UTC().Add(-20 * time.Minute),
	}))

	summary, err := store.ReconcileExchangeSnapshot(ctx, "chat-1", "bitget", LifecycleExchangeSnapshot{
		OrdersFresh:    true,
		PositionsFresh: false,
	}, "bootstrap_reconciliation")
	require.NoError(t, err)
	assert.Equal(t, 0, summary.PositionsClosed)

	var orderStatus string
	var posStatus string
	err = sqliteDB.QueryRow(ctx, `SELECT LOWER(status) FROM trading_orders WHERE order_id = $1`, "ord-open-stale-pos").Scan(&orderStatus)
	require.NoError(t, err)
	assert.Equal(t, "open", orderStatus)
	err = sqliteDB.QueryRow(ctx, `SELECT LOWER(status) FROM trading_positions WHERE order_id = $1`, "ord-open-stale-pos").Scan(&posStatus)
	require.NoError(t, err)
	assert.Equal(t, "open", posStatus)
}

func TestTradingLifecycleStore_ReconcileExchangeSnapshot_KeepsRowsBackedByOpenOrders(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "lifecycle-reconcile-open-order.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store, err := NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)
	ctx := context.Background()

	require.NoError(t, store.RecordOrderExecution(ctx, LifecycleExecutionRecord{
		OrderID:    "ord-pending",
		ChatID:     "chat-1",
		Exchange:   "bitget",
		Symbol:     "XRP/USDT",
		Side:       "buy",
		OrderType:  "limit",
		MarketType: "futures",
		Amount:     decimal.NewFromFloat(100),
		EntryPrice: decimal.NewFromFloat(0.6),
		OpenedAt:   time.Now().UTC().Add(-5 * time.Minute),
	}))

	summary, err := store.ReconcileExchangeSnapshot(ctx, "chat-1", "bitget", LifecycleExchangeSnapshot{
		OpenOrders: []ccxt.Order{
			{
				ID:        "ord-pending",
				Symbol:    "XRP/USDT",
				Side:      "buy",
				Type:      "limit",
				Amount:    decimal.NewFromFloat(100),
				Price:     decimal.NewFromFloat(0.6),
				CreatedAt: time.Now().UTC().Add(-4 * time.Minute),
			},
		},
		OrdersFresh:    true,
		PositionsFresh: true,
	}, "bootstrap_reconciliation")
	require.NoError(t, err)
	assert.Equal(t, 1, summary.OrdersSynced)
	assert.Equal(t, 0, summary.OrdersCancelled)
	assert.Equal(t, 0, summary.PositionsClosed)

	var posStatus string
	err = sqliteDB.QueryRow(ctx, `SELECT LOWER(status) FROM trading_positions WHERE order_id = $1`, "ord-pending").Scan(&posStatus)
	require.NoError(t, err)
	assert.Equal(t, "open", posStatus)
}

func TestTradingLifecycleStore_ReconcileExchangeSnapshot_ClosesExcessRowsBySize(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "lifecycle-reconcile-size.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store, err := NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)
	ctx := context.Background()

	require.NoError(t, store.RecordOrderExecution(ctx, LifecycleExecutionRecord{
		OrderID:    "ord-a",
		ChatID:     "chat-1",
		Exchange:   "bitget",
		Symbol:     "BTC/USDT",
		Side:       "buy",
		OrderType:  "market",
		MarketType: "futures",
		Amount:     decimal.NewFromFloat(1),
		EntryPrice: decimal.NewFromFloat(50000),
		OpenedAt:   time.Now().UTC().Add(-30 * time.Minute),
	}))
	require.NoError(t, store.RecordOrderExecution(ctx, LifecycleExecutionRecord{
		OrderID:    "ord-b",
		ChatID:     "chat-1",
		Exchange:   "bitget",
		Symbol:     "BTC/USDT",
		Side:       "buy",
		OrderType:  "market",
		MarketType: "futures",
		Amount:     decimal.NewFromFloat(1),
		EntryPrice: decimal.NewFromFloat(51000),
		OpenedAt:   time.Now().UTC().Add(-25 * time.Minute),
	}))

	summary, err := store.ReconcileExchangeSnapshot(ctx, "chat-1", "bitget", LifecycleExchangeSnapshot{
		Positions: []ccxt.Position{
			{
				Symbol:        "BTC/USDT",
				Side:          "long",
				Size:          decimal.NewFromFloat(1),
				EntryPrice:    decimal.NewFromFloat(50500),
				MarkPrice:     decimal.NewFromFloat(50600),
				UnrealizedPnl: decimal.NewFromFloat(100),
				Timestamp:     ccxt.UnixTimestamp(time.Now().UTC()),
			},
		},
		OrdersFresh:    true,
		PositionsFresh: true,
	}, "bootstrap_reconciliation")
	require.NoError(t, err)
	assert.Equal(t, 1, summary.PositionsSynced)
	assert.GreaterOrEqual(t, summary.PositionsClosed, 1)

	var closedCount int
	err = sqliteDB.QueryRow(
		ctx,
		`SELECT COUNT(*) FROM trading_positions WHERE order_id IN ($1, $2) AND LOWER(status) = 'closed'`,
		"ord-a",
		"ord-b",
	).Scan(&closedCount)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, closedCount, 1)
}
