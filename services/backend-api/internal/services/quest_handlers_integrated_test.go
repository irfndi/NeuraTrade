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

func TestIntegratedQuestHandlers_ExecuteRoutineUnknownDefinition(t *testing.T) {
	mockNotif := &NotificationService{}
	mockMonitoring := NewAutonomousMonitorManager(mockNotif)
	handlers := NewIntegratedQuestHandlers(nil, nil, nil, nil, mockNotif, mockMonitoring)

	quest := &Quest{
		ID:         "unknown-routine",
		Name:       "Unknown Routine",
		Type:       QuestTypeRoutine,
		Status:     QuestStatusActive,
		Checkpoint: map[string]interface{}{},
		Metadata: map[string]string{
			"chat_id":       "test123",
			"definition_id": "not-supported",
		},
	}

	err := handlers.ExecuteRoutine(context.Background(), quest)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown routine quest definition")
}

func TestIntegratedQuestHandlers_ExecuteRoutine_VolatilityWatchUsesPortfolioHealthFlow(t *testing.T) {
	mockNotif := &NotificationService{}
	mockMonitoring := NewAutonomousMonitorManager(mockNotif)
	handlers := NewIntegratedQuestHandlers(nil, nil, nil, nil, mockNotif, mockMonitoring)

	quest := &Quest{
		ID:           "volatility-watch",
		Name:         "Volatility Watch",
		Type:         QuestTypeTriggered,
		Status:       QuestStatusActive,
		CurrentCount: 0,
		Checkpoint:   map[string]interface{}{},
		Metadata: map[string]string{
			"chat_id":       "test123",
			"definition_id": "volatility_watch",
		},
	}

	err := handlers.ExecuteRoutine(context.Background(), quest)
	require.NoError(t, err)
	assert.Equal(t, 1, quest.CurrentCount)
	assert.Contains(t, quest.Checkpoint, "last_health_check")
	assert.Contains(t, quest.Checkpoint, "health_status")
}

func TestIntegratedQuestHandlers_ExecuteRoutine_FundGrowthGoalUpdatesCheckpoint(t *testing.T) {
	mockNotif := &NotificationService{}
	mockMonitoring := NewAutonomousMonitorManager(mockNotif)
	handlers := NewIntegratedQuestHandlers(nil, nil, nil, nil, mockNotif, mockMonitoring)

	quest := &Quest{
		ID:           "fund-growth",
		Name:         "Fund Growth Target",
		Type:         QuestTypeGoal,
		Status:       QuestStatusActive,
		CurrentCount: 40,
		TargetCount:  100,
		Checkpoint:   map[string]interface{}{},
		Metadata: map[string]string{
			"chat_id":       "test123",
			"definition_id": "fund_growth",
		},
	}

	err := handlers.ExecuteRoutine(context.Background(), quest)
	require.NoError(t, err)
	assert.Equal(t, 40.0, quest.Checkpoint["goal_progress_pct"])
	assert.Equal(t, 100, quest.Checkpoint["goal_target_count"])
	assert.Equal(t, 40, quest.Checkpoint["goal_current_count"])
	assert.Equal(t, false, quest.Checkpoint["goal_reached"])
}

func TestIntegratedQuestHandlers_SetQuestEngine(t *testing.T) {
	engine := NewQuestEngineWithNotification(NewInMemoryQuestStore(), nil, nil)
	handlers := NewIntegratedQuestHandlers(nil, nil, nil, nil, nil, nil)
	handlers.SetQuestEngine(engine)
	assert.Equal(t, engine, handlers.questEngine)
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
	state := handlers.evaluateRecoveryGateStateForScope(context.Background(), quest, TradingPortfolio{
		RiskDrawdown: 0.35,
	}, "", "")
	assert.Equal(t, recoveryModeMicroEntry, state.Mode)
	assert.False(t, state.EntryAllowed)
	assert.Equal(t, 2, state.CleanCycles)

	quest.Checkpoint["recovery_clean_cycles"] = 3
	state = handlers.evaluateRecoveryGateStateForScope(context.Background(), quest, TradingPortfolio{
		RiskDrawdown: 0.35,
	}, "", "")
	assert.Equal(t, recoveryModeMicroEntry, state.Mode)
	assert.True(t, state.EntryAllowed)

	state = handlers.evaluateRecoveryGateStateForScope(context.Background(), quest, TradingPortfolio{
		RiskDrawdown: 0.41,
	}, "", "")
	assert.Equal(t, recoveryModeDeriskOnly, state.Mode)
	assert.False(t, state.EntryAllowed)
}

