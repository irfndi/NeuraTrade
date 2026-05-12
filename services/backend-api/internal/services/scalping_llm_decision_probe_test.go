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
