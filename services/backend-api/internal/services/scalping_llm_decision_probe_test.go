package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/ai/llm"
	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestRunScalpingLLMDecisionProbeWithServiceUsesStructuredLLMDecision(t *testing.T) {
	mockLLM := &MockLLMClient{
		Responses: []*llm.CompletionResponse{
			{
				Provider:     llm.Provider("deepseek"),
				Model:        "deepseek-chat",
				LatencyMs:    120,
				FinishReason: "stop",
				Message: llm.Message{
					Content: `{"action":"hold","symbol":"","size_pct":0,"confidence":0.42,"reasoning":"Waiting for a cleaner edge.","stop_loss":null,"take_profit":null}`,
				},
			},
		},
	}
	svc := newScalpingLLMDecisionProbeTestService(mockLLM)

	result, err := runScalpingLLMDecisionProbeWithService(context.Background(), svc, ScalpingLLMDecisionProbeOptions{
		RequireHealthy: true,
		RequireValid:   true,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1, mockLLM.CallCount)
	require.Equal(t, 1, result.SignalCount)
	require.Equal(t, 1, result.SignalQualityCount)
	require.True(t, result.SignalQualityCoverage.Equal(decimal.NewFromInt(1)))
	require.False(t, result.LLMDegraded)
	require.True(t, result.ContractValid)
	require.Equal(t, "deepseek", result.Provider)
	require.NotNil(t, result.Decision)
	require.Equal(t, "hold", result.Decision.Action)
}

func TestRunScalpingLLMDecisionProbeWithServiceKeepsActionableDecisionOutOfHoldCategory(t *testing.T) {
	mockLLM := &MockLLMClient{
		Responses: []*llm.CompletionResponse{
			{
				Provider:     llm.Provider("deepseek"),
				Model:        "deepseek-chat",
				LatencyMs:    120,
				FinishReason: "stop",
				Message: llm.Message{
					Content: `{"action":"buy","symbol":"BTC/USDT","size_pct":5,"confidence":0.7,"reasoning":"Order book pressure supports a small entry.","stop_loss":98,"take_profit":104}`,
				},
			},
		},
	}
	svc := newScalpingLLMDecisionProbeTestService(mockLLM)

	result, err := runScalpingLLMDecisionProbeWithService(context.Background(), svc, ScalpingLLMDecisionProbeOptions{
		RequireHealthy: true,
		RequireValid:   true,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Decision)
	require.Equal(t, "buy", result.Decision.Action)
	require.Equal(t, reasonCategoryStrategyEntry, result.Decision.ReasonCategory)
	require.NotEqual(t, reasonCategoryStrategyHold, result.Decision.ReasonCategory)
	require.False(t, result.LLMDegraded)
	require.Equal(t, reasonCategoryStrategyEntry, runtimeDiagnosticString(result.RuntimeDiagnostics, "last_reason_category"))
	require.NotNil(t, result.PaperTrade)
	require.Empty(t, result.PaperTradeError)
	require.Equal(t, "BTC/USDT", result.PaperTrade.Symbol)
	require.Equal(t, "buy", result.PaperTrade.Side)
	require.True(t, result.PaperTrade.Fees.GreaterThan(decimal.Zero))
	require.True(t, result.PaperTrade.NetPnL.GreaterThan(decimal.Zero))
	require.Equal(t, "win", result.PaperTrade.Outcome)
}

func TestRunScalpingLLMDecisionProbeWithServiceNormalizesContradictoryHoldSpreadReasoning(t *testing.T) {
	cases := []struct {
		name                   string
		mockResponseContent    string
		expectedDecisionReason string
	}{
		{
			name:                   "rewrites blanket wide-spread hold when a tradable signal exists",
			mockResponseContent:    `{"action":"hold","symbol":"","size_pct":0,"confidence":0,"reasoning":"All signals have spread > 0.25%, but BTC spread 0.02% is tradable; holding anyway.","stop_loss":null,"take_profit":null}`,
			expectedDecisionReason: "Holding because no analyzed setup cleared the effective confidence and risk gates; liquidity was not used as a blanket rejection reason.",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mockLLM := &MockLLMClient{
				Responses: []*llm.CompletionResponse{
					{
						Provider:     llm.Provider("deepseek"),
						Model:        "deepseek-chat",
						LatencyMs:    120,
						FinishReason: "stop",
						Message: llm.Message{
							Content: tc.mockResponseContent,
						},
					},
				},
			}
			svc := newScalpingLLMDecisionProbeTestService(mockLLM)

			result, err := runScalpingLLMDecisionProbeWithService(context.Background(), svc, ScalpingLLMDecisionProbeOptions{
				RequireHealthy: true,
				RequireValid:   true,
			})

			require.NoError(t, err)
			require.NotNil(t, result)
			require.NotNil(t, result.Decision)
			require.True(t, result.ContractValid)
			require.NotEmpty(t, result.ReasoningDiagnostics)
			require.Contains(t, result.ReasoningDiagnostics[0], "cites wide spread")
			require.Equal(t, tc.expectedDecisionReason, result.Decision.Reasoning)
			require.LessOrEqual(t, len([]rune(result.Decision.Reasoning)), 320)
		})
	}
}

func TestScalpingHoldSpreadReasoningDiagnosticsFlagsContradictions(t *testing.T) {
	cases := []struct {
		name      string
		reasoning string
		signals   []aiMarketSignal
		ceiling   float64
	}{
		{
			name:      "blanket wide-spread claim contradicts tradable symbol",
			reasoning: "All signals have spread > 0.25%, but BTC spread 0.02% is tradable; holding anyway.",
			signals:   []aiMarketSignal{{Symbol: "BTC/USDT", BidAskSpread: 0.02}},
			ceiling:   0.22,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			diagnostics := scalpingHoldSpreadReasoningDiagnostics(tc.reasoning, tc.signals, tc.ceiling)

			require.NotEmpty(t, diagnostics)
			require.Contains(t, diagnostics[0], "cites wide spread")
			require.Contains(t, diagnostics[0], "BTC/USDT")
		})
	}
}

