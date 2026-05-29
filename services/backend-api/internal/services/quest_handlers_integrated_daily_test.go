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

func TestExecuteRoutineDailyReportRecordsBlockedReadinessWithoutLifecycleStore(t *testing.T) {
	handlers := &IntegratedQuestHandlers{}
	quest := &Quest{
		Metadata: map[string]string{
			"definition_id": "daily_report",
			"chat_id":       "daily-chat",
			"exchange":      "bitget",
		},
		Checkpoint: map[string]interface{}{},
	}

	err := handlers.ExecuteRoutine(context.Background(), quest)

	require.NoError(t, err)
	assert.Equal(t, false, quest.Checkpoint["daily_trading_live_ready"])
	assert.Equal(t, "blocked", quest.Checkpoint["daily_trading_readiness_status"])
	assert.Equal(t, "daily-chat", quest.Checkpoint["daily_report_chat_id"])
	assert.Equal(t, "bitget", quest.Checkpoint["daily_report_exchange"])
	assert.Equal(t, false, quest.Checkpoint["daily_trading_lifecycle_storage_verified"])
	assert.Equal(t, false, quest.Checkpoint["daily_trading_drawdown_verified"])
	assert.Equal(t, "diagnostic_placeholder", quest.Checkpoint["daily_trading_readiness_evidence_metrics_status"])
	assert.Equal(t, 1, quest.CurrentCount)

	metrics, ok := quest.Checkpoint["daily_trading_readiness_evidence_metrics"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, 0, metrics["closed_trades"])
	assert.Equal(t, 0, metrics["winning_trades"])
	assert.Equal(t, 0, metrics["losing_trades"])
	assert.Equal(t, 0, metrics["open_positions"])
	assert.Equal(t, "0.00", metrics["net_pnl"])
	assert.Equal(t, "0.00", metrics["avg_net_pnl"])
	assert.Equal(t, "0.00", metrics["max_drawdown_pct"])
	assert.Equal(t, false, metrics["drawdown_verified"])
	assert.Equal(t, true, metrics["diagnostic_only"])

	blockers, ok := quest.Checkpoint["daily_trading_readiness_blockers"].([]string)
	require.True(t, ok)
	assert.Contains(t, blockers, "daily_trading_strategy_not_implemented")
	assert.Contains(t, blockers, "daily_report_is_readiness_status_only")
	assert.Contains(t, blockers, "missing_daily_trading_evidence_artifact")
	assert.Contains(t, blockers, "missing_daily_strategy_signal_proof")
	assert.Contains(t, blockers, "missing_drawdown_evidence")
	assert.Contains(t, blockers, "lifecycle_store_unavailable")
}

func TestExecuteRoutineDailyReportBlocksMissingChatIDBeforeLifecycleQueries(t *testing.T) {
	ctx := context.Background()
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "daily-missing-chat.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})
	lifecycleStore, err := NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)

	require.NoError(t, lifecycleStore.RecordClosedOrder(ctx, LifecycleCloseRecord{
		OrderID:     "other-user-close",
		ChatID:      "other-chat",
		Exchange:    "bitget",
		Symbol:      "BTC/USDT",
		Side:        "buy",
		MarketType:  "spot",
		Filled:      decimal.NewFromInt(1),
		EntryPrice:  decimal.NewFromInt(100),
		ExitPrice:   decimal.NewFromInt(103),
		RealizedPnL: decimal.NewFromInt(3),
		Fees:        decimal.New(1, -1),
		Source:      "daily_trading",
		ClosedAt:    time.Now().UTC().Add(-2 * time.Hour),
	}))

	handlers := &IntegratedQuestHandlers{lifecycleStore: lifecycleStore}
	quest := &Quest{
		Metadata: map[string]string{
			"definition_id": "daily_report",
			"chat_id":       "   ",
			"exchange":      "bitget",
		},
		Checkpoint: map[string]interface{}{},
	}

	err = handlers.ExecuteRoutine(ctx, quest)

	require.NoError(t, err)
	assert.Equal(t, "", quest.Checkpoint["daily_report_chat_id"])
	assert.Equal(t, false, quest.Checkpoint["daily_trading_lifecycle_storage_verified"])
	assert.Equal(t, false, quest.Checkpoint["daily_trading_drawdown_verified"])
	assert.Equal(t, "diagnostic_placeholder", quest.Checkpoint["daily_trading_readiness_evidence_metrics_status"])
	assert.Equal(t, 1, quest.CurrentCount)

	metrics, ok := quest.Checkpoint["daily_trading_readiness_evidence_metrics"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, 0, metrics["closed_trades"])
	assert.Equal(t, "0.00", metrics["net_pnl"])

	blockers, ok := quest.Checkpoint["daily_trading_readiness_blockers"].([]string)
	require.True(t, ok)
	assert.Contains(t, blockers, "missing_chat_id_metadata")
	assert.NotContains(t, blockers, "lifecycle_store_unavailable")
}

