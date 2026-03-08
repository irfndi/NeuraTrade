package services

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/ai/llm"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

type errorLLMClient struct {
	err error
}

func (m *errorLLMClient) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return nil, m.err
}

func (m *errorLLMClient) Stream(ctx context.Context, req *llm.CompletionRequest) (<-chan llm.StreamEvent, error) {
	return nil, m.err
}

func (m *errorLLMClient) Provider() llm.Provider {
	return llm.ProviderOpenAI
}

func (m *errorLLMClient) Close() error {
	return nil
}

func TestAITradingDecision_Validation(t *testing.T) {
	tp := decimal.NewFromFloat(50000)
	sl := decimal.NewFromFloat(45000)

	decision := &AITradingDecision{
		Action:      "buy",
		Symbol:      "BTC/USDT",
		SizePercent: 5.0,
		Confidence:  0.85,
		Reasoning:   "Strong bullish momentum",
		StopLoss:    &sl,
		TakeProfit:  &tp,
	}

	assert.Equal(t, "buy", decision.Action)
	assert.Equal(t, "BTC/USDT", decision.Symbol)
	assert.Equal(t, 5.0, decision.SizePercent)
	assert.Equal(t, 0.85, decision.Confidence)
	assert.Equal(t, "Strong bullish momentum", decision.Reasoning)
	assert.Equal(t, "45000", decision.StopLoss.String())
	assert.Equal(t, "50000", decision.TakeProfit.String())
}

func TestTradingPortfolio(t *testing.T) {
	portfolio := TradingPortfolio{
		USDTBalance:   1000.0,
		TotalValue:    1500.0,
		OpenPositions: 2,
		UnrealizedPnL: 50.0,
	}

	assert.Equal(t, 1000.0, portfolio.USDTBalance)
	assert.Equal(t, 1500.0, portfolio.TotalValue)
	assert.Equal(t, 2, portfolio.OpenPositions)
	assert.Equal(t, 50.0, portfolio.UnrealizedPnL)
}

func TestAIScalpingConfig_Default(t *testing.T) {
	config := DefaultAIScalpingConfig()

	assert.Equal(t, "bitget", config.Exchange)
	assert.Equal(t, 5, config.Leverage)
	assert.Equal(t, 5.0, config.MaxCapitalPct)
	assert.Equal(t, 0.65, config.MinConfidence)
	assert.Equal(t, 3, config.MaxIterations)
	assert.True(t, config.AutoExecute)
	assert.False(t, config.AllowSpotFallback)
	assert.Equal(t, 8, config.MaxPairsToAnalyze)
	assert.Equal(t, 120, config.MaxCandidatePairs)
	assert.Equal(t, 4, config.OrderBookPairs)
	assert.True(t, config.EnforceFutures)
	assert.Equal(t, 90*time.Second, config.SymbolCooldown)
	assert.Equal(t, 3, config.FailureBudget)
	assert.Equal(t, 15*time.Minute, config.FailureWindow)
	assert.Equal(t, 2, config.StructuredRetries)
	assert.True(t, config.PreTradeGate)
	assert.Equal(t, 0.0, config.MinExpectancyEdge)
	assert.Equal(t, 8, config.MinExpectancyN)
	assert.Equal(t, 85.0, config.RegimeHighBand)
	assert.Equal(t, 15.0, config.RegimeLowBand)
}

func TestAIScalpingConfig_Custom(t *testing.T) {
	config := AIScalpingConfig{
		Exchange:          "binance",
		Leverage:          10,
		MaxCapitalPct:     10.0,
		MinConfidence:     0.5,
		MaxIterations:     5,
		Timeout:           300000000000,
		AutoExecute:       false,
		AllowSpotFallback: true,
		MaxPairsToAnalyze: 20,
		MaxCandidatePairs: 500,
	}

	assert.Equal(t, "binance", config.Exchange)
	assert.Equal(t, 10, config.Leverage)
	assert.Equal(t, 10.0, config.MaxCapitalPct)
	assert.Equal(t, 0.5, config.MinConfidence)
	assert.Equal(t, 5, config.MaxIterations)
	assert.False(t, config.AutoExecute)
	assert.True(t, config.AllowSpotFallback)
	assert.Equal(t, 20, config.MaxPairsToAnalyze)
	assert.Equal(t, 500, config.MaxCandidatePairs)
}

