package services

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/ai/llm"
	"github.com/irfndi/neuratrade/internal/autonomous"
	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegratedQuestHandlersResolveOperationalModePrefersStoredState(t *testing.T) {
	testCases := []struct {
		name          string
		opModeService *OperationalModeService
		quest         *Quest
		chatID        string
		expected      OperationalMode
	}{
		{
			name: "stored_live_overrides_dry_metadata",
			opModeService: &OperationalModeService{
				config: DefaultOperationalModeConfig(),
				states: map[string]*OperationalModeState{
					"chat-live": {
						ChatID: "chat-live",
						Mode:   OpModeLive,
					},
				},
			},
			quest:    &Quest{Metadata: map[string]string{"dry_run": "true"}},
			chatID:   "chat-live",
			expected: OpModeLive,
		},
		{
			name: "default_service_mode_stays_dry",
			opModeService: &OperationalModeService{
				config: DefaultOperationalModeConfig(),
				states: map[string]*OperationalModeState{},
			},
			quest:    &Quest{Metadata: map[string]string{"dry_run": "false"}},
			chatID:   "chat-default",
			expected: OpModeDry,
		},
		{
			name: "stored_paper_preserves_paper_mode",
			opModeService: &OperationalModeService{
				config: DefaultOperationalModeConfig(),
				states: map[string]*OperationalModeState{
					"chat-paper": {
						ChatID: "chat-paper",
						Mode:   ModePaper,
					},
				},
			},
			quest:    &Quest{Metadata: map[string]string{"dry_run": "false"}},
			chatID:   "chat-paper",
			expected: ModePaper,
		},
		{
			name: "unsupported_stored_mode_is_treated_as_dry",
			opModeService: &OperationalModeService{
				config: DefaultOperationalModeConfig(),
				states: map[string]*OperationalModeState{
					"chat-unknown": {
						ChatID: "chat-unknown",
						Mode:   OperationalMode("unknown"),
					},
				},
			},
			quest:    &Quest{Metadata: map[string]string{"paper_trading": "true"}},
			chatID:   "chat-unknown",
			expected: ModePaper,
		},
		{
			name: "unsupported_stored_mode_without_metadata_stays_dry",
			opModeService: &OperationalModeService{
				config: DefaultOperationalModeConfig(),
				states: map[string]*OperationalModeState{
					"chat-unknown-dry": {
						ChatID: "chat-unknown-dry",
						Mode:   OperationalMode("unknown"),
					},
				},
			},
			quest:    &Quest{Metadata: map[string]string{"dry_run": "false"}},
			chatID:   "chat-unknown-dry",
			expected: OpModeDry,
		},
		{
			name:          "metadata_dry_run_without_service",
			opModeService: nil,
			quest:         &Quest{Metadata: map[string]string{"dry_run": "true"}},
			chatID:        "chat-meta-dry",
			expected:      OpModeDry,
		},
		{
			name:          "metadata_paper_trading_without_service",
			opModeService: nil,
			quest:         &Quest{Metadata: map[string]string{"paper_trading": "true"}},
			chatID:        "chat-meta-paper",
			expected:      ModePaper,
		},
		{
			name:          "no_service_or_metadata_defaults_dry",
			opModeService: nil,
			quest:         &Quest{Metadata: map[string]string{}},
			chatID:        "chat-live-default",
			expected:      OpModeDry,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			handlers := &IntegratedQuestHandlers{opModeService: tc.opModeService}
			assert.Equal(t, tc.expected, handlers.resolveOperationalMode(tc.chatID, tc.quest))
		})
	}
}

func TestIntegratedQuestHandlersResolveOperationalModeRuntimePaperEnvOverridesStoredLive(t *testing.T) {
	t.Setenv("FEATURE_PAPER_TRADING", "true")
	t.Setenv("FEATURE_REAL_TRADING", "false")

	handlers := &IntegratedQuestHandlers{opModeService: &OperationalModeService{
		config: DefaultOperationalModeConfig(),
		states: map[string]*OperationalModeState{
			"paper-env-chat": {ChatID: "paper-env-chat", Mode: OpModeLive},
		},
	}}

	assert.Equal(t, ModePaper, handlers.resolveOperationalMode("paper-env-chat", &Quest{}))
}

