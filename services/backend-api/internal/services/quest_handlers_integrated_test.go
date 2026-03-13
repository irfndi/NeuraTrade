package services

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	appautonomy "github.com/irfndi/neuratrade/internal/app/autonomy"
	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
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

func TestIntegratedQuestHandlers_EnrichPortfolioControlPlane_UsesRawEquityForFullDrawdown(t *testing.T) {
	store := newLifecycleStoreForTest(t)
	ctx := context.Background()
	require.NoError(t, store.RecordOrderExecution(ctx, LifecycleExecutionRecord{
		OrderID:    "ord-drawdown",
		ChatID:     "chat-raw-equity",
		Exchange:   "bitget",
		Symbol:     "BTC/USDT",
		Side:       "buy",
		OrderType:  "market",
		MarketType: "futures",
		Amount:     decimal.NewFromFloat(10),
		EntryPrice: decimal.NewFromFloat(100),
		OpenedAt:   time.Now().UTC().Add(-5 * time.Minute),
	}))
	_, err := store.ReconcileExchangeSnapshot(ctx, "chat-raw-equity", "bitget", LifecycleExchangeSnapshot{
		Positions: []ccxt.Position{
			{
				Symbol:        "BTC/USDT",
				Side:          "long",
				Size:          decimal.NewFromFloat(10),
				EntryPrice:    decimal.NewFromFloat(100),
				MarkPrice:     decimal.NewFromFloat(10),
				UnrealizedPnl: decimal.NewFromFloat(-900),
				Timestamp:     ccxt.UnixTimestamp(time.Now().UTC()),
			},
		},
		PositionsFresh: true,
	}, "bootstrap_reconciliation")
	require.NoError(t, err)

	handlers := NewIntegratedQuestHandlers(nil, nil, nil, nil, nil, nil)
	handlers.SetLifecycleStore(store)
	handlers.SetDrawdownHalt(NewMaxDrawdownHalt(nil, DefaultMaxDrawdownConfig()))

	quest := &Quest{
		Checkpoint: map[string]interface{}{
			"risk_peak_equity": 1000.0,
		},
	}
	portfolio := &TradingPortfolio{
		USDTBalance: 200,
		TotalValue:  200,
	}

	handlers.enrichPortfolioControlPlane(ctx, quest, "chat-raw-equity", "bitget", portfolio)

	assert.InDelta(t, 1.0, portfolio.CurrentDrawdown, 0.0001)
	assert.InDelta(t, 1.0, portfolio.RiskDrawdown, 0.0001)
	assert.InDelta(t, 1.0, checkpointFloat(quest.Checkpoint["risk_current_drawdown"]), 0.0001)
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

	t.Run("execution error does not count", func(t *testing.T) {
		assert.False(t, shouldRecordEntryAttempt(&AITradingDecision{
			Action: "sell",
		}, assert.AnError))
	})

	t.Run("nil decision does not count", func(t *testing.T) {
		assert.False(t, shouldRecordEntryAttempt(nil, assert.AnError))
	})
}

func TestFilterManagedPositionsForEntryProtection_ExcludesBootstrapFallbackTargets(t *testing.T) {
	positions := []ManagedOpenPosition{
		{
			PositionID: "sync-bitget-ada-usdt-long",
			OrderID:    "",
			Symbol:     "ADA/USDT",
			Side:       "buy",
			Source:     "bootstrap_positions",
		},
		{
			PositionID: "manual-1",
			OrderID:    "ord-manual-1",
			Symbol:     "ADA/USDT",
			Side:       "buy",
			Source:     "manual_reconciliation",
		},
		{
			PositionID: "autonomous-1",
			OrderID:    "ord-autonomous-1",
			Symbol:     "ADA/USDT",
			Side:       "buy",
			Source:     "autonomous",
		},
	}

	filtered := filterManagedPositionsForEntryProtection(positions, "", "ADA/USDT", "buy")
	require.Len(t, filtered, 1)
	assert.Equal(t, "autonomous-1", filtered[0].PositionID)
}