func TestResolveAIScalpingConfigFromEnv(t *testing.T) {
	t.Setenv("NEURATRADE_SCALPING_EXCHANGE", "binance")
	t.Setenv("NEURATRADE_SCALPING_LEVERAGE", "12")
	t.Setenv("NEURATRADE_SCALPING_MAX_CAPITAL_PCT", "3.5")
	t.Setenv("NEURATRADE_SCALPING_MIN_CONFIDENCE", "0.61")
	t.Setenv("NEURATRADE_SCALPING_TIMEOUT_SECONDS", "45")
	t.Setenv("NEURATRADE_SCALPING_AUTO_EXECUTE", "false")
	t.Setenv("NEURATRADE_SCALPING_ALLOW_SPOT_FALLBACK", "true")
	t.Setenv("NEURATRADE_SCALPING_MAX_PAIRS", "11")
	t.Setenv("NEURATRADE_SCALPING_MAX_CANDIDATES", "210")
	t.Setenv("NEURATRADE_SCALPING_ORDERBOOK_PAIRS", "6")
	t.Setenv("NEURATRADE_SCALPING_ENFORCE_FUTURES_UNIVERSE", "false")
	t.Setenv("NEURATRADE_SCALPING_SYMBOL_COOLDOWN_SECONDS", "45")
	t.Setenv("NEURATRADE_SCALPING_SYMBOL_FAILURE_BUDGET", "4")
	t.Setenv("NEURATRADE_SCALPING_SYMBOL_FAILURE_WINDOW_SECONDS", "600")
	t.Setenv("NEURATRADE_SCALPING_STRUCTURED_RETRIES", "3")
	t.Setenv("NEURATRADE_SCALPING_SYMBOL_LOSS_STREAK_BUDGET", "3")
	t.Setenv("NEURATRADE_SCALPING_SYMBOL_LOSS_COOLDOWN_SECONDS", "900")
	t.Setenv("NEURATRADE_SCALPING_SYMBOL_LOSS_WINDOW_SECONDS", "3600")
	t.Setenv("NEURATRADE_SCALPING_PRETRADE_GATE", "true")
	t.Setenv("NEURATRADE_SCALPING_MIN_EXPECTANCY_EDGE", "0.03")
	t.Setenv("NEURATRADE_SCALPING_MIN_EXPECTANCY_SAMPLES", "12")
	t.Setenv("NEURATRADE_SCALPING_REGIME_HIGH_BAND", "88")
	t.Setenv("NEURATRADE_SCALPING_REGIME_LOW_BAND", "12")

	cfg := ResolveAIScalpingConfigFromEnv(DefaultAIScalpingConfig())
	assert.Equal(t, "binance", cfg.Exchange)
	assert.Equal(t, 12, cfg.Leverage)
	assert.Equal(t, 3.5, cfg.MaxCapitalPct)
	assert.Equal(t, 0.61, cfg.MinConfidence)
	assert.Equal(t, 45*time.Second, cfg.Timeout)
	assert.False(t, cfg.AutoExecute)
	assert.True(t, cfg.AllowSpotFallback)
	assert.Equal(t, 11, cfg.MaxPairsToAnalyze)
	assert.Equal(t, 210, cfg.MaxCandidatePairs)
	assert.Equal(t, 6, cfg.OrderBookPairs)
	assert.False(t, cfg.EnforceFutures)
	assert.Equal(t, 45*time.Second, cfg.SymbolCooldown)
	assert.Equal(t, 4, cfg.FailureBudget)
	assert.Equal(t, 10*time.Minute, cfg.FailureWindow)
	assert.Equal(t, 3, cfg.StructuredRetries)
	assert.Equal(t, 3, cfg.LossStreakBudget)
	assert.Equal(t, 15*time.Minute, cfg.LossCooldown)
	assert.Equal(t, 1*time.Hour, cfg.LossWindow)
	assert.True(t, cfg.PreTradeGate)
	assert.Equal(t, 0.03, cfg.MinExpectancyEdge)
	assert.Equal(t, 12, cfg.MinExpectancyN)
	assert.Equal(t, 88.0, cfg.RegimeHighBand)
	assert.Equal(t, 12.0, cfg.RegimeLowBand)
}

