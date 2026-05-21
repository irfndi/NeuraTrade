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

func TestExecuteRoutinePaperTradingReviewBlocksUntilEvidenceArtifactExists(t *testing.T) {
	handlers := &IntegratedQuestHandlers{}
	quest := &Quest{
		Metadata: map[string]string{
			"definition_id": "paper_trading_review",
			"chat_id":       "paper-chat",
		},
		Checkpoint: map[string]interface{}{},
	}

	err := handlers.ExecuteRoutine(context.Background(), quest)

	require.NoError(t, err)
	assert.Equal(t, false, quest.Checkpoint["paper_trading_ready"])
	assert.Equal(t, "blocked", quest.Checkpoint["paper_trading_readiness_status"])
	assert.Equal(t, "paper-chat", quest.Checkpoint["paper_trading_review_chat_id"])
	assert.Equal(t, "bitget", quest.Checkpoint["paper_trading_review_exchange"])
	assert.Equal(t, true, quest.Checkpoint["paper_trading_runtime_probe_passed"])
	assert.Equal(t, "filled", quest.Checkpoint["paper_trading_probe_entry_status"])
	assert.Equal(t, "filled", quest.Checkpoint["paper_trading_probe_take_profit_status"])
	assert.Equal(t, "filled", quest.Checkpoint["paper_trading_probe_stop_loss_status"])
	assert.Equal(t, "2", quest.Checkpoint["paper_trading_probe_realized_pnl"])
	assert.Equal(t, false, quest.Checkpoint["paper_trading_lifecycle_storage_verified"])
	assert.Equal(t, 1, quest.CurrentCount)

	metrics, ok := quest.Checkpoint["paper_trading_readiness_evidence_metrics"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, metrics["paper_runtime_probe_passed"])
	assert.Equal(t, false, metrics["lifecycle_storage_verified"])
	assert.Equal(t, 0, metrics["closed_trades"])
	assert.Equal(t, 0, metrics["open_positions"])
	assert.Equal(t, "0", metrics["net_pnl"])
	assert.Equal(t, "0", metrics["avg_net_pnl"])

	blockers, ok := quest.Checkpoint["paper_trading_readiness_blockers"].([]string)
	require.True(t, ok)
	assert.Contains(t, blockers, "missing_paper_trading_evidence_artifact")
	assert.Contains(t, blockers, "paper_review_is_diagnostic_only")
	assert.Contains(t, blockers, "lifecycle_store_unavailable")
	assert.NotContains(t, blockers, "paper_runtime_probe_failed")
}

func TestExecuteRoutinePaperTradingReviewRecordsLifecycleDiagnostics(t *testing.T) {
	ctx := context.Background()
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "paper-readiness.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})
	lifecycleStore, err := NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)

	now := time.Now().UTC()
	require.NoError(t, lifecycleStore.RecordClosedOrder(ctx, LifecycleCloseRecord{
		OrderID:     "paper-close-1",
		ChatID:      "paper-chat",
		Exchange:    "bitget",
		Symbol:      "BTC/USDT",
		Side:        "buy",
		MarketType:  "spot",
		Filled:      decimal.NewFromInt(1),
		EntryPrice:  decimal.NewFromInt(100),
		ExitPrice:   decimal.NewFromInt(102),
		RealizedPnL: decimal.NewFromInt(2),
		Fees:        decimal.New(1, -2),
		Source:      scalpingPaperLifecycleSource,
		ClosedAt:    now.Add(-2 * time.Hour),
	}))

	handlers := &IntegratedQuestHandlers{lifecycleStore: lifecycleStore}
	quest := &Quest{
		Metadata: map[string]string{
			"definition_id": "paper_trading_review",
			"chat_id":       "paper-chat",
			"exchange":      "bitget",
		},
		Checkpoint: map[string]interface{}{},
	}

	err = handlers.ExecuteRoutine(ctx, quest)

	require.NoError(t, err)
	assert.Equal(t, false, quest.Checkpoint["paper_trading_ready"])
	assert.Equal(t, true, quest.Checkpoint["paper_trading_runtime_probe_passed"])
	assert.Equal(t, 1, quest.Checkpoint["paper_trading_review_closed_trades"])
	assert.Equal(t, 1, quest.Checkpoint["paper_trading_review_wins"])
	assert.Equal(t, 0, quest.Checkpoint["paper_trading_review_losses"])
	assert.Equal(t, 0, quest.Checkpoint["paper_trading_review_open_positions"])
	assert.Equal(t, true, quest.Checkpoint["paper_trading_lifecycle_storage_verified"])

	metrics, ok := quest.Checkpoint["paper_trading_readiness_evidence_metrics"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, metrics["paper_runtime_probe_passed"])
	assert.Equal(t, true, metrics["lifecycle_storage_verified"])
	assert.Equal(t, 1, metrics["closed_trades"])
	assert.Equal(t, 0, metrics["open_positions"])
	assert.Equal(t, "1.99", metrics["net_pnl"])
	assert.Equal(t, "1.99", metrics["avg_net_pnl"])

	blockers, ok := quest.Checkpoint["paper_trading_readiness_blockers"].([]string)
	require.True(t, ok)
	assert.Contains(t, blockers, "missing_paper_trading_evidence_artifact")
	assert.NotContains(t, blockers, "lifecycle_store_unavailable")
	assert.NotContains(t, blockers, "no_persisted_closed_paper_trades")
}