func TestIntegratedQuestHandlersSyncScalpingStrategyModeFollowsOperatorMode(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "rollout-sync.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store := NewAutonomousRolloutStore(sqliteDB.DB)
	require.NoError(t, store.InitSchema(context.Background()))

	handlers := &IntegratedQuestHandlers{
		autonomyCoordinator: NewScalpingAutonomyCoordinator(store, AIScalpingConfig{}),
	}

	ctx := context.Background()
	require.NoError(t, handlers.syncScalpingStrategyMode(ctx, "1082762347", OpModeLive))

	state, err := store.GetRolloutState(ctx, ScalpingStrategyID("1082762347"))
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, autonomous.StageLive, state.CurrentStage)
	assert.Equal(t, autonomous.StatusActive, state.Status)

	require.NoError(t, handlers.syncScalpingStrategyMode(ctx, "1082762347", OpModeDry))

	state, err = store.GetRolloutState(ctx, ScalpingStrategyID("1082762347"))
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, autonomous.StageShadow, state.CurrentStage)
	assert.Equal(t, autonomous.StatusActive, state.Status)

	require.NoError(t, handlers.syncScalpingStrategyMode(ctx, "1082762347", ModePaper))

	state, err = store.GetRolloutState(ctx, ScalpingStrategyID("1082762347"))
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, autonomous.StagePaper, state.CurrentStage)
	assert.Equal(t, autonomous.StatusActive, state.Status)
}

func TestIntegratedQuestHandlersSyncScalpingStrategyMode_BlocksNonLiveTransitionWithExposure(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "rollout-sync-exposure.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store := NewAutonomousRolloutStore(sqliteDB.DB)
	require.NoError(t, store.InitSchema(context.Background()))

	lifecycleStore, err := NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)

	handlers := &IntegratedQuestHandlers{
		autonomyCoordinator: NewScalpingAutonomyCoordinator(store, AIScalpingConfig{}),
		lifecycleStore:      lifecycleStore,
	}

	chatID := "1082762347"
	ctx := WithScalpingAutonomyScope(context.Background(), ScalpingAutonomyScope{
		ChatID:     chatID,
		StrategyID: ScalpingStrategyID(chatID),
		Exchange:   "bitget",
	})

	require.NoError(t, handlers.syncScalpingStrategyMode(ctx, chatID, OpModeLive))
	require.NoError(t, lifecycleStore.RecordOrderExecution(ctx, LifecycleExecutionRecord{
		OrderID:    "ord-exposure",
		ChatID:     chatID,
		Exchange:   "bitget",
		Symbol:     "BTC/USDT",
		Side:       "buy",
		OrderType:  "market",
		MarketType: "futures",
		Amount:     decimal.NewFromFloat(0.01),
		EntryPrice: decimal.NewFromFloat(50000),
		OpenedAt:   time.Now().UTC(),
	}))

	err = handlers.syncScalpingStrategyMode(ctx, chatID, OpModeDry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot switch scalping:1082762347:default to non-live mode")

	state, err := store.GetRolloutState(ctx, ScalpingStrategyID(chatID))
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, autonomous.StageLive, state.CurrentStage)
}

func TestIntegratedQuestHandlersSyncScalpingStrategyMode_IgnoresPaperLifecycleExposure(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "rollout-sync-paper-exposure.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store := NewAutonomousRolloutStore(sqliteDB.DB)
	require.NoError(t, store.InitSchema(context.Background()))

	lifecycleStore, err := NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)

	handlers := &IntegratedQuestHandlers{
		autonomyCoordinator: NewScalpingAutonomyCoordinator(store, AIScalpingConfig{}),
		lifecycleStore:      lifecycleStore,
	}

	chatID := "1082762347"
	ctx := WithScalpingAutonomyScope(context.Background(), ScalpingAutonomyScope{
		ChatID:     chatID,
		StrategyID: ScalpingStrategyID(chatID),
		Exchange:   "bitget",
	})

	require.NoError(t, lifecycleStore.RecordOrderExecution(ctx, LifecycleExecutionRecord{
		OrderID:    "paper-ord-exposure",
		ChatID:     chatID,
		Exchange:   "bitget",
		Symbol:     "BTC/USDT",
		Side:       "buy",
		OrderType:  "market",
		MarketType: "futures",
		Amount:     decimal.NewFromFloat(0.01),
		EntryPrice: decimal.NewFromFloat(50000),
		Source:     scalpingPaperLifecycleSource,
		OpenedAt:   time.Now().UTC(),
	}))

	require.NoError(t, handlers.syncScalpingStrategyMode(ctx, chatID, ModePaper))

	state, err := store.GetRolloutState(ctx, ScalpingStrategyID(chatID))
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, autonomous.StagePaper, state.CurrentStage)
}