func TestAIMarketSignal(t *testing.T) {
	signal := aiMarketSignal{
		Symbol:             "BTC/USDT",
		Price:              50000.0,
		High24h:            52000.0,
		Low24h:             48000.0,
		Volume24h:          1000000000,
		BidAskSpread:       0.01,
		OrderBookImbalance: 0.5,
		PriceChange24h:     5.0,
	}

	assert.Equal(t, "BTC/USDT", signal.Symbol)
	assert.Equal(t, 50000.0, signal.Price)
	assert.Equal(t, 52000.0, signal.High24h)
	assert.Equal(t, 48000.0, signal.Low24h)
	assert.Equal(t, 1000000000.0, signal.Volume24h)
	assert.Equal(t, 0.01, signal.BidAskSpread)
	assert.Equal(t, 0.5, signal.OrderBookImbalance)
	assert.Equal(t, 5.0, signal.PriceChange24h)
}

func TestAIScalpingService_SymbolGuardFailureBudgetAndCooldown(t *testing.T) {
	svc := &AIScalpingService{
		config: AIScalpingConfig{
			SymbolCooldown: 2 * time.Minute,
			FailureBudget:  2,
			FailureWindow:  10 * time.Minute,
		},
		symbolGuards: make(map[string]symbolExecutionGuard),
	}

	svc.recordSymbolGuardResult("BTC/USDT", fmt.Errorf("network timeout"))
	svc.recordSymbolGuardResult("BTC/USDT", fmt.Errorf("network timeout"))

	err := svc.enforceSymbolGuard("BTC/USDT")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "symbol failure budget reached")

	svc.recordSymbolGuardResult("BTC/USDT", nil)
	err = svc.enforceSymbolGuard("BTC/USDT")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "symbol cooldown active")
}

func TestAIScalpingService_ParseDecisionWithRetries(t *testing.T) {
	mockLLM := &MockLLMClient{
		Responses: []*llm.CompletionResponse{
			{
				Message: llm.Message{
					Content: `{"action":"hold","symbol":"BTC/USDT","size_pct":0,"confidence":0.2,"reasoning":"repair fallback","stop_loss":null,"take_profit":null}`,
				},
			},
		},
	}

	svc := &AIScalpingService{
		config: AIScalpingConfig{
			Model:             "glm-5",
			MaxTokens:         1200,
			StructuredRetries: 2,
		},
		llmClient: mockLLM,
	}

	decision, err := svc.parseDecisionWithRetries(context.Background(), "not-json")
	assert.NoError(t, err)
	assert.NotNil(t, decision)
	assert.Equal(t, "hold", decision.Action)
	assert.Equal(t, 1, mockLLM.CallCount)
}

func TestAIScalpingService_ParseDecisionWithRetries_InvalidAction(t *testing.T) {
	mockLLM := &MockLLMClient{
		Responses: []*llm.CompletionResponse{
			{
				Message: llm.Message{
					Content: `{"action":"buy","symbol":"BTC/USDT","size_pct":1.0,"confidence":0.6,"reasoning":"fixed","stop_loss":41000,"take_profit":43000}`,
				},
			},
		},
	}

	svc := &AIScalpingService{
		config: AIScalpingConfig{
			Model:             "glm-5",
			MaxTokens:         1200,
			StructuredRetries: 2,
		},
		llmClient: mockLLM,
	}

	decision, err := svc.parseDecisionWithRetries(context.Background(), `{"action":"","symbol":"BTC/USDT","size_pct":1.0,"confidence":0.6,"reasoning":"bad"}`)
	assert.NoError(t, err)
	assert.Equal(t, "buy", decision.Action)
	assert.Equal(t, 1, mockLLM.CallCount)
}

