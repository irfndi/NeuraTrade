package services

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/database"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegratedQuestHandlers_MarketScanWithTA tests market scanning with TA
func TestIntegratedQuestHandlers_MarketScanWithTA(t *testing.T) {
	mockNotif := &NotificationService{}
	mockMonitoring := NewAutonomousMonitorManager(mockNotif)

	handlers := NewIntegratedQuestHandlers(
		nil, nil, nil, nil, mockNotif, mockMonitoring,
	)

	quest := &Quest{
		ID:           "test-market-scan",
		Name:         "Market Scanner",
		Type:         QuestTypeRoutine,
		Status:       QuestStatusActive,
		CurrentCount: 0,
		Checkpoint:   make(map[string]interface{}),
		Metadata:     map[string]string{"chat_id": "test123"},
	}

	ctx := context.Background()
	err := handlers.handleMarketScanWithTA(ctx, quest)

	assert.NoError(t, err)
	assert.Equal(t, 1, quest.CurrentCount)
	assert.Contains(t, quest.Checkpoint, "last_scan_time")
	assert.Contains(t, quest.Checkpoint, "symbols_scanned")
	assert.Contains(t, quest.Checkpoint, "chat_id")
}

// TestIntegratedQuestHandlers_FundingRateScan tests funding rate scanning
func TestIntegratedQuestHandlers_FundingRateScan(t *testing.T) {
	mockNotif := &NotificationService{}
	mockMonitoring := NewAutonomousMonitorManager(mockNotif)

	handlers := NewIntegratedQuestHandlers(
		nil, nil, nil, nil, mockNotif, mockMonitoring,
	)

	quest := &Quest{
		ID:           "test-funding-scan",
		Name:         "Funding Rate Scanner",
		Type:         QuestTypeRoutine,
		Status:       QuestStatusActive,
		CurrentCount: 0,
		Checkpoint:   make(map[string]interface{}),
		Metadata:     map[string]string{"chat_id": "test123"},
	}

	ctx := context.Background()
	err := handlers.handleFundingRateScan(ctx, quest)

	assert.NoError(t, err)
	assert.Equal(t, 1, quest.CurrentCount)
	assert.Contains(t, quest.Checkpoint, "last_funding_scan")
	assert.Contains(t, quest.Checkpoint, "rates_collected")
}

// TestIntegratedQuestHandlers_PortfolioHealthWithRisk tests portfolio health checks
func TestIntegratedQuestHandlers_PortfolioHealthWithRisk(t *testing.T) {
	mockNotif := &NotificationService{}
	mockMonitoring := NewAutonomousMonitorManager(mockNotif)

	handlers := NewIntegratedQuestHandlers(
		nil, nil, nil, nil, mockNotif, mockMonitoring,
	)

	quest := &Quest{
		ID:           "test-health-check",
		Name:         "Portfolio Health Check",
		Type:         QuestTypeRoutine,
		Status:       QuestStatusActive,
		CurrentCount: 0,
		Checkpoint:   make(map[string]interface{}),
		Metadata:     map[string]string{"chat_id": "test123"},
	}

	ctx := context.Background()
	err := handlers.handlePortfolioHealthWithRisk(ctx, quest)

	assert.NoError(t, err)
	assert.Equal(t, 1, quest.CurrentCount)
	assert.Contains(t, quest.Checkpoint, "last_health_check")
	assert.Contains(t, quest.Checkpoint, "health_status")
	assert.Contains(t, quest.Checkpoint, "checks_passed")
}

// TestIntegratedQuestHandlers_GetMonitoringSnapshot tests monitoring snapshot retrieval
func TestIntegratedQuestHandlers_GetMonitoringSnapshot(t *testing.T) {
	mockNotif := &NotificationService{}
	mockMonitoring := NewAutonomousMonitorManager(mockNotif)

	handlers := NewIntegratedQuestHandlers(
		nil, nil, nil, nil, mockNotif, mockMonitoring,
	)

	snapshot := handlers.GetMonitoringSnapshot("test123")

	assert.NotNil(t, snapshot)
	assert.Contains(t, snapshot, "chat_id")
	assert.Contains(t, snapshot, "uptime_hours")
	assert.Contains(t, snapshot, "total_quests")
}

