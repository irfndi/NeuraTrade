package services

import (
	"context"
	"encoding/json"
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

func TestScalpingLLMSignalSnapshotsIncludeDecisionTelemetry(t *testing.T) {
	observedAt := time.Now().UTC()
	snapshots := scalpingLLMSignalSnapshots([]aiMarketSignal{{
		Symbol:             "BTC/USDT",
		Price:              100,
		High24h:            110,
		Low24h:             98,
		Volume24h:          1_000_000,
		BidAskSpread:       0.02,
		OrderBookImbalance: 0.23,
		PriceChange24h:     1.5,
		RecentPriceChange:  0.08,
		RecentChangeAgeSec: 60,
		RecentChangeKnown:  true,
		RangePosition24h:   16.7,
		SuggestedAction:    "buy",
		ConfidenceHint:     0.68,
		CandidateScore:     0.74,
	}}, observedAt)

	require.Len(t, snapshots, 1)
	snapshot := snapshots[0]
	require.Equal(t, "BTC/USDT", snapshot.Symbol)
	require.True(t, snapshot.Price.Equal(decimal.NewFromInt(100)))
	require.Equal(t, 110.0, snapshot.High24h)
	require.Equal(t, 98.0, snapshot.Low24h)
	require.Equal(t, 1_000_000.0, snapshot.Volume24h)
	require.Equal(t, 0.02, snapshot.BidAskSpread)
	require.Equal(t, 0.23, snapshot.OrderBookImbalance)
	require.Equal(t, 1.5, snapshot.PriceChange24h)
	require.Equal(t, 0.08, snapshot.RecentPriceChange)
	require.Equal(t, 60.0, snapshot.RecentChangeAgeSec)
	require.True(t, snapshot.RecentChangeKnown)
	require.Equal(t, 16.7, snapshot.RangePosition24h)
	require.Equal(t, "buy", snapshot.SuggestedAction)
	require.Equal(t, 0.68, snapshot.ConfidenceHint)
	require.Equal(t, 0.74, snapshot.CandidateScore)
	require.Equal(t, observedAt, snapshot.ObservedAt)
}

func TestScalpingLLMSignalSnapshotsMarshalKnownZeroTelemetry(t *testing.T) {
	snapshots := scalpingLLMSignalSnapshots([]aiMarketSignal{{
		Symbol:            "BTC/USDT",
		Price:             100,
		RecentChangeKnown: true,
	}}, time.Now().UTC())

	payload, err := json.Marshal(snapshots[0])
	require.NoError(t, err)
	require.Contains(t, string(payload), `"spread_pct":0`)
	require.Contains(t, string(payload), `"recent_price_change_pct":0`)
	require.Contains(t, string(payload), `"recent_change_known":true`)
}

func TestRunScalpingLLMDecisionProbeWithServiceSnapshotsDecisionHints(t *testing.T) {
	mockLLM := &MockLLMClient{
		Responses: []*llm.CompletionResponse{
			{
				Provider:     llm.Provider("deepseek"),
				Model:        "deepseek-chat",
				LatencyMs:    120,
				FinishReason: "stop",
				Message: llm.Message{
					Content: `{"action":"hold","symbol":"","size_pct":0,"confidence":0,"reasoning":"Waiting for a cleaner edge.","stop_loss":null,"take_profit":null}`,
				},
			},
		},
	}
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
				volume:    2_500_000,
				high24h:   110,
				low24h:    98,
				change24h: 1.5,
				bid:       99.99,
				ask:       100.01,
				exchange:  "bitget",
			},
		},
		orderBooks: map[string]*ccxt.OrderBookResponse{
			"BTC/USDT": {
				OrderBook: ccxt.OrderBook{
					Bids: []ccxt.OrderBookEntry{{Price: decimal.NewFromFloat(99.99), Amount: decimal.NewFromInt(8)}},
					Asks: []ccxt.OrderBookEntry{{Price: decimal.NewFromFloat(100.01), Amount: decimal.NewFromInt(1)}},
				},
			},
		},
	}
	svc := NewAIScalpingService(AIScalpingConfig{
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
	}, mockLLM, nil, mockCCXT, nil, nil)

	result, err := runScalpingLLMDecisionProbeWithService(context.Background(), svc, ScalpingLLMDecisionProbeOptions{
		RequireHealthy: true,
		RequireValid:   true,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.SignalSnapshots, 1)
	require.Equal(t, "buy", result.SignalSnapshots[0].SuggestedAction)
	require.GreaterOrEqual(t, result.SignalSnapshots[0].ConfidenceHint, 0.30)
	require.Greater(t, result.SignalSnapshots[0].CandidateScore, 0.0)
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
	require.True(t, result.PaperTrade.EntryPrice.GreaterThan(decimal.NewFromInt(100)))
	require.True(t, result.PaperTrade.Fees.IsZero())
	require.True(t, result.PaperTrade.NetPnL.IsZero())
	require.Equal(t, "open", result.PaperTrade.Outcome)
	require.Equal(t, "exit_unobserved", result.PaperTrade.ExitReason)
	require.False(t, result.PaperTrade.ExitObserved)
}