func TestNormalizeHoldReasonCategory_RuntimeSignals(t *testing.T) {
	category := normalizeHoldReasonCategory(
		reasonCategoryStrategyHold,
		"model response parse fallback (context deadline exceeded)",
	)
	assert.Equal(t, reasonCategoryLLMTimeout, category)

	category = normalizeHoldReasonCategory("", "waiting for qualified setup")
	assert.Equal(t, reasonCategoryStrategyHold, category)
}

func TestAIScalpingService_GetAIDecision_UsesDeterministicFallbackOnLLMError(t *testing.T) {
	svc := &AIScalpingService{
		config: AIScalpingConfig{
			MaxCapitalPct: 0.5,
		},
		llmClient: &errorLLMClient{err: context.DeadlineExceeded},
	}

	decision, err := svc.getAIDecision(context.Background(), []aiMarketSignal{
		{
			Symbol:             "BTC/USDT",
			Price:              100,
			High24h:            104,
			Low24h:             96,
			Volume24h:          2500000,
			BidAskSpread:       0.02,
			OrderBookImbalance: 0.58,
			RangePosition24h:   18,
		},
	}, TradingPortfolio{})

	assert.NoError(t, err)
	assert.NotNil(t, decision)
	assert.Equal(t, "buy", decision.Action)
	assert.Equal(t, "BTC/USDT", decision.Symbol)
	assert.Equal(t, reasonCategoryDeterministicFallback, decision.ReasonCategory)
	assert.True(t, decision.ConfidenceKnown)
	assert.Greater(t, decision.Confidence, 0.70)
	assert.LessOrEqual(t, decision.SizePercent, 0.5)

	diagnostics := svc.RuntimeDiagnostics()
	assert.Contains(t, diagnostics["last_error"], "context deadline exceeded")
}

func TestAIScalpingService_GetAIDecision_UsesDeterministicFallbackAfterParseExhaustion(t *testing.T) {
	mockLLM := &MockLLMClient{
		Responses: []*llm.CompletionResponse{
			{
				Message: llm.Message{Content: "analysis without json"},
			},
			{
				Message: llm.Message{Content: "still not json"},
			},
		},
	}

	svc := &AIScalpingService{
		config: AIScalpingConfig{
			Model:             "glm-5",
			MaxTokens:         1200,
			StructuredRetries: 1,
			MinConfidence:     0.65,
			MaxCapitalPct:     5,
			RegimeHighBand:    85,
			RegimeLowBand:     15,
		},
		llmClient: mockLLM,
	}

	decision, err := svc.getAIDecision(context.Background(), []aiMarketSignal{
		{
			Symbol:             "ETH/USDT",
			Price:              200,
			High24h:            208,
			Low24h:             192,
			Volume24h:          3100000,
			BidAskSpread:       0.03,
			OrderBookImbalance: -0.61,
			RangePosition24h:   82,
		},
	}, TradingPortfolio{})

	assert.NoError(t, err)
	assert.NotNil(t, decision)
	assert.Equal(t, "sell", decision.Action)
	assert.Equal(t, "ETH/USDT", decision.Symbol)
	assert.Equal(t, reasonCategoryDeterministicFallback, decision.ReasonCategory)
	assert.True(t, decision.ConfidenceKnown)
	assert.Equal(t, 2, mockLLM.CallCount)
}

func TestParseAIDecisionJSONObject_DoesNotForceStrategyMetadata(t *testing.T) {
	decision, err := parseAIDecisionJSONObject(`{"action":"hold","symbol":"BTC/USDT","size_pct":0,"confidence":0.1,"reasoning":"wait"}`)
	assert.NoError(t, err)
	assert.Equal(t, "hold", decision.Action)
	assert.Equal(t, "", decision.ReasonCategory)
	assert.False(t, decision.ConfidenceKnown)
}