// TestProductionQuestExecutor tests the production quest executor
func TestProductionQuestExecutor(t *testing.T) {
	mockNotif := &NotificationService{}

	executor := NewProductionQuestExecutor(
		nil, nil, nil, nil, mockNotif,
	)

	assert.NotNil(t, executor)
	assert.NotNil(t, executor.engine)
	assert.NotNil(t, executor.monitoring)

	// Test start
	executor.Start()

	// Test status
	status := executor.GetStatus("test123")
	assert.NotNil(t, status)

	// Test stop
	executor.Stop()
}

// TestQuestEngine_IntegratedHandlerRegistration tests handler registration
func TestQuestEngine_IntegratedHandlerRegistration(t *testing.T) {
	engine := NewQuestEngineWithNotification(NewInMemoryQuestStore(), nil, nil)

	mockNotif := &NotificationService{}
	mockMonitoring := NewAutonomousMonitorManager(mockNotif)
	handlers := NewIntegratedQuestHandlers(nil, nil, nil, nil, mockNotif, mockMonitoring)

	// Should not panic
	engine.RegisterIntegratedHandlers(handlers)

	// Verify handlers are registered
	assert.NotNil(t, engine.handlers)
}

// TestQuestEngine_QuestExecutionWithCheckpoint tests quest execution with checkpoints
func TestQuestEngine_QuestExecutionWithCheckpoint(t *testing.T) {
	engine := NewQuestEngineWithNotification(NewInMemoryQuestStore(), nil, nil)

	executionCount := 0
	testHandler := func(ctx context.Context, quest *Quest) error {
		executionCount++
		quest.CurrentCount++
		quest.Checkpoint["executed_at"] = time.Now().UTC().Format(time.RFC3339)
		return nil
	}

	engine.RegisterHandler(QuestTypeRoutine, testHandler)

	quest := &Quest{
		ID:         "test-quest",
		Name:       "Test Quest",
		Type:       QuestTypeRoutine,
		Status:     QuestStatusActive,
		Checkpoint: make(map[string]interface{}),
		Metadata:   map[string]string{"chat_id": "test123"},
	}

	ctx := context.Background()
	err := testHandler(ctx, quest)

	assert.NoError(t, err)
	assert.Equal(t, 1, executionCount)
	assert.Equal(t, 1, quest.CurrentCount)
	assert.Contains(t, quest.Checkpoint, "executed_at")
}

// TestQuestEngine_ErrorHandling tests error handling in quest execution
func TestQuestEngine_ErrorHandling(t *testing.T) {
	engine := NewQuestEngineWithNotification(NewInMemoryQuestStore(), nil, nil)

	errorHandler := func(ctx context.Context, quest *Quest) error {
		return assert.AnError
	}

	engine.RegisterHandler(QuestTypeRoutine, errorHandler)

	quest := &Quest{
		ID:     "error-quest",
		Name:   "Error Quest",
		Type:   QuestTypeRoutine,
		Status: QuestStatusActive,
	}

	ctx := context.Background()
	err := errorHandler(ctx, quest)

	assert.Error(t, err)
}

// TestQuestEngine_MetadataPropagation tests metadata propagation
func TestQuestEngine_MetadataPropagation(t *testing.T) {
	engine := NewQuestEngineWithNotification(NewInMemoryQuestStore(), nil, nil)

	metadataHandler := func(ctx context.Context, quest *Quest) error {
		chatID, ok := quest.Metadata["chat_id"]
		if !ok {
			return assert.AnError
		}
		quest.Checkpoint["processed_chat_id"] = chatID
		quest.CurrentCount++
		return nil
	}

	engine.RegisterHandler(QuestTypeRoutine, metadataHandler)

	quest := &Quest{
		ID:         "metadata-quest",
		Name:       "Metadata Quest",
		Type:       QuestTypeRoutine,
		Status:     QuestStatusActive,
		Checkpoint: make(map[string]interface{}),
		Metadata: map[string]string{
			"chat_id": "test-chat-123",
			"user":    "test-user",
		},
	}

	ctx := context.Background()
	err := metadataHandler(ctx, quest)

	assert.NoError(t, err)
	assert.Equal(t, 1, quest.CurrentCount)
	assert.Equal(t, "test-chat-123", quest.Checkpoint["processed_chat_id"])
}

