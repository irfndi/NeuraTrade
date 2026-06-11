package services

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/irfndi/neuratrade/internal/ai/llm"
	appautonomy "github.com/irfndi/neuratrade/internal/app/autonomy"
	"github.com/irfndi/neuratrade/internal/autonomous"
	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
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

type mockMarketPrice struct {
	symbol    string
	price     float64
	volume    float64
	high24h   float64
	low24h    float64
	change24h float64
	bid       float64
	ask       float64
	exchange  string
}

func (m mockMarketPrice) GetPrice() decimal.Decimal       { return decimal.NewFromFloat(m.price) }
func (m mockMarketPrice) GetVolume() decimal.Decimal      { return decimal.NewFromFloat(m.volume) }
func (m mockMarketPrice) GetTimestamp() time.Time         { return time.Now().UTC() }
func (m mockMarketPrice) GetExchangeName() string         { return m.exchange }
func (m mockMarketPrice) GetSymbol() string               { return m.symbol }
func (m mockMarketPrice) GetBid() decimal.Decimal         { return decimal.NewFromFloat(m.bid) }
func (m mockMarketPrice) GetAsk() decimal.Decimal         { return decimal.NewFromFloat(m.ask) }
func (m mockMarketPrice) GetHigh() decimal.Decimal        { return decimal.NewFromFloat(m.high24h) }
func (m mockMarketPrice) GetLow() decimal.Decimal         { return decimal.NewFromFloat(m.low24h) }
func (m mockMarketPrice) GetPriceChange24h() float64      { return m.change24h }

type mockAIScalpingCCXT struct {
	mockCCXTForPortfolioSafety
	markets       *ccxt.MarketsResponse
	marketData    []ccxt.MarketPriceInterface
	singleTickers map[string]ccxt.MarketPriceInterface
	orderBooks    map[string]*ccxt.OrderBookResponse
	orderBookOps  []string
	marketsErr    error
	marketErr     error
	orderBookErr  error
}

func (m *mockAIScalpingCCXT) FetchMarkets(ctx context.Context, exchange string) (*ccxt.MarketsResponse, error) {
	if m.marketsErr != nil {
		return nil, m.marketsErr
	}
	return m.markets, nil
}

func (m *mockAIScalpingCCXT) FetchMarketData(ctx context.Context, exchanges []string, symbols []string) ([]ccxt.MarketPriceInterface, error) {
	if m.marketErr != nil {
		return nil, m.marketErr
	}
	return m.marketData, nil
}

func (m *mockAIScalpingCCXT) FetchSingleTicker(ctx context.Context, exchange, symbol string) (ccxt.MarketPriceInterface, error) {
	if m.marketErr != nil {
		return nil, m.marketErr
	}
	if m.singleTickers == nil {
		return nil, nil
	}
	return m.singleTickers[normalizeSymbolForComparison(symbol)], nil
}

func (m *mockAIScalpingCCXT) FetchOrderBook(ctx context.Context, exchange, symbol string, limit int) (*ccxt.OrderBookResponse, error) {
	m.orderBookOps = append(m.orderBookOps, normalizeSymbolForComparison(symbol))
	if m.orderBookErr != nil {
		return nil, m.orderBookErr
	}
	if m.orderBooks == nil {
		return nil, nil
	}
	return m.orderBooks[normalizeSymbolForComparison(symbol)], nil
}

func decimalPointer(value string) *decimal.Decimal {
	amount := decimal.RequireFromString(value)
	return &amount
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
	t.Setenv("AI_MODEL", "")
	t.Setenv("NEURATRADE_SCALPING_MODEL", "")

	config := DefaultAIScalpingConfig()

	assert.Equal(t, "bitget", config.Exchange)
	assert.Equal(t, defaultRuntimeAIModel, config.Model)
	assert.Equal(t, 5, config.Leverage)
	assert.Equal(t, 5.0, config.MaxCapitalPct)
	assert.Equal(t, 0.55, config.MinConfidence)
	assert.Equal(t, 3, config.MaxIterations)
	assert.True(t, config.AutoExecute)
	assert.False(t, config.AllowSpotFallback)
	assert.Equal(t, 8, config.MaxPairsToAnalyze)
	assert.Equal(t, 120, config.MaxCandidatePairs)
	assert.Equal(t, appautonomy.DefaultScalpingMaxBidAskSpreadPct, config.MaxBidAskSpreadPct)
	assert.Equal(t, 8, config.OrderBookPairs)
	assert.True(t, config.AutoExpandOrderBooks)
	assert.Equal(t, 12, config.AutoExpandThreshold)
	assert.True(t, config.EnforceFutures)
	assert.Equal(t, 90*time.Second, config.SymbolCooldown)
	assert.Equal(t, 3, config.FailureBudget)
	assert.Equal(t, 15*time.Minute, config.FailureWindow)
	assert.Equal(t, 2, config.StructuredRetries)
	assert.True(t, config.PreTradeGate)
	assert.GreaterOrEqual(t, config.MinExpectancyEdge, 0.005,
		"MinExpectancyEdge default must require at least 0.5%% net edge; with avg_loss > avg_win a 0.1%% threshold lets negative-expectancy trades through (READINESS_ASSESSMENT_2026_06_10)")
	assert.Equal(t, 50, config.MinExpectancyN)
	assert.Equal(t, 85.0, config.RegimeHighBand)
	assert.Equal(t, 15.0, config.RegimeLowBand)
	assert.Equal(t, 0.15, config.DeterministicFallback.MaxBidAskSpread)
	assert.Equal(t, 0.10, config.DeterministicFallback.MinImbalance)
	assert.Equal(t, 0.65, config.RegimeChopConfidence)
	assert.Equal(t, 0.72, config.DeterministicFallback.ConfidenceFloor)
	assert.Equal(t, 0.50, config.DeterministicFallback.SizeFraction)
}

func TestAIScalpingService_EffectiveOrderBookPairsDefaultCoversSmallUniverse(t *testing.T) {
	svc := &AIScalpingService{config: DefaultAIScalpingConfig()}

	assert.Equal(t, 8, svc.effectiveOrderBookPairs(8))
	assert.Equal(t, 3, svc.effectiveOrderBookPairs(3))
	assert.Equal(t, defaultOrderBookPairsBase, svc.effectiveOrderBookPairs(defaultAutoExpandThreshold+1))
}

func TestResolveEnvModel(t *testing.T) {
	t.Setenv("AI_MODEL", "")
	t.Setenv("NEURATRADE_SCALPING_MODEL", "")
	assert.Equal(t, defaultRuntimeAIModel, resolveEnvModel())

	t.Setenv("NEURATRADE_SCALPING_MODEL", "scalping-model")
	assert.Equal(t, "scalping-model", resolveEnvModel())

	t.Setenv("AI_MODEL", "global-model")
	assert.Equal(t, "global-model", resolveEnvModel())
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
	t.Setenv("NEURATRADE_HOME", t.TempDir())
	t.Setenv("NEURATRADE_SCALPING_EXCHANGE", "binance")
	t.Setenv("NEURATRADE_SCALPING_LEVERAGE", "12")
	t.Setenv("NEURATRADE_SCALPING_MAX_CAPITAL_PCT", "3.5")
	t.Setenv("NEURATRADE_SCALPING_MIN_CONFIDENCE", "0.61")
	t.Setenv("NEURATRADE_SCALPING_TIMEOUT_SECONDS", "45")
	t.Setenv("NEURATRADE_SCALPING_AUTO_EXECUTE", "false")
	t.Setenv("NEURATRADE_SCALPING_ALLOW_SPOT_FALLBACK", "true")
	t.Setenv("NEURATRADE_SCALPING_MAX_PAIRS", "11")
	t.Setenv("NEURATRADE_SCALPING_MAX_CANDIDATES", "210")
	t.Setenv(appautonomy.NeuraScalpingMaxBidAskSpreadPctEnv, "0.27")
	t.Setenv("NEURATRADE_SCALPING_ORDERBOOK_PAIRS", "6")
	t.Setenv("NEURATRADE_SCALPING_AUTO_EXPAND_ORDERBOOK_PAIRS", "true")
	t.Setenv("NEURATRADE_SCALPING_AUTO_EXPAND_ORDERBOOK_PAIRS_THRESHOLD", "9")
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
	t.Setenv("NEURATRADE_SCALPING_REGIME_CHOP_CONFIDENCE", "0.42")
	t.Setenv("NEURATRADE_SCALPING_FALLBACK_MAX_BID_ASK_SPREAD", "0.04")
	t.Setenv("NEURATRADE_SCALPING_FALLBACK_MIN_IMBALANCE", "0.42")
	t.Setenv("NEURATRADE_SCALPING_FALLBACK_CONFIDENCE_FLOOR", "0.79")
	t.Setenv("NEURATRADE_SCALPING_FALLBACK_RANGE_OFFSET", "0")
	t.Setenv("NEURATRADE_SCALPING_FALLBACK_SIZE_FRACTION", "0.33")

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
	assert.Equal(t, 0.27, cfg.MaxBidAskSpreadPct)
	assert.Equal(t, 6, cfg.OrderBookPairs)
	assert.True(t, cfg.AutoExpandOrderBooks)
	assert.Equal(t, 9, cfg.AutoExpandThreshold)
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
	assert.Equal(t, 0.42, cfg.RegimeChopConfidence)
	assert.Equal(t, 0.04, cfg.DeterministicFallback.MaxBidAskSpread)
	assert.Equal(t, 0.42, cfg.DeterministicFallback.MinImbalance)
	assert.Equal(t, 0.79, cfg.DeterministicFallback.ConfidenceFloor)
	assert.Equal(t, 0.0, cfg.DeterministicFallback.RangeOffset)
	assert.Equal(t, 0.33, cfg.DeterministicFallback.SizeFraction)
}

// TestResolveAIScalpingConfigFromEnv_RejectsInvalidChopConfidence is a
// regression guard for the env-resolver contract: invalid chop-confidence
// values (≤0, >1, NaN) must NOT override the canonical default. The runtime
// guard in classifyScalpingRegime is defense-in-depth; the resolver should
// already filter these so the startup log and the runtime behavior agree.
func TestResolveAIScalpingConfigFromEnv_RejectsInvalidChopConfidence(t *testing.T) {
	t.Setenv("NEURATRADE_HOME", t.TempDir())
	defaultConfidence := DefaultAIScalpingConfig().RegimeChopConfidence

	tests := []struct {
		name      string
		envValue  string
		wantValue float64
	}{
		{name: "zero rejected, default kept", envValue: "0", wantValue: defaultConfidence},
		{name: "negative rejected, default kept", envValue: "-0.5", wantValue: defaultConfidence},
		{name: "above one rejected, default kept", envValue: "1.5", wantValue: defaultConfidence},
		{name: "NaN rejected, default kept", envValue: "NaN", wantValue: defaultConfidence},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("NEURATRADE_SCALPING_REGIME_CHOP_CONFIDENCE", tc.envValue)
			cfg := ResolveAIScalpingConfigFromEnv(DefaultAIScalpingConfig())
			assert.InDelta(t, tc.wantValue, cfg.RegimeChopConfidence, 1e-9)
		})
	}
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