func TestIntegratedQuestHandlers_EvaluateRecoveryGateState_IgnoresRuntimeFailureStreak(t *testing.T) {
	t.Setenv("NEURATRADE_RECOVERY_CLEAN_CYCLES", "1")
	t.Setenv("NEURATRADE_RECOVERY_MICRO_ENTRY_MIN_DRAWDOWN", "0.30")
	t.Setenv("NEURATRADE_RECOVERY_DERISK_ONLY_DRAWDOWN", "0.40")

	handlers := NewIntegratedQuestHandlers(nil, nil, nil, nil, nil, nil)
	quest := &Quest{
		Checkpoint: map[string]interface{}{
			"recovery_clean_cycles_current": 1,
			"runtime_failure_streak":        1,
		},
	}

	state := handlers.evaluateRecoveryGateStateForScope(context.Background(), quest, TradingPortfolio{
		RiskDrawdown: 0.35,
	}, "", "")

	assert.Equal(t, recoveryModeMicroEntry, state.Mode)
	assert.True(t, state.EntryAllowed)
	assert.Equal(t, 0, state.CyclesToEntry)
}

func TestIntegratedQuestHandlers_EnrichPortfolioControlPlane_UsesCurrentDrawdownFromPeakEquity(t *testing.T) {
	handlers := NewIntegratedQuestHandlers(nil, nil, nil, nil, nil, nil)
	handlers.SetDrawdownHalt(NewMaxDrawdownHalt(nil, DefaultMaxDrawdownConfig()))

	quest := &Quest{
		Checkpoint: map[string]interface{}{
			"risk_peak_equity":   1000.0,
			"risk_max_drawdown":  0.35,
			"state_drift_active": false,
		},
	}
	portfolio := &TradingPortfolio{
		USDTBalance: 700,
		TotalValue:  700,
	}

	handlers.enrichPortfolioControlPlane(context.Background(), quest, "chat-1", "bitget", portfolio)

	assert.InDelta(t, 0.30, portfolio.CurrentDrawdown, 0.0001)
	assert.InDelta(t, 0.30, portfolio.RiskDrawdown, 0.0001)
	assert.InDelta(t, 0.35, portfolio.RiskMaxDrawdown, 0.0001)
	assert.InDelta(t, 1000.0, checkpointFloat(quest.Checkpoint["risk_peak_equity"]), 0.0001)
	assert.InDelta(t, 0.30, checkpointFloat(quest.Checkpoint["risk_current_drawdown"]), 0.0001)
	assert.InDelta(t, 0.35, checkpointFloat(quest.Checkpoint["risk_max_drawdown"]), 0.0001)
}

func newLifecycleStoreForTest(t *testing.T) *TradingLifecycleStore {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "quest-recent-loss.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store, err := NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)
	return store
}

func TestIntegratedQuestHandlers_RecentLossWindowRecoveryGate(t *testing.T) {
	t.Setenv("NEURATRADE_RECOVERY_CLEAN_CYCLES", "3")
	t.Setenv("NEURATRADE_RECOVERY_MICRO_ENTRY_MIN_DRAWDOWN", "0.30")
	t.Setenv("NEURATRADE_RECOVERY_DERISK_ONLY_DRAWDOWN", "0.40")
	t.Setenv("NEURATRADE_SCALPING_SYMBOL_LOSS_STREAK_BUDGET", "1")

	type closeRow struct {
		orderID   string
		symbol    string
		closedAgo time.Duration
	}

	tests := []struct {
		name                     string
		windowSeconds            string
		rows                     []closeRow
		expectedLosses           int
		expectedActive           bool
		expectedWindow           time.Duration
		expectedRecoveryMode     string
		expectedEntryAllowed     bool
		expectedReasonContains   string
		expectedConditionContain string
	}{
		{
			name:           "old losses outside window do not block",
			windowSeconds:  "900",
			expectedLosses: 0,
			expectedActive: false,
			expectedWindow: 15 * time.Minute,
			rows: []closeRow{
				{orderID: "old-loss-1", symbol: "BTC/USDT", closedAgo: 2 * time.Hour},
			},
			expectedRecoveryMode: recoveryModeMicroEntry,
			expectedEntryAllowed: true,
		},
		{
			name:           "fresh losses inside window block micro entry",
			windowSeconds:  "900",
			expectedLosses: 1,
			expectedActive: true,
			expectedWindow: 15 * time.Minute,
			rows: []closeRow{
				{orderID: "fresh-loss-1", symbol: "ETH/USDT", closedAgo: 0},
			},
			expectedRecoveryMode:     recoveryModeMicroEntry,
			expectedEntryAllowed:     false,
			expectedReasonContains:   "recent loss streak",
			expectedConditionContain: "loss streak below",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("NEURATRADE_SCALPING_SYMBOL_LOSS_WINDOW_SECONDS", tt.windowSeconds)
			handlers := NewIntegratedQuestHandlers(nil, nil, nil, nil, nil, nil)
			store := newLifecycleStoreForTest(t)
			handlers.SetLifecycleStore(store)

			for _, row := range tt.rows {
				require.NoError(t, store.RecordClosedOrder(context.Background(), LifecycleCloseRecord{
					OrderID:     row.orderID,
					ChatID:      "chat-1",
					Exchange:    "bitget",
					Symbol:      row.symbol,
					Side:        "buy",
					MarketType:  "spot",
					Filled:      decimal.NewFromFloat(0.1),
					EntryPrice:  decimal.NewFromFloat(100),
					ExitPrice:   decimal.NewFromFloat(95),
					RealizedPnL: decimal.NewFromFloat(-5),
					ClosedAt:    time.Now().UTC().Add(-row.closedAgo),
				}))
			}

			recentLoss := handlers.currentRecentLossStreak(context.Background(), "chat-1", "bitget")
			assert.Equal(t, tt.expectedLosses, recentLoss.ConsecutiveLosses)
			assert.Equal(t, tt.expectedActive, recentLoss.Active)
			assert.Equal(t, tt.expectedWindow, recentLoss.Window)

			quest := &Quest{
				Checkpoint: map[string]interface{}{
					"recovery_clean_cycles": 3,
				},
			}
			state := handlers.evaluateRecoveryGateStateForScope(context.Background(), quest, TradingPortfolio{
				RiskDrawdown: 0.34,
			}, "chat-1", "bitget")
			assert.Equal(t, tt.expectedRecoveryMode, state.Mode)
			assert.Equal(t, tt.expectedEntryAllowed, state.EntryAllowed)
			if tt.expectedReasonContains != "" {
				assert.Contains(t, state.GateReason, tt.expectedReasonContains)
			}
			if tt.expectedConditionContain != "" {
				assert.Contains(t, state.NextCondition, tt.expectedConditionContain)
			}
		})
	}
}