func TestAIScalpingService_DynamicRiskThresholds_RecoveryModes(t *testing.T) {
	svc := &AIScalpingService{
		config: AIScalpingConfig{
			MinConfidence: 0.65,
			MaxCapitalPct: 5.00,
		},
	}
	t.Setenv("NEURATRADE_RECOVERY_MICRO_ENTRY_CAP_PCT", "0.50")

	minConf, maxCap := svc.dynamicRiskThresholds(context.Background(), TradingPortfolio{
		RecoveryMode: recoveryModeMicroEntry,
	})
	assert.InDelta(t, 0.50, maxCap, 0.0001)
	assert.GreaterOrEqual(t, minConf, 0.65)

	minConf, maxCap = svc.dynamicRiskThresholds(context.Background(), TradingPortfolio{
		RecoveryMode: recoveryModeDeriskOnly,
	})
	assert.InDelta(t, 0.10, maxCap, 0.0001)
	assert.GreaterOrEqual(t, minConf, 0.85)
}

func TestAIScalpingService_ApplyControlledNoFillRecovery_StepLadder(t *testing.T) {
	svc := &AIScalpingService{
		config: AIScalpingConfig{
			MinConfidence: 0.80,
			MaxCapitalPct: 5.00,
		},
	}
	t.Setenv("NEURATRADE_NOFILL_RECOVERY_MINUTES", "180")
	t.Setenv("NEURATRADE_NOFILL_MIN_CONF_FLOOR", "0.70")
	t.Setenv("NEURATRADE_NOFILL_MAX_CAP_PCT_CAP", "1.50")

	minConfidence := 0.85
	maxCapital := 0.10
	svc.applyControlledNoFillRecovery(&minConfidence, &maxCapital, TradingPortfolio{
		NoFillMinutes: 200,
		OpenPositions: 0,
		DriftActive:   false,
	}, 0)
	assert.InDelta(t, 0.75, minConfidence, 0.0001)
	assert.InDelta(t, 0.50, maxCapital, 0.0001)

	minConfidence = 0.85
	maxCapital = 0.10
	svc.applyControlledNoFillRecovery(&minConfidence, &maxCapital, TradingPortfolio{
		NoFillMinutes: 420,
		OpenPositions: 0,
		DriftActive:   false,
	}, 0)
	assert.InDelta(t, 0.70, minConfidence, 0.0001)
	assert.InDelta(t, 1.00, maxCapital, 0.0001)

	minConfidence = 0.85
	maxCapital = 0.10
	svc.applyControlledNoFillRecovery(&minConfidence, &maxCapital, TradingPortfolio{
		NoFillMinutes: 720,
		OpenPositions: 0,
		DriftActive:   false,
	}, 0)
	assert.InDelta(t, 0.70, minConfidence, 0.0001)
	assert.InDelta(t, 1.50, maxCapital, 0.0001)
}

func TestAIScalpingService_ApplyControlledNoFillRecovery_RequiresClearState(t *testing.T) {
	svc := &AIScalpingService{
		config: AIScalpingConfig{
			MinConfidence: 0.80,
			MaxCapitalPct: 5.00,
		},
	}
	t.Setenv("NEURATRADE_NOFILL_RECOVERY_MINUTES", "180")

	tests := []TradingPortfolio{
		{NoFillMinutes: 240, OpenPositions: 1, DriftActive: false},
		{NoFillMinutes: 240, OpenPositions: 0, DriftActive: true},
	}
	for _, portfolio := range tests {
		minConfidence := 0.80
		maxCapital := 0.10
		svc.applyControlledNoFillRecovery(&minConfidence, &maxCapital, portfolio, 0)
		assert.InDelta(t, 0.80, minConfidence, 0.0001)
		assert.InDelta(t, 0.10, maxCapital, 0.0001)
	}

	// Consecutive losses should suppress recovery unlock.
	minConfidence := 0.80
	maxCapital := 0.10
	svc.applyControlledNoFillRecovery(&minConfidence, &maxCapital, TradingPortfolio{
		NoFillMinutes: 300,
		OpenPositions: 0,
		DriftActive:   false,
	}, 3)
	assert.InDelta(t, 0.80, minConfidence, 0.0001)
	assert.InDelta(t, 0.10, maxCapital, 0.0001)
}