// TestQuestEngine_ConcurrentExecution tests concurrent quest execution
func TestQuestEngine_ConcurrentExecution(t *testing.T) {
	engine := NewQuestEngineWithNotification(NewInMemoryQuestStore(), nil, nil)

	var executionCount atomic.Int32
	slowHandler := func(ctx context.Context, quest *Quest) error {
		time.Sleep(10 * time.Millisecond)
		executionCount.Add(1)
		quest.CurrentCount++
		return nil
	}

	engine.RegisterHandler(QuestTypeRoutine, slowHandler)

	quests := make([]*Quest, 5)
	for i := 0; i < 5; i++ {
		quests[i] = &Quest{
			ID:     "concurrent-quest-" + string(rune(i)),
			Name:   "Concurrent Quest",
			Type:   QuestTypeRoutine,
			Status: QuestStatusActive,
		}
	}

	ctx := context.Background()
	done := make(chan bool)
	for _, q := range quests {
		go func(quest *Quest) {
			_ = slowHandler(ctx, quest)
			done <- true
		}(q)
	}

	for i := 0; i < 5; i++ {
		<-done
	}

	assert.Equal(t, int32(5), executionCount.Load())
}

// TestHasExchange tests the hasExchange helper function
func TestHasExchange(t *testing.T) {
	exchanges := []string{"binance", "bybit", "okx"}

	assert.True(t, hasExchange(exchanges, "binance"))
	assert.True(t, hasExchange(exchanges, "bybit"))
	assert.True(t, hasExchange(exchanges, "okx"))
	assert.False(t, hasExchange(exchanges, "kraken"))
	assert.False(t, hasExchange(exchanges, ""))
	assert.False(t, hasExchange([]string{}, "binance"))
}

