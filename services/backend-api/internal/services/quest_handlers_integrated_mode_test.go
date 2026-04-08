package services

import (
	"context"
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