func TestAIScalpingService_ValidateDecision_HoldNormalization(t *testing.T) {
	svc := &AIScalpingService{}
	decision := &AITradingDecision{
		Action:      "hold",
		Symbol:      "AR/",
		SizePercent: 5,
		Confidence:  0,
		Reasoning:   "",
	}

	err := svc.validateDecision(decision, nil)
	assert.NoError(t, err)
	assert.Equal(t, "", decision.Symbol)
	assert.Equal(t, 0.0, decision.SizePercent)
	assert.Equal(t, "model selected hold (no detailed reasoning)", decision.Reasoning)
}

func TestAIScalpingService_SymbolLossCooldown(t *testing.T) {
	svc := &AIScalpingService{
		config: AIScalpingConfig{
			LossStreakBudget: 2,
			LossCooldown:     30 * time.Minute,
			LossWindow:       90 * time.Minute,
		},
		symbolGuards: make(map[string]symbolExecutionGuard),
	}

	svc.ReportTradeOutcome("ADA/USDT", decimal.NewFromFloat(-0.1))
	svc.ReportTradeOutcome("ADA/USDT", decimal.NewFromFloat(-0.2))

	err := svc.enforceSymbolGuard("ADA/USDT")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "symbol loss cooldown active")

	svc.ReportTradeOutcome("ADA/USDT", decimal.NewFromFloat(0.05))
	err = svc.enforceSymbolGuard("ADA/USDT")
	assert.NoError(t, err)
}

func TestAIScalpingService_PreTradeGate_RegimeBlock(t *testing.T) {
	svc := &AIScalpingService{
		config: DefaultAIScalpingConfig(),
	}
	decision := &AITradingDecision{
		Action:     "buy",
		Symbol:     "ADA/USDT",
		Confidence: 0.72,
	}
	signals := []aiMarketSignal{
		{
			Symbol:             "ADA/USDT",
			Price:              1.0,
			BidAskSpread:       0.05,
			OrderBookImbalance: -0.31,
			RangePosition24h:   94,
		},
	}

	result := svc.evaluatePreTradeGate(context.Background(), decision, signals)
	assert.False(t, result.Allowed)
	assert.Contains(t, result.Reason, "late-range buy rejected")
}

func TestAIScalpingService_PreTradeGate_ExpectancyBlock(t *testing.T) {
	original := globalScalpingPerformance
	globalScalpingPerformance = NewScalpingPerformance()
	t.Cleanup(func() {
		globalScalpingPerformance = original
	})

	for i := 0; i < 7; i++ {
		globalScalpingPerformance.RecordTrade(TradeRecord{
			Timestamp:  time.Now().UTC(),
			Symbol:     "ADA/USDT",
			Side:       "buy",
			PnL:        decimal.NewFromFloat(-0.2),
			Profitable: false,
		})
	}

	svc := &AIScalpingService{
		config: AIScalpingConfig{
			PreTradeGate:      true,
			MinExpectancyEdge: 0,
			MinExpectancyN:    5,
			RegimeHighBand:    85,
			RegimeLowBand:     15,
		},
	}
	decision := &AITradingDecision{
		Action:     "buy",
		Symbol:     "ADA/USDT",
		Confidence: 0.75,
	}
	signals := []aiMarketSignal{
		{
			Symbol:             "ADA/USDT",
			Price:              1.0,
			BidAskSpread:       0.03,
			OrderBookImbalance: 0.32,
			RangePosition24h:   55,
		},
	}

	result := svc.evaluatePreTradeGate(context.Background(), decision, signals)
	assert.False(t, result.Allowed)
	assert.Contains(t, result.Reason, "expectancy gate")
	assert.GreaterOrEqual(t, result.SampleSize, 5)
}