func TestAIScalpingService_ParseDecisionWithRetries_InfersHoldFromLooseText(t *testing.T) {
	mockLLM := &MockLLMClient{
		Responses: []*llm.CompletionResponse{
			{Message: llm.Message{Content: `{"action":"hold","symbol":"","size_pct":0,"confidence":0.1,"reasoning":"unexpected remote repair"}`}},
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

	decision, err := svc.parseDecisionWithRetries(
		context.Background(),
		`Recommended Action: hold
Confidence: 35%
Reason: Waiting for qualified setup until spread is executable.`,
	)

	require.NoError(t, err)
	require.NotNil(t, decision)
	assert.Equal(t, "hold", decision.Action)
	assert.Equal(t, "", decision.Symbol)
	assert.True(t, decision.ConfidenceKnown)
	assert.InDelta(t, 0.35, decision.Confidence, 0.0001)
	assert.Equal(t, reasonCategoryStrategyHold, decision.ReasonCategory)
	assert.Equal(t, 0, mockLLM.CallCount)
}

func TestAIScalpingService_ParseDecisionWithRetries_NoLLMAndInferenceFailure(t *testing.T) {
	svc := &AIScalpingService{
		config: AIScalpingConfig{
			Model:             "glm-5",
			MaxTokens:         1200,
			StructuredRetries: 2,
		},
	}

	decision, err := svc.parseDecisionWithRetries(context.Background(), "<<<garbled-response>>>")

	require.Error(t, err)
	assert.Nil(t, decision)
}

func TestAIScalpingService_ParseDecisionWithRetries_InfersActionableDecisionFromMalformedJSON(t *testing.T) {
	mockLLM := &MockLLMClient{
		Responses: []*llm.CompletionResponse{
			{Message: llm.Message{Content: `{"action":"hold","symbol":"","size_pct":0,"confidence":0.1,"reasoning":"unexpected remote repair"}`}},
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

	decision, err := svc.parseDecisionWithRetries(
		context.Background(),
		`{"action":"buy","symbol":"BTC/USDT","size_pct":"0.75","confidence":"68%","reason":"Breakout with tight spread","stop_loss":"41000","take_profit":"43000"`,
	)

	require.NoError(t, err)
	require.NotNil(t, decision)
	assert.Equal(t, "buy", decision.Action)
	assert.Equal(t, "BTC/USDT", decision.Symbol)
	assert.InDelta(t, 0.75, decision.SizePercent, 0.0001)
	assert.InDelta(t, 0.68, decision.Confidence, 0.0001)
	assert.True(t, decision.ConfidenceKnown)
	assert.Equal(t, "", decision.ReasonCategory)
	require.NotNil(t, decision.StopLoss)
	require.NotNil(t, decision.TakeProfit)
	assert.True(t, decision.StopLoss.Equal(decimal.NewFromInt(41000)))
	assert.True(t, decision.TakeProfit.Equal(decimal.NewFromInt(43000)))
	assert.Equal(t, 0, mockLLM.CallCount)
}

func TestAIScalpingService_ParseDecisionWithRetries_RejectsMalformedRecoveredActionableContract(t *testing.T) {
	mockLLM := &MockLLMClient{
		Responses: []*llm.CompletionResponse{
			{Message: llm.Message{Content: `{"action":"hold","symbol":"","size_pct":0,"confidence":0.1,"reasoning":"repair fallback"}`}},
		},
	}

	svc := &AIScalpingService{
		config: AIScalpingConfig{
			Model:             "glm-5",
			MaxTokens:         1200,
			StructuredRetries: 1,
		},
		llmClient: mockLLM,
	}

	decision, err := svc.parseDecisionWithRetries(context.Background(), `{"action":"buy","symbol":"BAN/US","size_pct":0,"confidence":0,"reasoning":"bad contract"}`)

	require.NoError(t, err)
	require.NotNil(t, decision)
	assert.Equal(t, "hold", decision.Action)
	assert.Equal(t, 1, mockLLM.CallCount)
}

func TestAIScalpingService_ParseDecisionWithRetries_AcceptsMixedCaseHold(t *testing.T) {
	svc := &AIScalpingService{}

	decision, err := svc.parseDecisionWithRetries(context.Background(), `{"action":"HOLD","symbol":"","size_pct":0,"confidence":0.42,"reasoning":"wait"}`)

	require.NoError(t, err)
	require.NotNil(t, decision)
	assert.Equal(t, "HOLD", decision.Action)
	assert.InDelta(t, 0.42, decision.Confidence, 0.0001)
}

func TestInferDecisionFromLooseText_ConfidenceNormalization(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected float64
		action   string
		known    bool
	}{
		{
			name:     "percentage_string",
			input:    `{"action":"buy","symbol":"FOO/USDT","size_pct":"10","confidence":"35%","reasoning":"looks good"}`,
			expected: 0.35,
			action:   "buy",
			known:    true,
		},
		{
			name:     "raw_decimal",
			input:    `{"action":"sell","symbol":"BAR/USDT","size_pct":"5","confidence":"0.35","reasoning":"looks bad"}`,
			expected: 0.35,
			action:   "sell",
			known:    true,
		},
		{
			name:     "loose_percent_spacing",
			input:    `{"action":"hold","confidence":" ~68 % ","reasoning":"uncertain and waiting for stronger confirmation"}`,
			expected: 0.68,
			action:   "hold",
			known:    true,
		},
		{
			name:     "over_100_clamped",
			input:    `{"action":"buy","symbol":"FOO/USDT","size_pct":"10","confidence":"135%","reasoning":"overconfident"}`,
			expected: 1.0,
			action:   "buy",
			known:    true,
		},
		{
			name:     "negative_clamped",
			input:    `{"action":"sell","symbol":"BAR/USDT","size_pct":"5","confidence":"-12%","reasoning":"underconfident"}`,
			expected: 0.0,
			action:   "sell",
			known:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, err := inferDecisionFromLooseText(tt.input)
			require.NoError(t, err)
			require.NotNil(t, decision)
			assert.Equal(t, tt.action, decision.Action)
			assert.Equal(t, tt.known, decision.ConfidenceKnown)
			assert.InDelta(t, tt.expected, decision.Confidence, 0.0001)
		})
	}
}

func TestInferDecisionFromLooseText_ParsesSemicolonSeparatedFields(t *testing.T) {
	decision, err := inferDecisionFromLooseText(
		`action: buy; symbol: BTC/USDT; size_pct: 0.75; confidence: 68%; reasoning: breakout; stop_loss: 41000; take_profit: 43000`,
	)

	require.NoError(t, err)
	require.NotNil(t, decision)
	assert.Equal(t, "buy", decision.Action)
	assert.Equal(t, "BTC/USDT", decision.Symbol)
	assert.InDelta(t, 0.75, decision.SizePercent, 0.0001)
	assert.InDelta(t, 0.68, decision.Confidence, 0.0001)
	require.NotNil(t, decision.StopLoss)
	require.NotNil(t, decision.TakeProfit)
	assert.True(t, decision.StopLoss.Equal(decimal.NewFromInt(41000)))
	assert.True(t, decision.TakeProfit.Equal(decimal.NewFromInt(43000)))
}

func TestInferDecisionFromLooseText_ParsesLeadingDotDecimals(t *testing.T) {
	decision, err := inferDecisionFromLooseText(
		`action: buy; symbol: BTC/USDT; size_pct: .75; confidence: -.25; reasoning: probe entry`,
	)

	require.NoError(t, err)
	require.NotNil(t, decision)
	assert.Equal(t, "buy", decision.Action)
	assert.InDelta(t, 0.75, decision.SizePercent, 0.0001)
	assert.InDelta(t, 0.0, decision.Confidence, 0.0001)
}

func TestInferDecisionFromLooseText_SingleQuotedPseudoJSON(t *testing.T) {
	decision, err := inferDecisionFromLooseText(
		`{'action':'buy','symbol':'BTC/USDT','size_pct':'0.75','confidence':'68%','reason':'Breakout with tight spread','stop_loss':'41000','take_profit':'43000'}`,
	)

	require.NoError(t, err)
	require.NotNil(t, decision)
	assert.Equal(t, "buy", decision.Action)
	assert.Equal(t, "BTC/USDT", decision.Symbol)
	assert.InDelta(t, 0.75, decision.SizePercent, 0.0001)
	assert.InDelta(t, 0.68, decision.Confidence, 0.0001)
	assert.True(t, decision.ConfidenceKnown)
	require.NotNil(t, decision.StopLoss)
	require.NotNil(t, decision.TakeProfit)
	assert.True(t, decision.StopLoss.Equal(decimal.NewFromInt(41000)))
	assert.True(t, decision.TakeProfit.Equal(decimal.NewFromInt(43000)))
}

func TestInferDecisionFromLooseText_MissingMandatoryFields(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "missing_symbol",
			input: `{"action":"buy","size_pct":"10","confidence":"0.8","reasoning":"ok"}`,
		},
		{
			name:  "missing_size_pct",
			input: `{"action":"buy","symbol":"FOO/USDT","confidence":"0.8","reasoning":"ok"}`,
		},
		{
			name:  "missing_confidence",
			input: `{"action":"buy","symbol":"FOO/USDT","size_pct":"10","reasoning":"ok"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, err := inferDecisionFromLooseText(tt.input)
			require.Error(t, err)
			assert.Nil(t, decision)
		})
	}
}

func TestInferDecisionFromLooseText_NaturalLanguageHoldPhrases(t *testing.T) {
	tests := []struct {
		input string
		known bool
	}{
		{input: `No trade. Confidence: 25%. Reason: preserve capital until spread tightens.`, known: true},
		{input: `I am staying out. Confidence: 0.40. Reason: waiting for qualified setup.`, known: true},
		{input: `Recommended Action: hold
Confidence: 55%
Reason: waiting for stronger confirmation.`, known: true},
		{input: `Let me analyze the market signals and make a trading decision.

## Analysis Summary
No valid trade setups. No signals meet the 0.65 confidence threshold.`, known: false},
	}

	for _, tt := range tests {
		decision, err := inferDecisionFromLooseText(tt.input)
		require.NoError(t, err)
		require.NotNil(t, decision)
		assert.Equal(t, "hold", decision.Action)
		assert.Equal(t, "", decision.Symbol)
		assert.Equal(t, tt.known, decision.ConfidenceKnown)
		assert.GreaterOrEqual(t, decision.Confidence, 0.0)
		assert.LessOrEqual(t, decision.Confidence, 1.0)
	}
}

func TestInferDecisionFromLooseText_ParsesNarrativeRecommendation(t *testing.T) {
	decision, err := inferDecisionFromLooseText(`Let me analyze the market signals and make a trading decision.

## Best Candidates:
**PEPE/USDT**:
- Strong ob_imbalance: 0.303 (> 0.2)
- Signal: STRONG BUY - meets criteria!
- Spread: 0.03% (well under 0.22%)
- Confidence: This is a strong signal

## Decision:
Both are valid, but PEPE has better spread and volume.
The PEPE signal has clear order book buy pressure.
I would estimate confidence around 0.70-0.75 for this trade.
Given the requirement to trade at size_pct of 12.7846%, with wallet of 46.93 USDT.
`)

	require.NoError(t, err)
	require.NotNil(t, decision)
	assert.Equal(t, "buy", decision.Action)
	assert.Equal(t, "PEPE/USDT", decision.Symbol)
	assert.InDelta(t, 12.7846, decision.SizePercent, 0.0001)
	assert.InDelta(t, 0.70, decision.Confidence, 0.0001)
}

func TestAIScalpingService_GatherMarketSignals_FetchesOrderbookForFullSmallUniverse(t *testing.T) {
	mockCCXT := &mockAIScalpingCCXT{
		markets: &ccxt.MarketsResponse{
			Exchange: "bitget",
			Symbols: []string{
				"AAA/USDT", "BBB/USDT", "CCC/USDT", "DDD/USDT",
				"EEE/USDT", "FFF/USDT", "GGG/USDT", "HHH/USDT",
			},
			Count: 8,
		},
		marketData: []ccxt.MarketPriceInterface{
			mockMarketPrice{symbol: "AAA/USDT", price: 1, volume: 1000, high24h: 1.1, low24h: 0.9, bid: 0.95, ask: 1.05, exchange: "bitget"},
			mockMarketPrice{symbol: "BBB/USDT", price: 1, volume: 1000, high24h: 1.1, low24h: 0.9, bid: 0.95, ask: 1.05, exchange: "bitget"},
			mockMarketPrice{symbol: "CCC/USDT", price: 1, volume: 1000, high24h: 1.1, low24h: 0.9, bid: 0.95, ask: 1.05, exchange: "bitget"},
			mockMarketPrice{symbol: "DDD/USDT", price: 1, volume: 1000, high24h: 1.1, low24h: 0.9, bid: 0.95, ask: 1.05, exchange: "bitget"},
			mockMarketPrice{symbol: "EEE/USDT", price: 1, volume: 1000, high24h: 1.1, low24h: 0.9, bid: 0.95, ask: 1.05, exchange: "bitget"},
			mockMarketPrice{symbol: "FFF/USDT", price: 1, volume: 1000, high24h: 1.1, low24h: 0.9, bid: 0.95, ask: 1.05, exchange: "bitget"},
			mockMarketPrice{symbol: "GGG/USDT", price: 1, volume: 1000, high24h: 1.1, low24h: 0.9, bid: 0.95, ask: 1.05, exchange: "bitget"},
			mockMarketPrice{symbol: "HHH/USDT", price: 1, volume: 1000, high24h: 1.1, low24h: 0.9, bid: 0.95, ask: 1.05, exchange: "bitget"},
		},
		orderBooks: map[string]*ccxt.OrderBookResponse{
			"AAA/USDT": {OrderBook: ccxt.OrderBook{Bids: []ccxt.OrderBookEntry{{Price: decimal.NewFromFloat(0.999), Amount: decimal.NewFromInt(5)}}, Asks: []ccxt.OrderBookEntry{{Price: decimal.NewFromFloat(1.001), Amount: decimal.NewFromInt(4)}}}},
			"BBB/USDT": {OrderBook: ccxt.OrderBook{Bids: []ccxt.OrderBookEntry{{Price: decimal.NewFromFloat(0.999), Amount: decimal.NewFromInt(5)}}, Asks: []ccxt.OrderBookEntry{{Price: decimal.NewFromFloat(1.001), Amount: decimal.NewFromInt(4)}}}},
			"CCC/USDT": {OrderBook: ccxt.OrderBook{Bids: []ccxt.OrderBookEntry{{Price: decimal.NewFromFloat(0.999), Amount: decimal.NewFromInt(5)}}, Asks: []ccxt.OrderBookEntry{{Price: decimal.NewFromFloat(1.001), Amount: decimal.NewFromInt(4)}}}},
			"DDD/USDT": {OrderBook: ccxt.OrderBook{Bids: []ccxt.OrderBookEntry{{Price: decimal.NewFromFloat(0.999), Amount: decimal.NewFromInt(5)}}, Asks: []ccxt.OrderBookEntry{{Price: decimal.NewFromFloat(1.001), Amount: decimal.NewFromInt(4)}}}},
			"EEE/USDT": {OrderBook: ccxt.OrderBook{Bids: []ccxt.OrderBookEntry{{Price: decimal.NewFromFloat(0.999), Amount: decimal.NewFromInt(5)}}, Asks: []ccxt.OrderBookEntry{{Price: decimal.NewFromFloat(1.001), Amount: decimal.NewFromInt(4)}}}},
			"FFF/USDT": {OrderBook: ccxt.OrderBook{Bids: []ccxt.OrderBookEntry{{Price: decimal.NewFromFloat(0.999), Amount: decimal.NewFromInt(5)}}, Asks: []ccxt.OrderBookEntry{{Price: decimal.NewFromFloat(1.001), Amount: decimal.NewFromInt(4)}}}},
			"GGG/USDT": {OrderBook: ccxt.OrderBook{Bids: []ccxt.OrderBookEntry{{Price: decimal.NewFromFloat(0.999), Amount: decimal.NewFromInt(5)}}, Asks: []ccxt.OrderBookEntry{{Price: decimal.NewFromFloat(1.001), Amount: decimal.NewFromInt(4)}}}},
			"HHH/USDT": {OrderBook: ccxt.OrderBook{Bids: []ccxt.OrderBookEntry{{Price: decimal.NewFromFloat(0.999), Amount: decimal.NewFromInt(5)}}, Asks: []ccxt.OrderBookEntry{{Price: decimal.NewFromFloat(1.001), Amount: decimal.NewFromInt(4)}}}},
		},
	}

	svc := &AIScalpingService{
		config: AIScalpingConfig{
			Exchange:             "bitget",
			MaxPairsToAnalyze:    8,
			MaxCandidatePairs:    8,
			MaxBidAskSpreadPct:   appautonomy.DefaultScalpingMaxBidAskSpreadPct,
			OrderBookPairs:       8,
			AutoExpandOrderBooks: true,
			AutoExpandThreshold:  12,
			EnforceFutures:       false,
		},
		ccxtService: mockCCXT,
	}

	signals, err := svc.gatherMarketSignals(context.Background())
	require.NoError(t, err)
	require.Len(t, signals, 8)
	assert.ElementsMatch(t, []string{
		normalizeSymbolForComparison("AAA/USDT"),
		normalizeSymbolForComparison("BBB/USDT"),
		normalizeSymbolForComparison("CCC/USDT"),
		normalizeSymbolForComparison("DDD/USDT"),
		normalizeSymbolForComparison("EEE/USDT"),
		normalizeSymbolForComparison("FFF/USDT"),
		normalizeSymbolForComparison("GGG/USDT"),
		normalizeSymbolForComparison("HHH/USDT"),
	}, mockCCXT.orderBookOps)
	for _, signal := range signals {
		orderBook := mockCCXT.orderBooks[signal.Symbol]
		require.NotNil(t, orderBook)
		require.NotEmpty(t, orderBook.OrderBook.Bids)
		require.NotEmpty(t, orderBook.OrderBook.Asks)
		expectedSpread := (orderBook.OrderBook.Asks[0].Price.InexactFloat64() - orderBook.OrderBook.Bids[0].Price.InexactFloat64()) / signal.Price * 100
		assert.InDelta(t, expectedSpread, signal.BidAskSpread, 1e-9)
	}
}

func TestAIScalpingService_GatherMarketSignals_TickerFallbackWhenOrderbookUnavailable(t *testing.T) {
	mockCCXT := &mockAIScalpingCCXT{
		markets: &ccxt.MarketsResponse{
			Exchange: "bitget",
			Symbols:  []string{"BTC/USDT", "ETH/USDT", "SOL/USDT", "DOGE/USDT"},
			Count:    4,
		},
		marketData: []ccxt.MarketPriceInterface{
			mockMarketPrice{symbol: "BTC/USDT", price: 50000, volume: 1000, high24h: 51000, low24h: 49000, bid: 49990, ask: 50010, exchange: "bitget"},
			mockMarketPrice{symbol: "ETH/USDT", price: 3000, volume: 5000, high24h: 3100, low24h: 2900, bid: 2998, ask: 3002, exchange: "bitget"},
			mockMarketPrice{symbol: "SOL/USDT", price: 150, volume: 10000, high24h: 160, low24h: 140, bid: 149.9, ask: 150.1, exchange: "bitget"},
			mockMarketPrice{symbol: "DOGE/USDT", price: 0.08, volume: 1000000, high24h: 0.09, low24h: 0.07, bid: 0.0799, ask: 0.0801, exchange: "bitget"},
		},
		// Intentionally nil orderBooks to force ticker bid/ask fallback for all pairs.
		orderBooks: nil,
	}

	svc := &AIScalpingService{
		config: AIScalpingConfig{
			Exchange:             "bitget",
			MaxPairsToAnalyze:    4,
			MaxCandidatePairs:    8,
			MaxBidAskSpreadPct:   appautonomy.DefaultScalpingMaxBidAskSpreadPct,
			OrderBookPairs:       4,
			AutoExpandOrderBooks: true,
			AutoExpandThreshold:  12,
			EnforceFutures:       false,
		},
		ccxtService: mockCCXT,
	}

	signals, err := svc.gatherMarketSignals(context.Background())
	require.NoError(t, err)
	require.Len(t, signals, 4)
	// Expected spreads derived from the ticker mock above; mirrors the formula
	// in tickerBidAskFallback: (ask - bid) / price * 100. Asserting exact values
	// (not just > 0) catches silent regressions in the fallback math.
	expectedSpreads := map[string]float64{
		"BTC/USDT":  (50010.0 - 49990.0) / 50000.0 * 100, // 0.04
		"ETH/USDT":  (3002.0 - 2998.0) / 3000.0 * 100,    // ≈0.13333
		"SOL/USDT":  (150.1 - 149.9) / 150.0 * 100,       // ≈0.13333
		"DOGE/USDT": (0.0801 - 0.0799) / 0.08 * 100,      // 0.25
	}
	for _, signal := range signals {
		expected, ok := expectedSpreads[signal.Symbol]
		require.Truef(t, ok, "unexpected symbol %s in signals", signal.Symbol)
		assert.InDeltaf(t, expected, signal.BidAskSpread, 1e-9,
			"ticker-derived spread mismatch for %s", signal.Symbol)
		assert.Greaterf(t, signal.BidAskSpread, 0.0,
			"BidAskSpread should be > 0 via ticker fallback for %s", signal.Symbol)
		// OrderBookImbalance must be zero — no orderbook data was available.
		assert.Equal(t, 0.0, signal.OrderBookImbalance,
			"OrderBookImbalance should be 0 when no orderbook for %s", signal.Symbol)
	}
	// Verify that FetchOrderBook was indeed called (all 4 pairs attempted).
	assert.Len(t, mockCCXT.orderBookOps, 4)
}

func TestAIScalpingService_DiscoverTradingPairs_PrefersTradableSpreadCandidates(t *testing.T) {
	mockCCXT := &mockAIScalpingCCXT{
		markets: &ccxt.MarketsResponse{
			Exchange: "bitget",
			Symbols:  []string{"ILLIQ/USDT", "TIGHTA/USDT", "TIGHTB/USDT"},
			Count:    3,
		},
		marketData: []ccxt.MarketPriceInterface{
			mockMarketPrice{symbol: "ILLIQ/USDT", price: 1.0, volume: 1_000_000_000_000, high24h: 1.2, low24h: 0.8, bid: 1.00, ask: 1.25, exchange: "bitget"},
			mockMarketPrice{symbol: "TIGHTA/USDT", price: 1.0, volume: 1, high24h: 1.02, low24h: 0.98, bid: 0.9999, ask: 1.0001, exchange: "bitget"},
			mockMarketPrice{symbol: "TIGHTB/USDT", price: 1.0, volume: 1, high24h: 1.03, low24h: 0.97, bid: 0.9998, ask: 1.0002, exchange: "bitget"},
		},
	}

	svc := &AIScalpingService{
		config: AIScalpingConfig{
			Exchange:          "bitget",
			MaxPairsToAnalyze: 2,
			MaxCandidatePairs: 10,
			EnforceFutures:    false,
		},
		ccxtService: mockCCXT,
	}

	pairs, err := svc.discoverTradingPairs(context.Background())
	require.NoError(t, err)
	require.Len(t, pairs, 2)
	assert.Contains(t, pairs, "TIGHTA/USDT")
	assert.Contains(t, pairs, "TIGHTB/USDT")
	assert.NotContains(t, pairs, "ILLIQ/USDT")
}

func TestAIScalpingService_DiscoverTradingPairs_ReusesStaleCacheWhenMarketDiscoveryFails(t *testing.T) {
	mockCCXT := &mockAIScalpingCCXT{
		marketsErr: fmt.Errorf("bitget market metadata timeout"),
	}

	svc := &AIScalpingService{
		config: AIScalpingConfig{
			Exchange:          "bitget",
			MaxPairsToAnalyze: 2,
			MaxCandidatePairs: 10,
			EnforceFutures:    false,
		},
		ccxtService: mockCCXT,
	}
	svc.updatePairCache("bitget", []string{"ONDO/USDT", "CHZ/USDT"})
	svc.pairCacheMu.Lock()
	svc.cacheUpdated = time.Now().Add(-10 * time.Minute)
	svc.pairCacheMu.Unlock()

	pairs, err := svc.discoverTradingPairs(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"ONDO/USDT", "CHZ/USDT"}, pairs)
}

func TestAIScalpingService_PreTradeGate_UsesVisibleSpreadThreshold(t *testing.T) {
	t.Setenv(appautonomy.NeuraScalpingMaxBidAskSpreadPctEnv, "0.22")
	svc := &AIScalpingService{config: AIScalpingConfig{PreTradeGate: true, RegimeLowBand: 15, RegimeHighBand: 85}}

	tests := []struct {
		name    string
		spread  float64
		allowed bool
	}{
		{name: "below_cutoff", spread: 0.219, allowed: true},
		{name: "at_cutoff", spread: 0.22, allowed: true},
		{name: "above_cutoff", spread: 0.221, allowed: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signal := aiMarketSignal{
				Symbol:             "GRASS/USDT",
				Price:              1,
				High24h:            1.1,
				Low24h:             0.9,
				Volume24h:          100000,
				BidAskSpread:       tt.spread,
				OrderBookImbalance: 0.76,
				RangePosition24h:   18,
			}
			decision := &AITradingDecision{Action: "buy", Symbol: "GRASS/USDT", Confidence: 0.78}

			result := svc.evaluatePreTradeGate(context.Background(), decision, []aiMarketSignal{signal})
			assert.Equal(t, tt.allowed, result.Allowed)

			err := svc.validateDecision(context.Background(), &AITradingDecision{
				Action:      "buy",
				Symbol:      "GRASS/USDT",
				SizePercent: 12.78,
				Confidence:  0.78,
				Reasoning:   "spread threshold regression",
				StopLoss:    decimalPointer("0.95"),
				TakeProfit:  decimalPointer("1.08"),
			}, []aiMarketSignal{signal})
			if tt.allowed {
				assert.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "spread")
			}
		})
	}
}

func TestAIScalpingService_EvaluatePreTradeGate_RespectsRegimeChopConfidence(t *testing.T) {
	// Coderabbit MAJOR on PR #462: RegimeChopConfidence must be applied
	// end-to-end in evaluatePreTradeGate's chop-blocking branch, not just
	// in classifyScalpingRegime. Lowering the knob should lower the
	// confidence floor that blocks low-confidence trades in chop regimes.
	// Without this, NEURATRADE_SCALPING_REGIME_CHOP_CONFIDENCE would only
	// affect the regime label, not the actual gating — making the env var
	// a no-op for chop-heavy markets (the exact scenario it was introduced
	// to address).
	signal := aiMarketSignal{
		Symbol:             "BTC/USDT",
		Price:              100,
		BidAskSpread:       0.05,
		OrderBookImbalance: 0.05, // < 0.10 → chop regime
		RangePosition24h:   50,
	}
	decision := &AITradingDecision{
		Action:     "buy",
		Symbol:     "BTC/USDT",
		Confidence: 0.50, // below both 0.65 (default) and 0.40 (relaxed)
	}

	tests := []struct {
		name     string
		chopConf float64
		allowed  bool
	}{
		{name: "default 0.65 blocks 0.50 confidence chop trade", chopConf: 0.65, allowed: false},
		{name: "lowered 0.40 allows 0.50 confidence chop trade", chopConf: 0.40, allowed: true},
		{name: "raised 0.80 still blocks 0.50 confidence chop trade", chopConf: 0.80, allowed: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &AIScalpingService{config: AIScalpingConfig{
				RegimeChopConfidence: tt.chopConf,
				MaxBidAskSpreadPct:   0.5,
				PreTradeGate:         true,
			}}
			result := svc.evaluatePreTradeGate(context.Background(), decision, []aiMarketSignal{signal})
			assert.Equal(t, "chop", result.Regime, "regime should be 'chop' for |imbalance|<0.10")
			assert.Equal(t, tt.allowed, result.Allowed, "chop confidence %.2f should set allowed=%t for decision confidence 0.50", tt.chopConf, tt.allowed)
			if !tt.allowed {
				assert.Contains(t, result.Reason, "choppy")
			}
		})
	}
}

func TestAIScalpingService_ValidateDecision_AdjustsLowRiskRewardForBuy(t *testing.T) {
	svc := &AIScalpingService{}
	decision := &AITradingDecision{
		Action:      "buy",
		Symbol:      "DOGE/USDT",
		SizePercent: 12.7847,
		Confidence:  0.68,
		Reasoning:   "live low rr regression",
		StopLoss:    decimalPointer("0.09350"),
		TakeProfit:  decimalPointer("0.09444"),
	}

	err := svc.validateDecision(context.Background(), decision, []aiMarketSignal{{
		Symbol:             "DOGE/USDT",
		Price:              0.09429,
		High24h:            0.095,
		Low24h:             0.091,
		Volume24h:          1_000_000,
		BidAskSpread:       0.011,
		OrderBookImbalance: 0.45,
		RangePosition24h:   18.92,
	}})

	require.NoError(t, err)
	require.NotNil(t, decision.TakeProfit)
	assert.Greater(t, decision.TakeProfit.InexactFloat64(), 0.09515)
}

func TestAIScalpingService_ValidateDecision_BlocksRecentBuyAboveRangeCeiling(t *testing.T) {
	svc := &AIScalpingService{}
	decision := &AITradingDecision{
		Action:      "buy",
		Symbol:      "UNI/USDT",
		SizePercent: 5,
		Confidence:  0.78,
		Reasoning:   "model buy in mid-range with recent momentum",
		StopLoss:    decimalPointer("3.35"),
		TakeProfit:  decimalPointer("3.60"),
	}

	err := svc.validateDecision(context.Background(), decision, []aiMarketSignal{{
		Symbol:             "UNI/USDT",
		Price:              3.47,
		High24h:            3.70,
		Low24h:             3.22,
		Volume24h:          1_500_000,
		BidAskSpread:       0.029,
		OrderBookImbalance: 0.63,
		RangePosition24h:   47,
		PriceChange24h:     0.25,
		RecentPriceChange:  0.09,
		RecentChangeKnown:  true,
	}})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "above recent-buy range ceiling")
}

func TestAIScalpingService_ValidateDecision_BlocksRecentBuyWithoutMomentum(t *testing.T) {
	svc := &AIScalpingService{}
	decision := &AITradingDecision{
		Action:      "buy",
		Symbol:      "DOGE/USDT",
		SizePercent: 5,
		Confidence:  0.72,
		Reasoning:   "model buy with stale/weak recent momentum",
		StopLoss:    decimalPointer("0.1010"),
		TakeProfit:  decimalPointer("0.1060"),
	}

	err := svc.validateDecision(context.Background(), decision, []aiMarketSignal{{
		Symbol:             "DOGE/USDT",
		Price:              0.1037,
		High24h:            0.108,
		Low24h:             0.099,
		Volume24h:          1_500_000,
		BidAskSpread:       0.018,
		OrderBookImbalance: 0.51,
		RangePosition24h:   30,
		PriceChange24h:     0.08,
		RecentPriceChange:  0.01,
		RecentChangeKnown:  true,
	}})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "without recent momentum confirmation")
}

func TestAIScalpingService_ValidateDecision_BlocksNoRecentBuyAboveRangeCeiling(t *testing.T) {
	svc := &AIScalpingService{}
	decision := &AITradingDecision{
		Action:      "buy",
		Symbol:      "CHZ/USDT",
		SizePercent: 5,
		Confidence:  0.70,
		Reasoning:   "model buy with only 24h/range hints",
		StopLoss:    decimalPointer("0.0460"),
		TakeProfit:  decimalPointer("0.0510"),
	}

	err := svc.validateDecision(context.Background(), decision, []aiMarketSignal{{
		Symbol:             "CHZ/USDT",
		Price:              0.0485,
		High24h:            0.051,
		Low24h:             0.045,
		Volume24h:          1_500_000,
		BidAskSpread:       0.050,
		OrderBookImbalance: 0.51,
		RangePosition24h:   53,
		PriceChange24h:     0.30,
		RecentChangeKnown:  false,
	}})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "without recent momentum confirmation above deep-low range ceiling")
}

func TestAIScalpingService_ValidateDecision_AllowsStrictRecentBuy(t *testing.T) {
	svc := &AIScalpingService{}
	decision := &AITradingDecision{
		Action:      "buy",
		Symbol:      "GOAT/USDT",
		SizePercent: 5,
		Confidence:  0.76,
		Reasoning:   "strict recent buy with confirmed trend",
		StopLoss:    decimalPointer("0.0177"),
		TakeProfit:  decimalPointer("0.0189"),
	}

	err := svc.validateDecision(context.Background(), decision, []aiMarketSignal{{
		Symbol:             "GOAT/USDT",
		Price:              0.0183,
		High24h:            0.0200,
		Low24h:             0.0175,
		Volume24h:          1_500_000,
		BidAskSpread:       0.020,
		OrderBookImbalance: 0.51,
		RangePosition24h:   32,
		PriceChange24h:     0.20,
		RecentPriceChange:  0.08,
		RecentChangeKnown:  true,
	}})

	require.NoError(t, err)
}

func TestAIScalpingService_ValidateDecision_BlocksSellWhenBroadTrendIsPositive(t *testing.T) {
	svc := &AIScalpingService{}
	decision := &AITradingDecision{
		Action:      "sell",
		Symbol:      "UNI/USDT",
		SizePercent: 12,
		Confidence:  0.72,
		Reasoning:   "model sell with weak recent momentum but positive 24h trend",
		StopLoss:    decimalPointer("3.62"),
		TakeProfit:  decimalPointer("3.48"),
	}

	err := svc.validateDecision(context.Background(), decision, []aiMarketSignal{{
		Symbol:             "UNI/USDT",
		Price:              3.568,
		High24h:            3.60,
		Low24h:             3.20,
		Volume24h:          1_500_000,
		BidAskSpread:       0.05605,
		OrderBookImbalance: -0.60517,
		RangePosition24h:   91.74,
		PriceChange24h:     0.0366,
		RecentPriceChange:  -0.112,
		RecentChangeKnown:  true,
	}})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "without 24h downside confirmation")
}

func TestAIScalpingService_ValidateDecision_BlocksSellWhenBroadDowntrendIsTooWeak(t *testing.T) {
	svc := &AIScalpingService{}
	decision := &AITradingDecision{
		Action:      "sell",
		Symbol:      "DOGE/USDT",
		SizePercent: 12,
		Confidence:  0.72,
		Reasoning:   "model sell with weak 24h downside and negative recent momentum",
		StopLoss:    decimalPointer("0.1065"),
		TakeProfit:  decimalPointer("0.1020"),
	}

	err := svc.validateDecision(context.Background(), decision, []aiMarketSignal{{
		Symbol:             "DOGE/USDT",
		Price:              0.10508,
		High24h:            0.107,
		Low24h:             0.102,
		Volume24h:          1_500_000,
		BidAskSpread:       0.00952,
		OrderBookImbalance: -0.50128,
		RangePosition24h:   54.89,
		PriceChange24h:     -0.0171,
		RecentPriceChange:  -0.0666,
		RecentChangeKnown:  true,
	}})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "required<=-0.0500%")
}

func TestAIScalpingService_ValidateDecision_RejectsCounterTrendBlowoffReversalSell(t *testing.T) {
	svc := &AIScalpingService{}
	decision := &AITradingDecision{
		Action:      "sell",
		Symbol:      "CHZ/USDT",
		SizePercent: 12,
		Confidence:  0.72,
		Reasoning:   "overextended blowoff reversal",
		StopLoss:    decimalPointer("0.0504"),
		TakeProfit:  decimalPointer("0.0487"),
	}

	err := svc.validateDecision(context.Background(), decision, []aiMarketSignal{{
		Symbol:             "CHZ/USDT",
		Price:              0.04983,
		High24h:            0.05,
		Low24h:             0.045,
		Volume24h:          1_500_000,
		BidAskSpread:       0.0602,
		OrderBookImbalance: -0.4513,
		RangePosition24h:   96.58,
		PriceChange24h:     0.0774,
		RecentPriceChange:  0.2817,
		RecentChangeKnown:  true,
	}})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "sell decision rejected without 24h downside confirmation")
}

func TestAIScalpingService_ValidateDecision_RejectsBlowoffSellWithPositiveBookPressure(t *testing.T) {
	svc := &AIScalpingService{}
	decision := &AITradingDecision{
		Action:      "sell",
		Symbol:      "CHZ/USDT",
		SizePercent: 12,
		Confidence:  0.72,
		Reasoning:   "overextended blowoff reversal",
		StopLoss:    decimalPointer("0.0504"),
		TakeProfit:  decimalPointer("0.0487"),
	}

	err := svc.validateDecision(context.Background(), decision, []aiMarketSignal{{
		Symbol:             "CHZ/USDT",
		Price:              0.04983,
		High24h:            0.05,
		Low24h:             0.045,
		Volume24h:          1_500_000,
		BidAskSpread:       0.0602,
		OrderBookImbalance: 0.1783,
		RangePosition24h:   96.58,
		PriceChange24h:     0.0774,
		RecentPriceChange:  0.2817,
		RecentChangeKnown:  true,
	}})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "sell decision rejected without 24h downside confirmation")
}

func TestAIScalpingService_ValidateDecision_RejectsBlowoffSellWithWeakBookPressure(t *testing.T) {
	svc := &AIScalpingService{}
	decision := &AITradingDecision{
		Action:      "sell",
		Symbol:      "ONDO/USDT",
		SizePercent: 12,
		Confidence:  0.72,
		Reasoning:   "overextended blowoff reversal",
		StopLoss:    decimalPointer("0.4005"),
		TakeProfit:  decimalPointer("0.3880"),
	}

	err := svc.validateDecision(context.Background(), decision, []aiMarketSignal{{
		Symbol:             "ONDO/USDT",
		Price:              0.3954,
		High24h:            0.3954,
		Low24h:             0.35,
		Volume24h:          1_500_000,
		BidAskSpread:       0.05061,
		OrderBookImbalance: -0.01196,
		RangePosition24h:   98.55,
		PriceChange24h:     0.1573,
		RecentPriceChange:  0.4575,
		RecentChangeKnown:  true,
	}})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "sell decision rejected without 24h downside confirmation")
}

func TestExtractLooseFieldValueWithMarker_UnicodePrefixPreservesAlignment(t *testing.T) {
	raw := "İ mode note\nConfidence: 35%\nReason: menunggu konfirmasi."

	confidence, ok := extractLooseFieldValue(raw, "confidence")
	require.True(t, ok)
	assert.Equal(t, "35%", confidence)

	reason, ok := extractLooseFieldValue(raw, "reason")
	require.True(t, ok)
	assert.Equal(t, "menunggu konfirmasi.", reason)
}

func TestSanitizeDecisionReasoning_UTF8SafeClamping(t *testing.T) {
	input := "  αβγδεζηθικ λμνξοπρσ\t🚀🚀🚀\n" + strings.Repeat("好", 40)

	reasoning := sanitizeDecisionReasoning(input, 18)

	assert.True(t, utf8.ValidString(reasoning))
	assert.NotContains(t, reasoning, "\n")
	assert.NotContains(t, reasoning, "  ")
	assert.Equal(t, 18, len([]rune(reasoning)))
	assert.True(t, strings.HasSuffix(reasoning, "..."))
}

func TestAIScalpingService_BuildUserPrompt_UsesEffectiveThresholdsOnly(t *testing.T) {
	svc := &AIScalpingService{config: AIScalpingConfig{Leverage: 5}}
	prompt := svc.buildUserPrompt(context.Background(), []aiMarketSignal{{
		Symbol:             "PEPE/USDT",
		Price:              1,
		BidAskSpread:       0.04,
		OrderBookImbalance: -0.39,
		RangePosition24h:   27,
	}}, TradingPortfolio{
		USDTBalance:                    50,
		TotalValue:                     50,
		AccountTier:                    "micro",
		StrategyPhase:                  "bootstrap",
		PhaseMinConfidence:             0.75,
		PhaseMaxCapitalPct:             1.0,
		EffectiveMinConfidence:         0.65,
		EffectiveMaxCapitalPct:         12.00,
		MinExecutableSizePct:           12.00,
		MinExecutableNotionalUSDT:      decimalPointer("6.00"),
		MinExecutableInitialMarginUSDT: decimalPointer("1.20"),
	})

	assert.Contains(t, prompt, "Effective Min Confidence (must obey): 0.65")
	assert.Contains(t, prompt, "Effective Max Capital % (must obey): 12.00")
	assert.Contains(t, prompt, "Wallet Basis For size_pct: 50.00")
	assert.Contains(t, prompt, "Executable Size Band % (must obey if action != hold): 12.0000 - 12.0000")
	assert.Contains(t, prompt, "Exchange Minimum Futures Notional: 6.00 USDT")
	assert.Contains(t, prompt, "Estimated Initial Margin @ 5x: 1.20 USDT")
	assert.Contains(t, prompt, "do not multiply size_pct by leverage")
	assert.Contains(t, prompt, "Policy note: account-tier and recovery adjustments are already reflected")
	assert.Contains(t, prompt, "use them only when present in the JSON for that exact symbol")
	assert.Contains(t, prompt, "never let them override buy safety gates")
	assert.Contains(t, prompt, "recent_price_change_pct=0.0276 is +0.0276%")
	assert.Contains(t, prompt, "price_change_24h_pct=0.04821 is +0.04821%")
	assert.Contains(t, prompt, "only buy when the signal clears the buy safety gates")
	assert.Contains(t, prompt, "Historical performance is already reflected in those effective thresholds")
	assert.NotContains(t, prompt, "Phase Min Confidence (reference only)")
	assert.NotContains(t, prompt, "Phase Max Capital % (reference only)")
}

func TestAIScalpingService_BuildUserPrompt_PreservesFullSignalUniverse(t *testing.T) {
	svc := &AIScalpingService{config: DefaultAIScalpingConfig()}
	signals := []aiMarketSignal{
		{
			Symbol:             "BTC/USDT",
			Price:              100,
			High24h:            104,
			Low24h:             96,
			Volume24h:          2500000,
			BidAskSpread:       0.02,
			OrderBookImbalance: 0.58,
			PriceChange24h:     1.2,
			RangePosition24h:   18,
		},
		{
			Symbol:             "ETH/USDT",
			Price:              100,
			High24h:            104,
			Low24h:             96,
			Volume24h:          2500000,
			BidAskSpread:       0.02,
			OrderBookImbalance: 0.04,
			PriceChange24h:     0.1,
			RangePosition24h:   50,
		},
		{
			Symbol:             "SOL/USDT",
			Price:              100,
			High24h:            104,
			Low24h:             96,
			Volume24h:          2500000,
			BidAskSpread:       0.35,
			OrderBookImbalance: 0.58,
			PriceChange24h:     1.2,
			RangePosition24h:   18,
		},
	}

	prompt := svc.buildUserPrompt(context.Background(), signals, TradingPortfolio{
		EffectiveMinConfidence: 0.55,
		EffectiveMaxCapitalPct: 5,
	})

	assert.Contains(t, prompt, `"symbol": "BTC/USDT"`)
	assert.Contains(t, prompt, `"symbol": "ETH/USDT"`)
	assert.Contains(t, prompt, `"symbol": "SOL/USDT"`)
	assert.Contains(t, prompt, `"suggested_action": "buy"`)
	assert.Contains(t, prompt, `"confidence_hint":`)
}

func TestAIScalpingService_BuildUserPrompt_SurfacesWalletBasisFallback(t *testing.T) {
	svc := &AIScalpingService{config: AIScalpingConfig{Leverage: 5}}
	prompt := svc.buildUserPrompt(context.Background(), nil, TradingPortfolio{
		USDTBalance:            0,
		TotalValue:             46.93,
		AccountTier:            "micro",
		StrategyPhase:          "bootstrap",
		EffectiveMinConfidence: 0.65,
		EffectiveMaxCapitalPct: 12.78,
	})

	assert.Contains(t, prompt, "Wallet Basis For size_pct: 46.93")
}

func TestAIScalpingService_BuildUserPrompt_UsesDecimalBackedDisplayedBalances(t *testing.T) {
	svc := &AIScalpingService{config: AIScalpingConfig{Leverage: 5}}
	prompt := svc.buildUserPrompt(context.Background(), nil, TradingPortfolio{
		USDTBalanceDecimal:     decimal.RequireFromString("46.93"),
		TotalValueDecimal:      decimal.RequireFromString("48.11"),
		AccountTier:            "micro",
		StrategyPhase:          "bootstrap",
		EffectiveMinConfidence: 0.65,
		EffectiveMaxCapitalPct: 12.78,
	})

	assert.Contains(t, prompt, "USDT Balance: 46.93")
	assert.Contains(t, prompt, "Total Value: 48.11")
	assert.Contains(t, prompt, "Wallet Basis For size_pct: 46.93")
}

func TestAIScalpingService_BuildSystemPrompt_AllowsFractionalSizePct(t *testing.T) {
	svc := &AIScalpingService{config: AIScalpingConfig{Leverage: 5}}

	prompt := svc.buildSystemPrompt()

	assert.Contains(t, prompt, `"size_pct": 0.01-100`)
	assert.Contains(t, prompt, "size_pct is a direct percentage of wallet value converted into order notional")
}

func TestAIScalpingService_BuildSystemPrompt_GuardsSpreadThresholdReasoning(t *testing.T) {
	svc := &AIScalpingService{config: AIScalpingConfig{Leverage: 5, MaxBidAskSpreadPct: 0.22}}

	prompt := svc.buildSystemPrompt()

	assert.Contains(t, prompt, "compare spread_pct directly to the liquidity ceiling")
	assert.Contains(t, prompt, "never call a spread at or below the ceiling too wide")
	assert.Contains(t, prompt, "spread <= 0.2200%")
}

func TestAIScalpingService_BuildSystemPrompt_ClarifiesPercentPointSignalUnits(t *testing.T) {
	svc := &AIScalpingService{config: AIScalpingConfig{
		Leverage: 5,
		DeterministicFallback: DeterministicFallbackConfig{
			BuyMinPriceChangePct: 0.08,
		},
	}}

	prompt := svc.buildSystemPrompt()

	assert.Contains(t, prompt, "recent_price_change_pct 0.0276 means +0.0276%, not +2.76%")
	assert.Contains(t, prompt, "price_change_24h_pct 0.04821 means +0.04821%, not +4.821%")
	assert.Contains(t, prompt, "Never invent suggested_action, confidence_hint, or candidate_score")
	assert.Contains(t, prompt, "never use them to override buy safety gates")
	assert.Contains(t, prompt, "recent_price_change_pct is short-window momentum in percentage points")
	assert.Contains(t, prompt, "values below 0.0800 are below the buy momentum confirmation gate")
	assert.Contains(t, prompt, "buy only if recent_price_change_pct >= 0.0800")
	assert.Contains(t, prompt, "spread_pct <= 0.0400")
	assert.Contains(t, prompt, "price_change_24h_pct >= 0.0200")
	assert.Contains(t, prompt, "range_pos_24h <= 35.0")
	assert.Contains(t, prompt, "if recent_price_change_pct is absent, buy only at deep-low range_pos_24h <= 20.0")
}

func TestAIScalpingService_BuildSystemPrompt_IncludesBackendSellSafetyGates(t *testing.T) {
	svc := &AIScalpingService{config: AIScalpingConfig{Leverage: 5, MaxBidAskSpreadPct: 0.22}}

	prompt := svc.buildSystemPrompt()

	assert.Contains(t, prompt, "Sell safety gates")
	assert.Contains(t, prompt, "spread_pct <= 0.2200%")
	assert.Contains(t, prompt, "price_change_24h_pct <= -0.0500%")
	assert.Contains(t, prompt, "range_pos_24h > 15.0")
	assert.Contains(t, prompt, "ob_imbalance <= -0.20")
	assert.Contains(t, prompt, "Before returning hold, evaluate every symbol against the sell safety gates")
	assert.Contains(t, prompt, "do not apply the 0.0400% buy spread gate to sell decisions")
	assert.Contains(t, prompt, "choose the strongest sell instead of hold")
	assert.NotContains(t, prompt, "Blowoff reversal sells")
}

func TestAIScalpingService_BuildSystemPrompt_UsesValidatedCandidateConstants(t *testing.T) {
	svc := &AIScalpingService{config: AIScalpingConfig{Leverage: 5, MaxBidAskSpreadPct: 0.04}}

	prompt := svc.buildSystemPrompt()

	assert.Contains(t, prompt, "spread_pct <= 0.0600%")
	assert.Contains(t, prompt, "range_pos_24h <= 20.0")
	assert.Contains(t, prompt, "recent_price_change_pct <= -0.1500%")
	assert.Contains(t, prompt, "price_change_24h_pct <= 0.0000%")
	assert.Contains(t, prompt, "spread_pct <= 0.1000%")
	assert.Contains(t, prompt, "ob_imbalance <= -0.30")
	assert.Contains(t, prompt, "range_pos_24h is between 25.0 and 75.0")
	assert.Contains(t, prompt, "recent_price_change_pct is between -0.6000% and 0.2000%")
	assert.Contains(t, prompt, "price_change_24h_pct is between 0.0000% and 0.5000%")
}

func TestAIScalpingService_EstimateNetExpectancy_PrefersScopedRealizedJournal(t *testing.T) {
	original := globalScalpingPerformance
	globalScalpingPerformance = NewScalpingPerformance()
	t.Cleanup(func() {
		globalScalpingPerformance = original
	})

	db := setupTestDB(t)
	tm, err := NewTradeMemory(db)
	require.NoError(t, err)

	_, err = db.Exec(`CREATE TABLE realized_pnl_journal (
		id TEXT PRIMARY KEY,
		order_id TEXT NOT NULL UNIQUE,
		chat_id TEXT,
		exchange TEXT NOT NULL,
		symbol TEXT NOT NULL,
		side TEXT NOT NULL,
		filled_amount NUMERIC NOT NULL DEFAULT 0,
		entry_price NUMERIC NOT NULL DEFAULT 0,
		exit_price NUMERIC NOT NULL DEFAULT 0,
		realized_pnl NUMERIC NOT NULL DEFAULT 0,
		fees NUMERIC NOT NULL DEFAULT 0,
		source TEXT NOT NULL DEFAULT 'autonomous',
		closed_at TIMESTAMP NOT NULL,
		created_at TIMESTAMP NOT NULL
	)`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO ai_trade_memory (id, timestamp, exchange, symbol, action, outcome, pnl, confidence) VALUES
		('legacy_1', datetime('now'), 'bitget', 'BTC/USDT', 'buy', 'loss', -5, 0.60),
		('legacy_2', datetime('now'), 'bitget', 'BTC/USDT', 'buy', 'loss', -3, 0.55)`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO realized_pnl_journal (
		id, order_id, chat_id, exchange, symbol, side, filled_amount, entry_price, exit_price, realized_pnl, fees, source, closed_at, created_at
	) VALUES
		('rp_1', 'ord_1', 'chat-1', 'bitget', 'BTC/USDT', 'buy', 1, 100, 103, 3, 0, 'autonomous', datetime('now'), datetime('now')),
		('rp_2', 'ord_2', 'chat-1', 'bitget', 'BTC/USDT', 'buy', 1, 100, 99, -1, 0, 'autonomous', datetime('now'), datetime('now')),
		('rp_3', 'ord_3', 'chat-foreign', 'bitget', 'BTC/USDT', 'buy', 1, 100, 110, 10, 0, 'autonomous', datetime('now'), datetime('now'))`)
	require.NoError(t, err)

	svc := &AIScalpingService{
		config:      AIScalpingConfig{MinExpectancyN: 1},
		tradeMemory: tm,
	}
	ctx := WithScalpingAutonomyScope(context.Background(), ScalpingAutonomyScope{
		ChatID:   "chat-1",
		Exchange: "bitget",
	})

	t.Run("action_scoped", func(t *testing.T) {
		expectancy, sample, found := svc.estimateNetExpectancy(ctx, "BTC/USDT", "buy")
		assert.True(t, found)
		assert.Equal(t, 2, sample)
		assert.InDelta(t, 1.0, expectancy, 0.0001)
	})

	t.Run("action_miss_does_not_mix_opposite_side_scoped_results", func(t *testing.T) {
		expectancy, sample, found := svc.estimateNetExpectancy(ctx, "BTC/USDT", "sell")
		assert.False(t, found)
		assert.Zero(t, sample)
		assert.Zero(t, expectancy)
	})
}

func TestAIScalpingService_EstimateNetExpectancy_ScopedSampleBelowMinThresholdDoesNotCountAsUsableEdge(t *testing.T) {
	original := globalScalpingPerformance
	globalScalpingPerformance = NewScalpingPerformance()
	t.Cleanup(func() {
		globalScalpingPerformance = original
	})

	db := setupTestDB(t)
	tm, err := NewTradeMemory(db)
	require.NoError(t, err)

	_, err = db.Exec(`CREATE TABLE realized_pnl_journal (
		id TEXT PRIMARY KEY,
		order_id TEXT NOT NULL UNIQUE,
		chat_id TEXT,
		exchange TEXT NOT NULL,
		symbol TEXT NOT NULL,
		side TEXT NOT NULL,
		filled_amount NUMERIC NOT NULL DEFAULT 0,
		entry_price NUMERIC NOT NULL DEFAULT 0,
		exit_price NUMERIC NOT NULL DEFAULT 0,
		realized_pnl NUMERIC NOT NULL DEFAULT 0,
		fees NUMERIC NOT NULL DEFAULT 0,
		source TEXT NOT NULL DEFAULT 'autonomous',
		closed_at TIMESTAMP NOT NULL,
		created_at TIMESTAMP NOT NULL
	)`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO realized_pnl_journal (
		id, order_id, chat_id, exchange, symbol, side, filled_amount, entry_price, exit_price, realized_pnl, fees, source, closed_at, created_at
	) VALUES
		('rp_1', 'ord_1', 'chat-1', 'bitget', 'BTC/USDT', 'buy', 1, 100, 103, 3, 0, 'autonomous', datetime('now'), datetime('now'))`)
	require.NoError(t, err)

	svc := &AIScalpingService{
		config:      AIScalpingConfig{MinExpectancyN: 2},
		tradeMemory: tm,
	}
	ctx := WithScalpingAutonomyScope(context.Background(), ScalpingAutonomyScope{
		ChatID:   "chat-1",
		Exchange: "bitget",
	})

	expectancy, sample, found := svc.estimateNetExpectancy(ctx, "BTC/USDT", "buy")
	assert.False(t, found)
	assert.Equal(t, 1, sample)
	assert.InDelta(t, 3.0, expectancy, 0.0001)
}

func TestAIScalpingService_EstimateNetExpectancy_ScopedSampleBelowMinThresholdFallsBackToLegacyHistory(t *testing.T) {
	original := globalScalpingPerformance
	globalScalpingPerformance = NewScalpingPerformance()
	t.Cleanup(func() {
		globalScalpingPerformance = original
	})

	db := setupTestDB(t)
	tm, err := NewTradeMemory(db)
	require.NoError(t, err)

	_, err = db.Exec(`CREATE TABLE realized_pnl_journal (
		id TEXT PRIMARY KEY,
		order_id TEXT NOT NULL UNIQUE,
		chat_id TEXT,
		exchange TEXT NOT NULL,
		symbol TEXT NOT NULL,
		side TEXT NOT NULL,
		filled_amount NUMERIC NOT NULL DEFAULT 0,
		entry_price NUMERIC NOT NULL DEFAULT 0,
		exit_price NUMERIC NOT NULL DEFAULT 0,
		realized_pnl NUMERIC NOT NULL DEFAULT 0,
		fees NUMERIC NOT NULL DEFAULT 0,
		source TEXT NOT NULL DEFAULT 'autonomous',
		closed_at TIMESTAMP NOT NULL,
		created_at TIMESTAMP NOT NULL
	)`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO realized_pnl_journal (
		id, order_id, chat_id, exchange, symbol, side, filled_amount, entry_price, exit_price, realized_pnl, fees, source, closed_at, created_at
	) VALUES
		('rp_1', 'ord_1', 'chat-1', 'bitget', 'BTC/USDT', 'buy', 1, 100, 103, 3, 0, 'autonomous', datetime('now'), datetime('now'))`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO ai_trade_memory (id, timestamp, exchange, symbol, action, outcome, pnl, confidence) VALUES
		('legacy_1', datetime('now'), 'bitget', 'BTC/USDT', 'buy', 'win', 2, 0.70),
		('legacy_2', datetime('now'), 'bitget', 'BTC/USDT', 'buy', 'loss', -1, 0.70)`)
	require.NoError(t, err)

	svc := &AIScalpingService{
		config:      AIScalpingConfig{MinExpectancyN: 2},
		tradeMemory: tm,
	}
	ctx := WithScalpingAutonomyScope(context.Background(), ScalpingAutonomyScope{
		ChatID:   "chat-1",
		Exchange: "bitget",
	})

	expectancy, sample, found := svc.estimateNetExpectancy(ctx, "BTC/USDT", "buy")
	assert.True(t, found)
	assert.Equal(t, 2, sample)
	assert.InDelta(t, 0.5, expectancy, 0.0001)
}

func TestAIScalpingService_EstimateNetExpectancy_ScopedJournalUsesFeeAdjustedNetPnL(t *testing.T) {
	original := globalScalpingPerformance
	globalScalpingPerformance = NewScalpingPerformance()
	t.Cleanup(func() {
		globalScalpingPerformance = original
	})

	db := setupTestDB(t)
	tm, err := NewTradeMemory(db)
	require.NoError(t, err)

	_, err = db.Exec(`CREATE TABLE realized_pnl_journal (
		id TEXT PRIMARY KEY,
		order_id TEXT NOT NULL UNIQUE,
		chat_id TEXT,
		exchange TEXT NOT NULL,
		symbol TEXT NOT NULL,
		side TEXT NOT NULL,
		filled_amount NUMERIC NOT NULL DEFAULT 0,
		entry_price NUMERIC NOT NULL DEFAULT 0,
		exit_price NUMERIC NOT NULL DEFAULT 0,
		realized_pnl NUMERIC NOT NULL DEFAULT 0,
		fees NUMERIC NOT NULL DEFAULT 0,
		source TEXT NOT NULL DEFAULT 'autonomous',
		closed_at TIMESTAMP NOT NULL,
		created_at TIMESTAMP NOT NULL
	)`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO realized_pnl_journal (
		id, order_id, chat_id, exchange, symbol, side, filled_amount, entry_price, exit_price, realized_pnl, fees, source, closed_at, created_at
	) VALUES
		('rp_1', 'ord_1', 'chat-1', 'bitget', 'BTC/USDT', 'buy', 1, 100, 101, 1.0, -0.2, 'autonomous', datetime('now'), datetime('now')),
		('rp_2', 'ord_2', 'chat-1', 'bitget', 'BTC/USDT', 'buy', 1, 100, 99, -1.0, -0.2, 'autonomous', datetime('now'), datetime('now'))`)
	require.NoError(t, err)

	svc := &AIScalpingService{
		config:      AIScalpingConfig{MinExpectancyN: 1},
		tradeMemory: tm,
	}
	ctx := WithScalpingAutonomyScope(context.Background(), ScalpingAutonomyScope{
		ChatID:   "chat-1",
		Exchange: "bitget",
	})

	expectancy, sample, found := svc.estimateNetExpectancy(ctx, "BTC/USDT", "buy")
	assert.True(t, found)
	assert.Equal(t, 2, sample)
	assert.InDelta(t, -0.2, expectancy, 0.0001)
}

func TestAIScalpingService_EstimateNetExpectancy_ScopedQueryErrorFallsBackToLegacyHistory(t *testing.T) {
	original := globalScalpingPerformance
	globalScalpingPerformance = NewScalpingPerformance()
	t.Cleanup(func() {
		globalScalpingPerformance = original
	})

	db := setupTestDB(t)
	tm, err := NewTradeMemory(db)
	require.NoError(t, err)

	_, err = db.Exec(`CREATE TABLE realized_pnl_journal (
		closed_at TIMESTAMP NOT NULL,
		side TEXT NOT NULL
	)`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO ai_trade_memory (id, timestamp, exchange, symbol, action, outcome, pnl, confidence) VALUES
		('legacy_1', datetime('now'), 'bitget', 'BTC/USDT', 'buy', 'win', 2, 0.70),
		('legacy_2', datetime('now'), 'bitget', 'BTC/USDT', 'buy', 'loss', -1, 0.70)`)
	require.NoError(t, err)

	svc := &AIScalpingService{
		config:      AIScalpingConfig{MinExpectancyN: 2},
		tradeMemory: tm,
	}
	ctx := WithScalpingAutonomyScope(context.Background(), ScalpingAutonomyScope{
		ChatID:   "chat-1",
		Exchange: "bitget",
	})

	expectancy, sample, found := svc.estimateNetExpectancy(ctx, "BTC/USDT", "buy")
	assert.True(t, found)
	assert.Equal(t, 2, sample)
	assert.InDelta(t, 0.5, expectancy, 0.0001)
}

func TestAIScalpingService_EstimateNetExpectancy_BreakevenScopedHistoryDoesNotFallbackToLegacy(t *testing.T) {
	original := globalScalpingPerformance
	globalScalpingPerformance = NewScalpingPerformance()
	t.Cleanup(func() {
		globalScalpingPerformance = original
	})

	db := setupTestDB(t)
	tm, err := NewTradeMemory(db)
	require.NoError(t, err)

	setupRealizedPnLJournal(t, db)

	_, err = db.Exec(`INSERT INTO realized_pnl_journal (
		id, order_id, chat_id, exchange, symbol, side, filled_amount, entry_price, exit_price, realized_pnl, fees, source, closed_at, created_at
	) VALUES
		('flat_1', 'ord_flat_1', 'chat-1', 'bitget', 'BTC/USDT', 'buy', 1, 100, 100, 0, 0, 'autonomous', datetime('now'), datetime('now'))`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO ai_trade_memory (id, timestamp, exchange, symbol, action, outcome, pnl, confidence) VALUES
		('legacy_1', datetime('now'), 'bitget', 'BTC/USDT', 'buy', 'win', 2, 0.70),
		('legacy_2', datetime('now'), 'bitget', 'BTC/USDT', 'buy', 'loss', -1, 0.70)`)
	require.NoError(t, err)

	svc := &AIScalpingService{
		config:      AIScalpingConfig{MinExpectancyN: 2},
		tradeMemory: tm,
	}
	ctx := WithScalpingAutonomyScope(context.Background(), ScalpingAutonomyScope{
		ChatID:   "chat-1",
		Exchange: "bitget",
	})

	expectancy, sample, found := svc.estimateNetExpectancy(ctx, "BTC/USDT", "buy")
	assert.False(t, found)
	assert.Zero(t, sample)
	assert.Zero(t, expectancy)
}

func TestNormalizeHoldReasonCategory_RuntimeSignals(t *testing.T) {
	category := normalizeHoldReasonCategory(
		reasonCategoryStrategyHold,
		"model response parse fallback (context deadline exceeded)",
	)
	assert.Equal(t, reasonCategoryLLMTimeout, category)

	category = normalizeHoldReasonCategory("", "waiting for qualified setup")
	assert.Equal(t, reasonCategoryStrategyHold, category)

	category = normalizeHoldReasonCategory("", "Model output was incomplete and indecisive, ending with 'I'm torn' without committing to a trade.")
	assert.Equal(t, reasonCategoryLLMParseContract, category)
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

	require.NoError(t, err)
	require.NotNil(t, decision)
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
			PriceChange24h:     -0.8,
		},
	}, TradingPortfolio{})

	require.NoError(t, err)
	require.NotNil(t, decision)
	assert.Equal(t, "sell", decision.Action)
	assert.Equal(t, "ETH/USDT", decision.Symbol)
	assert.Equal(t, reasonCategoryDeterministicFallback, decision.ReasonCategory)
	assert.True(t, decision.ConfidenceKnown)
	assert.Equal(t, 2, mockLLM.CallCount)
}

func TestAIScalpingService_GetAIDecision_DefaultsActionableLLMDecisionToEntryCategory(t *testing.T) {
	mockLLM := &MockLLMClient{
		Responses: []*llm.CompletionResponse{
			{
				Provider: llm.Provider("deepseek"),
				Model:    "deepseek-chat",
				Message:  llm.Message{Content: `{"action":"buy","symbol":"BTC/USDT","size_pct":5,"confidence":0.7,"reasoning":"Order book pressure supports a small entry.","stop_loss":98,"take_profit":104}`},
			},
		},
	}
	svc := &AIScalpingService{
		config: AIScalpingConfig{
			Model:         "deepseek-chat",
			MaxTokens:     1200,
			MinConfidence: 0.55,
			MaxCapitalPct: 5,
		},
		llmClient: mockLLM,
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

	require.NoError(t, err)
	require.NotNil(t, decision)
	assert.Equal(t, "buy", decision.Action)
	assert.Equal(t, "BTC/USDT", decision.Symbol)
	assert.Equal(t, reasonCategoryStrategyEntry, decision.ReasonCategory)
	assert.NotEqual(t, reasonCategoryStrategyHold, decision.ReasonCategory)
	assert.True(t, decision.ConfidenceKnown)

	diagnostics := svc.RuntimeDiagnostics()
	assert.Equal(t, reasonCategoryStrategyEntry, diagnostics["last_reason_category"])
	assert.Equal(t, "deepseek", diagnostics["last_successful_provider"])
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

func TestAIScalpingService_DynamicRiskThresholds_FloorsToExecutableMinimum(t *testing.T) {
	svc := &AIScalpingService{
		config: AIScalpingConfig{
			Exchange:      "bitget",
			Leverage:      5,
			MinConfidence: 0.65,
			MaxCapitalPct: 5.00,
		},
	}

	minConf, maxCap := svc.dynamicRiskThresholds(context.Background(), TradingPortfolio{
		USDTBalance: 46.93,
		TotalValue:  46.93,
	})
	sizing := appautonomy.ResolveExecutableSizingConstraints("bitget", decimal.NewFromFloat(46.93), 5)

	assert.InDelta(t, sizing.MinExecutableSizePct, maxCap, 0.01)
	assert.GreaterOrEqual(t, minConf, 0.65)
}

func TestAIScalpingService_DynamicRiskThresholds_BlockNonExecutableWallet(t *testing.T) {
	svc := &AIScalpingService{
		config: AIScalpingConfig{
			Exchange:      "bitget",
			Leverage:      5,
			MinConfidence: 0.65,
			MaxCapitalPct: 5.00,
		},
	}
	sizing := appautonomy.ResolveExecutableSizingConstraints("bitget", decimal.NewFromFloat(5), 5)

	minConf, maxCap := svc.dynamicRiskThresholds(context.Background(), TradingPortfolio{
		USDTBalance:                    5,
		TotalValue:                     5,
		NonExecutableDueToWallet:       true,
		MinExecutableSizePct:           0,
		MinExecutableNotionalUSDT:      positiveDecimalPointer(sizing.MinOrderNotional),
		MinExecutableInitialMarginUSDT: positiveDecimalPointer(sizing.MinInitialMargin),
	})

	assert.Zero(t, maxCap)
	assert.GreaterOrEqual(t, minConf, 0.65)
}

func TestAIScalpingService_ExecuteDecision_BumpsSizeToExecutableMinimum(t *testing.T) {
	orderExecutor := new(MockScalpingOrderExecutor)
	svc := &AIScalpingService{
		config: AIScalpingConfig{
			Exchange:      "bitget",
			Leverage:      5,
			MaxCapitalPct: 5.00,
		},
		orderExecutor: orderExecutor,
		symbolGuards:  make(map[string]symbolExecutionGuard),
	}

	sizing := appautonomy.ResolveExecutableSizingConstraints("bitget", decimal.NewFromFloat(46.93), 5)
	portfolio := TradingPortfolio{
		USDTBalance:               46.93,
		MinExecutableSizePct:      sizing.MinExecutableSizePct,
		MinExecutableNotionalUSDT: positiveDecimalPointer(sizing.MinOrderNotional),
	}
	decision := &AITradingDecision{
		Action:      "buy",
		Symbol:      "BTC/USDT",
		SizePercent: 1.0,
		Confidence:  0.82,
		Reasoning:   "execution floor regression check",
	}

	orderExecutor.On("GetOpenOrders", mock.Anything, "bitget", "BTC/USDT").
		Return([]map[string]interface{}{}, nil).Once()
	orderExecutor.On("IsPaperTrading").Return(false).Once()
	orderExecutor.On("PlaceOrderWithDetails", mock.Anything, mock.MatchedBy(func(details TradeDetails) bool {
		return details.MarketType == "futures" &&
			details.WalletPercent >= sizing.MinExecutableSizePct-0.01 &&
			details.AmountUSDT.Equal(appautonomy.BitgetFuturesMinNotional())
	})).Return("order-123", nil).Once()

	err := svc.executeDecision(context.Background(), decision, portfolio, 5.0)

	require.NoError(t, err)
	assert.InDelta(t, sizing.MinExecutableSizePct, decision.SizePercent, 0.01)
	orderExecutor.AssertExpectations(t)
}

func TestAIScalpingService_ExecuteDecision_UsesWalletBasisFallback(t *testing.T) {
	orderExecutor := new(MockScalpingOrderExecutor)
	svc := &AIScalpingService{
		config: AIScalpingConfig{
			Exchange: "binance",
		},
		orderExecutor: orderExecutor,
		symbolGuards:  make(map[string]symbolExecutionGuard),
	}

	decision := &AITradingDecision{
		Action:      "buy",
		Symbol:      "BTC/USDT",
		SizePercent: 10.0,
		Confidence:  0.80,
		Reasoning:   "wallet basis fallback",
	}
	portfolio := TradingPortfolio{
		USDTBalance: 0,
		TotalValue:  50,
	}

	orderExecutor.On("GetOpenOrders", mock.Anything, "binance", "BTC/USDT").
		Return([]map[string]interface{}{}, nil).Once()
	orderExecutor.On("IsPaperTrading").Return(false).Once()
	orderExecutor.On("PlaceOrderWithDetails", mock.Anything, mock.MatchedBy(func(details TradeDetails) bool {
		return details.AmountUSDT.Equal(decimal.NewFromFloat(5)) && details.WalletPercent == 10.0
	})).Return("order-wallet-basis", nil).Once()

	err := svc.executeDecision(context.Background(), decision, portfolio, 15.0)

	require.NoError(t, err)
	orderExecutor.AssertExpectations(t)
}

func TestAIScalpingService_ExecuteTradingCycle_HoldsWhenWalletBelowExchangeMinimum(t *testing.T) {
	svc := &AIScalpingService{
		config: AIScalpingConfig{
			Exchange: "bitget",
			Leverage: 5,
			Timeout:  5 * time.Second,
		},
		symbolGuards: make(map[string]symbolExecutionGuard),
	}

	decision, err := svc.ExecuteTradingCycle(context.Background(), TradingPortfolio{
		USDTBalance: 5,
		TotalValue:  5,
	})

	require.NoError(t, err)
	require.NotNil(t, decision)
	assert.Equal(t, "hold", decision.Action)
	assert.Contains(t, decision.Reasoning, "below exchange minimum notional 6.00 USDT")
	if assert.NotNil(t, decision.ExecutionGate) {
		assert.False(t, decision.ExecutionGate.Allowed)
	}
	assert.NotEmpty(t, decision.AccountTier)
	assert.Greater(t, decision.EffectiveMinConfidence, 0.0)
	assert.Zero(t, decision.EffectiveMaxCapitalPct)
}

func TestAIScalpingService_ExecuteTradingCycle_HoldsWhenWalletBasisIsZero(t *testing.T) {
	svc := &AIScalpingService{
		config: AIScalpingConfig{
			Exchange: "bitget",
			Leverage: 5,
			Timeout:  5 * time.Second,
		},
		symbolGuards: make(map[string]symbolExecutionGuard),
	}

	decision, err := svc.ExecuteTradingCycle(context.Background(), TradingPortfolio{})

	require.NoError(t, err)
	require.NotNil(t, decision)
	assert.Equal(t, "hold", decision.Action)
	assert.Contains(t, decision.Reasoning, "wallet basis is zero")
	if assert.NotNil(t, decision.ExecutionGate) {
		assert.False(t, decision.ExecutionGate.Allowed)
	}
	assert.NotEmpty(t, decision.AccountTier)
	assert.Greater(t, decision.EffectiveMinConfidence, 0.0)
	assert.Zero(t, decision.EffectiveMaxCapitalPct)
}

func TestValidateRecoveredDecisionContract_AllowsCompactSymbols(t *testing.T) {
	err := validateRecoveredDecisionContract(&AITradingDecision{
		Action:      "buy",
		Symbol:      "BTCUSDT",
		SizePercent: 5,
		Confidence:  0.8,
	})
	require.NoError(t, err)
}

func TestAIScalpingService_ExecuteTradingCycle_PromotesGenericHoldToFallbackWhenViableCandidatesExist(t *testing.T) {
	mockLLM := &MockLLMClient{
		Responses: []*llm.CompletionResponse{{
			Message: llm.Message{Content: `{"action":"hold","symbol":"","size_pct":0,"confidence":0,"reasoning":""}`},
		}},
	}
	mockCCXT := &mockAIScalpingCCXT{
		markets: &ccxt.MarketsResponse{Exchange: "binance", Symbols: []string{"DOGE/USDT"}, Count: 1},
		marketData: []ccxt.MarketPriceInterface{
			mockMarketPrice{symbol: "DOGE/USDT", price: 0.09120, volume: 1_000_000, high24h: 0.095, low24h: 0.091, bid: 0.09119, ask: 0.09121, exchange: "binance"},
		},
		orderBooks: map[string]*ccxt.OrderBookResponse{
			"DOGE/USDT": {
				Exchange: "binance",
				Symbol:   "DOGE/USDT",
				OrderBook: ccxt.OrderBook{
					Bids: []ccxt.OrderBookEntry{{Price: decimal.NewFromFloat(0.09119), Amount: decimal.NewFromFloat(1500)}},
					Asks: []ccxt.OrderBookEntry{{Price: decimal.NewFromFloat(0.09121), Amount: decimal.NewFromFloat(400)}},
				},
			},
		},
	}

	svc := &AIScalpingService{
		config: AIScalpingConfig{
			Exchange:           "binance",
			Model:              "glm-5",
			MaxCapitalPct:      5.0,
			MinConfidence:      0.65,
			Timeout:            5 * time.Second,
			AutoExecute:        false,
			PreTradeGate:       true,
			MaxPairsToAnalyze:  1,
			MaxCandidatePairs:  1,
			OrderBookPairs:     1,
			MaxBidAskSpreadPct: 0.22,
			EnforceFutures:     false,
		},
		llmClient:    mockLLM,
		ccxtService:  mockCCXT,
		symbolGuards: make(map[string]symbolExecutionGuard),
	}

	decision, err := svc.ExecuteTradingCycle(context.Background(), TradingPortfolio{USDTBalance: 1000, TotalValue: 1000})
	require.NoError(t, err)
	require.NotNil(t, decision)
	assert.Equal(t, "buy", decision.Action)
	assert.Equal(t, "DOGE/USDT", decision.Symbol)
	assert.Equal(t, reasonCategoryDeterministicFallback, decision.ReasonCategory)
	assert.True(t, decision.CandidateFunnelKnown)
	assert.Equal(t, 1, decision.CandidateFunnel.CandidateViableCount)
	diagnostics := svc.RuntimeDiagnostics()
	assert.Equal(t, 1, diagnostics["meta_hold_promotions"])
}

func TestShouldPromoteGenericHoldToFallback_PlaceholderReasoning(t *testing.T) {
	assert.True(t, shouldPromoteGenericHoldToFallback(&AITradingDecision{Action: "hold", Reasoning: "brief explanation"}, appautonomy.CandidateFunnelSnapshot{CandidateViableCount: 1}))
	assert.True(t, shouldPromoteGenericHoldToFallback(&AITradingDecision{Action: "hold", Reasoning: "The user wants me to normalize this failed trading decision output into strict JSON format.", ReasonCategory: reasonCategoryLLMParseContract}, appautonomy.CandidateFunnelSnapshot{CandidateViableCount: 1}))
	assert.True(t, shouldPromoteGenericHoldToFallback(&AITradingDecision{Action: "hold", Reasoning: "Let me analyze the market signals and make a trading decision. ## Key Constraints to Follow"}, appautonomy.CandidateFunnelSnapshot{CandidateViableCount: 1}))
	assert.True(t, shouldPromoteGenericHoldToFallback(&AITradingDecision{Action: "hold", Reasoning: "Let me analyze the market signals and make a trading decision."}, appautonomy.CandidateFunnelSnapshot{CandidateViableCount: 0}))
	assert.True(t, shouldPromoteGenericHoldToFallback(&AITradingDecision{Action: "hold", Reasoning: "The"}, appautonomy.CandidateFunnelSnapshot{}))
	assert.True(t, shouldPromoteGenericHoldToFallback(&AITradingDecision{Action: "hold", Reasoning: "Incomplete analysis - output was cut off before reaching a final trading decision"}, appautonomy.CandidateFunnelSnapshot{}))
	assert.True(t, shouldPromoteGenericHoldToFallback(&AITradingDecision{Action: "hold", Reasoning: "The response is incomplete meta-commentary about parsing a trading decision with no explicit final trade action specified."}, appautonomy.CandidateFunnelSnapshot{}))
	assert.True(t, shouldPromoteGenericHoldToFallback(&AITradingDecision{Action: "hold", Reasoning: "Model output was incomplete and indecisive, ending with I'm torn without committing to a trade."}, appautonomy.CandidateFunnelSnapshot{CandidateViableCount: 1}))
	assert.False(t, shouldPromoteGenericHoldToFallback(&AITradingDecision{Action: "hold", Reasoning: "waiting for clearer setup"}, appautonomy.CandidateFunnelSnapshot{CandidateViableCount: 1}))
}

func TestShouldApplyMicroConfidenceGrace(t *testing.T) {
	policy := appautonomy.ScalpingCyclePolicy{AccountTier: appautonomy.AccountTierMicro}
	funnel := appautonomy.CandidateFunnelSnapshot{CandidateViableCount: 2}

	assert.True(t, shouldApplyMicroConfidenceGrace(policy, funnel, &AITradingDecision{Action: "buy", Confidence: 0.69}, 0.72))
	assert.False(t, shouldApplyMicroConfidenceGrace(policy, funnel, &AITradingDecision{Action: "buy", Confidence: 0.66}, 0.72))
	assert.False(t, shouldApplyMicroConfidenceGrace(appautonomy.ScalpingCyclePolicy{AccountTier: appautonomy.AccountTierSmall}, funnel, &AITradingDecision{Action: "buy", Confidence: 0.69}, 0.72))
	assert.False(t, shouldApplyMicroConfidenceGrace(policy, appautonomy.CandidateFunnelSnapshot{}, &AITradingDecision{Action: "buy", Confidence: 0.69}, 0.72))
}

func TestAIScalpingService_ScalpingCyclePolicy_UsesScopedLossStreakInsteadOfGlobalSingleton(t *testing.T) {
	previous := globalScalpingPerformance
	globalScalpingPerformance = NewScalpingPerformance()
	t.Cleanup(func() {
		globalScalpingPerformance = previous
	})

	for i := 0; i < 3; i++ {
		globalScalpingPerformance.RecordTrade(TradeRecord{
			Timestamp:  time.Now().UTC(),
			PnL:        decimal.NewFromFloat(-1),
			Profitable: false,
		})
	}

	svc := &AIScalpingService{
		config: AIScalpingConfig{
			MinConfidence: 0.65,
			MaxCapitalPct: 5.00,
		},
	}

	baseline := svc.scalpingCyclePolicy(context.Background(), TradingPortfolio{})
	assert.InDelta(t, 0.65, baseline.EffectiveMinConfidence, 0.0001)
	assert.NotContains(t, baseline.PolicyAdjustments, "loss_streak_confidence_tightening")

	scoped := svc.scalpingCyclePolicy(context.Background(), TradingPortfolio{
		RecentConsecutiveLosses: 3,
	})
	assert.Greater(t, scoped.EffectiveMinConfidence, baseline.EffectiveMinConfidence)
	assert.Contains(t, scoped.PolicyAdjustments, "loss_streak_confidence_tightening")
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

func TestAutonomyGateBlockCode_MapsOperatorActionableReasons(t *testing.T) {
	testCases := []struct {
		name     string
		reason   string
		expected string
	}{
		{name: "shadow", reason: "strategy_not_live (stage: shadow, status: active)", expected: appautonomy.CandidateRejectRolloutShadow},
		{name: "paper", reason: "strategy_not_live (stage: paper, status: active)", expected: appautonomy.CandidateRejectRolloutPaper},
		{name: "safe_mode", reason: "safe-mode active after rollback", expected: appautonomy.CandidateRejectSafeMode},
		{name: "kill_switch", reason: "kill-switch engaged by operator", expected: appautonomy.CandidateRejectKillSwitch},
		{name: "connectivity", reason: "exchange connectivity degraded", expected: appautonomy.CandidateRejectConnectivity},
		{name: "risk_budget", reason: "risk budget exhausted", expected: appautonomy.CandidateRejectRiskBudget},
		{name: "generic_gate", reason: "live gate paused by operator", expected: appautonomy.CandidateRejectAutonomyGateClosed},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, autonomyGateBlockCode(tc.reason))
		})
	}
}

func TestShouldAllowPaperModeAutonomyExecution(t *testing.T) {
	paperRollout := &autonomous.RolloutState{
		CurrentStage: autonomous.StagePaper,
		Status:       autonomous.StatusActive,
	}
	paperGate := &autonomous.GateState{
		IsOpen:       false,
		BlockReasons: []string{"strategy_not_live (stage: paper, status: active)"},
		Checks: autonomous.GateChecks{
			SafeModeOff:         true,
			KillSwitchOff:       true,
			RiskBudgetAvailable: true,
			ExchangeConnected:   true,
			StrategyLive:        false,
		},
	}

	assert.True(t, shouldAllowPaperModeAutonomyExecution(
		WithOperationalMode(context.Background(), ModePaper),
		paperRollout,
		paperGate,
	))

	emptyReasonsGate := *paperGate
	emptyReasonsGate.BlockReasons = nil
	assert.True(t, shouldAllowPaperModeAutonomyExecution(
		WithOperationalMode(context.Background(), ModePaper),
		paperRollout,
		&emptyReasonsGate,
	))

	strategyLiveGate := *paperGate
	strategyLiveGate.Checks.StrategyLive = true
	assert.False(t, shouldAllowPaperModeAutonomyExecution(
		WithOperationalMode(context.Background(), ModePaper),
		paperRollout,
		&strategyLiveGate,
	))

	assert.False(t, shouldAllowPaperModeAutonomyExecution(
		WithOperationalMode(context.Background(), OpModeDry),
		paperRollout,
		paperGate,
	))

	connectivityBlocked := *paperGate
	connectivityBlocked.Checks.ExchangeConnected = false
	connectivityBlocked.BlockReasons = []string{
		"strategy_not_live (stage: paper, status: active)",
		"exchange_not_connected",
	}
	assert.False(t, shouldAllowPaperModeAutonomyExecution(
		WithOperationalMode(context.Background(), ModePaper),
		paperRollout,
		&connectivityBlocked,
	))
}

func TestClassifyExecutionBlockCode_MapsRetryableErrors(t *testing.T) {
	testCases := []struct {
		name     string
		err      error
		expected string
	}{
		{name: "missing_orderbook", err: fmt.Errorf("missing orderbook quality signals for ADA/USDT"), expected: appautonomy.CandidateRejectMissingOrderbookSignal},
		{name: "connectivity", err: fmt.Errorf("request failed: timeout while placing order"), expected: appautonomy.CandidateRejectConnectivity},
		{name: "cooldown", err: fmt.Errorf("symbol cooldown active for ADA/USDT"), expected: appautonomy.CandidateRejectRiskBudget},
		{name: "protected_spot_fallback", err: fmt.Errorf("protected spot fallback unavailable for ADAUSDT"), expected: appautonomy.CandidateRejectAutonomyRuntime},
		{name: "fallback_runtime", err: fmt.Errorf("futures-only mode prevented spot fallback"), expected: appautonomy.CandidateRejectAutonomyRuntime},
		{name: "connectivity_with_leverage_word", err: fmt.Errorf("connection reset while checking leverage metadata"), expected: appautonomy.CandidateRejectConnectivity},
		{name: "nil", err: nil, expected: ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, classifyExecutionBlockCode(tc.err))
		})
	}
}

func TestShouldDowngradeExecutionErrorToHold_ProtectedSpotFallback(t *testing.T) {
	err := fmt.Errorf("protected spot fallback unavailable for ADAUSDT")

	assert.True(t, shouldDowngradeExecutionErrorToHold(err))
	assert.Equal(t, reasonCategoryExecutionUnavailable, classifyRuntimeReasoning(err.Error()))
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

	err := svc.validateDecision(context.Background(), decision, nil)
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

func TestAIScalpingService_ReportTradeOutcome_DoesNotRecordGlobalPerformance(t *testing.T) {
	original := globalScalpingPerformance
	globalScalpingPerformance = NewScalpingPerformance()
	t.Cleanup(func() {
		globalScalpingPerformance = original
	})

	svc := &AIScalpingService{
		config: AIScalpingConfig{
			LossWindow: 90 * time.Minute,
		},
		symbolGuards: make(map[string]symbolExecutionGuard),
	}

	svc.ReportTradeOutcome("ADA/USDT", decimal.NewFromFloat(-0.1))

	perf := globalScalpingPerformance.GetPerformance()
	assert.Equal(t, 0, readIntMetric(perf["total_trades"]))
}

func TestAIScalpingService_ShouldApplyPerformanceFeedbackOncePerMilestone(t *testing.T) {
	svc := &AIScalpingService{}

	assert.False(t, svc.shouldApplyPerformanceFeedback(0))
	assert.False(t, svc.shouldApplyPerformanceFeedback(19))
	assert.True(t, svc.shouldApplyPerformanceFeedback(20))
	assert.False(t, svc.shouldApplyPerformanceFeedback(20))
	assert.False(t, svc.shouldApplyPerformanceFeedback(21))
	assert.True(t, svc.shouldApplyPerformanceFeedback(40))
	assert.False(t, svc.shouldApplyPerformanceFeedback(40))
}

func TestAIScalpingService_ApplyPerformanceFeedback_ClampsAndPersistsFallbackConfig(t *testing.T) {
	original := globalScalpingPerformance
	globalScalpingPerformance = NewScalpingPerformance()
	t.Cleanup(func() {
		globalScalpingPerformance = original
	})

	for i := 0; i < scalpingFeedbackIntervalTrades; i++ {
		globalScalpingPerformance.RecordTrade(TradeRecord{
			Timestamp:  time.Now().UTC(),
			PnL:        decimal.NewFromFloat(-1),
			Profitable: false,
		})
	}

	svc := &AIScalpingService{
		config: AIScalpingConfig{
			DeterministicFallback: DeterministicFallbackConfig{
				ConfidenceFloor: 0.72,
				SizeFraction:    0.50,
			},
		},
	}

	svc.ApplyPerformanceFeedback()

	fallbackCfg := svc.config.DeterministicFallback
	assert.InDelta(t, scalpingFeedbackConfidenceMax, fallbackCfg.ConfidenceFloor, 0.0001)
	assert.InDelta(t, 0.125, fallbackCfg.SizeFraction, 0.0001)
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
			BidAskSpread:       0.035,
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

func TestAIScalpingService_ExecuteTradingCycle_AppliesGateAdjustedMaxCapitalToDecision(t *testing.T) {
	mockLLM := &MockLLMClient{
		Responses: []*llm.CompletionResponse{
			{
				Message: llm.Message{
					Content: `{"action":"buy","symbol":"ADA/USDT","size_pct":1.0,"confidence":0.8,"reasoning":"balanced orderbook with room to mean revert","stop_loss":99,"take_profit":102}`,
				},
			},
		},
	}
	mockCCXT := &mockAIScalpingCCXT{
		markets: &ccxt.MarketsResponse{
			Exchange: "binance",
			Symbols:  []string{"ADA/USDT"},
			Count:    1,
		},
		marketData: []ccxt.MarketPriceInterface{
			mockMarketPrice{
				symbol:   "ADA/USDT",
				price:    100,
				volume:   250000,
				high24h:  110,
				low24h:   98,
				bid:      99.97,
				ask:      100.03,
				exchange: "binance",
			},
		},
		orderBooks: map[string]*ccxt.OrderBookResponse{
			"ADA/USDT": {
				Exchange: "binance",
				Symbol:   "ADA/USDT",
				OrderBook: ccxt.OrderBook{
					Symbol: "ADA/USDT",
					Bids: []ccxt.OrderBookEntry{
						{Price: decimal.NewFromFloat(99.97), Amount: decimal.NewFromFloat(5)},
					},
					Asks: []ccxt.OrderBookEntry{
						{Price: decimal.NewFromFloat(100.03), Amount: decimal.NewFromFloat(4.5)},
					},
				},
			},
		},
	}

	svc := &AIScalpingService{
		config: AIScalpingConfig{
			Exchange:          "binance",
			Model:             "glm-5",
			MaxCapitalPct:     5.0,
			MinConfidence:     0.65,
			Timeout:           5 * time.Second,
			AutoExecute:       false,
			PreTradeGate:      true,
			MaxPairsToAnalyze: 1,
			MaxCandidatePairs: 1,
			OrderBookPairs:    1,
			EnforceFutures:    false,
		},
		llmClient:    mockLLM,
		ccxtService:  mockCCXT,
		symbolGuards: make(map[string]symbolExecutionGuard),
	}

	decision, err := svc.ExecuteTradingCycle(context.Background(), TradingPortfolio{
		USDTBalance: 1000,
		TotalValue:  1000,
	})
	require.NoError(t, err)
	require.NotNil(t, decision)

	assert.InDelta(t, 0.65, decision.EffectiveMinConfidence, 0.0001)
	assert.InDelta(t, 3.25, decision.EffectiveMaxCapitalPct, 0.0001)
	assert.True(t, decision.CandidateFunnelKnown)
}

func TestAIScalpingService_DeterministicFallbackCandidate_RejectsIneligibleSignals(t *testing.T) {
	svc := &AIScalpingService{
		config: AIScalpingConfig{
			MaxCapitalPct: 0.5,
		},
	}

	// Each signal is engineered to hit one specific rejection path given the
	// DefaultDeterministicFallbackConfig() knobs (MaxBidAskSpread=0.15,
	// MinImbalance=0.10, BuyRangeMax=45, SellRangeMin=55). Loosening those
	// defaults (PR-2) requires re-tuning these test vectors so the underlying
	// rejection logic is still exercised rather than short-circuited.
	tests := []aiMarketSignal{
		{
			// Path: spread gate — BidAskSpread=0.20 > effectiveMaxSpread=0.15.
			Symbol:             "BTC/USDT",
			Price:              100,
			High24h:            104,
			Low24h:             96,
			Volume24h:          2500000,
			BidAskSpread:       0.20,
			OrderBookImbalance: 0.60,
			RangePosition24h:   20,
		},
		{
			// Path: imbalance gate — |OrderBookImbalance|=0.05 < effectiveMinImbalance=0.10.
			Symbol:             "BTC/USDT",
			Price:              100,
			High24h:            104,
			Low24h:             96,
			Volume24h:          2500000,
			BidAskSpread:       0.03,
			OrderBookImbalance: 0.05,
			RangePosition24h:   20,
		},
		{
			// Path: range/direction mismatch — positive imbalance with
			// RangePosition24h=90 (deep sell zone) matches no action branch.
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
		_, _, ok := svc.deterministicFallbackCandidate(context.Background(), signal, TradingPortfolio{}, false)
		assert.False(t, ok)
	}
}

func TestAIScalpingService_ClassifyScalpingRegime_ChopConfidenceFromConfig(t *testing.T) {
	// Imbalance < 0.10 should land in the chop branch, and the returned
	// confidence must match s.config.RegimeChopConfidence. Verifies the new
	// NEURATRADE_SCALPING_REGIME_CHOP_CONFIDENCE wiring (PR-2) and the
	// default fallback when the config is mis-set.
	signal := aiMarketSignal{
		Symbol:             "BTC/USDT",
		Price:              100,
		BidAskSpread:       0.05,
		OrderBookImbalance: 0.05, // < 0.10 → chop branch
		RangePosition24h:   50,
	}

	tests := []struct {
		name     string
		config   AIScalpingConfig
		wantConf float64
	}{
		{
			name:     "explicit 0.42 propagated through",
			config:   AIScalpingConfig{RegimeChopConfidence: 0.42, MaxBidAskSpreadPct: 0.5},
			wantConf: 0.42,
		},
		{
			name:     "default 0.65 used when unset",
			config:   AIScalpingConfig{RegimeChopConfidence: 0, MaxBidAskSpreadPct: 0.5},
			wantConf: 0.65,
		},
		{
			name:     "out-of-range value clamped to default",
			config:   AIScalpingConfig{RegimeChopConfidence: 1.5, MaxBidAskSpreadPct: 0.5},
			wantConf: 0.65,
		},
		{
			// NaN must not propagate as a regime confidence. The guard
			// !(x > 0 && x <= 1) catches NaN because NaN comparisons return
			// false; the more obvious x <= 0 || x > 1 would silently let
			// NaN through. Regression guard for the env-resolver path where
			// strconv.ParseFloat accepts the literal "NaN".
			name:     "NaN value falls back to default",
			config:   AIScalpingConfig{RegimeChopConfidence: math.NaN(), MaxBidAskSpreadPct: 0.5},
			wantConf: 0.65,
		},
		{
			name:     "negative value falls back to default",
			config:   AIScalpingConfig{RegimeChopConfidence: -0.1, MaxBidAskSpreadPct: 0.5},
			wantConf: 0.65,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &AIScalpingService{config: tt.config}
			regime, conf, blockReason := svc.classifyScalpingRegime(signal, "buy")
			assert.Equal(t, "chop", regime)
			assert.InDelta(t, tt.wantConf, conf, 1e-9)
			assert.Empty(t, blockReason)
		})
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
	_, _, ok := svc.deterministicFallbackCandidate(context.Background(), lowConfidenceSignal, TradingPortfolio{}, false)
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
	decision, _, ok := svc.deterministicFallbackCandidate(context.Background(), eligibleSignal, TradingPortfolio{
		PhaseMaxCapitalPct: 0.25,
	}, false)
	assert.True(t, ok)
	assert.NotNil(t, decision)
	if decision != nil {
		assert.LessOrEqual(t, decision.SizePercent, 0.25)
	}
}

func TestAIScalpingService_SignalsWithDecisionHintsAnnotatesEligibleCandidates(t *testing.T) {
	svc := &AIScalpingService{config: DefaultAIScalpingConfig()}
	signals := []aiMarketSignal{
		{
			Symbol:             "BTC/USDT",
			Price:              100,
			High24h:            104,
			Low24h:             96,
			Volume24h:          2500000,
			BidAskSpread:       0.02,
			OrderBookImbalance: 0.58,
			PriceChange24h:     1.2,
			RangePosition24h:   18,
		},
		{
			Symbol:             "ETH/USDT",
			Price:              100,
			High24h:            104,
			Low24h:             96,
			Volume24h:          2500000,
			BidAskSpread:       0.35,
			OrderBookImbalance: 0.58,
			PriceChange24h:     1.2,
			RangePosition24h:   18,
		},
	}

	enriched := svc.signalsWithDecisionHints(context.Background(), signals, TradingPortfolio{
		EffectiveMinConfidence: 0.55,
		EffectiveMaxCapitalPct: 5,
	})

	require.Len(t, enriched, 2)
	assert.Equal(t, "buy", enriched[0].SuggestedAction)
	assert.GreaterOrEqual(t, enriched[0].ConfidenceHint, 0.55)
	assert.Greater(t, enriched[0].CandidateScore, 0.0)
	assert.Empty(t, enriched[1].SuggestedAction)
	assert.Zero(t, signals[0].ConfidenceHint, "input signals should not be mutated")
}

func TestAIScalpingService_SignalsWithDecisionHintsKeepsDecisionReadySignals(t *testing.T) {
	svc := &AIScalpingService{config: DefaultAIScalpingConfig()}
	signals := []aiMarketSignal{
		{
			Symbol:             "BTC/USDT",
			Price:              100,
			High24h:            104,
			Low24h:             96,
			Volume24h:          2500000,
			BidAskSpread:       0.02,
			OrderBookImbalance: 0.58,
			PriceChange24h:     1.2,
			RangePosition24h:   18,
		},
		{
			Symbol:             "ETH/USDT",
			Price:              100,
			High24h:            104,
			Low24h:             96,
			Volume24h:          2500000,
			BidAskSpread:       0.02,
			OrderBookImbalance: 0.04,
			PriceChange24h:     0.1,
			RangePosition24h:   50,
		},
		{
			Symbol:             "SOL/USDT",
			Price:              100,
			High24h:            104,
			Low24h:             96,
			Volume24h:          2500000,
			BidAskSpread:       0.35,
			OrderBookImbalance: 0.58,
			PriceChange24h:     1.2,
			RangePosition24h:   18,
		},
	}

	enriched := svc.signalsWithDecisionHints(context.Background(), signals, TradingPortfolio{
		EffectiveMinConfidence: 0.55,
		EffectiveMaxCapitalPct: 5,
	})

	require.Len(t, enriched, 3)
	assert.Equal(t, "BTC/USDT", enriched[0].Symbol)
	assert.Equal(t, "buy", enriched[0].SuggestedAction)
	assert.GreaterOrEqual(t, enriched[0].ConfidenceHint, 0.55)
	assert.Zero(t, enriched[1].SuggestedAction)
	assert.Zero(t, enriched[2].SuggestedAction)
}

func TestAIScalpingService_SignalsWithDecisionHintsUsesEffectiveConfidenceThreshold(t *testing.T) {
	svc := &AIScalpingService{config: DefaultAIScalpingConfig()}
	signal := aiMarketSignal{
		Symbol:             "BTC/USDT",
		Price:              100,
		High24h:            104,
		Low24h:             96,
		Volume24h:          1,
		BidAskSpread:       0.08,
		OrderBookImbalance: 0.35,
		PriceChange24h:     0,
		RangePosition24h:   18,
	}
	signals := []aiMarketSignal{
		signal,
		{
			Symbol:             "ETH/USDT",
			Price:              100,
			High24h:            104,
			Low24h:             96,
			Volume24h:          2500000,
			BidAskSpread:       0.35,
			OrderBookImbalance: 0.58,
			PriceChange24h:     1.2,
			RangePosition24h:   18,
		},
	}
	portfolio := TradingPortfolio{
		EffectiveMinConfidence: 0.60,
		EffectiveMaxCapitalPct: 5,
	}

	enriched := svc.signalsWithDecisionHints(context.Background(), signals, portfolio)
	_, _, eligibleWithDefaultFloor := svc.deterministicFallbackCandidate(context.Background(), signal, TradingPortfolio{}, false)

	require.Len(t, enriched, 2)
	require.Equal(t, "buy", enriched[0].SuggestedAction)
	require.GreaterOrEqual(t, enriched[0].ConfidenceHint, portfolio.EffectiveMinConfidence)
	require.Less(t, enriched[0].ConfidenceHint, svc.deterministicFallbackConfig().ConfidenceFloor)
	require.False(t, eligibleWithDefaultFloor, "empty portfolio would incorrectly apply the fallback confidence floor")
}

func TestAIScalpingService_SignalsWithDecisionHintsDoesNotExposeRelaxedCandidates(t *testing.T) {
	svc := &AIScalpingService{
		config: AIScalpingConfig{
			MaxBidAskSpreadPct: 0.22,
			DeterministicFallback: DeterministicFallbackConfig{
				MaxBidAskSpread: 0.08,
				MinImbalance:    0.20,
				BuyRangeMax:     45,
				SellRangeMin:    55,
				RangeAnchor:     55,
				RangeOffset:     45,
				SizeFraction:    0.50,
			},
		},
	}
	signal := aiMarketSignal{
		Symbol:             "BOME/USDT",
		Price:              1,
		High24h:            1.1,
		Low24h:             0.9,
		Volume24h:          1500000,
		BidAskSpread:       0.19,
		OrderBookImbalance: 0.15,
		RangePosition24h:   24,
	}
	portfolio := TradingPortfolio{EffectiveMinConfidence: 0.65, EffectiveMaxCapitalPct: 12.7847}

	_, _, strictOK := svc.deterministicFallbackCandidate(context.Background(), signal, portfolio, false)
	_, _, relaxedOK := svc.deterministicFallbackCandidate(context.Background(), signal, portfolio, true)
	enriched := svc.signalsWithDecisionHints(context.Background(), []aiMarketSignal{signal}, portfolio)

	require.False(t, strictOK)
	require.True(t, relaxedOK)
	require.Len(t, enriched, 1)
	assert.Empty(t, enriched[0].SuggestedAction)
	assert.Zero(t, enriched[0].ConfidenceHint)
	assert.Zero(t, enriched[0].CandidateScore)
}

func TestAIScalpingService_SignalsWithDecisionHintsKeepsDiagnosticsWhenNoCandidate(t *testing.T) {
	svc := &AIScalpingService{config: DefaultAIScalpingConfig()}
	signals := []aiMarketSignal{
		{
			Symbol:             "ETH/USDT",
			Price:              100,
			High24h:            104,
			Low24h:             96,
			Volume24h:          2500000,
			BidAskSpread:       0.02,
			OrderBookImbalance: 0.04,
			PriceChange24h:     0.1,
			RangePosition24h:   50,
		},
		{
			Symbol:             "SOL/USDT",
			Price:              100,
			High24h:            104,
			Low24h:             96,
			Volume24h:          2500000,
			BidAskSpread:       0.35,
			OrderBookImbalance: 0.58,
			PriceChange24h:     1.2,
			RangePosition24h:   18,
		},
	}

	enriched := svc.signalsWithDecisionHints(context.Background(), signals, TradingPortfolio{
		EffectiveMinConfidence: 0.55,
		EffectiveMaxCapitalPct: 5,
	})

	require.Len(t, enriched, 2)
	assert.Equal(t, signals, enriched)
}

func TestCandidateSignalsFromMarketSignals_SkipsNonFiniteDecimalInputs(t *testing.T) {
	candidates := candidateSignalsFromMarketSignals([]aiMarketSignal{
		{
			Symbol:    "ADA/USDT",
			Price:     math.NaN(),
			High24h:   110,
			Low24h:    90,
			Volume24h: 1000,
		},
		{
			Symbol:    "SOL/USDT",
			Price:     100,
			High24h:   110,
			Low24h:    90,
			Volume24h: math.Inf(1),
		},
		{
			Symbol:             "BTC/USDT",
			Price:              100,
			High24h:            110,
			Low24h:             90,
			Volume24h:          5000,
			BidAskSpread:       math.NaN(),
			OrderBookImbalance: math.NaN(),
			RangePosition24h:   math.NaN(),
		},
	})

	require.Len(t, candidates, 1)
	assert.Equal(t, "BTC/USDT", candidates[0].Symbol)
}

func TestAIScalpingService_DeterministicFallbackCandidate_UsesConfigOverrides(t *testing.T) {
	svc := &AIScalpingService{
		config: AIScalpingConfig{
			MaxCapitalPct: 5,
			MinConfidence: 0.60,
			DeterministicFallback: DeterministicFallbackConfig{
				MaxBidAskSpread: 0.01,
			},
		},
	}

	signal := aiMarketSignal{
		Symbol:             "BTC/USDT",
		Price:              100,
		High24h:            104,
		Low24h:             96,
		Volume24h:          2500000,
		BidAskSpread:       0.02,
		OrderBookImbalance: 0.58,
		RangePosition24h:   18,
	}

	decision, confidence, ok := svc.deterministicFallbackCandidate(context.Background(), signal, TradingPortfolio{}, false)
	assert.False(t, ok)
	assert.Nil(t, decision)
	assert.Zero(t, confidence)
}

func TestAIScalpingService_DeterministicFallbackCandidate_BypassesImbalanceGateForTickerOnly(t *testing.T) {
	svc := &AIScalpingService{
		config: AIScalpingConfig{
			MaxBidAskSpreadPct:    0.22,
			DeterministicFallback: DefaultDeterministicFallbackConfig(),
		},
	}

	signal := aiMarketSignal{
		Symbol:             "BTC/USDT",
		Price:              50000,
		High24h:            51000,
		Low24h:             49000,
		Volume24h:          1000,
		BidAskSpread:       0.04, // ticker-derived spread is small and within tolerance
		OrderBookImbalance: 0,    // no orderbook data: ticker-only signal
		RangePosition24h:   18,
	}

	decision, confidence, ok := svc.deterministicFallbackCandidate(context.Background(), signal, TradingPortfolio{}, true)
	require.True(t, ok, "ticker-only signal with valid spread should bypass imbalance gate")
	require.NotNil(t, decision)
	assert.Equal(t, "buy", decision.Action)
	assert.Greater(t, confidence, 0.0)
}

func TestAIScalpingService_DeterministicFallbackCandidate_AlignsWithPolicySpreadAndImbalanceFloor(t *testing.T) {
	svc := &AIScalpingService{
		config: AIScalpingConfig{
			MaxBidAskSpreadPct: 0.22,
			DeterministicFallback: DeterministicFallbackConfig{
				MaxBidAskSpread: 0.08,
				MinImbalance:    0.35,
				BuyRangeMax:     45,
				SellRangeMin:    55,
				RangeAnchor:     55,
				RangeOffset:     45,
				SizeFraction:    0.50,
			},
		},
	}

	decision, _, ok := svc.deterministicFallbackCandidate(context.Background(), aiMarketSignal{
		Symbol:             "BOME/USDT",
		Price:              1,
		High24h:            1.1,
		Low24h:             0.9,
		Volume24h:          1500000,
		BidAskSpread:       0.19,
		OrderBookImbalance: 0.28,
		RangePosition24h:   18,
	}, TradingPortfolio{EffectiveMinConfidence: 0.65, EffectiveMaxCapitalPct: 12.7847}, false)

	require.True(t, ok)
	require.NotNil(t, decision)
	assert.Equal(t, "buy", decision.Action)
	assert.Equal(t, reasonCategoryDeterministicFallback, decision.ReasonCategory)
}

func TestAIScalpingService_DeterministicFallbackCandidate_BlocksAgainstNegativeMomentum(t *testing.T) {
	svc := &AIScalpingService{
		config: AIScalpingConfig{
			MaxBidAskSpreadPct: 0.22,
			DeterministicFallback: DeterministicFallbackConfig{
				MaxBidAskSpread: 0.08,
				MinImbalance:    0.20,
				BuyRangeMax:     45,
				SellRangeMin:    55,
				SizeFraction:    0.50,
			},
		},
	}

	decision, _, ok := svc.deterministicFallbackCandidate(context.Background(), aiMarketSignal{
		Symbol:             "DOGE/USDT",
		Price:              1,
		High24h:            1.1,
		Low24h:             0.9,
		Volume24h:          1500000,
		BidAskSpread:       0.035,
		OrderBookImbalance: 0.60,
		PriceChange24h:     -4.0,
		RangePosition24h:   18,
	}, TradingPortfolio{AccountTier: appautonomy.AccountTierMicro, EffectiveMinConfidence: 0.65, EffectiveMaxCapitalPct: 12.0}, false)

	assert.False(t, ok)
	assert.Nil(t, decision)
}

func TestAIScalpingService_DeterministicFallbackCandidate_AllowsMomentumAlignedEntry(t *testing.T) {
	svc := &AIScalpingService{
		config: AIScalpingConfig{
			MaxBidAskSpreadPct: 0.22,
			DeterministicFallback: DeterministicFallbackConfig{
				MaxBidAskSpread: 0.08,
				MinImbalance:    0.20,
				BuyRangeMax:     45,
				SellRangeMin:    55,
				SizeFraction:    0.50,
			},
		},
	}

	decision, _, ok := svc.deterministicFallbackCandidate(context.Background(), aiMarketSignal{
		Symbol:             "DOGE/USDT",
		Price:              1,
		High24h:            1.1,
		Low24h:             0.9,
		Volume24h:          1500000,
		BidAskSpread:       0.035,
		OrderBookImbalance: 0.60,
		PriceChange24h:     0.2,
		RangePosition24h:   18,
	}, TradingPortfolio{AccountTier: appautonomy.AccountTierMicro, EffectiveMinConfidence: 0.65, EffectiveMaxCapitalPct: 12.0}, false)

	require.True(t, ok)
	require.NotNil(t, decision)
	assert.Equal(t, "buy", decision.Action)
	assert.Contains(t, decision.Reasoning, "momentum")
}

func TestAIScalpingService_DeterministicFallbackCandidate_UsesRecentMomentumWhenKnown(t *testing.T) {
	svc := &AIScalpingService{
		config: AIScalpingConfig{
			MaxBidAskSpreadPct: 0.22,
			DeterministicFallback: DeterministicFallbackConfig{
				MaxBidAskSpread: 0.08,
				MinImbalance:    0.20,
				BuyRangeMax:     45,
				SellRangeMin:    55,
				SizeFraction:    0.50,
			},
		},
	}

	decision, _, ok := svc.deterministicFallbackCandidate(context.Background(), aiMarketSignal{
		Symbol:             "DOGE/USDT",
		Price:              1,
		High24h:            1.1,
		Low24h:             0.9,
		Volume24h:          1500000,
		BidAskSpread:       0.035,
		OrderBookImbalance: 0.60,
		PriceChange24h:     0.2,
		RecentPriceChange:  0.12,
		RecentChangeKnown:  true,
		RangePosition24h:   18,
	}, TradingPortfolio{AccountTier: appautonomy.AccountTierMicro, EffectiveMinConfidence: 0.65, EffectiveMaxCapitalPct: 12.0}, false)

	require.True(t, ok)
	require.NotNil(t, decision)
	assert.Equal(t, "buy", decision.Action)
	assert.Contains(t, decision.Reasoning, "momentum 0.120%")
}

func TestAIScalpingService_DeterministicFallbackCandidate_RejectsRecentBuyInDowntrend(t *testing.T) {
	svc := &AIScalpingService{
		config: AIScalpingConfig{
			MaxBidAskSpreadPct: 0.22,
			DeterministicFallback: DeterministicFallbackConfig{
				MaxBidAskSpread: 0.08,
				MinImbalance:    0.20,
				BuyRangeMax:     45,
				SellRangeMin:    55,
				SizeFraction:    0.50,
			},
		},
	}

	decision, _, ok := svc.deterministicFallbackCandidate(context.Background(), aiMarketSignal{
		Symbol:             "BILL/USDT",
		Price:              0.118057,
		High24h:            0.13,
		Low24h:             0.118,
		Volume24h:          1500000,
		BidAskSpread:       0.03388,
		OrderBookImbalance: 0.55583,
		PriceChange24h:     -0.18599,
		RecentPriceChange:  0.08817,
		RecentChangeKnown:  true,
		RangePosition24h:   1.96,
	}, TradingPortfolio{AccountTier: appautonomy.AccountTierMicro, EffectiveMinConfidence: 0.65, EffectiveMaxCapitalPct: 12.0}, false)

	assert.False(t, ok)
	assert.Nil(t, decision)
}

func TestAIScalpingService_DeterministicFallbackCandidate_RejectsRecentBuyWithoutPositiveTrend(t *testing.T) {
	svc := &AIScalpingService{
		config: AIScalpingConfig{
			MaxBidAskSpreadPct: 0.22,
			DeterministicFallback: DeterministicFallbackConfig{
				MaxBidAskSpread: 0.08,
				MinImbalance:    0.20,
				BuyRangeMax:     45,
				SellRangeMin:    55,
				SizeFraction:    0.50,
			},
		},
	}

	decision, _, ok := svc.deterministicFallbackCandidate(context.Background(), aiMarketSignal{
		Symbol:             "DOGE/USDT",
		Price:              0.10404,
		High24h:            0.12,
		Low24h:             0.094,
		Volume24h:          1500000,
		BidAskSpread:       0.00961,
		OrderBookImbalance: 0.81364,
		PriceChange24h:     0,
		RecentPriceChange:  0.16367,
		RecentChangeKnown:  true,
		RangePosition24h:   38.89,
	}, TradingPortfolio{AccountTier: appautonomy.AccountTierMicro, EffectiveMinConfidence: 0.65, EffectiveMaxCapitalPct: 12.0}, false)

	assert.False(t, ok)
	assert.Nil(t, decision)
}

func TestAIScalpingService_DeterministicFallbackCandidate_RejectsRecentBuyNearMaxSpread(t *testing.T) {
	svc := &AIScalpingService{
		config: AIScalpingConfig{
			MaxBidAskSpreadPct: 0.22,
			DeterministicFallback: DeterministicFallbackConfig{
				MaxBidAskSpread: 0.08,
				MinImbalance:    0.20,
				BuyRangeMax:     45,
				SellRangeMin:    55,
				SizeFraction:    0.50,
			},
		},
	}

	decision, _, ok := svc.deterministicFallbackCandidate(context.Background(), aiMarketSignal{
		Symbol:             "PROS/USDT",
		Price:              0.6286,
		High24h:            0.75,
		Low24h:             0.50,
		Volume24h:          1500000,
		BidAskSpread:       0.07954,
		OrderBookImbalance: 0.63331,
		PriceChange24h:     -0.08872,
		RecentPriceChange:  0.12743,
		RecentChangeKnown:  true,
		RangePosition24h:   24.94,
	}, TradingPortfolio{AccountTier: appautonomy.AccountTierMicro, EffectiveMinConfidence: 0.65, EffectiveMaxCapitalPct: 12.0}, false)

	assert.False(t, ok)
	assert.Nil(t, decision)
}

func TestAIScalpingService_DeterministicFallbackCandidate_RejectsENJRecentBuyLossShape(t *testing.T) {
	svc := &AIScalpingService{
		config: AIScalpingConfig{
			MaxBidAskSpreadPct: 0.22,
			DeterministicFallback: DeterministicFallbackConfig{
				MaxBidAskSpread: 0.08,
				MinImbalance:    0.20,
				BuyRangeMax:     45,
				SellRangeMin:    55,
				SizeFraction:    0.50,
			},
		},
	}

	decision, _, ok := svc.deterministicFallbackCandidate(context.Background(), aiMarketSignal{
		Symbol:             "ENJ/USDT",
		Price:              0.0875,
		High24h:            0.09,
		Low24h:             0.079,
		Volume24h:          1_500_000,
		BidAskSpread:       0.04410,
		OrderBookImbalance: 0.4130,
		PriceChange24h:     3.7757,
		RecentPriceChange:  0.1325,
		RecentChangeKnown:  true,
		RangePosition24h:   22.7160,
	}, TradingPortfolio{AccountTier: appautonomy.AccountTierMicro, EffectiveMinConfidence: 0.65, EffectiveMaxCapitalPct: 12.0}, false)

	require.False(t, ok)
	require.Nil(t, decision)
}

func TestAIScalpingService_AnnotateRecentSignalMomentum(t *testing.T) {
	svc := &AIScalpingService{}
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)

	first := aiMarketSignal{Symbol: "DOGE/USDT", Price: 100}
	svc.annotateRecentSignalMomentum(now, &first)
	require.False(t, first.RecentChangeKnown)

	second := aiMarketSignal{Symbol: "DOGE/USDT", Price: 101}
	svc.annotateRecentSignalMomentum(now.Add(time.Minute), &second)
	require.True(t, second.RecentChangeKnown)
	assert.InDelta(t, 1.0, second.RecentPriceChange, 1e-9)
	assert.InDelta(t, 60.0, second.RecentChangeAgeSec, 1e-9)
}

func TestAIScalpingService_DeterministicFallbackCandidate_AllowsMomentumContinuationSell(t *testing.T) {
	svc := &AIScalpingService{
		config: AIScalpingConfig{
			MaxBidAskSpreadPct: 0.22,
			DeterministicFallback: DeterministicFallbackConfig{
				MaxBidAskSpread: 0.08,
				MinImbalance:    0.20,
				BuyRangeMax:     45,
				SellRangeMin:    55,
				SizeFraction:    0.50,
			},
		},
	}

	decision, _, ok := svc.deterministicFallbackCandidate(context.Background(), aiMarketSignal{
		Symbol:             "WIF/USDT",
		Price:              1,
		High24h:            1.2,
		Low24h:             0.8,
		Volume24h:          1500000,
		BidAskSpread:       0.06,
		OrderBookImbalance: -0.23,
		PriceChange24h:     -0.8,
		RangePosition24h:   50,
	}, TradingPortfolio{AccountTier: appautonomy.AccountTierMicro, EffectiveMinConfidence: 0.65, EffectiveMaxCapitalPct: 12.0}, false)

	require.True(t, ok)
	require.NotNil(t, decision)
	assert.Equal(t, "sell", decision.Action)
	assert.Contains(t, decision.Reasoning, "momentum")
}

func TestAIScalpingService_DeterministicFallbackCandidate_BlocksSellWhenBroadTrendIsPositive(t *testing.T) {
	svc := &AIScalpingService{
		config: AIScalpingConfig{
			MaxBidAskSpreadPct: 0.22,
			DeterministicFallback: DeterministicFallbackConfig{
				MaxBidAskSpread: 0.08,
				MinImbalance:    0.20,
				BuyRangeMax:     45,
				SellRangeMin:    55,
				SizeFraction:    0.50,
			},
		},
	}

	decision, _, ok := svc.deterministicFallbackCandidate(context.Background(), aiMarketSignal{
		Symbol:             "ONDO/USDT",
		Price:              0.3855,
		High24h:            0.40,
		Low24h:             0.30,
		Volume24h:          1_500_000,
		BidAskSpread:       0.05188,
		OrderBookImbalance: -0.45264,
		PriceChange24h:     0.1315,
		RecentPriceChange:  -0.3103,
		RecentChangeKnown:  true,
		RangePosition24h:   96.32,
	}, TradingPortfolio{AccountTier: appautonomy.AccountTierMicro, EffectiveMinConfidence: 0.65, EffectiveMaxCapitalPct: 12.0}, false)

	require.False(t, ok)
	require.Nil(t, decision)
}

func TestAIScalpingService_DeterministicFallbackCandidate_BlocksControlledBreakdownSellAfterObservedLosses(t *testing.T) {
	svc := &AIScalpingService{
		config: AIScalpingConfig{
			MaxBidAskSpreadPct: 0.22,
			DeterministicFallback: DeterministicFallbackConfig{
				MaxBidAskSpread: 0.08,
				MinImbalance:    0.20,
				BuyRangeMax:     45,
				SellRangeMin:    55,
				SizeFraction:    0.50,
			},
		},
	}

	decision, _, ok := svc.deterministicFallbackCandidate(context.Background(), aiMarketSignal{
		Symbol:             "WIF/USDT",
		Price:              1,
		High24h:            1.2,
		Low24h:             0.8,
		Volume24h:          1500000,
		BidAskSpread:       0.06,
		OrderBookImbalance: -0.24,
		PriceChange24h:     -0.8,
		RecentPriceChange:  -0.12,
		RecentChangeKnown:  true,
		RangePosition24h:   24,
	}, TradingPortfolio{AccountTier: appautonomy.AccountTierMicro, EffectiveMinConfidence: 0.65, EffectiveMaxCapitalPct: 12.0}, false)

	require.False(t, ok)
	require.Nil(t, decision)
}

func TestAIScalpingService_DeterministicFallbackCandidate_BlocksRecentMidRangeSellAfterObservedLosses(t *testing.T) {
	svc := &AIScalpingService{
		config: AIScalpingConfig{
			MaxBidAskSpreadPct: 0.22,
			DeterministicFallback: DeterministicFallbackConfig{
				MaxBidAskSpread: 0.08,
				MinImbalance:    0.20,
				BuyRangeMax:     45,
				SellRangeMin:    55,
				SizeFraction:    0.50,
			},
		},
	}

	for _, signal := range []aiMarketSignal{
		{
			Symbol:             "AEVOUSDT",
			Price:              0.02562,
			High24h:            0.03,
			Low24h:             0.022,
			Volume24h:          1_500_000,
			BidAskSpread:       0.03903,
			OrderBookImbalance: -0.6375,
			PriceChange24h:     -1.9517,
			RecentPriceChange:  -0.078,
			RecentChangeKnown:  true,
			RangePosition24h:   45.24,
		},
		{
			Symbol:             "1000LUNCUSDT",
			Price:              0.07704,
			High24h:            0.09,
			Low24h:             0.05,
			Volume24h:          1_500_000,
			BidAskSpread:       0.01298,
			OrderBookImbalance: -0.5356,
			PriceChange24h:     -1.18,
			RecentPriceChange:  -0.0519,
			RecentChangeKnown:  true,
			RangePosition24h:   65.05,
		},
	} {
		decision, _, ok := svc.deterministicFallbackCandidate(context.Background(), signal, TradingPortfolio{AccountTier: appautonomy.AccountTierMicro, EffectiveMinConfidence: 0.65, EffectiveMaxCapitalPct: 12.0}, false)

		require.False(t, ok, signal.Symbol)
		require.Nil(t, decision, signal.Symbol)
	}
}

func TestAIScalpingService_DeterministicFallbackCandidate_AllowsBufferedMidRangeBuy(t *testing.T) {
	svc := &AIScalpingService{
		config: AIScalpingConfig{
			MaxBidAskSpreadPct: 0.22,
			DeterministicFallback: DeterministicFallbackConfig{
				MaxBidAskSpread: 0.08,
				MinImbalance:    0.20,
				BuyRangeMax:     45,
				SellRangeMin:    55,
				SizeFraction:    0.50,
			},
		},
	}

	decision, _, ok := svc.deterministicFallbackCandidate(context.Background(), aiMarketSignal{
		Symbol:             "FARTCOIN/USDT",
		Price:              1,
		High24h:            1.2,
		Low24h:             0.8,
		Volume24h:          1500000,
		BidAskSpread:       0.07,
		OrderBookImbalance: 0.22,
		PriceChange24h:     0.8,
		RangePosition24h:   48,
	}, TradingPortfolio{AccountTier: appautonomy.AccountTierMicro, EffectiveMinConfidence: 0.65, EffectiveMaxCapitalPct: 12.0}, false)

	require.True(t, ok)
	require.NotNil(t, decision)
	assert.Equal(t, "buy", decision.Action)
	assert.Contains(t, decision.Reasoning, "range position")
}

func TestAIScalpingService_DeterministicFallbackCandidate_RejectsObservedPullbackBuy(t *testing.T) {
	svc := &AIScalpingService{
		config: AIScalpingConfig{
			MaxBidAskSpreadPct: 0.22,
			DeterministicFallback: DeterministicFallbackConfig{
				MaxBidAskSpread: 0.08,
				MinImbalance:    0.20,
				BuyRangeMax:     45,
				SellRangeMin:    55,
				SizeFraction:    0.50,
			},
		},
	}

	decision, _, ok := svc.deterministicFallbackCandidate(context.Background(), aiMarketSignal{
		Symbol:             "ONDO/USDT",
		Price:              0.379,
		High24h:            0.40,
		Low24h:             0.30,
		Volume24h:          1_500_000,
		BidAskSpread:       0.0264,
		OrderBookImbalance: -0.4007,
		PriceChange24h:     0.1043,
		RecentPriceChange:  -0.551,
		RecentChangeKnown:  true,
		RangePosition24h:   75.95,
	}, TradingPortfolio{AccountTier: appautonomy.AccountTierMicro, EffectiveMinConfidence: 0.65, EffectiveMaxCapitalPct: 12.0}, false)

	require.False(t, ok)
	require.Nil(t, decision)
}

func TestAIScalpingService_DeterministicFallbackCandidate_BlocksCounterTrendBlowoffReversalSell(t *testing.T) {
	svc := &AIScalpingService{
		config: AIScalpingConfig{
			MaxBidAskSpreadPct: 0.22,
			DeterministicFallback: DeterministicFallbackConfig{
				MaxBidAskSpread: 0.08,
				MinImbalance:    0.20,
				BuyRangeMax:     45,
				SellRangeMin:    55,
				SizeFraction:    0.50,
			},
		},
	}

	decision, _, ok := svc.deterministicFallbackCandidate(context.Background(), aiMarketSignal{
		Symbol:             "CHZ/USDT",
		Price:              0.04983,
		High24h:            0.05,
		Low24h:             0.045,
		Volume24h:          1_500_000,
		BidAskSpread:       0.0602,
		OrderBookImbalance: -0.4513,
		PriceChange24h:     0.0774,
		RecentPriceChange:  0.2817,
		RecentChangeKnown:  true,
		RangePosition24h:   96.58,
	}, TradingPortfolio{AccountTier: appautonomy.AccountTierMicro, EffectiveMinConfidence: 0.65, EffectiveMaxCapitalPct: 12.0}, false)

	require.False(t, ok)
	require.Nil(t, decision)
}

func TestAIScalpingService_DeterministicFallbackCandidate_BlocksWeakBlowoffSellPressure(t *testing.T) {
	svc := &AIScalpingService{
		config: AIScalpingConfig{
			MaxBidAskSpreadPct: 0.22,
			DeterministicFallback: DeterministicFallbackConfig{
				MaxBidAskSpread: 0.08,
				MinImbalance:    0.20,
				BuyRangeMax:     45,
				SellRangeMin:    55,
				SizeFraction:    0.50,
			},
		},
	}

	decision, _, ok := svc.deterministicFallbackCandidate(context.Background(), aiMarketSignal{
		Symbol:             "DASH/USDT",
		Price:              123.9,
		High24h:            124.2,
		Low24h:             100.0,
		Volume24h:          1_500_000,
		BidAskSpread:       0.0423,
		OrderBookImbalance: -0.2947,
		PriceChange24h:     12.0436,
		RecentPriceChange:  0.5104,
		RecentChangeKnown:  true,
		RangePosition24h:   95.16,
	}, TradingPortfolio{AccountTier: appautonomy.AccountTierMicro, EffectiveMinConfidence: 0.65, EffectiveMaxCapitalPct: 12.0}, false)

	require.False(t, ok)
	require.Nil(t, decision)
}

func TestAIScalpingService_DeterministicFallbackCandidate_UsesMomentumConfigOverride(t *testing.T) {
	svc := &AIScalpingService{
		config: AIScalpingConfig{
			MaxBidAskSpreadPct: 0.22,
			DeterministicFallback: DeterministicFallbackConfig{
				MaxBidAskSpread:       0.08,
				MinImbalance:          0.20,
				BuyRangeMax:           45,
				SellRangeMin:          55,
				BuyMinPriceChangePct:  -5.0,
				SellMaxPriceChangePct: 5.0,
				SizeFraction:          0.50,
			},
		},
	}

	decision, _, ok := svc.deterministicFallbackCandidate(context.Background(), aiMarketSignal{
		Symbol:             "DOGE/USDT",
		Price:              1,
		High24h:            1.1,
		Low24h:             0.9,
		Volume24h:          1500000,
		BidAskSpread:       0.035,
		OrderBookImbalance: 0.60,
		PriceChange24h:     -4.0,
		RangePosition24h:   18,
	}, TradingPortfolio{AccountTier: appautonomy.AccountTierMicro, EffectiveMinConfidence: 0.65, EffectiveMaxCapitalPct: 12.0}, false)

	require.True(t, ok)
	require.NotNil(t, decision)
	assert.Equal(t, "buy", decision.Action)
}

func TestAIScalpingService_DeterministicFallbackCandidate_BlocksWeakProjectedNetEdgeForMicro(t *testing.T) {
	svc := &AIScalpingService{
		config: AIScalpingConfig{
			MaxBidAskSpreadPct: 0.22,
			DeterministicFallback: DeterministicFallbackConfig{
				MaxBidAskSpread: 0.08,
				MinImbalance:    0.20,
				BuyRangeMax:     45,
				SellRangeMin:    55,
				SizeFraction:    0.50,
			},
		},
	}

	decision, _, ok := svc.deterministicFallbackCandidate(context.Background(), aiMarketSignal{
		Symbol:             "DOGE/USDT",
		Price:              100,
		High24h:            100.5,
		Low24h:             99.5,
		Volume24h:          1500000,
		BidAskSpread:       7.0,
		OrderBookImbalance: 0.60,
		RangePosition24h:   18,
	}, TradingPortfolio{AccountTier: appautonomy.AccountTierMicro, EffectiveMinConfidence: 0.65, EffectiveMaxCapitalPct: 12.0}, false)

	assert.False(t, ok)
	assert.Nil(t, decision)
}

func TestAIScalpingService_DeterministicFallbackCandidate_AllowsStrongProjectedNetEdgeForMicro(t *testing.T) {
	svc := &AIScalpingService{
		config: AIScalpingConfig{
			MaxBidAskSpreadPct: 0.22,
			DeterministicFallback: DeterministicFallbackConfig{
				MaxBidAskSpread: 0.08,
				MinImbalance:    0.20,
				BuyRangeMax:     45,
				SellRangeMin:    55,
				SizeFraction:    0.50,
			},
		},
	}

	decision, _, ok := svc.deterministicFallbackCandidate(context.Background(), aiMarketSignal{
		Symbol:             "BOME/USDT",
		Price:              1,
		High24h:            1.1,
		Low24h:             0.9,
		Volume24h:          1500000,
		BidAskSpread:       0.139,
		OrderBookImbalance: 0.368,
		RangePosition24h:   44.2,
	}, TradingPortfolio{AccountTier: appautonomy.AccountTierMicro, EffectiveMinConfidence: 0.65, EffectiveMaxCapitalPct: 12.0}, false)

	require.True(t, ok)
	require.NotNil(t, decision)
	assert.Equal(t, "buy", decision.Action)
	assert.Contains(t, decision.Reasoning, "projected net edge")
}

func TestFallbackProjectedNetEdgePct(t *testing.T) {
	standardEdge, _ := fallbackProjectedNetEdgePct(0.14, decimal.NewFromFloat(0.0192)).Float64()
	marginalEdge, _ := fallbackProjectedNetEdgePct(0.22, decimal.NewFromFloat(0.0040)).Float64()
	assert.InDelta(t, 1.58, standardEdge, 0.0001)
	assert.InDelta(t, -0.02, marginalEdge, 0.0001)
}

func TestAIScalpingService_ValidateDecisionAllowsValidatedReversalBuy(t *testing.T) {
	cfg := DefaultAIScalpingConfig()
	cfg.MaxBidAskSpreadPct = scalpingRecentBuyMaxSpreadPct
	svc := &AIScalpingService{config: cfg}
	stopLoss := decimal.NewFromFloat(99)
	takeProfit := decimal.NewFromFloat(102)
	decision := &AITradingDecision{
		Action:      "buy",
		Symbol:      "REV/USDT",
		SizePercent: 5,
		Confidence:  0.70,
		StopLoss:    &stopLoss,
		TakeProfit:  &takeProfit,
	}

	err := svc.validateDecision(context.Background(), decision, []aiMarketSignal{{
		Symbol:             "REV/USDT",
		Price:              100,
		High24h:            115,
		Low24h:             95,
		Volume24h:          5_000_000,
		BidAskSpread:       0.05,
		OrderBookImbalance: 0.05,
		RangePosition24h:   18,
		PriceChange24h:     -0.10,
		RecentPriceChange:  -0.20,
		RecentChangeKnown:  true,
	}})

	require.NoError(t, err)
}

func TestAIScalpingService_ValidateDecisionAllowsValidatedSellWindow(t *testing.T) {
	cfg := DefaultAIScalpingConfig()
	cfg.MaxBidAskSpreadPct = scalpingRecentBuyMaxSpreadPct
	svc := &AIScalpingService{config: cfg}
	stopLoss := decimal.NewFromFloat(101)
	takeProfit := decimal.NewFromFloat(98)
	decision := &AITradingDecision{
		Action:      "sell",
		Symbol:      "SW/USDT",
		SizePercent: 5,
		Confidence:  0.70,
		StopLoss:    &stopLoss,
		TakeProfit:  &takeProfit,
	}

	err := svc.validateDecision(context.Background(), decision, []aiMarketSignal{{
		Symbol:             "SW/USDT",
		Price:              100,
		High24h:            115,
		Low24h:             95,
		Volume24h:          5_000_000,
		BidAskSpread:       0.09,
		OrderBookImbalance: -0.45,
		RangePosition24h:   55,
		PriceChange24h:     0.30,
		RecentPriceChange:  -0.10,
		RecentChangeKnown:  true,
	}})

	require.NoError(t, err)
}

func TestAIScalpingService_DeterministicFallbackCandidate_AllowsValidatedCandidateShapes(t *testing.T) {
	svc := &AIScalpingService{config: DefaultAIScalpingConfig()}

	buyDecision, _, ok := svc.deterministicFallbackCandidate(context.Background(), aiMarketSignal{
		Symbol:             "REV/USDT",
		Price:              100,
		High24h:            115,
		Low24h:             95,
		Volume24h:          5_000_000,
		BidAskSpread:       0.05,
		OrderBookImbalance: 0.05,
		RangePosition24h:   18,
		PriceChange24h:     -0.10,
		RecentPriceChange:  -0.20,
		RecentChangeKnown:  true,
	}, TradingPortfolio{}, false)
	require.True(t, ok)
	require.NotNil(t, buyDecision)
	require.Equal(t, "buy", buyDecision.Action)
	require.Contains(t, buyDecision.Reasoning, "validated reversal buy")

	sellDecision, _, ok := svc.deterministicFallbackCandidate(context.Background(), aiMarketSignal{
		Symbol:             "SW/USDT",
		Price:              100,
		High24h:            115,
		Low24h:             95,
		Volume24h:          5_000_000,
		BidAskSpread:       0.09,
		OrderBookImbalance: -0.45,
		RangePosition24h:   55,
		PriceChange24h:     0.30,
		RecentPriceChange:  -0.10,
		RecentChangeKnown:  true,
	}, TradingPortfolio{}, false)
	require.True(t, ok)
	require.NotNil(t, sellDecision)
	require.Equal(t, "sell", sellDecision.Action)
	require.Contains(t, sellDecision.Reasoning, "validated sell window")
}

func TestAIScalpingService_DeterministicFallbackCandidate_BlocksNegativeSymbolExpectancyForMicro(t *testing.T) {
	db := setupTestDB(t)
	tm, err := NewTradeMemory(db)
	require.NoError(t, err)

	_, err = db.Exec(`CREATE TABLE realized_pnl_journal (
		id TEXT PRIMARY KEY,
		order_id TEXT NOT NULL UNIQUE,
		chat_id TEXT,
		exchange TEXT NOT NULL,
		symbol TEXT NOT NULL,
		side TEXT NOT NULL,
		filled_amount NUMERIC NOT NULL DEFAULT 0,
		entry_price NUMERIC NOT NULL DEFAULT 0,
		exit_price NUMERIC NOT NULL DEFAULT 0,
		realized_pnl NUMERIC NOT NULL DEFAULT 0,
		fees NUMERIC NOT NULL DEFAULT 0,
		source TEXT NOT NULL DEFAULT 'autonomous',
		closed_at TIMESTAMP NOT NULL,
		created_at TIMESTAMP NOT NULL
	)`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO realized_pnl_journal (
		id, order_id, chat_id, exchange, symbol, side, filled_amount, entry_price, exit_price, realized_pnl, fees, source, closed_at, created_at
	) VALUES
		('rp_1', 'ord_1', 'chat-1', 'bitget', 'OPN/USDT', 'buy', 1, 100, 99, -1.0, -0.1, 'autonomous', datetime('now'), datetime('now')),
		('rp_2', 'ord_2', 'chat-1', 'bitget', 'OPN/USDT', 'buy', 1, 100, 99, -0.8, -0.1, 'autonomous', datetime('now'), datetime('now')),
		('rp_3', 'ord_3', 'chat-1', 'bitget', 'OPN/USDT', 'buy', 1, 100, 99, -0.6, -0.1, 'autonomous', datetime('now'), datetime('now'))`)
	require.NoError(t, err)

	svc := &AIScalpingService{
		config: AIScalpingConfig{
			MaxBidAskSpreadPct: 0.22,
			MinExpectancyN:     5,
			DeterministicFallback: DeterministicFallbackConfig{
				MaxBidAskSpread: 0.08,
				MinImbalance:    0.20,
				BuyRangeMax:     45,
				SellRangeMin:    55,
				SizeFraction:    0.50,
			},
		},
		tradeMemory: tm,
	}
	ctx := WithScalpingAutonomyScope(context.Background(), ScalpingAutonomyScope{ChatID: "chat-1", Exchange: "bitget"})

	decision, _, ok := svc.deterministicFallbackCandidate(ctx, aiMarketSignal{
		Symbol:             "OPN/USDT",
		Price:              1,
		High24h:            1.1,
		Low24h:             0.9,
		Volume24h:          1500000,
		BidAskSpread:       0.08,
		OrderBookImbalance: 0.50,
		RangePosition24h:   20,
	}, TradingPortfolio{AccountTier: appautonomy.AccountTierMicro, EffectiveMinConfidence: 0.65, EffectiveMaxCapitalPct: 12.0}, false)

	assert.False(t, ok)
	assert.Nil(t, decision)
}

func TestAIScalpingService_DeterministicFallbackDecision_RelaxedPassSelectsCandidate(t *testing.T) {
	svc := &AIScalpingService{
		config: AIScalpingConfig{
			MaxBidAskSpreadPct: 0.22,
			DeterministicFallback: DeterministicFallbackConfig{
				MaxBidAskSpread: 0.08,
				MinImbalance:    0.20,
				BuyRangeMax:     45,
				SellRangeMin:    55,
				RangeAnchor:     55,
				RangeOffset:     45,
				SizeFraction:    0.50,
			},
		},
	}

	decision := svc.deterministicFallbackDecision(context.Background(), []aiMarketSignal{{
		Symbol:             "BOME/USDT",
		Price:              1,
		High24h:            1.1,
		Low24h:             0.9,
		Volume24h:          1500000,
		BidAskSpread:       0.19,
		OrderBookImbalance: 0.15,
		RangePosition24h:   24,
	}}, TradingPortfolio{EffectiveMinConfidence: 0.65, EffectiveMaxCapitalPct: 12.7847})

	require.NotNil(t, decision)
	assert.Equal(t, "buy", decision.Action)
	assert.Equal(t, reasonCategoryDeterministicFallback, decision.ReasonCategory)
}

func TestAIScalpingService_MaxBidAskSpreadPct_ClampsConfiguredValue(t *testing.T) {
	svc := &AIScalpingService{config: AIScalpingConfig{MaxBidAskSpreadPct: 99}}
	assert.InDelta(t, 5.0, svc.maxBidAskSpreadPct(), 0.000001)

	svc.config.MaxBidAskSpreadPct = 0.00001
	assert.InDelta(t, 0.0001, svc.maxBidAskSpreadPct(), 0.000001)
}

func TestAIScalpingService_DeterministicFallbackCandidate_ClampsNegativeVolume(t *testing.T) {
	svc := &AIScalpingService{
		config: AIScalpingConfig{
			MaxCapitalPct: 5,
			MinConfidence: 0.60,
		},
	}

	signal := aiMarketSignal{
		Symbol:             "BTC/USDT",
		Price:              100,
		High24h:            104,
		Low24h:             96,
		Volume24h:          -1000,
		BidAskSpread:       0.005,
		OrderBookImbalance: 0.90,
		RangePosition24h:   5,
	}

	decision, confidence, ok := svc.deterministicFallbackCandidate(context.Background(), signal, TradingPortfolio{}, false)
	require.True(t, ok)
	require.NotNil(t, decision)
	assert.False(t, math.IsNaN(confidence) || math.IsInf(confidence, 0))
}

func TestAIScalpingService_GetAIDecision_ClassifiesUnsupportedActionAsParseContract(t *testing.T) {
	mockLLM := &MockLLMClient{
		Responses: []*llm.CompletionResponse{
			{
				Message: llm.Message{
					Content: `{"action":"jump","symbol":"BTC/USDT","size_pct":1.0,"confidence":0.7,"reasoning":"invalid action","stop_loss":99,"take_profit":101}`,
				},
			},
			{
				Message: llm.Message{
					Content: `{"action":"jump","symbol":"BTC/USDT","size_pct":1.0,"confidence":0.7,"reasoning":"still invalid","stop_loss":99,"take_profit":101}`,
				},
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
		},
		llmClient: mockLLM,
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

	require.NoError(t, err)
	require.NotNil(t, decision)
	assert.Equal(t, reasonCategoryDeterministicFallback, decision.ReasonCategory)

	diagnostics := svc.RuntimeDiagnostics()
	assert.Equal(t, reasonCategoryLLMParseContract, diagnostics["last_reason_category"])
	assert.Contains(t, diagnostics["last_error"], "unsupported action")
}

func TestAIScalpingService_DeterministicFallbackDecision_NoCandidateUsesRuntimeHold(t *testing.T) {
	svc := &AIScalpingService{}
	decision := svc.deterministicFallbackDecision(context.Background(), []aiMarketSignal{
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

func TestExtractSLTPFromText_ColonPatterns(t *testing.T) {
	tests := []struct {
		name   string
		raw    string
		wantSL string
		wantTP string
	}{
		{
			name:   "standard_colon",
			raw:    `stop_loss: 41000; take_profit: 43000`,
			wantSL: "41000",
			wantTP: "43000",
		},
		{
			name:   "equals_sign",
			raw:    `sl=41000 tp=43000`,
			wantSL: "41000",
			wantTP: "43000",
		},
		{
			name:   "mixed_case",
			raw:    `Stop_Loss: 41000 Take_Profit: 43000`,
			wantSL: "41000",
			wantTP: "43000",
		},
		{
			name:   "with_spaces_around_colon",
			raw:    `stop_loss : 41000 take_profit : 43000`,
			wantSL: "41000",
			wantTP: "43000",
		},
		{
			name:   "decimal_prices",
			raw:    `stop_loss: 0.0935; take_profit: 0.09444`,
			wantSL: "0.0935",
			wantTP: "0.09444",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractSLTPFromText(tt.raw)
			require.NotNil(t, result.stopLoss, "stop_loss should be found")
			require.NotNil(t, result.takeProfit, "take_profit should be found")
			assert.Equal(t, tt.wantSL, result.stopLoss.String())
			assert.Equal(t, tt.wantTP, result.takeProfit.String())
		})
	}
}

func TestExtractSLTPFromText_NarrativePatterns(t *testing.T) {
	tests := []struct {
		name   string
		raw    string
		wantSL decimal.Decimal
		wantTP decimal.Decimal
	}{
		{
			name:   "stop_loss_at",
			raw:    `I would set stop loss at 41000 and take profit at 43000`,
			wantSL: decimal.NewFromInt(41000),
			wantTP: decimal.NewFromInt(43000),
		},
		{
			name:   "stop_loss_at_with_price_prefix",
			raw:    `entry is near the current price, stop loss at 98.50, take profit at 104.20`,
			wantSL: decimal.RequireFromString("98.50"),
			wantTP: decimal.RequireFromString("104.20"),
		},
		{
			name:   "sl_tp_in_reasoning",
			raw:    `Reasoning: The order book shows strong buy pressure. Confidence: 0.75. Action: buy. Symbol: BTC/USDT. Size: 5.0. SL: 41000. TP: 43000.`,
			wantSL: decimal.NewFromInt(41000),
			wantTP: decimal.NewFromInt(43000),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractSLTPFromText(tt.raw)
			require.NotNil(t, result.stopLoss, "stop_loss should be found")
			require.NotNil(t, result.takeProfit, "take_profit should be found")
			assert.True(t, result.stopLoss.Equal(tt.wantSL), "stop_loss mismatch: got %s, want %s", result.stopLoss.String(), tt.wantSL.String())
			assert.True(t, result.takeProfit.Equal(tt.wantTP), "take_profit mismatch: got %s, want %s", result.takeProfit.String(), tt.wantTP.String())
		})
	}
}

func TestExtractSLTPFromText_NoValues(t *testing.T) {
	result := extractSLTPFromText("This is a reasoning text without any stop loss or take profit values.")
	assert.Nil(t, result.stopLoss)
	assert.Nil(t, result.takeProfit)

	result = extractSLTPFromText("")
	assert.Nil(t, result.stopLoss)
	assert.Nil(t, result.takeProfit)
}

func TestExtractSLTPFromText_OnlyOneValue(t *testing.T) {
	result := extractSLTPFromText("stop_loss: 41000 but no take profit mentioned")
	require.NotNil(t, result.stopLoss)
	assert.Equal(t, "41000", result.stopLoss.String())
	assert.Nil(t, result.takeProfit)

	result = extractSLTPFromText("take_profit: 43000 but no stop loss mentioned")
	require.NotNil(t, result.takeProfit)
	assert.Equal(t, "43000", result.takeProfit.String())
	assert.Nil(t, result.stopLoss)
}

func TestInferDecisionFromLooseText_PreservesSLTPFromProse(t *testing.T) {
	raw := `action: buy
symbol: BTC/USDT
confidence: 68%
size_pct: 5.0
reasoning: Breakout with tight spread
I would set stop loss at 41000 and take profit at 43000`

	decision, err := inferDecisionFromLooseText(raw)
	require.NoError(t, err)
	require.NotNil(t, decision)
	assert.Equal(t, "buy", decision.Action)
	assert.Equal(t, "BTC/USDT", decision.Symbol)

	require.NotNil(t, decision.StopLoss)
	require.NotNil(t, decision.TakeProfit)
	assert.True(t, decision.StopLoss.Equal(decimal.NewFromInt(41000)))
	assert.True(t, decision.TakeProfit.Equal(decimal.NewFromInt(43000)))
}

func TestInferDecisionFromLooseText_PreservesSLTPFromColonPatterns(t *testing.T) {
	raw := `action: buy; symbol: BTC/USDT; size_pct: 0.75; confidence: 68%; reasoning: breakout; sl: 41000; tp: 43000`

	decision, err := inferDecisionFromLooseText(raw)
	require.NoError(t, err)
	require.NotNil(t, decision)
	assert.Equal(t, "buy", decision.Action)

	require.NotNil(t, decision.StopLoss)
	require.NotNil(t, decision.TakeProfit)
	assert.True(t, decision.StopLoss.Equal(decimal.NewFromInt(41000)))
	assert.True(t, decision.TakeProfit.Equal(decimal.NewFromInt(43000)))
}

func TestInferDecisionFromLooseText_PreservesSLTPFromDecimalProse(t *testing.T) {
	raw := `action: buy; symbol: DOGE/USDT; size_pct: 12.7; confidence: 72%; reasoning: entry; stop loss at 0.09350, take profit at 0.09444`

	decision, err := inferDecisionFromLooseText(raw)
	require.NoError(t, err)
	require.NotNil(t, decision)
	assert.Equal(t, "buy", decision.Action)

	require.NotNil(t, decision.StopLoss)
	require.NotNil(t, decision.TakeProfit)
	assert.True(t, decision.StopLoss.Equal(decimal.RequireFromString("0.09350")))
	assert.True(t, decision.TakeProfit.Equal(decimal.RequireFromString("0.09444")))
}

func TestParseDecisionWithRetries_PreservesSLTPFromOriginalAfterLocalRepair_DecisionMissingSLTP(t *testing.T) {
	mockLLM := &MockLLMClient{}
	svc := &AIScalpingService{
		config: AIScalpingConfig{
			Model:             "glm-5",
			MaxTokens:         1200,
			StructuredRetries: 1,
		},
		llmClient: mockLLM,
	}

	raw := `action: buy; symbol: BTC/USDT; size_pct: 0.75; confidence: 68%; reasoning: breakout; sl: 41000; tp: 43000`

	decision, err := svc.parseDecisionWithRetries(context.Background(), raw)
	require.NoError(t, err)
	require.NotNil(t, decision)
	assert.Equal(t, "buy", decision.Action)
	assert.Equal(t, "BTC/USDT", decision.Symbol)

	require.NotNil(t, decision.StopLoss)
	require.NotNil(t, decision.TakeProfit)
	assert.True(t, decision.StopLoss.Equal(decimal.NewFromInt(41000)))
	assert.True(t, decision.TakeProfit.Equal(decimal.NewFromInt(43000)))
	assert.Equal(t, 0, mockLLM.CallCount)
}

func TestParseDecisionWithRetries_PreservesSLTPFromOriginalAfterLLMRepair(t *testing.T) {
	mockLLM := &MockLLMClient{
		Responses: []*llm.CompletionResponse{
			{
				Message: llm.Message{
					Content: `{"action":"buy","symbol":"BTC/USDT","size_pct":5,"confidence":0.7,"reasoning":"breakout"}`,
				},
			},
		},
	}

	svc := &AIScalpingService{
		config: AIScalpingConfig{
			Model:             "glm-5",
			MaxTokens:         1200,
			StructuredRetries: 1,
		},
		llmClient: mockLLM,
	}

	raw := `Let me analyze the market signals and make a trading decision.
I see strong buy pressure on BTC/USDT with good confidence.
I recommend a buy with stop_loss at 41000 and take_profit at 43000.`

	decision, err := svc.parseDecisionWithRetries(context.Background(), raw)
	require.NoError(t, err)
	require.NotNil(t, decision)
	assert.Equal(t, "buy", decision.Action)
	assert.Equal(t, "BTC/USDT", decision.Symbol)
	assert.Equal(t, 1, mockLLM.CallCount)

	require.NotNil(t, decision.StopLoss)
	require.NotNil(t, decision.TakeProfit)
	assert.True(t, decision.StopLoss.Equal(decimal.NewFromInt(41000)))
	assert.True(t, decision.TakeProfit.Equal(decimal.NewFromInt(43000)))
}

func TestBuildSLTPHint(t *testing.T) {
	t.Run("both_present", func(t *testing.T) {
		hint := buildSLTPHint("sl: 41000; tp: 43000")
		assert.Contains(t, hint, "stop_loss=41000")
		assert.Contains(t, hint, "take_profit=43000")
	})

	t.Run("only_sl", func(t *testing.T) {
		hint := buildSLTPHint("sl: 41000")
		assert.Contains(t, hint, "stop_loss=41000")
		assert.NotContains(t, hint, "take_profit")
	})

	t.Run("none", func(t *testing.T) {
		hint := buildSLTPHint("no sltp values here")
		assert.Empty(t, hint)
	})
}

func TestApplySLTPFromOriginal(t *testing.T) {
	t.Run("fills_missing_sltp", func(t *testing.T) {
		decision := &AITradingDecision{
			Action:      "buy",
			Symbol:      "BTC/USDT",
			SizePercent: 5,
			Confidence:  0.7,
			Reasoning:   "test",
		}
		applySLTPFromOriginal(decision, "sl: 41000; tp: 43000")

		require.NotNil(t, decision.StopLoss)
		require.NotNil(t, decision.TakeProfit)
		assert.True(t, decision.StopLoss.Equal(decimal.NewFromInt(41000)))
		assert.True(t, decision.TakeProfit.Equal(decimal.NewFromInt(43000)))
	})

	t.Run("skips_hold_decision", func(t *testing.T) {
		decision := &AITradingDecision{
			Action: "hold",
		}
		applySLTPFromOriginal(decision, "sl: 41000; tp: 43000")
		assert.Nil(t, decision.StopLoss)
		assert.Nil(t, decision.TakeProfit)
	})

	t.Run("does_not_overwrite_existing", func(t *testing.T) {
		existingSL := decimal.NewFromInt(40000)
		existingTP := decimal.NewFromInt(44000)
		decision := &AITradingDecision{
			Action:     "buy",
			StopLoss:   &existingSL,
			TakeProfit: &existingTP,
		}
		applySLTPFromOriginal(decision, "sl: 41000; tp: 43000")
		assert.True(t, decision.StopLoss.Equal(existingSL))
		assert.True(t, decision.TakeProfit.Equal(existingTP))
	})

	t.Run("fills_partial", func(t *testing.T) {
		existingSL := decimal.NewFromInt(40000)
		decision := &AITradingDecision{
			Action:   "buy",
			StopLoss: &existingSL,
		}
		applySLTPFromOriginal(decision, "sl: 41000; tp: 43000")
		assert.True(t, decision.StopLoss.Equal(existingSL), "should keep existing SL")
		require.NotNil(t, decision.TakeProfit, "should fill missing TP")
		assert.True(t, decision.TakeProfit.Equal(decimal.NewFromInt(43000)))
	})
}

func TestAIScalpingService_ValidateDecision_RefusesLiveTradeWithMissingSLTP(t *testing.T) {
	svc := &AIScalpingService{}
	liveCtx := WithOperationalMode(context.Background(), OpModeLive)
	paperCtx := WithOperationalMode(context.Background(), ModePaper)
	dryCtx := WithOperationalMode(context.Background(), OpModeDry)
	neutralCtx := context.Background()

	signals := []aiMarketSignal{{
		Symbol:             "DOGE/USDT",
		Price:              0.09429,
		High24h:            0.095,
		Low24h:             0.091,
		Volume24h:          1_000_000,
		BidAskSpread:       0.011,
		OrderBookImbalance: 0.45,
		RangePosition24h:   18.92,
	}}

	t.Run("live_mode_with_missing_sl_tp_returns_error_and_does_not_mutate_decision", func(t *testing.T) {
		decision := &AITradingDecision{
			Action:      "buy",
			Symbol:      "DOGE/USDT",
			SizePercent: 5.0,
			Confidence:  0.80,
		}
		err := svc.validateDecision(liveCtx, decision, signals)
		require.Error(t, err, "live mode must refuse a buy with missing SL/TP rather than apply synthetic defaults")
		assert.Nil(t, decision.StopLoss, "decision.StopLoss must remain nil when the trade is refused")
		assert.Nil(t, decision.TakeProfit, "decision.TakeProfit must remain nil when the trade is refused")
		assert.Contains(t, err.Error(), "refusing live trade")
		assert.Contains(t, err.Error(), "DOGE/USDT")
	})

	t.Run("live_mode_with_missing_tp_only_also_refuses", func(t *testing.T) {
		decision := &AITradingDecision{
			Action:      "buy",
			Symbol:      "DOGE/USDT",
			SizePercent: 5.0,
			Confidence:  0.80,
			StopLoss:    decimalPointer("0.09350"),
		}
		err := svc.validateDecision(liveCtx, decision, signals)
		require.Error(t, err, "live mode must refuse when only TP is missing - asymmetric risk:reward is unacceptable")
	})

	t.Run("paper_mode_with_missing_sl_tp_applies_defaults", func(t *testing.T) {
		decision := &AITradingDecision{
			Action:      "buy",
			Symbol:      "DOGE/USDT",
			SizePercent: 5.0,
			Confidence:  0.80,
		}
		err := svc.validateDecision(paperCtx, decision, signals)
		require.NoError(t, err, "paper mode must keep the prior behavior of applying default SL/TP for evidence collection")
		require.NotNil(t, decision.StopLoss)
		require.NotNil(t, decision.TakeProfit)
		assert.True(t, decision.StopLoss.LessThan(decimal.NewFromFloat(0.09429)))
		assert.True(t, decision.TakeProfit.GreaterThan(decimal.NewFromFloat(0.09429)))
	})

	t.Run("dry_mode_with_missing_sl_tp_applies_defaults", func(t *testing.T) {
		decision := &AITradingDecision{
			Action:      "buy",
			Symbol:      "DOGE/USDT",
			SizePercent: 5.0,
			Confidence:  0.80,
		}
		err := svc.validateDecision(dryCtx, decision, signals)
		require.NoError(t, err)
		require.NotNil(t, decision.StopLoss)
		require.NotNil(t, decision.TakeProfit)
		assert.True(t, decision.StopLoss.LessThan(decimal.NewFromFloat(0.09429)))
		assert.True(t, decision.TakeProfit.GreaterThan(decimal.NewFromFloat(0.09429)))
	})

	t.Run("neutral_context_with_missing_sl_tp_applies_defaults", func(t *testing.T) {
		decision := &AITradingDecision{
			Action:      "buy",
			Symbol:      "DOGE/USDT",
			SizePercent: 5.0,
			Confidence:  0.80,
		}
		err := svc.validateDecision(neutralCtx, decision, signals)
		require.NoError(t, err, "neutral context (no mode) must default to non-live behavior for back-compat")
		require.NotNil(t, decision.StopLoss)
		require.NotNil(t, decision.TakeProfit)
	})

	t.Run("live_mode_with_both_sl_and_tp_present_passes_through", func(t *testing.T) {
		decision := &AITradingDecision{
			Action:      "buy",
			Symbol:      "DOGE/USDT",
			SizePercent: 5.0,
			Confidence:  0.80,
			StopLoss:    decimalPointer("0.09350"),
			TakeProfit:  decimalPointer("0.09600"),
		}
		err := svc.validateDecision(liveCtx, decision, signals)
		require.NoError(t, err)
	})

	t.Run("paper_mode_with_missing_sl_tp_increments_prometheus_counter", func(t *testing.T) {
		svcWithClient := &AIScalpingService{
			llmClient: &errorLLMClient{},
		}
		decision := &AITradingDecision{
			Action:      "buy",
			Symbol:      "DOGE/USDT",
			SizePercent: 5.0,
			Confidence:  0.80,
		}
		err := svcWithClient.validateDecision(paperCtx, decision, signals)
		require.NoError(t, err, "paper mode must apply defaults")
		require.NotNil(t, decision.StopLoss)
		require.NotNil(t, decision.TakeProfit)
	})
}

// TestDefaultExitLevelsWithLeverage verifies that the leverage-adjusted
// fallback SL/TP scales inversely with leverage to maintain constant
// capital risk. Base: 0.8% SL / 1.2% TP at 1x. At Nx, both divide by N.
func TestDefaultExitLevelsWithLeverage(t *testing.T) {
	const entry = 100.0

	tests := []struct {
		name         string
		action       string
		leverage     int
		wantStopDist float64 // expected distance from entry in price units
		wantTakeDist float64
	}{
		{"1x buy", "buy", 1, 0.8, 1.2},
		{"5x buy", "buy", 5, 0.16, 0.24},
		{"20x buy", "buy", 20, 0.04, 0.06},
		{"1x sell", "sell", 1, 0.8, 1.2},
		{"10x sell", "sell", 10, 0.08, 0.12},
		{"0 leverage treated as 1x", "buy", 0, 0.8, 1.2},
		{"negative leverage treated as 1x", "buy", -3, 0.8, 1.2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sl, tp := defaultExitLevelsWithLeverage(entry, tt.action, tt.leverage)

			if tt.action == "sell" {
				expectedSL := entry + tt.wantStopDist
				expectedTP := entry - tt.wantTakeDist
				if !approximatelyEqual(sl.InexactFloat64(), expectedSL, 0.0001) {
					t.Errorf("sell SL: got %.4f, want %.4f", sl.InexactFloat64(), expectedSL)
				}
				if !approximatelyEqual(tp.InexactFloat64(), expectedTP, 0.0001) {
					t.Errorf("sell TP: got %.4f, want %.4f", tp.InexactFloat64(), expectedTP)
				}
				if sl.LessThanOrEqual(decimal.NewFromFloat(entry)) {
					t.Errorf("sell SL %.4f must be > entry %.4f", sl.InexactFloat64(), entry)
				}
				if tp.GreaterThanOrEqual(decimal.NewFromFloat(entry)) {
					t.Errorf("sell TP %.4f must be < entry %.4f", tp.InexactFloat64(), entry)
				}
			} else {
				expectedSL := entry - tt.wantStopDist
				expectedTP := entry + tt.wantTakeDist
				if !approximatelyEqual(sl.InexactFloat64(), expectedSL, 0.0001) {
					t.Errorf("buy SL: got %.4f, want %.4f", sl.InexactFloat64(), expectedSL)
				}
				if !approximatelyEqual(tp.InexactFloat64(), expectedTP, 0.0001) {
					t.Errorf("buy TP: got %.4f, want %.4f", tp.InexactFloat64(), expectedTP)
				}
				if sl.GreaterThanOrEqual(decimal.NewFromFloat(entry)) {
					t.Errorf("buy SL %.4f must be < entry %.4f", sl.InexactFloat64(), entry)
				}
				if tp.LessThanOrEqual(decimal.NewFromFloat(entry)) {
					t.Errorf("buy TP %.4f must be > entry %.4f", tp.InexactFloat64(), entry)
				}
			}
		})
	}
}

// approximatelyEqual compares two floats with a tolerance.
func approximatelyEqual(a, b, tolerance float64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff <= tolerance
}

// TestScalpingBlowoffSellTrendConfirmed_AlwaysDisabled guards the
// hard-disabled blowoff sell signal. It must always return false
// regardless of input. The function is intentionally a placeholder
// until observed paper evidence shows counter-trend blowoff shorts
// can beat fees (see ai_scalping.go line ~3783).
func TestScalpingBlowoffSellTrendConfirmed_AlwaysDisabled(t *testing.T) {
	tests := []struct {
		name   string
		signal aiMarketSignal
	}{
		{"empty signal", aiMarketSignal{}},
		{"strong blowoff — all conditions met", aiMarketSignal{
			PriceChange24h:     0.10,
			RecentChangeKnown:  true,
			OrderBookImbalance: -0.5,
			RangePosition24h:   0.05,
			BidAskSpread:       0.01,
		}},
		{"weak blowoff", aiMarketSignal{PriceChange24h: 0.01}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := scalpingBlowoffSellTrendConfirmed(tt.signal); got {
				t.Errorf("scalpingBlowoffSellTrendConfirmed(%+v) = true, want false (hard-disabled)", tt.signal)
			}
		})
	}
}
