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

func TestExecuteRoutineArbitrageReadinessReviewBlocksLiveReadinessWithoutProof(t *testing.T) {
	handlers := &IntegratedQuestHandlers{}
	quest := &Quest{
		Metadata: map[string]string{
			"definition_id": "arbitrage_readiness_review",
			"chat_id":       "arb-chat",
			"exchange":      "bitget",
		},
		Checkpoint: map[string]interface{}{},
	}

	err := handlers.ExecuteRoutine(context.Background(), quest)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "lifecycle store unavailable")
	assert.Equal(t, false, quest.Checkpoint["arbitrage_live_ready"])
	assert.Equal(t, "blocked", quest.Checkpoint["arbitrage_readiness_status"])
	assert.Equal(t, "arb-chat", quest.Checkpoint["arbitrage_review_chat_id"])
	assert.Equal(t, "bitget", quest.Checkpoint["arbitrage_review_exchange"])
	assert.Equal(t, false, quest.Checkpoint["arbitrage_no_trade_safety"])
	assert.Equal(t, "", quest.Checkpoint["arbitrage_no_trade_reason"])
	assert.Equal(t, 0, quest.CurrentCount)

	blockers, ok := quest.Checkpoint["arbitrage_readiness_blockers"].([]string)
	require.True(t, ok)
	assert.Contains(t, blockers, "arbitrage_execution_not_proven_safe")
	assert.Contains(t, blockers, "funding_rate_scan_uses_placeholder_data")
	assert.Contains(t, blockers, "missing_fee_slippage_funding_accounting_proof")
	assert.Contains(t, blockers, "missing_inventory_exposure_safety_proof")
	assert.Contains(t, blockers, "missing_no_trade_safety_window")
	assert.Contains(t, blockers, "order_executor_unavailable")
	assert.Contains(t, blockers, "lifecycle_store_unavailable")
}

func TestExecuteRoutineArbitrageReadinessReviewRecordsLifecycleBlockers(t *testing.T) {
	ctx := context.Background()
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "arbitrage-readiness.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})
	lifecycleStore, err := NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)

	now := time.Now().UTC()
	require.NoError(t, lifecycleStore.RecordClosedOrder(ctx, LifecycleCloseRecord{
		OrderID:     "arb-loss-1",
		ChatID:      "arb-chat",
		Exchange:    "bitget",
		Symbol:      "BTC/USDT",
		Side:        "buy",
		MarketType:  "spot",
		Filled:      decimal.NewFromInt(1),
		EntryPrice:  decimal.NewFromInt(100),
		ExitPrice:   decimal.NewFromInt(99),
		RealizedPnL: decimal.NewFromInt(-1),
		Fees:        decimal.NewFromFloat(0.2),
		Source:      "arbitrage",
		ClosedAt:    now.Add(-2 * time.Hour),
	}))
	require.NoError(t, lifecycleStore.RecordOrderExecution(ctx, LifecycleExecutionRecord{
		OrderID:    "arb-open-inventory",
		ChatID:     "arb-chat",
		Exchange:   "bitget",
		Symbol:     "ETH/USDT",
		Side:       "buy",
		OrderType:  "market",
		MarketType: "spot",
		Amount:     decimal.NewFromFloat(0.5),
		EntryPrice: decimal.NewFromInt(2000),
		Source:     "arbitrage",
		OpenedAt:   now.Add(-3 * time.Hour),
	}))

	handlers := &IntegratedQuestHandlers{lifecycleStore: lifecycleStore}
	quest := &Quest{
		Metadata: map[string]string{
			"definition_id": "arbitrage_readiness_review",
			"chat_id":       "arb-chat",
			"exchange":      "bitget",
		},
		Checkpoint: map[string]interface{}{},
	}

	err = handlers.ExecuteRoutine(ctx, quest)

	require.NoError(t, err)
	assert.Equal(t, false, quest.Checkpoint["arbitrage_live_ready"])
	assert.Equal(t, 1, quest.Checkpoint["arbitrage_review_closed_trades"])
	assert.Equal(t, 0, quest.Checkpoint["arbitrage_review_wins"])
	assert.Equal(t, 1, quest.Checkpoint["arbitrage_review_losses"])
	assert.Equal(t, 1, quest.Checkpoint["arbitrage_review_open_positions"])
	assert.Equal(t, 1, quest.CurrentCount)

	blockers, ok := quest.Checkpoint["arbitrage_readiness_blockers"].([]string)
	require.True(t, ok)
	assert.Contains(t, blockers, "no_representative_closed_opportunity_sample")
	assert.Contains(t, blockers, "no_winning_opportunity_observed")
	assert.Contains(t, blockers, "non_positive_net_pnl")
	assert.Contains(t, blockers, "non_positive_avg_net_pnl")
	assert.Contains(t, blockers, "open_positions_need_inventory_review")
}
