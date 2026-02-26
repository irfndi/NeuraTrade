package services

import (
	"context"
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