func TestIntegratedQuestHandlers_GetUserExchange_SQLiteLookup(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "quest-user-exchange.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	ctx := context.Background()
	_, err = sqliteDB.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS telegram_operator_wallets (
			id TEXT PRIMARY KEY,
			chat_id TEXT NOT NULL,
			provider TEXT NOT NULL,
			status TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL
		)
	`)
	require.NoError(t, err)
	_, err = sqliteDB.Exec(ctx, `
		INSERT INTO telegram_operator_wallets (id, chat_id, provider, status, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`, "w1", "chat-1", "bitget", "connected", time.Now().UTC())
	require.NoError(t, err)

	handlers := NewIntegratedQuestHandlers(nil, nil, nil, nil, nil, nil)
	handlers.SetDB(sqliteDB.DB)

	assert.Equal(t, "bitget", handlers.getUserExchange("chat-1"))
	assert.Equal(t, "bitget", handlers.getUserExchange("chat-missing"))
}

func TestShouldSendScalpingDecisionNotification_DefaultActionableOnly(t *testing.T) {
	t.Setenv("NEURATRADE_TELEGRAM_NOTIFY_AI_DECISIONS", "")
	t.Setenv("NEURATRADE_TELEGRAM_ACTIONABLE_ONLY", "")

	assert.False(t, shouldSendScalpingDecisionNotification(AIReasoningNotification{
		DecisionType: "scalping_cycle",
		Action:       "hold",
	}))

	assert.True(t, shouldSendScalpingDecisionNotification(AIReasoningNotification{
		DecisionType: "scalping",
		Action:       "buy",
	}), "buy actions are now considered actionable regardless of decision type")

	assert.True(t, shouldSendScalpingDecisionNotification(AIReasoningNotification{
		DecisionType: "pnl_reconciliation",
		Action:       "record",
	}))
	assert.True(t, shouldSendScalpingDecisionNotification(AIReasoningNotification{
		DecisionType: "risk_reduction",
		Action:       "hold",
	}))
	assert.True(t, shouldSendScalpingDecisionNotification(AIReasoningNotification{
		DecisionType: "scalping_digest",
		Action:       "hold",
	}))
}

func TestShouldSendScalpingDecisionNotification_EnvOverrides(t *testing.T) {
	t.Setenv("NEURATRADE_TELEGRAM_ACTIONABLE_ONLY", "false")
	t.Setenv("NEURATRADE_TELEGRAM_NOTIFY_AI_DECISIONS", "")

	assert.True(t, shouldSendScalpingDecisionNotification(AIReasoningNotification{
		DecisionType: "scalping_cycle",
		Action:       "hold",
	}))

	t.Setenv("NEURATRADE_TELEGRAM_ACTIONABLE_ONLY", "true")
	t.Setenv("NEURATRADE_TELEGRAM_NOTIFY_AI_DECISIONS", "true")
	assert.True(t, shouldSendScalpingDecisionNotification(AIReasoningNotification{
		DecisionType: "scalping_cycle",
		Action:       "hold",
	}))
}

func TestIntegratedQuestHandlers_EvaluateRecoveryGateState_HybridModes(t *testing.T) {
	t.Setenv("NEURATRADE_RECOVERY_CLEAN_CYCLES", "3")
	t.Setenv("NEURATRADE_RECOVERY_MICRO_ENTRY_MIN_DRAWDOWN", "0.30")
	t.Setenv("NEURATRADE_RECOVERY_DERISK_ONLY_DRAWDOWN", "0.40")
	t.Setenv("NEURATRADE_RECOVERY_MICRO_ENTRY_CAP_PCT", "0.50")

	handlers := NewIntegratedQuestHandlers(nil, nil, nil, nil, nil, nil)
	quest := &Quest{
		Checkpoint: map[string]interface{}{
			"recovery_clean_cycles": 2,
		},
	}
	state := handlers.evaluateRecoveryGateState(quest, TradingPortfolio{
		RiskDrawdown: 0.35,
	})
	assert.Equal(t, recoveryModeMicroEntry, state.Mode)
	assert.False(t, state.EntryAllowed)
	assert.Equal(t, 2, state.CleanCycles)

	quest.Checkpoint["recovery_clean_cycles"] = 3
	state = handlers.evaluateRecoveryGateState(quest, TradingPortfolio{
		RiskDrawdown: 0.35,
	})
	assert.Equal(t, recoveryModeMicroEntry, state.Mode)
	assert.True(t, state.EntryAllowed)

	state = handlers.evaluateRecoveryGateState(quest, TradingPortfolio{
		RiskDrawdown: 0.41,
	})
	assert.Equal(t, recoveryModeDeriskOnly, state.Mode)
	assert.False(t, state.EntryAllowed)
}

func resetScalpingPerformanceForTest(t *testing.T) {
	previous := globalScalpingPerformance
	globalScalpingPerformance = NewScalpingPerformance()
	t.Cleanup(func() {
		globalScalpingPerformance = previous
	})
}

func TestCurrentRecentLossStreak_RespectsWindow(t *testing.T) {
	resetScalpingPerformanceForTest(t)
	t.Setenv("NEURATRADE_SCALPING_SYMBOL_LOSS_WINDOW_SECONDS", "1800")

	GetScalpingPerformance().RecordTrade(TradeRecord{
		Timestamp:  time.Now().UTC().Add(-2 * time.Hour),
		Symbol:     "BTC/USDT",
		Side:       "buy",
		Amount:     decimal.NewFromFloat(0.1),
		EntryPrice: decimal.NewFromFloat(100),
		ExitPrice:  decimal.NewFromFloat(95),
		PnL:        decimal.NewFromFloat(-5),
		Profitable: false,
	})

	state := currentRecentLossStreak()
	assert.Equal(t, 1, state.ConsecutiveLosses)
	assert.False(t, state.Active)
	assert.Equal(t, 30*time.Minute, state.Window)

	GetScalpingPerformance().RecordTrade(TradeRecord{
		Timestamp:  time.Now().UTC(),
		Symbol:     "BTC/USDT",
		Side:       "buy",
		Amount:     decimal.NewFromFloat(0.1),
		EntryPrice: decimal.NewFromFloat(100),
		ExitPrice:  decimal.NewFromFloat(94),
		PnL:        decimal.NewFromFloat(-6),
		Profitable: false,
	})

	state = currentRecentLossStreak()
	assert.Equal(t, 2, state.ConsecutiveLosses)
	assert.True(t, state.Active)
}

func TestIntegratedQuestHandlers_EvaluateRecoveryGateState_RecentLossWindow(t *testing.T) {
	resetScalpingPerformanceForTest(t)
	t.Setenv("NEURATRADE_RECOVERY_CLEAN_CYCLES", "3")
	t.Setenv("NEURATRADE_RECOVERY_MICRO_ENTRY_MIN_DRAWDOWN", "0.30")
	t.Setenv("NEURATRADE_RECOVERY_DERISK_ONLY_DRAWDOWN", "0.40")
	t.Setenv("NEURATRADE_SCALPING_SYMBOL_LOSS_STREAK_BUDGET", "1")
	t.Setenv("NEURATRADE_SCALPING_SYMBOL_LOSS_WINDOW_SECONDS", "900")

	handlers := NewIntegratedQuestHandlers(nil, nil, nil, nil, nil, nil)
	quest := &Quest{
		Checkpoint: map[string]interface{}{
			"recovery_clean_cycles": 3,
		},
	}

	// Old losses outside the recent window should not block micro-entry unlock.
	GetScalpingPerformance().RecordTrade(TradeRecord{
		Timestamp:  time.Now().UTC().Add(-2 * time.Hour),
		Symbol:     "ETH/USDT",
		Side:       "sell",
		Amount:     decimal.NewFromFloat(0.1),
		EntryPrice: decimal.NewFromFloat(100),
		ExitPrice:  decimal.NewFromFloat(103),
		PnL:        decimal.NewFromFloat(-3),
		Profitable: false,
	})

	state := handlers.evaluateRecoveryGateState(quest, TradingPortfolio{
		RiskDrawdown: 0.34,
	})
	assert.Equal(t, recoveryModeMicroEntry, state.Mode)
	assert.True(t, state.EntryAllowed)

	// Fresh recent loss streak should close the gate.
	GetScalpingPerformance().RecordTrade(TradeRecord{
		Timestamp:  time.Now().UTC(),
		Symbol:     "ETH/USDT",
		Side:       "sell",
		Amount:     decimal.NewFromFloat(0.1),
		EntryPrice: decimal.NewFromFloat(100),
		ExitPrice:  decimal.NewFromFloat(104),
		PnL:        decimal.NewFromFloat(-4),
		Profitable: false,
	})

	state = handlers.evaluateRecoveryGateState(quest, TradingPortfolio{
		RiskDrawdown: 0.34,
	})
	assert.Equal(t, recoveryModeMicroEntry, state.Mode)
	assert.False(t, state.EntryAllowed)
	assert.Contains(t, state.GateReason, "recent loss streak")
	assert.Contains(t, state.NextCondition, "loss streak below")
}

func TestIntegratedQuestHandlers_UpdateRecoveryCleanCycles_ResetOnFailure(t *testing.T) {
	handlers := NewIntegratedQuestHandlers(nil, nil, nil, nil, nil, nil)
	quest := &Quest{Checkpoint: map[string]interface{}{}}

	handlers.updateRecoveryCleanCycles(quest, true, "")
	handlers.updateRecoveryCleanCycles(quest, true, "")
	assert.Equal(t, 2, checkpointInt(quest.Checkpoint["recovery_clean_cycles"]))

	handlers.updateRecoveryCleanCycles(quest, false, "runtime_error")
	assert.Equal(t, 0, checkpointInt(quest.Checkpoint["recovery_clean_cycles"]))
	assert.Equal(t, "runtime_error", checkpointString(quest.Checkpoint["recovery_last_reset_reason"]))
}

func TestIntegratedQuestHandlers_EvaluateEntryAttemptGateState_BudgetLimit(t *testing.T) {
	t.Setenv("NEURATRADE_LIVENESS_IDLE_MINUTES", "45")
	t.Setenv("NEURATRADE_LIVENESS_MAX_ATTEMPTS_PER_HOUR", "3")

	handlers := NewIntegratedQuestHandlers(nil, nil, nil, nil, nil, nil)
	now := time.Now().UTC()
	quest := &Quest{
		Checkpoint: map[string]interface{}{
			"runtime_entry_attempt_window_started_at": now.Add(-15 * time.Minute).Format(time.RFC3339),
			"runtime_entry_attempts_1h":               3,
		},
	}
	state := handlers.evaluateEntryAttemptGateState(quest, TradingPortfolio{
		USDTBalance:        10,
		TotalValue:         100,
		OpenPositions:      0,
		DriftActive:        false,
		RecoveryEntryOK:    true,
		NoFillMinutes:      60,
		RiskDrawdown:       0.20,
		RecoveryMode:       recoveryModeNormal,
		RecoveryCleanCycle: 0,
	}, now)

	assert.True(t, state.Forced)
	assert.False(t, state.AllowNow)
	assert.Equal(t, 3, state.AttemptsInWindow)
	assert.Contains(t, state.BlockReason, "budget reached")
	assert.Contains(t, state.NextCondition, "Next entry-attempt window opens")
}

func TestIntegratedQuestHandlers_RecordEntryAttempt_RotatesWindow(t *testing.T) {
	t.Setenv("NEURATRADE_LIVENESS_MAX_ATTEMPTS_PER_HOUR", "3")

	handlers := NewIntegratedQuestHandlers(nil, nil, nil, nil, nil, nil)
	now := time.Now().UTC()
	quest := &Quest{
		Checkpoint: map[string]interface{}{
			"runtime_entry_attempt_window_started_at": now.Add(-2 * time.Hour).Format(time.RFC3339),
			"runtime_entry_attempts_1h":               2,
		},
	}

	handlers.recordEntryAttempt(quest, now, entryAttemptGateState{MaxAttemptsPerHour: 3})
	assert.Equal(t, 1, checkpointInt(quest.Checkpoint["runtime_entry_attempts_1h"]))
	assert.Equal(t, "1/3 in current 1h window", checkpointString(quest.Checkpoint["runtime_entry_attempt_window_progress"]))

	windowStart, ok := checkpointRFC3339(quest.Checkpoint["runtime_entry_attempt_window_started_at"])
	require.True(t, ok)
	assert.WithinDuration(t, now, windowStart, time.Second)
}

func TestNormalizeAINotificationSemantics_RuntimeDegradedNotStrategyHold(t *testing.T) {
	notif := AIReasoningNotification{
		DecisionType:    "scalping_digest",
		Summary:         "Hold digest: waiting for qualified setup",
		ConfidenceKnown: false,
		ReasonCategory:  aiReasonStrategyHold,
		Reasons: []string{
			"model response parse fallback (repair attempt 2 failed: context deadline exceeded)",
		},
		Action: "hold",
	}

	normalized := normalizeAINotificationSemantics(notif)
	assert.False(t, normalized.ConfidenceKnown)
	assert.NotEqual(t, aiReasonStrategyHold, normalized.ReasonCategory)
	assert.Equal(t, normalized.ReasonCategory, normalized.HoldCategory)
}

func TestNormalizeAINotificationSemantics_StrategyHoldPreservedWhenConfidenceKnown(t *testing.T) {
	notif := AIReasoningNotification{
		DecisionType:    "scalping_digest",
		Summary:         "Hold digest: waiting for qualified setup",
		ConfidenceKnown: true,
		ReasonCategory:  aiReasonStrategyHold,
		Reasons: []string{
			"No candidate passed pretrade validity/liquidity filters",
		},
		Action: "hold",
	}

	normalized := normalizeAINotificationSemantics(notif)
	assert.True(t, normalized.ConfidenceKnown)
	assert.Equal(t, aiReasonStrategyHold, normalized.ReasonCategory)
	assert.Equal(t, aiReasonStrategyHold, normalized.HoldCategory)
}

// hasExchange checks if a specific exchange exists in the list
func hasExchange(exchanges []string, exchangeName string) bool {
	for _, ex := range exchanges {
		if ex == exchangeName {
			return true
		}
	}
	return false
}
