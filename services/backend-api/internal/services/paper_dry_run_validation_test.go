package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/database"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type noopPaperDryRunLogger struct{}

func (noopPaperDryRunLogger) WithFields(map[string]interface{}) Logger {
	return noopPaperDryRunLogger{}
}
func (noopPaperDryRunLogger) Info(string)  {}
func (noopPaperDryRunLogger) Warn(string)  {}
func (noopPaperDryRunLogger) Error(string) {}

func TestPaperDryRunValidationFlowRecordsPerformanceAndProtectionExits(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	userID := "11111111-1111-1111-1111-111111111111"
	questID := int64(42)
	size := decimal.NewFromInt(1)
	entryPrice := decimal.NewFromInt(100)
	takeProfitPrice := decimal.NewFromInt(102)
	stopLossPrice := decimal.NewFromInt(98)
	entryFees := decimal.NewFromFloat(0.01)
	exitFees := decimal.NewFromFloat(0.02)

	config := DefaultPaperExecutionConfig()
	config.EnableRandomness = false
	config.ExecutionDelayMs = 0
	config.SlippagePercentage = decimal.Zero
	simulator := NewPaperExecutionSimulator(config)

	entryOrder, err := simulator.CreateOrder(PaperOrderRequest{
		UserID:   userID,
		Exchange: "binance-testnet",
		Symbol:   "BTC/USDT",
		Type:     PaperOrderTypeMarket,
		Side:     PaperOrderSideBuy,
		Size:     size,
	})
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(entryOrder.ID, "paper_"))

	filledEntry, err := simulator.SimulateFill(ctx, entryOrder, entryPrice)
	require.NoError(t, err)
	require.Equal(t, PaperOrderStatusFilled, filledEntry.Status)
	require.True(t, filledEntry.AvgFillPrice.Equal(entryPrice))

	takeProfitOrder, err := simulator.CreateOrder(PaperOrderRequest{
		UserID:   userID,
		Exchange: "binance-testnet",
		Symbol:   "BTC/USDT",
		Type:     PaperOrderTypeLimit,
		Side:     PaperOrderSideSell,
		Size:     size,
		Price:    takeProfitPrice,
	})
	require.NoError(t, err)
	filledTakeProfit, err := simulator.SimulateFill(ctx, takeProfitOrder, decimal.NewFromInt(103))
	require.NoError(t, err)
	require.Equal(t, PaperOrderStatusFilled, filledTakeProfit.Status)
	require.True(t, filledTakeProfit.AvgFillPrice.Equal(takeProfitPrice))

	stopLossOrder, err := simulator.CreateOrder(PaperOrderRequest{
		UserID:    userID,
		Exchange:  "binance-testnet",
		Symbol:    "ETH/USDT",
		Type:      PaperOrderTypeStop,
		Side:      PaperOrderSideSell,
		Size:      size,
		StopPrice: stopLossPrice,
	})
	require.NoError(t, err)
	filledStopLoss, err := simulator.SimulateFill(ctx, stopLossOrder, stopLossPrice)
	require.NoError(t, err)
	require.Equal(t, PaperOrderStatusFilled, filledStopLoss.Status)
	require.True(t, filledStopLoss.AvgFillPrice.Equal(stopLossPrice))

	dbPool, mockPool, err := database.NewMockDBPoolFromNewPool()
	require.NoError(t, err)
	defer mockPool.Close()

	recorder := NewPaperTradeRecorder(dbPool, noopPaperDryRunLogger{})
	expectPaperOpenTrade(mockPool, 1, userID, &questID, "scalping-dry-run", "binance-testnet", "BTC/USDT", "buy", entryPrice, size, entryFees, entryPrice.Mul(size), now)
	opened, err := recorder.RecordOpenTrade(ctx, &PaperTrade{
		UserID:     userID,
		QuestID:    &questID,
		StrategyID: "scalping-dry-run",
		Exchange:   "binance-testnet",
		Symbol:     "BTC/USDT",
		Side:       "buy",
		EntryPrice: filledEntry.AvgFillPrice,
		Size:       filledEntry.FilledSize,
		Fees:       entryFees,
		CostBasis:  filledEntry.AvgFillPrice.Mul(filledEntry.FilledSize),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), opened.ID)

	winPnL := takeProfitPrice.Sub(entryPrice).Mul(size).Sub(exitFees)
	expectPaperGetTrade(mockPool, *opened, now)
	expectPaperCloseTrade(mockPool, *opened, takeProfitPrice, exitFees, winPnL, now)
	closedWin, err := recorder.RecordCloseTrade(ctx, opened.ID, filledTakeProfit.AvgFillPrice, exitFees)
	require.NoError(t, err)
	require.Equal(t, "closed", closedWin.Status)
	require.True(t, closedWin.PnL.Equal(winPnL))

	expectPaperOpenTrade(mockPool, 2, userID, &questID, "scalping-dry-run", "binance-testnet", "ETH/USDT", "buy", entryPrice, size, entryFees, entryPrice.Mul(size), now)
	openedLoss, err := recorder.RecordOpenTrade(ctx, &PaperTrade{
		UserID:     userID,
		QuestID:    &questID,
		StrategyID: "scalping-dry-run",
		Exchange:   "binance-testnet",
		Symbol:     "ETH/USDT",
		Side:       "buy",
		EntryPrice: entryPrice,
		Size:       size,
		Fees:       entryFees,
		CostBasis:  entryPrice.Mul(size),
	})
	require.NoError(t, err)

	lossPnL := stopLossPrice.Sub(entryPrice).Mul(size).Sub(exitFees)
	expectPaperGetTrade(mockPool, *openedLoss, now)
	expectPaperCloseTrade(mockPool, *openedLoss, stopLossPrice, exitFees, lossPnL, now)
	closedLoss, err := recorder.RecordCloseTrade(ctx, openedLoss.ID, filledStopLoss.AvgFillPrice, exitFees)
	require.NoError(t, err)
	require.True(t, closedLoss.PnL.Equal(lossPnL))

	mockPool.ExpectQuery("FROM paper_trades").
		WithArgs(userID).
		WillReturnRows(pgxmock.NewRows([]string{
			"total_trades", "winning_trades", "losing_trades", "total_pnl", "avg_pnl", "total_fees",
		}).AddRow(int64(2), int64(1), int64(1), winPnL.Add(lossPnL), winPnL.Add(lossPnL).Div(decimal.NewFromInt(2)), entryFees.Mul(decimal.NewFromInt(2)).Add(exitFees.Mul(decimal.NewFromInt(2)))))
	summary, err := recorder.GetUserSummary(ctx, userID)
	require.NoError(t, err)
	assert.Equal(t, int64(2), summary.TotalTrades)
	assert.True(t, summary.WinRate.Equal(decimal.NewFromInt(50)))

	performance := NewScalpingPerformance()
	performance.RecordTrade(TradeRecord{
		Timestamp:  now,
		Symbol:     "BTC/USDT",
		Side:       "buy",
		Amount:     size,
		EntryPrice: entryPrice,
		ExitPrice:  takeProfitPrice,
		Notional:   entryPrice.Mul(size),
		PnL:        winPnL,
		Profitable: true,
	})
	performance.RecordTrade(TradeRecord{
		Timestamp:  now,
		Symbol:     "ETH/USDT",
		Side:       "buy",
		Amount:     size,
		EntryPrice: entryPrice,
		ExitPrice:  stopLossPrice,
		Notional:   entryPrice.Mul(size),
		PnL:        lossPnL,
		Profitable: false,
	})
	stats := performance.GetPerformance()
	assert.Equal(t, 2, stats["total_trades"])
	assert.Equal(t, 1, stats["profitable_trades"])
	assert.Equal(t, 1, stats["losing_trades"])
	assert.Equal(t, 0.5, stats["win_rate"])
	assert.Equal(t, "-0.04", stats["total_pnl"])
	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestPaperTradingSpawnerUsesSimulatorWithoutProductionExecutor(t *testing.T) {
	config := DefaultSubagentSpawnerConfig()
	config.PaperTrading = true
	config.DefaultExchange = "binance-testnet"
	spawner := NewSubagentSpawner(time.Second, 1, config, nil, nil, nil)

	result := spawner.runExecutorAgent(context.Background(), &ExecutorAgent{
		ID: "executor-paper",
		Decision: &TradingDecision{
			Action:      "buy",
			Symbol:      "BTC/USDT",
			SizePercent: 1,
			EntryPrice:  100,
		},
	})

	require.True(t, result.Success)
	data, ok := result.Data.(map[string]interface{})
	require.True(t, ok)
	orderID, ok := data["order_id"].(string)
	require.True(t, ok)
	assert.True(t, strings.HasPrefix(orderID, "paper_"))
	assert.Equal(t, true, data["paper_trading"])
	assert.Equal(t, "binance-testnet", data["exchange"])
}