func TestIsAutonomousManagedPosition(t *testing.T) {
	assert.True(t, isAutonomousManagedPosition(ManagedOpenPosition{Source: "autonomous"}))
	assert.True(t, isAutonomousManagedPosition(ManagedOpenPosition{Source: "autonomous_scalping"}))
	assert.True(t, isAutonomousManagedPosition(ManagedOpenPosition{Source: ""}))
	assert.False(t, isAutonomousManagedPosition(ManagedOpenPosition{Source: "manual_reconciliation"}))
	assert.False(t, isAutonomousManagedPosition(ManagedOpenPosition{Source: "bootstrap_positions"}))
	assert.False(t, isAutonomousManagedPosition(ManagedOpenPosition{PositionID: "sync-bitget-btc-usdt-long", Source: "autonomous"}))
}

func TestIntegratedQuestHandlers_ResetScalpingFailureState_ClearsCheckpointError(t *testing.T) {
	handlers := NewIntegratedQuestHandlers(nil, nil, nil, nil, nil, nil)
	quest := &Quest{Checkpoint: map[string]interface{}{
		"runtime_failure_streak":  3,
		"runtime_last_failure":    "portfolio safety blocked: maximum allowed 0.00",
		"runtime_last_failure_at": time.Now().UTC().Format(time.RFC3339),
		"runtime_cooldown_until":  time.Now().UTC().Add(2 * time.Minute).Format(time.RFC3339),
		"runtime_hold_cooldown":   true,
		"error":                   "portfolio safety blocked: Position size 6.00 exceeds maximum allowed 0.00 (throttled to 0%)",
	}}

	handlers.resetScalpingFailureState(quest)

	assert.NotContains(t, quest.Checkpoint, "runtime_failure_streak")
	assert.NotContains(t, quest.Checkpoint, "runtime_last_failure")
	assert.NotContains(t, quest.Checkpoint, "runtime_last_failure_at")
	assert.NotContains(t, quest.Checkpoint, "runtime_cooldown_until")
	assert.NotContains(t, quest.Checkpoint, "runtime_hold_cooldown")
	assert.NotContains(t, quest.Checkpoint, "error")
}

func TestShouldSkipClosedOrderFeedback(t *testing.T) {
	tests := []struct {
		name                     string
		order                    map[string]interface{}
		pnl                      decimal.Decimal
		symbol                   string
		side                     string
		openPositionBySymbolSide map[string]struct{}
		expected                 bool
	}{
		{
			name:   "skips probable entry fill when pnl zero and matching open position",
			order:  map[string]interface{}{"tradeSide": "open"},
			pnl:    decimal.Zero,
			symbol: "ADA/USDT",
			side:   "buy",
			openPositionBySymbolSide: map[string]struct{}{
				"ADA/USDT:buy": {},
			},
			expected: true,
		},
		{
			name:   "does not skip when pnl is non-zero",
			order:  map[string]interface{}{"tradeSide": "open"},
			pnl:    decimal.NewFromFloat(0.01),
			symbol: "ADA/USDT",
			side:   "buy",
			openPositionBySymbolSide: map[string]struct{}{
				"ADA/USDT:buy": {},
			},
			expected: false,
		},
		{
			name:                     "does not skip when open position is missing",
			order:                    map[string]interface{}{"tradeSide": "open"},
			pnl:                      decimal.Zero,
			symbol:                   "ADA/USDT",
			side:                     "buy",
			openPositionBySymbolSide: map[string]struct{}{},
			expected:                 false,
		},
		{
			name:   "does not skip close semantic",
			order:  map[string]interface{}{"tradeSide": "close_long"},
			pnl:    decimal.Zero,
			symbol: "ADA/USDT",
			side:   "buy",
			openPositionBySymbolSide: map[string]struct{}{
				"ADA/USDT:buy": {},
			},
			expected: false,
		},
		{
			name:   "does not skip reduce-only semantic",
			order:  map[string]interface{}{"tradeSide": "open", "reduceOnly": true},
			pnl:    decimal.Zero,
			symbol: "ADA/USDT",
			side:   "buy",
			openPositionBySymbolSide: map[string]struct{}{
				"ADA/USDT:buy": {},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := shouldSkipClosedOrderFeedback(tt.order, tt.pnl, tt.symbol, tt.side, tt.openPositionBySymbolSide)
			assert.Equal(t, tt.expected, actual)
		})
	}
}

