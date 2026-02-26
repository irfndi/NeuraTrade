package handlers

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/database"
	"github.com/irfndi/neuratrade/internal/services"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAutonomousHandler_BuildLifecyclePerformanceSummary(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "autonomous-lifecycle-performance.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store, err := services.NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)

	now := time.Now().UTC()
	ctx := context.Background()
	require.NoError(t, store.RecordClosedOrder(ctx, services.LifecycleCloseRecord{
		OrderID:     "ord-win",
		ChatID:      "chat-1",
		Exchange:    "bitget",
		Symbol:      "ADA/USDT",
		Side:        "buy",
		MarketType:  "futures",
		Filled:      decimal.NewFromFloat(2),
		EntryPrice:  decimal.NewFromFloat(1),
		ExitPrice:   decimal.NewFromFloat(1.05),
		RealizedPnL: decimal.NewFromFloat(0.1),
		ClosedAt:    now.Add(-30 * time.Minute),
	}))
	require.NoError(t, store.RecordClosedOrder(ctx, services.LifecycleCloseRecord{
		OrderID:     "ord-loss",
		ChatID:      "chat-1",
		Exchange:    "bitget",
		Symbol:      "DOGE/USDT",
		Side:        "buy",
		MarketType:  "futures",
		Filled:      decimal.NewFromFloat(5),
		EntryPrice:  decimal.NewFromFloat(0.2),
		ExitPrice:   decimal.NewFromFloat(0.19),
		RealizedPnL: decimal.NewFromFloat(-0.05),
		ClosedAt:    now.Add(-15 * time.Minute),
	}))

	handler := NewAutonomousHandler(nil, nil, nil)
	handler.SetLifecycleStore(store)

	summary, ok := handler.buildLifecyclePerformanceSummary(ctx, "chat-1", "24h")
	require.True(t, ok)
	assert.Equal(t, "24h", summary.Timeframe)
	assert.Equal(t, "0.05", summary.PnL)
	assert.Equal(t, "50.0%", summary.WinRate)
	assert.NotEqual(t, "N/A", summary.Sharpe)
	assert.NotEqual(t, "N/A", summary.Sortino)
	assert.NotEqual(t, "N/A", summary.Drawdown)
	assert.Equal(t, 2, summary.Trades)
	assert.Contains(t, summary.Note, "Exchange-reconciled")
}

func TestAutonomousHandler_EnrichPortfolioWithLifecycle(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "autonomous-lifecycle-portfolio.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store, err := services.NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, store.RecordOrderExecution(ctx, services.LifecycleExecutionRecord{
		OrderID:    "open-ord-1",
		ChatID:     "chat-1",
		Exchange:   "bitget",
		Symbol:     "ADA/USDT",
		Side:       "buy",
		OrderType:  "market",
		MarketType: "futures",
		Amount:     decimal.NewFromFloat(5),
		EntryPrice: decimal.NewFromFloat(1),
		StopLoss:   decimal.NewFromFloat(0.99),
		TakeProfit: decimal.NewFromFloat(1.02),
		OpenedAt:   time.Now().UTC().Add(-5 * time.Minute),
	}))

	handler := NewAutonomousHandler(nil, nil, nil)
	handler.SetLifecycleStore(store)

	response := PortfolioResponse{
		TotalEquity: "0.00",
		Positions:   []PortfolioPosition{},
	}
	handler.enrichPortfolioWithLifecycle(ctx, "chat-1", &response)

	assert.Equal(t, 1, response.OpenOrders)
	require.Len(t, response.Positions, 1)
	assert.Equal(t, "ADA/USDT", response.Positions[0].Symbol)
	assert.Equal(t, "buy", response.Positions[0].Side)
	assert.Contains(t, response.Note, "lifecycle store")
}