func TestIntegratedQuestHandlersSyncScalpingStrategyMode_DetectsLiveExposureHiddenBehindPaperLimit(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "rollout-sync-live-hidden-by-paper.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store := NewAutonomousRolloutStore(sqliteDB.DB)
	require.NoError(t, store.InitSchema(context.Background()))

	lifecycleStore, err := NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)

	handlers := &IntegratedQuestHandlers{
		autonomyCoordinator: NewScalpingAutonomyCoordinator(store, AIScalpingConfig{}),
		lifecycleStore:      lifecycleStore,
	}

	chatID := "1082762347"
	ctx := WithScalpingAutonomyScope(context.Background(), ScalpingAutonomyScope{
		ChatID:     chatID,
		StrategyID: ScalpingStrategyID(chatID),
		Exchange:   "bitget",
	})

	require.NoError(t, handlers.syncScalpingStrategyMode(ctx, chatID, OpModeLive))
	require.NoError(t, lifecycleStore.RecordOrderExecution(ctx, LifecycleExecutionRecord{
		OrderID:    "live-hidden-behind-paper",
		ChatID:     chatID,
		Exchange:   "bitget",
		Symbol:     "BTC/USDT",
		Side:       "buy",
		OrderType:  "market",
		MarketType: "futures",
		Amount:     decimal.NewFromFloat(0.01),
		EntryPrice: decimal.NewFromFloat(50000),
		Source:     scalpingLifecycleSource,
		OpenedAt:   time.Now().UTC().Add(-time.Hour),
	}))
	_, err = sqliteDB.DB.ExecContext(ctx, `
		UPDATE trading_orders
		SET status = 'closed'
		WHERE order_id = $1
	`, "live-hidden-behind-paper")
	require.NoError(t, err)

	for i := 0; i < 25; i++ {
		require.NoError(t, lifecycleStore.RecordOrderExecution(ctx, LifecycleExecutionRecord{
			OrderID:    fmt.Sprintf("paper-ord-hidden-%02d", i),
			ChatID:     chatID,
			Exchange:   "bitget",
			Symbol:     fmt.Sprintf("PAPER%02d/USDT", i),
			Side:       "buy",
			OrderType:  "market",
			MarketType: "futures",
			Amount:     decimal.NewFromFloat(0.01),
			EntryPrice: decimal.NewFromFloat(1 + float64(i)),
			Source:     scalpingPaperLifecycleSource,
			OpenedAt:   time.Now().UTC(),
		}))
	}

	err = handlers.syncScalpingStrategyMode(ctx, chatID, ModePaper)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot switch scalping:1082762347:default to non-live mode")
}

type syncFailureBalanceFetcher struct{}

func (syncFailureBalanceFetcher) FetchBalance(context.Context, string) (*ccxt.BalanceResponse, error) {
	return &ccxt.BalanceResponse{}, nil
}

