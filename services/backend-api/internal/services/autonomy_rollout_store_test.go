package services

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/autonomous"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAutonomousRolloutStore_NilDatabase(t *testing.T) {
	store := NewAutonomousRolloutStore(nil)
	ctx := context.Background()

	err := store.InitSchema(ctx)
	require.Error(t, err)

	_, err = store.GetRolloutState(ctx, "scalping:chat-1:default")
	require.Error(t, err)

	err = store.SaveRolloutState(ctx, &autonomous.RolloutState{StrategyID: "scalping:chat-1:default"})
	require.Error(t, err)

	err = store.SaveRollbackEvent(ctx, &autonomous.RollbackEvent{ID: "rb-1", StrategyID: "scalping:chat-1:default"})
	require.Error(t, err)

	_, err = store.GetRollbackHistory(ctx, "scalping:chat-1:default", 10)
	require.Error(t, err)
}

func TestAutonomousRolloutStore_RoundTrip(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "autonomy-store.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store := NewAutonomousRolloutStore(sqliteDB.DB)
	ctx := context.Background()
	require.NoError(t, store.InitSchema(ctx))

	state := &autonomous.RolloutState{
		StrategyID:   "scalping:1082762347:default",
		CurrentStage: autonomous.StagePaper,
		Status:       autonomous.StatusActive,
		EnteredAt:    time.Now().UTC().Add(-15 * time.Minute),
		PromotionCriteria: autonomous.PromotionCriteria{
			MinTrades:        10,
			MinWinRate:       0.55,
			MaxSlippage:      decimal.NewFromFloat(0.4),
			MaxRejectionRate: 0.2,
			MinUptime:        99,
			DurationRequired: 2 * time.Hour,
		},
		Metrics: autonomous.RolloutMetrics{
			TotalTrades:    14,
			WinningTrades:  9,
			LosingTrades:   5,
			TotalPnL:       decimal.NewFromFloat(44.25),
			AvgSlippage:    decimal.NewFromFloat(0.12),
			RejectionCount: 1,
			UptimePercent:  99.9,
			MaxDrawdown:    decimal.NewFromFloat(8.2),
			LastUpdated:    time.Now().UTC(),
		},
	}

	require.NoError(t, store.SaveRolloutState(ctx, state))

	persisted, err := store.GetRolloutState(ctx, state.StrategyID)
	require.NoError(t, err)
	require.NotNil(t, persisted)
	assert.Equal(t, state.StrategyID, persisted.StrategyID)
	assert.Equal(t, state.CurrentStage, persisted.CurrentStage)
	assert.Equal(t, state.Status, persisted.Status)
	assert.Equal(t, state.Metrics.TotalTrades, persisted.Metrics.TotalTrades)
	assert.True(t, persisted.Metrics.TotalPnL.Equal(state.Metrics.TotalPnL))

	chatState, err := store.GetChatRolloutState(ctx, "1082762347")
	require.NoError(t, err)
	require.NotNil(t, chatState)
	assert.Equal(t, state.StrategyID, chatState.StrategyID)
}

func TestAutonomousRolloutStore_RollbackHistory(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "autonomy-rollback.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store := NewAutonomousRolloutStore(sqliteDB.DB)
	ctx := context.Background()
	require.NoError(t, store.InitSchema(ctx))

	strategyID := "scalping:1082762347:default"
	for i := 0; i < 3; i++ {
		event := &autonomous.RollbackEvent{
			ID:         "rb-" + string(rune('a'+i)),
			StrategyID: strategyID,
			Trigger:    autonomous.TriggerRejectionRate,
			FromStage:  autonomous.StageLive,
			ToStage:    autonomous.StagePaper,
			Reason:     "test rollback",
			Timestamp:  time.Now().UTC().Add(time.Duration(i) * time.Minute),
		}
		require.NoError(t, store.SaveRollbackEvent(ctx, event))
	}

	history, err := store.GetRollbackHistory(ctx, strategyID, 2)
	require.NoError(t, err)
	require.Len(t, history, 2)
	assert.Equal(t, strategyID, history[0].StrategyID)
	assert.Equal(t, strategyID, history[1].StrategyID)
	assert.True(t, history[0].Timestamp.After(history[1].Timestamp) || history[0].Timestamp.Equal(history[1].Timestamp))
}