func TestRunScalpingLLMDecisionProbeWithServiceUsesSeededRecentMomentum(t *testing.T) {
	mockLLM := &MockLLMClient{
		Responses: []*llm.CompletionResponse{
			{
				Provider:     llm.Provider("deepseek"),
				Model:        "deepseek-chat",
				LatencyMs:    120,
				FinishReason: "stop",
				Message: llm.Message{
					Content: `{"action":"buy","symbol":"BTC/USDT","size_pct":5,"confidence":0.7,"reasoning":"Recent momentum confirms the low-range buy.","stop_loss":98,"take_profit":104}`,
				},
			},
		},
	}
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
				high24h:   110,
				low24h:    96,
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
	svc := NewAIScalpingService(AIScalpingConfig{
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
	}, mockLLM, nil, mockCCXT, nil, nil)
	svc.seedScalpingSignalObservationHistory([]ScalpingLLMSignalSnapshot{{
		Symbol:     "BTC/USDT",
		Price:      decimal.NewFromFloat(99),
		ObservedAt: time.Now().UTC().Add(-time.Minute),
	}})

	result, err := runScalpingLLMDecisionProbeWithService(context.Background(), svc, ScalpingLLMDecisionProbeOptions{
		RequireHealthy: true,
		RequireValid:   true,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.ContractValid)
	require.True(t, result.PreTradeGateAllowed)
	require.NotNil(t, result.PaperTrade)
	require.NotNil(t, result.Decision)
	require.True(t, result.Decision.SignalRecentChangeKnown)
	require.Greater(t, result.Decision.SignalRecentPriceChangePct, 0.05)
}

func TestRunScalpingLLMDecisionProbeWithServiceNormalizesContradictoryHoldSpreadReasoning(t *testing.T) {
	cases := []struct {
		name                          string
		mockResponseContent           string
		expectedDecisionReason        string
		expectedRawDiagnosticContains string
	}{
		{
			name:                          "rewrites blanket wide-spread hold when a tradable signal exists",
			mockResponseContent:           `{"action":"hold","symbol":"","size_pct":0,"confidence":0,"reasoning":"All signals have spread > 0.25%, but BTC spread 0.02% is tradable; holding anyway.","stop_loss":null,"take_profit":null}`,
			expectedDecisionReason:        "Holding because no analyzed setup cleared the effective confidence and risk gates; liquidity was not used as a blanket rejection reason.",
			expectedRawDiagnosticContains: "cites wide spread",
		},
		{
			name:                          "rewrites diagnostic hold after preserving raw diagnostics",
			mockResponseContent:           `{"action":"hold","symbol":"","size_pct":0,"confidence":0,"reasoning":"BTC confidence_hint is strong, but holding anyway.","stop_loss":null,"take_profit":null}`,
			expectedDecisionReason:        "Holding because no analyzed setup cleared the effective confidence and risk gates.",
			expectedRawDiagnosticContains: "absent confidence_hint",
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
			require.Empty(t, result.ReasoningDiagnostics)
			require.NotEmpty(t, result.RawReasoningDiagnostics)
			require.Contains(t, result.RawReasoningDiagnostics[0], tc.expectedRawDiagnosticContains)
			require.Equal(t, tc.expectedDecisionReason, result.Decision.Reasoning)
			require.LessOrEqual(t, len([]rune(result.Decision.Reasoning)), 320)
		})
	}
}

