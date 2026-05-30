package services

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecuteRoutineDailyReportBlocksLiveReadinessWithoutStrategyProof(t *testing.T) {
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
	assert.Equal(t, 1, quest.CurrentCount)

	blockers, ok := quest.Checkpoint["daily_trading_readiness_blockers"].([]string)
	require.True(t, ok)
	assert.Contains(t, blockers, "daily_trading_strategy_not_implemented")
	assert.Contains(t, blockers, "daily_report_is_reporting_only")
	assert.Contains(t, blockers, "lifecycle_store_unavailable")
}