func TestIntegratedQuestHandlersExecuteAIScalping_ReturnsErrorWhenServiceNil(t *testing.T) {
	handlers := &IntegratedQuestHandlers{
		ccxtService:   syncFailureBalanceFetcher{},
		opModeService: &OperationalModeService{config: DefaultOperationalModeConfig(), states: map[string]*OperationalModeState{}},
	}

	quest := &Quest{
		Metadata:   map[string]string{"chat_id": "1082762347"},
		Checkpoint: map[string]interface{}{},
	}

	err := handlers.executeAIScalping(context.Background(), quest, "1082762347", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not available")
}

func TestIntegratedQuestHandlersHandleScalpingExecution_FallsBackWhenNoAIService(t *testing.T) {
	handlers := &IntegratedQuestHandlers{
		ccxtService:   syncFailureBalanceFetcher{},
		opModeService: &OperationalModeService{config: DefaultOperationalModeConfig(), states: map[string]*OperationalModeState{}},
	}

	quest := &Quest{
		Metadata:   map[string]string{"chat_id": "1082762347"},
		Checkpoint: map[string]interface{}{},
	}

	err := handlers.handleScalpingExecution(context.Background(), quest)
	require.NoError(t, err)
	assert.Equal(t, "ai_unavailable_hold", quest.Checkpoint["status"])
}

func TestIntegratedQuestHandlersExecuteAIScalping_PaperModeUsesVirtualBalanceAndPaperMetadata(t *testing.T) {
	mockLLM := &MockLLMClient{
		Responses: []*llm.CompletionResponse{
			{
				Message: llm.Message{
					Content: `{"action":"hold","symbol":"","size_pct":0,"confidence":0.41,"reasoning":"No valid trade setup; waiting for qualified setup.","stop_loss":null,"take_profit":null}`,
				},
			},
		},
	}
	mockCCXT := &mockAIScalpingCCXT{
		markets: &ccxt.MarketsResponse{
			Exchange: "bitget",
			Symbols:  []string{"AAA/USDT", "BBB/USDT"},
			Count:    2,
		},
		marketData: []ccxt.MarketPriceInterface{
			mockMarketPrice{symbol: "AAA/USDT", price: 1, volume: 1000, high24h: 1.05, low24h: 0.95, bid: 0.999, ask: 1.001, exchange: "bitget"},
			mockMarketPrice{symbol: "BBB/USDT", price: 1, volume: 900, high24h: 1.04, low24h: 0.96, bid: 0.999, ask: 1.001, exchange: "bitget"},
		},
		orderBooks: map[string]*ccxt.OrderBookResponse{
			"AAA/USDT": {OrderBook: ccxt.OrderBook{Bids: []ccxt.OrderBookEntry{{Price: decimal.NewFromFloat(0.999), Amount: decimal.NewFromInt(5)}}, Asks: []ccxt.OrderBookEntry{{Price: decimal.NewFromFloat(1.001), Amount: decimal.NewFromInt(4)}}}},
			"BBB/USDT": {OrderBook: ccxt.OrderBook{Bids: []ccxt.OrderBookEntry{{Price: decimal.NewFromFloat(0.999), Amount: decimal.NewFromInt(6)}}, Asks: []ccxt.OrderBookEntry{{Price: decimal.NewFromFloat(1.001), Amount: decimal.NewFromInt(5)}}}},
		},
	}
	aiSvc := NewAIScalpingService(
		AIScalpingConfig{
			Exchange:          "bitget",
			Model:             "glm-5",
			Timeout:           2 * time.Second,
			MaxTokens:         600,
			AutoExecute:       false,
			MaxPairsToAnalyze: 2,
			MaxCandidatePairs: 2,
			OrderBookPairs:    2,
			EnforceFutures:    false,
			MinConfidence:     0.65,
			MaxCapitalPct:     5,
			PreTradeGate:      true,
			StructuredRetries: 1,
		},
		mockLLM,
		nil,
		mockCCXT,
		nil,
		nil,
	)

	handlers := &IntegratedQuestHandlers{
		ccxtService:       mockCCXT,
		opModeService:     &OperationalModeService{config: DefaultOperationalModeConfig(), states: map[string]*OperationalModeState{"paper-chat": {ChatID: "paper-chat", Mode: ModePaper}}},
		aiScalpingService: aiSvc,
	}

	quest := &Quest{
		Metadata:   map[string]string{"chat_id": "paper-chat"},
		Checkpoint: map[string]interface{}{},
	}

	err := handlers.executeAIScalping(context.Background(), quest, "paper-chat", aiSvc)
	require.NoError(t, err)
	assert.Equal(t, "true", quest.Metadata["paper_trading"])
	assert.Equal(t, "true", quest.Metadata["dry_run"])
	assert.Equal(t, true, quest.Checkpoint["dry_run"])
	assert.Equal(t, 1000.0, quest.Checkpoint["virtual_balance"])
	assert.Equal(t, "hold", quest.Checkpoint["status"])
	assert.Equal(t, "hold", quest.Checkpoint["ai_action"])
	assert.Equal(t, 1, mockLLM.CallCount)
	assert.Zero(t, mockCCXT.fetchCalls, "paper mode should not fetch live balance")
}

func TestIntegratedQuestHandlersPersistScalpingExecutionLifecycle_LinksPaperOrder(t *testing.T) {
	ctx := context.Background()
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "paper-scalping-lifecycle.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	lifecycleStore, err := NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)
	telemetryStore := NewScalpingTelemetryStore(sqliteDB, nil)
	require.NoError(t, telemetryStore.EnsureSchema(ctx))

	cycleID, err := telemetryStore.InsertCycleRecord(ctx, CycleRecord{
		ID:         "paper-cycle-1",
		ChatID:     "paper-chat",
		Exchange:   "bitget",
		CycleAt:    time.Now().UTC(),
		Symbol:     "AAA/USDT",
		Action:     "buy",
		Confidence: 0.92,
	})
	require.NoError(t, err)

	entry := decimal.NewFromFloat(1.23)
	stop := decimal.NewFromFloat(1.20)
	take := decimal.NewFromFloat(1.29)
	handlers := &IntegratedQuestHandlers{
		lifecycleStore: lifecycleStore,
		telemetryStore: telemetryStore,
	}

	handlers.persistScalpingExecutionLifecycle(
		ctx,
		ModePaper,
		&AITradingDecision{
			Action:      "buy",
			Symbol:      "AAA/USDT",
			SizePercent: 2.5,
			OrderID:     "paper-order-aaa-buy",
			EntryPrice:  &entry,
			StopLoss:    &stop,
			TakeProfit:  &take,
		},
		"paper-chat",
		"bitget",
		TradingPortfolio{USDTBalance: 1000},
		true,
		cycleID,
	)

	var orderStatus, orderSource, positionStatus, telemetryOrderID string
	var amount decimal.Decimal
	err = sqliteDB.DB.QueryRowContext(ctx, `
		SELECT status, source, amount
		FROM trading_orders
		WHERE order_id = $1
	`, "paper-order-aaa-buy").Scan(&orderStatus, &orderSource, &amount)
	require.NoError(t, err)
	assert.Equal(t, "open", orderStatus)
	assert.Equal(t, scalpingPaperLifecycleSource, orderSource)
	assert.True(t, amount.Equal(decimal.NewFromInt(25)), "amount should use paper portfolio size percentage")

	err = sqliteDB.DB.QueryRowContext(ctx, `
		SELECT status
		FROM trading_positions
		WHERE order_id = $1
	`, "paper-order-aaa-buy").Scan(&positionStatus)
	require.NoError(t, err)
	assert.Equal(t, "open", positionStatus)

	err = sqliteDB.DB.QueryRowContext(ctx, `
		SELECT order_id
		FROM scalping_cycle_telemetry
		WHERE id = $1
	`, cycleID).Scan(&telemetryOrderID)
	require.NoError(t, err)
	assert.Equal(t, "paper-order-aaa-buy", telemetryOrderID)
}

func TestIntegratedQuestHandlersCloseTriggeredPaperScalpingPositions_TakeProfitRecordsPnLAndTelemetry(t *testing.T) {
	t.Setenv("NEURATRADE_PAPER_SCALPING_TAKER_FEE_RATE", "0.001")
	ctx := context.Background()
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "paper-scalping-close-tp.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	lifecycleStore, err := NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)
	telemetryStore := NewScalpingTelemetryStore(sqliteDB, nil)
	require.NoError(t, telemetryStore.EnsureSchema(ctx))

	cycleID, err := telemetryStore.InsertCycleRecord(ctx, CycleRecord{
		ID:         "paper-cycle-tp",
		ChatID:     "paper-chat",
		Exchange:   "bitget",
		CycleAt:    time.Now().UTC().Add(-2 * time.Minute),
		Symbol:     "AAA/USDT",
		Action:     "buy",
		Confidence: 0.91,
	})
	require.NoError(t, err)

	openedAt := time.Now().UTC().Add(-2 * time.Minute)
	require.NoError(t, lifecycleStore.RecordOrderExecution(ctx, LifecycleExecutionRecord{
		OrderID:    "paper-order-tp",
		ChatID:     "paper-chat",
		Exchange:   "bitget",
		Symbol:     "AAA/USDT",
		Side:       "buy",
		OrderType:  "market",
		MarketType: "futures",
		Amount:     decimal.NewFromInt(1000),
		EntryPrice: decimal.NewFromInt(100),
		StopLoss:   decimal.NewFromInt(95),
		TakeProfit: decimal.NewFromInt(110),
		Source:     scalpingPaperLifecycleSource,
		OpenedAt:   openedAt,
	}))
	require.NoError(t, telemetryStore.LinkOrderToCycle(ctx, cycleID, "paper-order-tp"))

	handlers := &IntegratedQuestHandlers{
		ccxtService: &mockAIScalpingCCXT{
			singleTickers: map[string]ccxt.MarketPriceInterface{
				"AAA/USDT": mockMarketPrice{symbol: "AAA/USDT", price: 111, exchange: "bitget"},
			},
		},
		lifecycleStore: lifecycleStore,
		telemetryStore: telemetryStore,
	}
	quest := &Quest{Checkpoint: map[string]interface{}{}}

	closed, err := handlers.closeTriggeredPaperScalpingPositions(ctx, quest, "paper-chat", "bitget")
	require.NoError(t, err)
	assert.Equal(t, 1, closed)
	assert.Equal(t, 1, quest.Checkpoint["paper_close_triggered_positions"])

	var positionStatus string
	var closePrice, grossPnL decimal.Decimal
	require.NoError(t, sqliteDB.DB.QueryRowContext(ctx, `
		SELECT status, close_price, realized_pnl
		FROM trading_positions
		WHERE order_id = $1
	`, "paper-order-tp").Scan(&positionStatus, &closePrice, &grossPnL))
	assert.Equal(t, "closed", positionStatus)
	assert.True(t, closePrice.Equal(decimal.NewFromInt(111)))
	assert.True(t, grossPnL.Equal(decimal.NewFromInt(110)))

	var source, outcome string
	var fees, telemetryPnL decimal.Decimal
	var holdSeconds int
	require.NoError(t, sqliteDB.DB.QueryRowContext(ctx, `
		SELECT j.source, j.fees, t.outcome, t.pnl, t.hold_duration_seconds
		FROM realized_pnl_journal j
		JOIN scalping_cycle_telemetry t ON t.order_id = j.order_id
		WHERE j.order_id = $1
	`, "paper-order-tp").Scan(&source, &fees, &outcome, &telemetryPnL, &holdSeconds))
	assert.Equal(t, scalpingPaperTakeProfitCloseSource, source)
	assert.True(t, fees.Equal(decimal.RequireFromString("2.110")))
	assert.Equal(t, "win", outcome)
	assert.True(t, telemetryPnL.Equal(decimal.RequireFromString("107.890")))
	assert.GreaterOrEqual(t, holdSeconds, 119)

	perf, err := lifecycleStore.GetRealizedPerformance(ctx, "paper-chat", "bitget", time.Now().UTC().Add(-1*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 1, perf.Trades)
	assert.Equal(t, 1, perf.Wins)
	assert.True(t, perf.RealizedPnL.Equal(decimal.RequireFromString("107.890")))
}

func TestIntegratedQuestHandlersCloseTriggeredPaperScalpingPositions_StopLossRecordsLossAfterFees(t *testing.T) {
	t.Setenv("NEURATRADE_PAPER_SCALPING_TAKER_FEE_RATE", "0.001")
	ctx := context.Background()
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "paper-scalping-close-sl.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	lifecycleStore, err := NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)
	telemetryStore := NewScalpingTelemetryStore(sqliteDB, nil)
	require.NoError(t, telemetryStore.EnsureSchema(ctx))

	cycleID, err := telemetryStore.InsertCycleRecord(ctx, CycleRecord{
		ID:         "paper-cycle-sl",
		ChatID:     "paper-chat",
		Exchange:   "bitget",
		CycleAt:    time.Now().UTC().Add(-90 * time.Second),
		Symbol:     "AAA/USDT",
		Action:     "sell",
		Confidence: 0.88,
	})
	require.NoError(t, err)

	require.NoError(t, lifecycleStore.RecordOrderExecution(ctx, LifecycleExecutionRecord{
		OrderID:    "paper-order-sl",
		ChatID:     "paper-chat",
		Exchange:   "bitget",
		Symbol:     "AAA/USDT",
		Side:       "sell",
		OrderType:  "market",
		MarketType: "futures",
		Amount:     decimal.NewFromInt(1000),
		EntryPrice: decimal.NewFromInt(100),
		StopLoss:   decimal.NewFromInt(105),
		TakeProfit: decimal.NewFromInt(95),
		Source:     scalpingPaperLifecycleSource,
		OpenedAt:   time.Now().UTC().Add(-90 * time.Second),
	}))
	require.NoError(t, telemetryStore.LinkOrderToCycle(ctx, cycleID, "paper-order-sl"))

	handlers := &IntegratedQuestHandlers{
		ccxtService: &mockAIScalpingCCXT{
			singleTickers: map[string]ccxt.MarketPriceInterface{
				"AAA/USDT": mockMarketPrice{symbol: "AAA/USDT", price: 106, exchange: "bitget"},
			},
		},
		lifecycleStore: lifecycleStore,
		telemetryStore: telemetryStore,
	}

	closed, err := handlers.closeTriggeredPaperScalpingPositions(ctx, &Quest{Checkpoint: map[string]interface{}{}}, "paper-chat", "bitget")
	require.NoError(t, err)
	assert.Equal(t, 1, closed)

	var source, outcome string
	var grossPnL, fees, telemetryPnL decimal.Decimal
	require.NoError(t, sqliteDB.DB.QueryRowContext(ctx, `
		SELECT j.source, j.realized_pnl, j.fees, t.outcome, t.pnl
		FROM realized_pnl_journal j
		JOIN scalping_cycle_telemetry t ON t.order_id = j.order_id
		WHERE j.order_id = $1
	`, "paper-order-sl").Scan(&source, &grossPnL, &fees, &outcome, &telemetryPnL))
	assert.Equal(t, scalpingPaperStopLossCloseSource, source)
	assert.True(t, grossPnL.Equal(decimal.NewFromInt(-60)))
	assert.True(t, fees.Equal(decimal.RequireFromString("2.060")))
	assert.Equal(t, "loss", outcome)
	assert.True(t, telemetryPnL.Equal(decimal.RequireFromString("-62.060")))

	perf, err := lifecycleStore.GetRealizedPerformance(ctx, "paper-chat", "bitget", time.Now().UTC().Add(-1*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 1, perf.Trades)
	assert.Equal(t, 1, perf.Losses)
	assert.True(t, perf.RealizedPnL.Equal(decimal.RequireFromString("-62.060")))
}

func TestIntegratedQuestHandlersPersistScalpingExecutionLifecycle_LinksLiveTelemetryWithoutLifecycleStore(t *testing.T) {
	ctx := context.Background()
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "live-scalping-telemetry.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	telemetryStore := NewScalpingTelemetryStore(sqliteDB, nil)
	require.NoError(t, telemetryStore.EnsureSchema(ctx))

	cycleID, err := telemetryStore.InsertCycleRecord(ctx, CycleRecord{
		ID:         "live-cycle-1",
		ChatID:     "live-chat",
		Exchange:   "bitget",
		CycleAt:    time.Now().UTC(),
		Symbol:     "AAA/USDT",
		Action:     "buy",
		Confidence: 0.92,
	})
	require.NoError(t, err)

	handlers := &IntegratedQuestHandlers{telemetryStore: telemetryStore}
	handlers.persistScalpingExecutionLifecycle(
		ctx,
		OpModeLive,
		&AITradingDecision{
			Action:      "buy",
			Symbol:      "AAA/USDT",
			SizePercent: 2.5,
			OrderID:     "live-order-aaa-buy",
		},
		"live-chat",
		"bitget",
		TradingPortfolio{USDTBalance: 1000},
		true,
		cycleID,
	)

	var telemetryOrderID string
	err = sqliteDB.DB.QueryRowContext(ctx, `
		SELECT order_id
		FROM scalping_cycle_telemetry
		WHERE id = $1
	`, cycleID).Scan(&telemetryOrderID)
	require.NoError(t, err)
	assert.Equal(t, "live-order-aaa-buy", telemetryOrderID)
}
