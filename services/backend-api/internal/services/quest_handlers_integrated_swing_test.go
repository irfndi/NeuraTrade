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

func TestExecuteRoutineSwingTradingReviewBlocksLiveReadinessWithoutStrategyProof(t *testing.T) {
	handlers := &IntegratedQuestHandlers{}
	quest := &Quest{
		Metadata: map[string]string{
			"definition_id": "swing_trading_review",
			"chat_id":       "swing-chat",
		},
		Checkpoint: map[string]interface{}{},
	}

	err := handlers.ExecuteRoutine(context.Background(), quest)

	require.NoError(t, err)
	assert.Equal(t, false, quest.Checkpoint["swing_trading_live_ready"])
	assert.Equal(t, "blocked", quest.Checkpoint["swing_trading_readiness_status"])
	assert.Equal(t, "swing-chat", quest.Checkpoint["swing_review_chat_id"])
	assert.Equal(t, "bitget", quest.Checkpoint["swing_review_exchange"])
	assert.Equal(t, false, quest.Checkpoint["swing_trading_lifecycle_storage_verified"])
	assert.Equal(t, false, quest.Checkpoint["swing_trading_drawdown_verified"])
	assert.Equal(t, "diagnostic_placeholder", quest.Checkpoint["swing_trading_readiness_evidence_metrics_status"])
	assert.Equal(t, 1, quest.CurrentCount)

	metrics, ok := quest.Checkpoint["swing_trading_readiness_evidence_metrics"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, 0, metrics["closed_trades"])
	assert.Equal(t, 0, metrics["winning_trades"])
	assert.Equal(t, 0, metrics["losing_trades"])
	assert.Equal(t, 0, metrics["open_positions"])
	assert.Equal(t, "0.00", metrics["net_pnl"])
	assert.Equal(t, "0.00", metrics["avg_net_pnl"])
	assert.Equal(t, "0.00", metrics["max_drawdown_pct"])
	assert.Equal(t, false, metrics["drawdown_verified"])
	assert.Equal(t, false, metrics["execution_path_verified"])
	assert.Equal(t, false, metrics["market_data_verified"])
	assert.Equal(t, false, metrics["risk_limits_enforced"])
	assert.Equal(t, false, metrics["backtest_comparison_verified"])
	assert.Equal(t, false, metrics["hold_window_verified"])
	assert.Equal(t, true, metrics["diagnostic_only"])

	blockers, ok := quest.Checkpoint["swing_trading_readiness_blockers"].([]string)
	require.True(t, ok)
	assert.Contains(t, blockers, "swing_trading_strategy_not_implemented")
	assert.Contains(t, blockers, "swing_review_is_readiness_status_only")
	assert.Contains(t, blockers, "no_swing_signal_engine")
	assert.Contains(t, blockers, "missing_paper_live_market_hold_window_evidence")
	assert.Contains(t, blockers, "lifecycle_store_unavailable")
}

func TestExecuteRoutineSwingTradingReviewBlocksMissingChatIDBeforeLifecycleQueries(t *testing.T) {
	ctx := context.Background()
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "swing-missing-chat.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})
	lifecycleStore, err := NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)

	require.NoError(t, lifecycleStore.RecordClosedOrder(ctx, LifecycleCloseRecord{
		OrderID:     "other-swing-close",
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
		Source:      "swing_trading",
		ClosedAt:    time.Now().UTC().Add(-2 * time.Hour),
	}))

	handlers := &IntegratedQuestHandlers{lifecycleStore: lifecycleStore}
	quest := &Quest{
		Metadata: map[string]string{
			"definition_id": "swing_trading_review",
			"chat_id":       "   ",
			"exchange":      "bitget",
		},
		Checkpoint: map[string]interface{}{},
	}

	err = handlers.ExecuteRoutine(ctx, quest)

	require.NoError(t, err)
	assert.Equal(t, "", quest.Checkpoint["swing_review_chat_id"])
	assert.Equal(t, false, quest.Checkpoint["swing_trading_lifecycle_storage_verified"])
	assert.Equal(t, "diagnostic_placeholder", quest.Checkpoint["swing_trading_readiness_evidence_metrics_status"])
	assert.Equal(t, 1, quest.CurrentCount)

	metrics, ok := quest.Checkpoint["swing_trading_readiness_evidence_metrics"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, 0, metrics["closed_trades"])
	assert.Equal(t, "0.00", metrics["net_pnl"])

	blockers, ok := quest.Checkpoint["swing_trading_readiness_blockers"].([]string)
	require.True(t, ok)
	assert.Contains(t, blockers, "missing_chat_id_metadata")
	assert.NotContains(t, blockers, "lifecycle_store_unavailable")
}

