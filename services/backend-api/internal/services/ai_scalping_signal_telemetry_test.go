package services

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAnnotateDecisionSignalTelemetry(t *testing.T) {
	t.Run("matching symbol", func(t *testing.T) {
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
	})

	t.Run("nil decision", func(t *testing.T) {
		require.NotPanics(t, func() {
			annotateDecisionSignalTelemetry(nil, []aiMarketSignal{{Symbol: "BTC/USDT"}})
		})
	})

	t.Run("empty signals leave decision unchanged", func(t *testing.T) {
		decision := &AITradingDecision{
			Symbol:                   "BTC/USDT",
			Action:                   "buy",
			SignalBidAskSpreadPct:    0.123,
			SignalOrderBookImbalance: -0.456,
			SignalRangePosition24h:   78.9,
			SignalPriceChange24hPct:  1.23,
		}

		annotateDecisionSignalTelemetry(decision, nil)

		require.InDelta(t, 0.123, decision.SignalBidAskSpreadPct, 1e-9)
		require.InDelta(t, -0.456, decision.SignalOrderBookImbalance, 1e-9)
		require.InDelta(t, 78.9, decision.SignalRangePosition24h, 1e-9)
		require.InDelta(t, 1.23, decision.SignalPriceChange24hPct, 1e-9)
	})

	t.Run("unmatched symbol leaves zero values", func(t *testing.T) {
		decision := &AITradingDecision{Symbol: "UNMATCHED/USDT", Action: "sell"}

		annotateDecisionSignalTelemetry(decision, []aiMarketSignal{
			{
				Symbol:             "ETH/USDT",
				BidAskSpread:       0.02,
				OrderBookImbalance: -0.21,
				RangePosition24h:   62,
				PriceChange24h:     1.7,
			},
		})

		require.Zero(t, decision.SignalBidAskSpreadPct)
		require.Zero(t, decision.SignalOrderBookImbalance)
		require.Zero(t, decision.SignalRangePosition24h)
		require.Zero(t, decision.SignalPriceChange24hPct)
	})
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

func TestApplyDecisionSignalQualityToCycleRecord(t *testing.T) {
	decision := &AITradingDecision{
		SignalBidAskSpreadPct:    0.015,
		SignalOrderBookImbalance: 0.27,
		SignalRangePosition24h:   63.5,
		SignalPriceChange24hPct:  -1.9,
	}
	record := &CycleRecord{}

	applyDecisionSignalQualityToCycleRecord(record, decision)

	require.InDelta(t, decision.SignalBidAskSpreadPct, record.BidAskSpreadPct, 1e-9)
	require.InDelta(t, decision.SignalOrderBookImbalance, record.OrderBookImbalance, 1e-9)
	require.InDelta(t, decision.SignalRangePosition24h, record.RangePosition24h, 1e-9)
	require.InDelta(t, decision.SignalPriceChange24hPct, record.PriceChange24hPct, 1e-9)
}