func TestIntegratedQuestHandlers_UpdateRecoveryCleanCycles_ResetOnFailure(t *testing.T) {
	handlers := NewIntegratedQuestHandlers(nil, nil, nil, nil, nil, nil)
	quest := &Quest{Checkpoint: map[string]interface{}{}}

	handlers.updateRecoveryCleanCycles(quest, true, "")
	handlers.updateRecoveryCleanCycles(quest, true, "")
	assert.Equal(t, 2, checkpointInt(quest.Checkpoint["recovery_clean_cycles_current"]))
	assert.Equal(t, 2, checkpointInt(quest.Checkpoint["recovery_clean_cycles"]))

	handlers.updateRecoveryCleanCycles(quest, false, "runtime_error")
	assert.Equal(t, 0, checkpointInt(quest.Checkpoint["recovery_clean_cycles_current"]))
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

func TestIntegratedQuestHandlers_RecordEntryAttempt_SetsBlockReasonAtBudgetEdge(t *testing.T) {
	t.Setenv("NEURATRADE_LIVENESS_MAX_ATTEMPTS_PER_HOUR", "3")

	handlers := NewIntegratedQuestHandlers(nil, nil, nil, nil, nil, nil)
	now := time.Now().UTC()
	windowStart := now.Add(-20 * time.Minute)
	quest := &Quest{Checkpoint: map[string]interface{}{}}

	handlers.recordEntryAttempt(quest, now, entryAttemptGateState{
		AttemptsInWindow:   2,
		MaxAttemptsPerHour: 3,
		WindowStartedAt:    windowStart,
	})

	assert.Equal(t, 3, checkpointInt(quest.Checkpoint["runtime_entry_attempts_1h"]))
	assert.Equal(t, "3/3 in current 1h window", checkpointString(quest.Checkpoint["runtime_entry_attempt_window_progress"]))
	assert.Contains(t, checkpointString(quest.Checkpoint["runtime_entry_attempt_block_reason"]), "budget reached")
	assert.Equal(
		t,
		checkpointString(quest.Checkpoint["runtime_entry_attempt_block_reason"]),
		checkpointString(quest.Checkpoint["runtime_entry_gate_reason"]),
	)
	assert.Contains(t, checkpointString(quest.Checkpoint["runtime_next_unblock_condition"]), "Next entry-attempt window opens")
}

func TestShouldRecordEntryAttempt(t *testing.T) {
	t.Run("hold does not count", func(t *testing.T) {
		assert.False(t, shouldRecordEntryAttempt(&AITradingDecision{
			Action: "hold",
		}, nil))
	})

	t.Run("executed order counts", func(t *testing.T) {
		assert.True(t, shouldRecordEntryAttempt(&AITradingDecision{
			Action:  "buy",
			OrderID: "order-123",
		}, nil))
	})

	t.Run("execution error counts", func(t *testing.T) {
		assert.True(t, shouldRecordEntryAttempt(&AITradingDecision{
			Action: "sell",
		}, assert.AnError))
	})

	t.Run("nil decision does not count", func(t *testing.T) {
		assert.False(t, shouldRecordEntryAttempt(nil, assert.AnError))
	})
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