func TestExecuteRoutineSwingTradingReviewRecordsLifecycleBlockers(t *testing.T) {
	ctx := context.Background()
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "swing-readiness.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})
	lifecycleStore, err := NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)

	now := time.Now().UTC()
	require.NoError(t, lifecycleStore.RecordClosedOrder(ctx, LifecycleCloseRecord{
		OrderID:     "swing-loss-1",
		ChatID:      "swing-chat",
		Exchange:    "bitget",
		Symbol:      "BTC/USDT",
		Side:        "buy",
		MarketType:  "spot",
		Filled:      decimal.NewFromInt(1),
		EntryPrice:  decimal.NewFromInt(100),
		ExitPrice:   decimal.NewFromInt(99),
		RealizedPnL: decimal.NewFromInt(-1),
		Fees:        decimal.NewFromFloat(0.1),
		Source:      "swing_trading",
		ClosedAt:    now.Add(-2 * time.Hour),
	}))
	require.NoError(t, lifecycleStore.RecordOrderExecution(ctx, LifecycleExecutionRecord{
		OrderID:    "stale-swing-open",
		ChatID:     "swing-chat",
		Exchange:   "bitget",
		Symbol:     "ETH/USDT",
		Side:       "buy",
		OrderType:  "market",
		MarketType: "spot",
		Amount:     decimal.NewFromFloat(0.5),
		EntryPrice: decimal.NewFromInt(2000),
		Source:     "swing_trading",
		OpenedAt:   now.Add(-20 * 24 * time.Hour),
	}))

	handlers := &IntegratedQuestHandlers{lifecycleStore: lifecycleStore}
	quest := &Quest{
		Metadata: map[string]string{
			"definition_id": "swing_trading_review",
			"chat_id":       "swing-chat",
			"exchange":      "bitget",
		},
		Checkpoint: map[string]interface{}{},
	}

	err = handlers.ExecuteRoutine(ctx, quest)

	require.NoError(t, err)
	assert.Equal(t, false, quest.Checkpoint["swing_trading_live_ready"])
	assert.Equal(t, 1, quest.Checkpoint["swing_review_closed_trades"])
	assert.Equal(t, 0, quest.Checkpoint["swing_review_wins"])
	assert.Equal(t, 1, quest.Checkpoint["swing_review_losses"])
	assert.Equal(t, 1, quest.Checkpoint["swing_review_open_positions"])
	assert.Equal(t, 1, quest.Checkpoint["swing_review_stale_open_positions"])
	assert.Equal(t, true, quest.Checkpoint["swing_trading_lifecycle_storage_verified"])
	assert.Equal(t, false, quest.Checkpoint["swing_trading_drawdown_verified"])
	assert.Equal(t, "diagnostic_lifecycle", quest.Checkpoint["swing_trading_readiness_evidence_metrics_status"])
	assert.Equal(t, 1, quest.CurrentCount)

	metrics, ok := quest.Checkpoint["swing_trading_readiness_evidence_metrics"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, 1, metrics["closed_trades"])
	assert.Equal(t, 0, metrics["winning_trades"])
	assert.Equal(t, 1, metrics["losing_trades"])
	assert.Equal(t, 1, metrics["open_positions"])
	assert.Equal(t, "-1.10", metrics["net_pnl"])
	assert.Equal(t, "-1.10", metrics["avg_net_pnl"])
	assert.Equal(t, "0.00", metrics["max_drawdown_pct"])
	assert.Equal(t, false, metrics["drawdown_verified"])
	assert.Equal(t, false, metrics["execution_path_verified"])
	assert.Equal(t, false, metrics["market_data_verified"])
	assert.Equal(t, false, metrics["risk_limits_enforced"])
	assert.Equal(t, false, metrics["backtest_comparison_verified"])
	assert.Equal(t, false, metrics["hold_window_verified"])
	assert.Equal(t, true, metrics["diagnostic_only"])

	blockers, ok := quest.Checkpoint["swing_trading_readiness_blockers"].([]string)
	require.True(t, ok)
	assert.Contains(t, blockers, "no_representative_closed_trade_sample")
	assert.Contains(t, blockers, "no_winning_trade_observed")
	assert.Contains(t, blockers, "non_positive_net_pnl")
	assert.Contains(t, blockers, "non_positive_avg_net_pnl")
	assert.Contains(t, blockers, "open_positions_need_lifecycle_review")
	assert.Contains(t, blockers, "stale_open_positions_detected")
}