func TestIntegratedQuestHandlers_IngestClosedOrderFeedback_SkipsProbableEntryFill(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "quest-closed-feedback-skip-entry.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store, err := NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, store.RecordOrderExecution(ctx, LifecycleExecutionRecord{
		OrderID:    "entry-1",
		ChatID:     "chat-1",
		Exchange:   "bitget",
		Symbol:     "ADA/USDT",
		Side:       "buy",
		OrderType:  "market",
		MarketType: "futures",
		Amount:     decimal.NewFromFloat(2),
		EntryPrice: decimal.NewFromFloat(1),
		OpenedAt:   time.Now().UTC().Add(-30 * time.Second),
	}))

	handlers := NewIntegratedQuestHandlers(nil, nil, nil, nil, nil, nil)
	handlers.SetDB(sqliteDB.DB)
	handlers.SetLifecycleStore(store)

	orderExecutor := new(MockScalpingOrderExecutor)
	handlers.SetOrderExecutor(orderExecutor)

	quest := &Quest{
		ID:         "quest-skip-probable-entry-fill",
		Checkpoint: map[string]interface{}{},
		Metadata: map[string]string{
			"chat_id": "chat-1",
		},
	}

	orderExecutor.
		On("GetClosedOrders", mock.Anything, "bitget", "ADA/USDT", 20).
		Return([]map[string]interface{}{
			{
				"orderId":      "entry-1",
				"side":         "buy",
				"tradeSide":    "open",
				"avgOpenPrice": "1.0",
				"avgPrice":     "1.0",
				"filled":       "2.0",
				"pnl":          "0",
			},
		}, nil).
		Once()

	handlers.ingestClosedOrderFeedback(ctx, quest, "bitget", "ADA/USDT")

	var tradeRows int
	err = sqliteDB.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM trades WHERE order_id = $1`, "entry-1").Scan(&tradeRows)
	require.NoError(t, err)
	assert.Equal(t, 0, tradeRows)

	positions, err := store.ListManagedOpenPositions(ctx, "chat-1", "bitget", 20)
	require.NoError(t, err)
	require.Len(t, positions, 1)
	assert.Equal(t, "entry-1", positions[0].OrderID)

	processed := getProcessedOrderIDs(quest.Checkpoint["processed_closed_order_ids"])
	assert.False(t, processed["entry-1"])

	orderExecutor.AssertExpectations(t)
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

func TestShouldNotifyPnLReconciliation(t *testing.T) {
	now := time.Now().UTC()
	quest := &Quest{Checkpoint: map[string]interface{}{}}

	assert.True(t, shouldNotifyPnLReconciliation(quest, "summary-a", now))
	recordPnLReconciliationNotification(quest, "summary-a", now)
	assert.False(t, shouldNotifyPnLReconciliation(quest, "summary-a", now.Add(5*time.Minute)))
	assert.True(t, shouldNotifyPnLReconciliation(quest, "summary-b", now.Add(5*time.Minute)))
	assert.True(t, shouldNotifyPnLReconciliation(quest, "summary-a", now.Add(16*time.Minute)))
}

func TestRecordPnLReconciliationNotification_InitializesCheckpoint(t *testing.T) {
	quest := &Quest{}
	now := time.Now().UTC()
	recordPnLReconciliationNotification(quest, "summary-a", now)
	require.NotNil(t, quest.Checkpoint)
	assert.Equal(t, "summary-a", quest.Checkpoint["last_pnl_reconciliation_summary"])
	assert.Equal(t, now.Format(time.RFC3339), quest.Checkpoint["last_pnl_reconciliation_sent_at"])
}

func TestStructuredHoldBlock_UsesDecisionPolicySpreadThreshold(t *testing.T) {
	decision := &AITradingDecision{
		Action:                 "hold",
		ReasonCategory:         aiReasonStrategyHold,
		MaxBidAskSpreadPct:     0.22,
		ExecutionGate:          &appautonomy.ExecutionGateSnapshot{Allowed: false, BlockCode: appautonomy.CandidateRejectSpreadTooWide, BlockReason: "spread gate"},
		ConfidenceKnown:        true,
		EffectiveMinConfidence: 0.65,
	}

	code, next, human := structuredHoldBlock(decision)
	assert.Equal(t, appautonomy.CandidateRejectSpreadTooWide, code)
	assert.Equal(t, "spread gate", human)
	assert.Contains(t, next, "0.22%")
}

func TestIntegratedQuestHandlers_IngestClosedOrderFeedback_PersistsLegacyTradeClose(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "quest-trade-journal.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	handlers := NewIntegratedQuestHandlers(nil, nil, nil, nil, nil, nil)
	handlers.SetDB(sqliteDB.DB)

	orderExecutor := new(MockScalpingOrderExecutor)
	handlers.SetOrderExecutor(orderExecutor)

	quest := &Quest{
		ID:         "quest-1",
		Checkpoint: map[string]interface{}{},
		Metadata: map[string]string{
			"chat_id": "chat-1",
		},
	}

	entryPrice := decimal.NewFromFloat(1.0)
	handlers.persistLegacyTradeEntry(context.Background(), quest, &AITradingDecision{
		Action:      "buy",
		Symbol:      "ADA/USDT",
		SizePercent: 1.0,
		OrderID:     "ord-1",
		EntryPrice:  &entryPrice,
	}, "bitget", TradingPortfolio{
		USDTBalance:   100,
		StrategyPhase: "bootstrap",
	}, "ord-1")

	closedAt := time.Now().UTC()
	orderExecutor.
		On("GetClosedOrders", mock.Anything, "bitget", "ADA/USDT", 20).
		Return([]map[string]interface{}{
			{
				"orderId":      "ord-1",
				"side":         "buy",
				"avgOpenPrice": "1.0",
				"avgPrice":     "1.05",
				"filled":       "2.0",
				"pnl":          "0.10",
				"fees":         "0.01",
				"uTime":        closedAt.UnixMilli(),
			},
		}, nil).
		Once()

	handlers.ingestClosedOrderFeedback(context.Background(), quest, "bitget", "ADA/USDT")

	var status string
	var exitPrice float64
	var pnl float64
	var fees float64
	err = sqliteDB.DB.QueryRowContext(
		context.Background(),
		`SELECT status, exit_price, pnl, fees FROM trades WHERE order_id = $1`,
		"ord-1",
	).Scan(&status, &exitPrice, &pnl, &fees)
	require.NoError(t, err)
	assert.Equal(t, "closed", status)
	assert.InDelta(t, 1.05, exitPrice, 0.0001)
	assert.InDelta(t, 0.10, pnl, 0.0001)
	assert.InDelta(t, 0.01, fees, 0.0001)
	assert.Contains(t, quest.Checkpoint["processed_closed_order_ids"], "ord-1")

	orderExecutor.AssertExpectations(t)
}

func TestIntegratedQuestHandlers_IngestClosedOrderFeedback_SkipsLegacyJournalWhenSchemaUnavailable(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "quest-trade-journal-schema-unavailable.db")
	sqliteDB, err := database.NewSQLiteConnection(dbPath)
	require.NoError(t, err)

	handlers := NewIntegratedQuestHandlers(nil, nil, nil, nil, nil, nil)
	handlers.db = sqliteDB.DB

	orderExecutor := new(MockScalpingOrderExecutor)
	handlers.SetOrderExecutor(orderExecutor)

	quest := &Quest{
		ID:         "quest-schema-unavailable",
		Checkpoint: map[string]interface{}{},
		Metadata: map[string]string{
			"chat_id": "chat-1",
		},
	}

	orderExecutor.
		On("GetClosedOrders", mock.Anything, "bitget", "ADA/USDT", 20).
		Return([]map[string]interface{}{
			{
				"orderId":      "ord-schema-unavailable",
				"side":         "buy",
				"avgOpenPrice": "1.0",
				"avgPrice":     "1.05",
				"filled":       "1.0",
				"pnl":          "0.05",
			},
		}, nil).
		Once()

	require.NoError(t, sqliteDB.Close())

	require.NotPanics(t, func() {
		handlers.ingestClosedOrderFeedback(context.Background(), quest, "bitget", "ADA/USDT")
	})

	sqliteDB2, err := database.NewSQLiteConnection(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB2.Close()
	})

	var tradesTableCount int
	err = sqliteDB2.DB.QueryRowContext(
		context.Background(),
		`SELECT COUNT(1) FROM sqlite_master WHERE type = 'table' AND name = 'trades'`,
	).Scan(&tradesTableCount)
	require.NoError(t, err)
	assert.Equal(t, 0, tradesTableCount)

	orderExecutor.AssertExpectations(t)
}

func TestIntegratedQuestHandlers_IngestClosedOrderFeedback_LegacyCloseWithoutOptionalFields(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "quest-trade-journal-missing-fields.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	handlers := NewIntegratedQuestHandlers(nil, nil, nil, nil, nil, nil)
	handlers.SetDB(sqliteDB.DB)

	orderExecutor := new(MockScalpingOrderExecutor)
	handlers.SetOrderExecutor(orderExecutor)

	quest := &Quest{
		ID:         "quest-missing-fields",
		Checkpoint: map[string]interface{}{},
		Metadata: map[string]string{
			"chat_id": "chat-1",
		},
	}

	entryPrice := decimal.NewFromFloat(100.0)
	handlers.persistLegacyTradeEntry(context.Background(), quest, &AITradingDecision{
		Action:      "buy",
		Symbol:      "ETH/USDT",
		SizePercent: 0.25,
		OrderID:     "ord-no-optional",
		EntryPrice:  &entryPrice,
	}, "bitget", TradingPortfolio{
		USDTBalance:   50,
		StrategyPhase: "bootstrap",
	}, "ord-no-optional")

	orderExecutor.
		On("GetClosedOrders", mock.Anything, "bitget", "ETH/USDT", 20).
		Return([]map[string]interface{}{
			{
				"orderId":      "ord-no-optional",
				"side":         "buy",
				"avgOpenPrice": "100.0",
				"avgPrice":     "101.0",
				"filled":       "0.25",
				"pnl":          "0.25",
			},
		}, nil).
		Once()

	handlers.ingestClosedOrderFeedback(context.Background(), quest, "bitget", "ETH/USDT")

	var status string
	err = sqliteDB.DB.QueryRowContext(
		context.Background(),
		`SELECT status FROM trades WHERE order_id = $1`,
		"ord-no-optional",
	).Scan(&status)
	require.NoError(t, err)
	assert.Equal(t, "closed", status)
	assert.Contains(t, quest.Checkpoint["processed_closed_order_ids"], "ord-no-optional")

	orderExecutor.AssertExpectations(t)
}

func TestIntegratedQuestHandlers_PersistLegacyTradeEntry_StoresBaseSizeAndCostBasis(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "quest-trade-journal-size-cost-basis.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	handlers := NewIntegratedQuestHandlers(nil, nil, nil, nil, nil, nil)
	handlers.SetDB(sqliteDB.DB)

	quest := &Quest{
		ID:         "quest-size-cost-basis",
		Checkpoint: map[string]interface{}{},
		Metadata: map[string]string{
			"chat_id": "chat-1",
		},
	}

	entryPrice := decimal.NewFromFloat(100)
	handlers.persistLegacyTradeEntry(context.Background(), quest, &AITradingDecision{
		Action:      "buy",
		Symbol:      "BTC/USDT",
		SizePercent: 1.0,
		OrderID:     "entry-size-cost-basis",
		EntryPrice:  &entryPrice,
	}, "bitget", TradingPortfolio{
		USDTBalance:   50,
		StrategyPhase: "bootstrap",
	}, "entry-size-cost-basis")

	var size float64
	var costBasis float64
	err = sqliteDB.DB.QueryRowContext(
		context.Background(),
		`SELECT size, cost_basis FROM trades WHERE order_id = $1`,
		"entry-size-cost-basis",
	).Scan(&size, &costBasis)
	require.NoError(t, err)
	assert.InDelta(t, 0.005, size, 0.0000001)
	assert.InDelta(t, 0.5, costBasis, 0.0000001)
}

func TestIntegratedQuestHandlers_PersistLegacyTradeEntry_StoresZeroSizeWithoutEntryPrice(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "quest-trade-journal-size-fallback.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	handlers := NewIntegratedQuestHandlers(nil, nil, nil, nil, nil, nil)
	handlers.SetDB(sqliteDB.DB)

	quest := &Quest{
		ID:         "quest-size-fallback",
		Checkpoint: map[string]interface{}{},
		Metadata: map[string]string{
			"chat_id": "chat-1",
		},
	}

	handlers.persistLegacyTradeEntry(context.Background(), quest, &AITradingDecision{
		Action:      "buy",
		Symbol:      "BTC/USDT",
		SizePercent: 0.42,
		OrderID:     "entry-size-fallback",
	}, "bitget", TradingPortfolio{
		USDTBalance:   0,
		StrategyPhase: "bootstrap",
	}, "entry-size-fallback")

	var size float64
	var costBasis float64
	err = sqliteDB.DB.QueryRowContext(
		context.Background(),
		`SELECT size, cost_basis FROM trades WHERE order_id = $1`,
		"entry-size-fallback",
	).Scan(&size, &costBasis)
	require.NoError(t, err)
	assert.InDelta(t, 0.0, size, 0.0000001)
	assert.InDelta(t, 0.0, costBasis, 0.0000001)
}

func TestIntegratedQuestHandlers_SetDB_RetriesTradeJournalInitAfterDBReplacement(t *testing.T) {
	dbPath1 := filepath.Join(t.TempDir(), "quest-trade-journal-first.db")
	sqliteDB1, err := database.NewSQLiteConnection(dbPath1)
	require.NoError(t, err)

	handlers := NewIntegratedQuestHandlers(nil, nil, nil, nil, nil, nil)

	require.NoError(t, sqliteDB1.Close())
	handlers.SetDB(sqliteDB1.DB)

	sqliteDB2, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "quest-trade-journal-second.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB2.Close()
	})

	handlers.SetDB(sqliteDB2.DB)

	quest := &Quest{
		ID:         "quest-retry-schema",
		Checkpoint: map[string]interface{}{},
		Metadata: map[string]string{
			"chat_id": "chat-1",
		},
	}

	entryPrice := decimal.NewFromFloat(10)
	handlers.persistLegacyTradeEntry(context.Background(), quest, &AITradingDecision{
		Action:      "buy",
		Symbol:      "SOL/USDT",
		SizePercent: 1.0,
		OrderID:     "retry-entry",
		EntryPrice:  &entryPrice,
	}, "bitget", TradingPortfolio{
		USDTBalance:   100,
		StrategyPhase: "bootstrap",
	}, "retry-entry")

	var count int
	err = sqliteDB2.DB.QueryRowContext(context.Background(), `SELECT COUNT(1) FROM trades WHERE order_id = $1`, "retry-entry").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestIntegratedQuestHandlers_ApplyScalpingCycleDecisionDiagnostics_ClearsStaleDecisionMetadata(t *testing.T) {
	handlers := NewIntegratedQuestHandlers(nil, nil, nil, nil, nil, nil)
	quest := &Quest{Checkpoint: map[string]interface{}{
		"account_tier":                          "micro",
		"effective_min_confidence":              0.72,
		"effective_max_capital_pct":             1.25,
		"effective_max_concurrent_positions":    1,
		"effective_policy_adjustments":          []string{"recovery_cap"},
		"effective_policy_adjustment_counts":    map[string]int{"recovery_cap": 1},
		"candidate_universe_count":              8,
		"candidate_ranked_count":                3,
		"candidate_viable_count":                1,
		"top_candidate_rejections":              []map[string]interface{}{{"symbol": "OPN/USDT", "reason": "spread_too_wide"}},
		"top_candidate_rejection_reason_counts": map[string]int{"spread_too_wide": 1},
		"rollout_stage_current":                 "paper",
		"rollout_status_current":                "paused",
		"rollout_gate_reason_current":           "safe mode",
	}}

	handlers.applyScalpingCycleDecisionDiagnostics(quest, &AITradingDecision{})

	for _, key := range []string{
		"account_tier",
		"effective_min_confidence",
		"effective_max_capital_pct",
		"effective_max_concurrent_positions",
		"effective_policy_adjustments",
		"effective_policy_adjustment_counts",
		"candidate_universe_count",
		"candidate_ranked_count",
		"candidate_viable_count",
		"top_candidate_rejections",
		"top_candidate_rejection_reason_counts",
		"rollout_stage_current",
		"rollout_status_current",
		"rollout_gate_reason_current",
	} {
		_, ok := quest.Checkpoint[key]
		assert.False(t, ok, key)
	}
}

func TestIntegratedQuestHandlers_ApplyScalpingCycleDecisionDiagnostics_ClearsNilDecisionMetadata(t *testing.T) {
	handlers := NewIntegratedQuestHandlers(nil, nil, nil, nil, nil, nil)
	quest := &Quest{Checkpoint: map[string]interface{}{
		"account_tier":                          "small",
		"effective_min_confidence":              0.70,
		"effective_max_capital_pct":             2.5,
		"effective_max_concurrent_positions":    3,
		"effective_policy_adjustments":          []string{"performance_cap_applied"},
		"effective_policy_adjustment_counts":    map[string]int{"performance_cap_applied": 1},
		"top_candidate_rejection_reason_counts": map[string]int{"spread_too_wide": 1},
	}}

	handlers.applyScalpingCycleDecisionDiagnostics(quest, nil)

	for _, key := range []string{
		"account_tier",
		"effective_min_confidence",
		"effective_max_capital_pct",
		"effective_max_concurrent_positions",
		"effective_policy_adjustments",
		"effective_policy_adjustment_counts",
		"top_candidate_rejection_reason_counts",
	} {
		_, ok := quest.Checkpoint[key]
		assert.False(t, ok, key)
	}
}

func TestIntegratedQuestHandlers_ApplyScalpingCycleDecisionDiagnostics_PopulatesCountMetadata(t *testing.T) {
	handlers := NewIntegratedQuestHandlers(nil, nil, nil, nil, nil, nil)
	quest := &Quest{Checkpoint: map[string]interface{}{}}
	decision := &AITradingDecision{
		PolicyAdjustments: []string{
			"weak_recent_win_rate",
			"weak_recent_win_rate",
			"negative_expectancy_cap",
		},
		CandidateFunnelKnown: true,
		CandidateFunnel: appautonomy.CandidateFunnelSnapshot{
			CandidateUniverseCount: 4,
			CandidateRankedCount:   3,
			CandidateViableCount:   1,
			TopCandidateRejections: []appautonomy.CandidateRejection{
				{Symbol: "AAA/USDT", Reason: appautonomy.CandidateRejectSpreadTooWide},
				{Symbol: "BBB/USDT", Reason: appautonomy.CandidateRejectSpreadTooWide},
				{Symbol: "CCC/USDT", Reason: appautonomy.CandidateRejectMissingOrderbookSignal},
			},
		},
	}

	handlers.applyScalpingCycleDecisionDiagnostics(quest, decision)

	adjCounts, ok := quest.Checkpoint["effective_policy_adjustment_counts"].(map[string]int)
	require.True(t, ok)
	assert.Equal(t, 2, adjCounts["weak_recent_win_rate"])
	assert.Equal(t, 1, adjCounts["negative_expectancy_cap"])

	rejectCounts, ok := quest.Checkpoint["top_candidate_rejection_reason_counts"].(map[string]int)
	require.True(t, ok)
	assert.Equal(t, 2, rejectCounts[appautonomy.CandidateRejectSpreadTooWide])
	assert.Equal(t, 1, rejectCounts[appautonomy.CandidateRejectMissingOrderbookSignal])
}

func TestIntegratedQuestHandlers_RecordTradeDecision_DoesNotSynthesizeTradeMemoryID(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "quest-trade-decision.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	handlers := NewIntegratedQuestHandlers(nil, nil, nil, nil, nil, nil)
	handlers.SetDB(sqliteDB.DB)

	quest := &Quest{
		ID:         "quest-no-order-id",
		Checkpoint: map[string]interface{}{"trade_memory_id": "stale"},
		Metadata: map[string]string{
			"chat_id":       "chat-1",
			"definition_id": "def-1",
		},
	}

	handlers.recordTradeDecision(context.Background(), quest, &AITradingDecision{
		Action:      "buy",
		Symbol:      "SOL/USDT",
		SizePercent: 1.0,
		Confidence:  0.8,
		OrderID:     "",
	}, "bitget", TradingPortfolio{
		USDTBalance:   100,
		StrategyPhase: "bootstrap",
	})

	_, ok := quest.Checkpoint["trade_memory_id"]
	assert.False(t, ok)

	require.NoError(t, handlers.ensureTradeJournalSchema())

	var count int
	err = sqliteDB.DB.QueryRowContext(context.Background(), `SELECT COUNT(1) FROM trades`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
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