func TestScalpingHoldSpreadReasoningDiagnosticsFlagsContradictions(t *testing.T) {
	cases := []struct {
		name            string
		reasoning       string
		signals         []aiMarketSignal
		ceiling         float64
		wantDiagnostics bool
		wantSymbol      string
	}{
		{
			name:            "blanket wide-spread claim contradicts tradable symbol",
			reasoning:       "All signals have spread > 0.25%, but BTC spread 0.02% is tradable; holding anyway.",
			signals:         []aiMarketSignal{{Symbol: "BTC/USDT", BidAskSpread: 0.02}},
			ceiling:         0.22,
			wantDiagnostics: true,
			wantSymbol:      "BTC/USDT",
		},
		{
			name:            "generic confidence threshold does not imply wide spread",
			reasoning:       "BTC spread 0.02% is tradable, but confidence > floor is not enough for entry.",
			signals:         []aiMarketSignal{{Symbol: "BTC/USDT", BidAskSpread: 0.02}},
			ceiling:         0.22,
			wantDiagnostics: false,
		},
		{
			name:            "generic momentum threshold does not imply wide spread",
			reasoning:       "BTC spread is within the gate, but momentum remains above the neutral threshold.",
			signals:         []aiMarketSignal{{Symbol: "BTC/USDT", BidAskSpread: 0.02}},
			ceiling:         0.22,
			wantDiagnostics: false,
		},
		{
			name:            "buy safety spread threshold does not imply liquidity ceiling contradiction",
			reasoning:       "No signal meets all buy safety gates: BTC spread 0.07% exceeds 0.06%, so hold.",
			signals:         []aiMarketSignal{{Symbol: "BTC/USDT", BidAskSpread: 0.07}},
			ceiling:         0.22,
			wantDiagnostics: false,
		},
		{
			name:            "buy gate shorthand spread threshold does not imply liquidity ceiling contradiction",
			reasoning:       "No signal meets all buy gates: BTC has wide spread 0.11% above 0.06%, so hold.",
			signals:         []aiMarketSignal{{Symbol: "BTC/USDT", BidAskSpread: 0.11}},
			ceiling:         0.22,
			wantDiagnostics: false,
		},
		{
			name:            "buy gate scoped symbol spread does not apply to unrelated symbol",
			reasoning:       "No symbol meets all buy safety gates: BTC spread ok but range_pos_24h >35; ETH spread too wide.",
			signals:         []aiMarketSignal{{Symbol: "BTC/USDT", BidAskSpread: 0.02}, {Symbol: "ETH/USDT", BidAskSpread: 0.08}},
			ceiling:         0.22,
			wantDiagnostics: false,
		},
		{
			name:            "buy gate blanket spread claim uses buy safety threshold",
			reasoning:       "No symbols meet buy safety gates because all signals have spread too wide.",
			signals:         []aiMarketSignal{{Symbol: "BTC/USDT", BidAskSpread: 0.02}},
			ceiling:         0.22,
			wantDiagnostics: true,
			wantSymbol:      "BTC/USDT",
		},
		{
			name:            "short symbol does not match unrelated word",
			reasoning:       "Holding because consolidation remains broad and spread is above the comfort threshold.",
			signals:         []aiMarketSignal{{Symbol: "SOL/USDT", BidAskSpread: 0.02}},
			ceiling:         0.22,
			wantDiagnostics: false,
		},
		{
			name:            "short symbol matches whole token",
			reasoning:       "SOL spread is above the comfort threshold.",
			signals:         []aiMarketSignal{{Symbol: "SOL/USDT", BidAskSpread: 0.02}},
			ceiling:         0.22,
			wantDiagnostics: true,
			wantSymbol:      "SOL/USDT",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			diagnostics := scalpingHoldSpreadReasoningDiagnostics(tc.reasoning, tc.signals, tc.ceiling)

			if !tc.wantDiagnostics {
				require.Empty(t, diagnostics)
				return
			}
			require.NotEmpty(t, diagnostics)
			require.Contains(t, diagnostics[0], "cites wide spread")
			require.Contains(t, diagnostics[0], tc.wantSymbol)
		})
	}
}

func TestScalpingPctUnitReasoningDiagnosticsFlagsInflatedPercentPoints(t *testing.T) {
	signals := []aiMarketSignal{{
		Symbol:             "ONDO/USDT",
		PriceChange24h:     0.04821,
		RecentPriceChange:  0.0276,
		RecentChangeKnown:  true,
		OrderBookImbalance: 0.4,
	}}

	diagnostics := scalpingPctUnitReasoningDiagnostics(
		"ONDO has positive 24h momentum of 4.821% and recent momentum of 2.76%, so buy.",
		signals,
	)

	require.Len(t, diagnostics, 2)
	require.Contains(t, diagnostics[0], "recent_price_change_pct")
	require.Contains(t, diagnostics[0], "ONDO/USDT")
	require.Contains(t, diagnostics[1], "price_change_24h_pct")
	require.Contains(t, diagnostics[1], "ONDO/USDT")
}