func TestAIScalpingService_DeterministicFallbackCandidate_RejectsIneligibleSignals(t *testing.T) {
	svc := &AIScalpingService{
		config: AIScalpingConfig{
			MaxCapitalPct: 0.5,
		},
	}

	tests := []aiMarketSignal{
		{
			Symbol:             "BTC/USDT",
			Price:              100,
			High24h:            104,
			Low24h:             96,
			Volume24h:          2500000,
			BidAskSpread:       0.09,
			OrderBookImbalance: 0.60,
			RangePosition24h:   20,
		},
		{
			Symbol:             "BTC/USDT",
			Price:              100,
			High24h:            104,
			Low24h:             96,
			Volume24h:          2500000,
			BidAskSpread:       0.03,
			OrderBookImbalance: 0.20,
			RangePosition24h:   20,
		},
		{
			Symbol:             "BTC/USDT",
			Price:              100,
			High24h:            104,
			Low24h:             96,
			Volume24h:          2500000,
			BidAskSpread:       0.03,
			OrderBookImbalance: 0.60,
			RangePosition24h:   90,
		},
	}

	for _, signal := range tests {
		_, _, ok := svc.deterministicFallbackCandidate(signal, TradingPortfolio{})
		assert.False(t, ok)
	}
}

func TestAIScalpingService_DeterministicFallbackCandidate_RespectsConfidenceAndPhaseCap(t *testing.T) {
	svc := &AIScalpingService{
		config: AIScalpingConfig{
			MaxCapitalPct: 0.5,
			MinConfidence: 0.82,
		},
	}

	lowConfidenceSignal := aiMarketSignal{
		Symbol:             "BTC/USDT",
		Price:              100,
		High24h:            104,
		Low24h:             96,
		Volume24h:          10000,
		BidAskSpread:       0.079,
		OrderBookImbalance: 0.36,
		RangePosition24h:   50,
	}
	_, _, ok := svc.deterministicFallbackCandidate(lowConfidenceSignal, TradingPortfolio{})
	assert.False(t, ok)

	eligibleSignal := aiMarketSignal{
		Symbol:             "BTC/USDT",
		Price:              100,
		High24h:            104,
		Low24h:             96,
		Volume24h:          2500000,
		BidAskSpread:       0.02,
		OrderBookImbalance: 0.58,
		RangePosition24h:   18,
	}
	decision, _, ok := svc.deterministicFallbackCandidate(eligibleSignal, TradingPortfolio{
		PhaseMaxCapitalPct: 0.25,
	})
	assert.True(t, ok)
	assert.NotNil(t, decision)
	assert.LessOrEqual(t, decision.SizePercent, 0.25)
}

func TestAIScalpingService_DeterministicFallbackDecision_NoCandidateUsesRuntimeHold(t *testing.T) {
	svc := &AIScalpingService{}
	decision := svc.deterministicFallbackDecision([]aiMarketSignal{
		{
			Symbol:             "BTC/USDT",
			Price:              100,
			High24h:            104,
			Low24h:             96,
			Volume24h:          2500000,
			BidAskSpread:       0.03,
			OrderBookImbalance: 0.60,
			RangePosition24h:   95,
		},
	}, TradingPortfolio{})

	assert.Equal(t, "hold", decision.Action)
	assert.Equal(t, reasonCategoryDeterministicFallback, decision.ReasonCategory)
	assert.False(t, decision.ConfidenceKnown)
}

func TestAIScalpingService_ExchangeForContextPrefersScopedExchange(t *testing.T) {
	svc := &AIScalpingService{
		config: AIScalpingConfig{
			Exchange: "BiTGet",
		},
	}
	ctx := WithScalpingAutonomyScope(context.Background(), ScalpingAutonomyScope{
		ChatID:   "123",
		Exchange: "Binance ",
	})

	assert.Equal(t, "binance", svc.exchangeForContext(ctx))
	assert.Equal(t, "bitget", svc.exchangeForContext(context.Background()))
}
