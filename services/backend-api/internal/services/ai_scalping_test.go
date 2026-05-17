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

func (m mockMarketPrice) GetPrice() float64          { return m.price }
func (m mockMarketPrice) GetVolume() float64         { return m.volume }
func (m mockMarketPrice) GetTimestamp() time.Time    { return time.Now().UTC() }
func (m mockMarketPrice) GetExchangeName() string    { return m.exchange }
func (m mockMarketPrice) GetSymbol() string          { return m.symbol }
func (m mockMarketPrice) GetBid() float64            { return m.bid }
func (m mockMarketPrice) GetAsk() float64            { return m.ask }
func (m mockMarketPrice) GetHigh() float64           { return m.high24h }
func (m mockMarketPrice) GetLow() float64            { return m.low24h }
func (m mockMarketPrice) GetPriceChange24h() float64 { return m.change24h }

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
	assert.Equal(t, 0.001, config.MinExpectancyEdge)
	assert.Equal(t, 8, config.MinExpectancyN)
	assert.Equal(t, 85.0, config.RegimeHighBand)
	assert.Equal(t, 15.0, config.RegimeLowBand)
	assert.Equal(t, 0.08, config.DeterministicFallback.MaxBidAskSpread)
	assert.Equal(t, 0.35, config.DeterministicFallback.MinImbalance)
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
	assert.Equal(t, 0.04, cfg.DeterministicFallback.MaxBidAskSpread)
	assert.Equal(t, 0.42, cfg.DeterministicFallback.MinImbalance)
	assert.Equal(t, 0.79, cfg.DeterministicFallback.ConfidenceFloor)
	assert.Equal(t, 0.0, cfg.DeterministicFallback.RangeOffset)
	assert.Equal(t, 0.33, cfg.DeterministicFallback.SizeFraction)
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
				RangePosition24h:   50,
			}
			decision := &AITradingDecision{Action: "buy", Symbol: "GRASS/USDT", Confidence: 0.78}

			result := svc.evaluatePreTradeGate(context.Background(), decision, []aiMarketSignal{signal})
			assert.Equal(t, tt.allowed, result.Allowed)

			err := svc.validateDecision(&AITradingDecision{
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

	err := svc.validateDecision(decision, []aiMarketSignal{{
		Symbol:             "DOGE/USDT",
		Price:              0.09429,
		High24h:            0.095,
		Low24h:             0.091,
		Volume24h:          1_000_000,
		BidAskSpread:       0.011,
		OrderBookImbalance: 0.45,
		RangePosition24h:   39.92,
	}})

	require.NoError(t, err)
	require.NotNil(t, decision.TakeProfit)
	assert.Greater(t, decision.TakeProfit.InexactFloat64(), 0.09515)
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
	assert.Contains(t, prompt, "Historical performance is already reflected in those effective thresholds")
	assert.NotContains(t, prompt, "Phase Min Confidence (reference only)")
	assert.NotContains(t, prompt, "Phase Max Capital % (reference only)")
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
	assert.Contains(t, prompt, "spread <= 0.22%")
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
				low24h:   90,
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
		_, _, ok := svc.deterministicFallbackCandidate(context.Background(), signal, TradingPortfolio{}, false)
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

func TestAIScalpingService_FocusActionableMarketSignalsKeepsDecisionReadySignals(t *testing.T) {
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

	focused := svc.focusActionableMarketSignals(context.Background(), signals)

	require.Len(t, focused, 1)
	assert.Equal(t, "BTC/USDT", focused[0].Symbol)
}

func TestAIScalpingService_FocusActionableMarketSignalsKeepsDiagnosticsWhenNoCandidate(t *testing.T) {
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

	focused := svc.focusActionableMarketSignals(context.Background(), signals)

	require.Len(t, focused, 2)
	assert.Equal(t, signals, focused)
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
		BidAskSpread:       0.05,
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
		BidAskSpread:       0.05,
		OrderBookImbalance: 0.60,
		PriceChange24h:     -0.2,
		RangePosition24h:   18,
	}, TradingPortfolio{AccountTier: appautonomy.AccountTierMicro, EffectiveMinConfidence: 0.65, EffectiveMaxCapitalPct: 12.0}, false)

	require.True(t, ok)
	require.NotNil(t, decision)
	assert.Equal(t, "buy", decision.Action)
	assert.Contains(t, decision.Reasoning, "24h change")
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
		BidAskSpread:       0.05,
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
		BidAskSpread:       0.22,
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
	assert.InDelta(t, 1.66, fallbackProjectedNetEdgePct(0.14, 0.0192), 0.0001)
	assert.InDelta(t, 0.06, fallbackProjectedNetEdgePct(0.22, 0.0040), 0.0001)
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