func TestExecuteRoutineDailyReportReturnsLifecycleQueryErrors(t *testing.T) {
	ctx := context.Background()
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "daily-query-error.db"))
	require.NoError(t, err)
	lifecycleStore, err := NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)
	require.NoError(t, sqliteDB.Close())

	handlers := &IntegratedQuestHandlers{lifecycleStore: lifecycleStore}
	quest := &Quest{
		Metadata: map[string]string{
			"definition_id": "daily_report",
			"chat_id":       "daily-chat",
			"exchange":      "bitget",
		},
		Checkpoint: map[string]interface{}{},
	}

	err = handlers.ExecuteRoutine(ctx, quest)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "daily trading report: realized performance")
	assert.Equal(t, 0, quest.CurrentCount)

	blockers, ok := quest.Checkpoint["daily_trading_readiness_blockers"].([]string)
	require.True(t, ok)
	assert.Contains(t, blockers, "realized_performance_query_failed")
}

func TestExecuteRoutineDailyReportRecordsLifecycleMetrics(t *testing.T) {
	ctx := context.Background()
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "daily-readiness.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})
	lifecycleStore, err := NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)

	now := time.Now().UTC()
	require.NoError(t, lifecycleStore.RecordClosedOrder(ctx, LifecycleCloseRecord{
		OrderID:     "daily-win-1",
		ChatID:      "daily-chat",
		Exchange:    "bitget",
		Symbol:      "BTC/USDT",
		Side:        "buy",
		MarketType:  "spot",
		Filled:      decimal.NewFromInt(1),
		EntryPrice:  decimal.NewFromInt(100),
		ExitPrice:   decimal.NewFromInt(103),
		RealizedPnL: decimal.NewFromInt(3),
		Fees:        decimal.New(1, -1),
		Source:      "daily_trading",
		ClosedAt:    now.Add(-2 * time.Hour),
	}))
	require.NoError(t, lifecycleStore.RecordClosedOrder(ctx, LifecycleCloseRecord{
		OrderID:     "daily-loss-1",
		ChatID:      "daily-chat",
		Exchange:    "bitget",
		Symbol:      "ETH/USDT",
		Side:        "buy",
		MarketType:  "spot",
		Filled:      decimal.NewFromInt(1),
		EntryPrice:  decimal.NewFromInt(100),
		ExitPrice:   decimal.NewFromInt(99),
		RealizedPnL: decimal.NewFromInt(-1),
		Fees:        decimal.New(1, -1),
		Source:      "daily_trading",
		ClosedAt:    now.Add(-1 * time.Hour),
	}))

	handlers := &IntegratedQuestHandlers{lifecycleStore: lifecycleStore}
	quest := &Quest{
		Metadata: map[string]string{
			"definition_id": "daily_report",
			"chat_id":       "daily-chat",
			"exchange":      "bitget",
		},
		Checkpoint: map[string]interface{}{},
	}

	err = handlers.ExecuteRoutine(ctx, quest)

	require.NoError(t, err)
	assert.Equal(t, false, quest.Checkpoint["daily_trading_live_ready"])
	assert.Equal(t, 2, quest.Checkpoint["daily_report_closed_trades"])
	assert.Equal(t, 1, quest.Checkpoint["daily_report_wins"])
	assert.Equal(t, 1, quest.Checkpoint["daily_report_losses"])
	assert.Equal(t, 0, quest.Checkpoint["daily_report_open_positions"])
	assert.Equal(t, true, quest.Checkpoint["daily_trading_lifecycle_storage_verified"])
	assert.Equal(t, false, quest.Checkpoint["daily_trading_drawdown_verified"])
	assert.Equal(t, "diagnostic_lifecycle", quest.Checkpoint["daily_trading_readiness_evidence_metrics_status"])
	assert.Equal(t, 1, quest.CurrentCount)

	metrics, ok := quest.Checkpoint["daily_trading_readiness_evidence_metrics"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, 2, metrics["closed_trades"])
	assert.Equal(t, 1, metrics["winning_trades"])
	assert.Equal(t, 1, metrics["losing_trades"])
	assert.Equal(t, 0, metrics["open_positions"])
	assert.Equal(t, "1.80", metrics["net_pnl"])
	assert.Equal(t, "0.90", metrics["avg_net_pnl"])
	assert.Equal(t, "0.00", metrics["max_drawdown_pct"])
	assert.Equal(t, false, metrics["drawdown_verified"])
	assert.Equal(t, true, metrics["diagnostic_only"])

	blockers, ok := quest.Checkpoint["daily_trading_readiness_blockers"].([]string)
	require.True(t, ok)
	assert.Contains(t, blockers, "daily_trading_strategy_not_implemented")
	assert.Contains(t, blockers, "missing_drawdown_evidence")
	assert.NotContains(t, blockers, "lifecycle_store_unavailable")
	assert.NotContains(t, blockers, "no_representative_closed_trade_sample")
	assert.NotContains(t, blockers, "no_winning_trade_observed")
	assert.NotContains(t, blockers, "no_losing_trade_observed")
	assert.NotContains(t, blockers, "non_positive_net_pnl")
	assert.NotContains(t, blockers, "non_positive_avg_net_pnl")
}