func TestScalpingPctUnitReasoningDiagnosticsAllowsCorrectPercentPoints(t *testing.T) {
	signals := []aiMarketSignal{{
		Symbol:             "ONDO/USDT",
		PriceChange24h:     0.04821,
		RecentPriceChange:  0.0276,
		RecentChangeKnown:  true,
		OrderBookImbalance: 0.4,
	}}

	diagnostics := scalpingPctUnitReasoningDiagnostics(
		"ONDO has price_change_24h_pct 0.04821% and recent_price_change_pct 0.0276%, below the buy gates.",
		signals,
	)

	require.Empty(t, diagnostics)
}

func TestScalpingPctUnitReasoningDiagnosticsIgnoresThresholdSubstrings(t *testing.T) {
	signals := []aiMarketSignal{{
		Symbol:         "CHZ/USDT",
		PriceChange24h: -0.06,
	}}

	diagnostics := scalpingPctUnitReasoningDiagnostics(
		"CHZ spread 0.065% > 0.06% and price_change_24h_pct -0.060% fails the buy gate.",
		signals,
	)

	require.Empty(t, diagnostics)
}

func TestContainsStandalonePctCitationRejectsEmbeddedPercent(t *testing.T) {
	require.False(t, containsStandalonePctCitation("spread threshold is 0.06%", "6%"))
	require.False(t, containsStandalonePctCitation("range is 16%", "6%"))
	require.True(t, containsStandalonePctCitation("momentum is 6%", "6%"))
	require.True(t, containsStandalonePctCitation("momentum is -6%", "6%"))
}

func TestNormalizeDiagnosticHoldReasoningRewritesPctUnitConfusion(t *testing.T) {
	decision := &AITradingDecision{
		Action:    "hold",
		Reasoning: "CHZ price_change_24h_pct is -5.412%, then corrected to -0.05412%, so hold.",
	}
	signals := []aiMarketSignal{{
		Symbol:         "CHZ/USDT",
		PriceChange24h: -0.05412,
	}}

	require.NotEmpty(t, scalpingPctUnitReasoningDiagnostics(decision.Reasoning, signals))
	normalizeDiagnosticHoldReasoning(decision, signals)

	require.Equal(t, "Holding because no analyzed setup cleared the effective confidence and risk gates.", decision.Reasoning)
	require.Empty(t, scalpingPctUnitReasoningDiagnostics(decision.Reasoning, signals))
}

func TestScalpingHintReasoningDiagnosticsFlagsInventedHints(t *testing.T) {
	signals := []aiMarketSignal{{
		Symbol:             "ONDO/USDT",
		OrderBookImbalance: 0.4,
	}}

	diagnostics := scalpingHintReasoningDiagnostics(
		"ONDO confidence_hint suggests buy and candidate_score is strong, so take the setup.",
		signals,
	)

	require.Len(t, diagnostics, 2)
	require.Contains(t, diagnostics[0], "absent confidence_hint")
	require.Contains(t, diagnostics[1], "absent candidate_score")
}

func TestScalpingHintReasoningDiagnosticsAllowsProvidedHintsAndNegatedMentions(t *testing.T) {
	signals := []aiMarketSignal{{
		Symbol:          "ONDO/USDT",
		SuggestedAction: "buy",
		ConfidenceHint:  0.65,
		CandidateScore:  0.24,
	}}

	require.Empty(t, scalpingHintReasoningDiagnostics(
		"ONDO confidence_hint 0.65 and suggested_action buy are present, but range blocks the setup.",
		signals,
	))
	require.Empty(t, scalpingHintReasoningDiagnostics(
		"ONDO confidence_hint not provided and candidate_score is missing, so hold.",
		[]aiMarketSignal{{Symbol: "ONDO/USDT"}},
	))
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
				high24h:   110,
				low24h:    98,
				change24h: 1.5,
				bid:       99.99,
				ask:       100.01,
				exchange:  "bitget",
			},
		},
		// Orderbook with equal bid/ask amounts produces |OrderBookImbalance|=0,
		// keeping the signal below the deterministic-fallback MinImbalance floor
		// so signalsWithDecisionHints does not pre-populate a ConfidenceHint.
		// This preserves the test scenario where the LLM's reasoning references
		// an absent confidence_hint (PR-2 relaxed the default imbalance floor
		// from 0.35 to 0.10; the prior 5-vs-4 split produced 0.111 which
		// short-circuited the diagnostic chain under the new default).
		orderBooks: map[string]*ccxt.OrderBookResponse{
			"BTC/USDT": {
				OrderBook: ccxt.OrderBook{
					Bids: []ccxt.OrderBookEntry{{Price: decimal.NewFromFloat(99.99), Amount: decimal.NewFromInt(1)}},
					Asks: []ccxt.OrderBookEntry{{Price: decimal.NewFromFloat(100.01), Amount: decimal.NewFromInt(1)}},
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
