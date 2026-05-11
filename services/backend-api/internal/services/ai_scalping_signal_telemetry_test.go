package services

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAnnotateDecisionSignalTelemetry(t *testing.T) {
	decision := &AITradingDecision{Symbol: "BTC/USDT", Action: "buy"}
	signals := []aiMarketSignal{
		{
			Symbol:             "ETH/USDT",
			BidAskSpread:       0.02,
			OrderBookImbalance: -0.21,
			RangePosition24h:   62,
			PriceChange24h:     1.7,
		},
		{
			Symbol:             "BTC/USDT",
			BidAskSpread:       0.031,
			OrderBookImbalance: 0.42,
			RangePosition24h:   38.5,
			PriceChange24h:     -0.84,
		},
	}

	annotateDecisionSignalTelemetry(decision, signals)

	require.InDelta(t, 0.031, decision.SignalBidAskSpreadPct, 1e-9)
	require.InDelta(t, 0.42, decision.SignalOrderBookImbalance, 1e-9)
	require.InDelta(t, 38.5, decision.SignalRangePosition24h, 1e-9)
	require.InDelta(t, -0.84, decision.SignalPriceChange24hPct, 1e-9)
}

func TestCopyPreTradeTelemetryCopiesSignalQuality(t *testing.T) {
	source := &AITradingDecision{
		PreTradeRegime:               "trend",
		PreTradeExpectancy:           0.12,
		PreTradeExpectancySampleSize: 18,
		SignalBidAskSpreadPct:        0.025,
		SignalOrderBookImbalance:     -0.37,
		SignalRangePosition24h:       71,
		SignalPriceChange24hPct:      2.4,
	}
	target := &AITradingDecision{}

	copyPreTradeTelemetry(target, source)

	require.Equal(t, source.PreTradeRegime, target.PreTradeRegime)
	require.InDelta(t, source.PreTradeExpectancy, target.PreTradeExpectancy, 1e-9)
	require.Equal(t, source.PreTradeExpectancySampleSize, target.PreTradeExpectancySampleSize)
	require.InDelta(t, source.SignalBidAskSpreadPct, target.SignalBidAskSpreadPct, 1e-9)
	require.InDelta(t, source.SignalOrderBookImbalance, target.SignalOrderBookImbalance, 1e-9)
	require.InDelta(t, source.SignalRangePosition24h, target.SignalRangePosition24h, 1e-9)
	require.InDelta(t, source.SignalPriceChange24hPct, target.SignalPriceChange24hPct, 1e-9)
}