func TestRunScalpingLLMDecisionProbeWithServiceFlagsLLMDegradation(t *testing.T) {
	svc := newScalpingLLMDecisionProbeTestService(&errorLLMClient{err: errors.New("provider exhausted")})

	result, err := runScalpingLLMDecisionProbeWithService(context.Background(), svc, ScalpingLLMDecisionProbeOptions{})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.LLMDegraded)
	require.Contains(t, runtimeDiagnosticString(result.RuntimeDiagnostics, "last_error"), "provider exhausted")
}

func newScalpingLLMDecisionProbeTestService(client llm.Client) *AIScalpingService {
	mockCCXT := &mockAIScalpingCCXT{
		markets: &ccxt.MarketsResponse{
			Exchange: "bitget",
			Symbols:  []string{"BTC/USDT"},
			Count:    1,
		},
		marketData: []ccxt.MarketPriceInterface{
			mockMarketPrice{
				symbol:    "BTC/USDT",
				price:     100,
				volume:    1_000_000,
				high24h:   105,
				low24h:    95,
				change24h: 1.5,
				bid:       99.99,
				ask:       100.01,
				exchange:  "bitget",
			},
		},
		orderBooks: map[string]*ccxt.OrderBookResponse{
			"BTC/USDT": {
				OrderBook: ccxt.OrderBook{
					Bids: []ccxt.OrderBookEntry{{Price: decimal.NewFromFloat(99.99), Amount: decimal.NewFromInt(5)}},
					Asks: []ccxt.OrderBookEntry{{Price: decimal.NewFromFloat(100.01), Amount: decimal.NewFromInt(4)}},
				},
			},
		},
	}
	return NewAIScalpingService(AIScalpingConfig{
		Exchange:             "bitget",
		Model:                "deepseek-chat",
		MaxTokens:            1200,
		Timeout:              10 * time.Second,
		MinConfidence:        0.30,
		MaxCapitalPct:        10,
		MaxPairsToAnalyze:    1,
		MaxCandidatePairs:    1,
		OrderBookPairs:       1,
		MaxBidAskSpreadPct:   0.25,
		AutoExpandOrderBooks: true,
		EnforceFutures:       false,
		PreTradeGate:         true,
	}, client, nil, mockCCXT, nil, nil)
}