func expectPaperOpenTrade(
	mockPool pgxmock.PgxPoolIface,
	id int64,
	userID string,
	questID *int64,
	strategyID string,
	exchange string,
	symbol string,
	side string,
	entryPrice decimal.Decimal,
	size decimal.Decimal,
	fees decimal.Decimal,
	costBasis decimal.Decimal,
	now time.Time,
) {
	mockPool.ExpectQuery("INSERT INTO paper_trades").
		WithArgs(userID, questID, strategyID, exchange, symbol, side, entryPrice, size, fees, costBasis).
		WillReturnRows(paperTradeRows().AddRow(
			id, userID, questID, strategyID, exchange, symbol, side, entryPrice, decimal.Zero,
			size, fees, decimal.Zero, costBasis, "open", now, nil, now, now,
		))
}

func expectPaperGetTrade(mockPool pgxmock.PgxPoolIface, trade PaperTrade, now time.Time) {
	mockPool.ExpectQuery("FROM paper_trades").
		WithArgs(trade.ID).
		WillReturnRows(paperTradeRows().AddRow(
			trade.ID, trade.UserID, trade.QuestID, trade.StrategyID, trade.Exchange, trade.Symbol,
			trade.Side, trade.EntryPrice, decimal.Zero, trade.Size, trade.Fees, decimal.Zero,
			trade.CostBasis, "open", now, nil, now, now,
		))
}

func expectPaperCloseTrade(mockPool pgxmock.PgxPoolIface, trade PaperTrade, exitPrice decimal.Decimal, exitFees decimal.Decimal, pnl decimal.Decimal, now time.Time) {
	closedAt := now
	mockPool.ExpectQuery("UPDATE paper_trades").
		WithArgs(exitPrice, exitFees, pnl, pgxmock.AnyArg(), trade.ID).
		WillReturnRows(paperTradeRows().AddRow(
			trade.ID, trade.UserID, trade.QuestID, trade.StrategyID, trade.Exchange, trade.Symbol,
			trade.Side, trade.EntryPrice, exitPrice, trade.Size, trade.Fees.Add(exitFees), pnl,
			trade.CostBasis, "closed", now, &closedAt, now, now,
		))
}

func paperTradeRows() *pgxmock.Rows {
	return pgxmock.NewRows([]string{
		"id", "user_id", "quest_id", "strategy_id", "exchange", "symbol", "side",
		"entry_price", "exit_price", "size", "fees", "pnl", "cost_basis", "status",
		"opened_at", "closed_at", "created_at", "updated_at",
	})
}
