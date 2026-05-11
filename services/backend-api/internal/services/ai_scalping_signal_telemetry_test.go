package services

import (
	"testing"

	appautonomy "github.com/irfndi/neuratrade/internal/app/autonomy"
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

		require.True(t, decision.SignalQualityKnown)
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
			SignalQualityKnown:       true,
		}

		annotateDecisionSignalTelemetry(decision, nil)

		require.True(t, decision.SignalQualityKnown)
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

		require.False(t, decision.SignalQualityKnown)
		require.Zero(t, decision.SignalBidAskSpreadPct)
		require.Zero(t, decision.SignalOrderBookImbalance)
		require.Zero(t, decision.SignalRangePosition24h)
		require.Zero(t, decision.SignalPriceChange24hPct)
	})

	t.Run("perp symbol uses normalized exact match before fuzzy fallback", func(t *testing.T) {
		decision := &AITradingDecision{Symbol: "BTC/USDT", Action: "buy"}

		annotateDecisionSignalTelemetry(decision, []aiMarketSignal{
			{
				Symbol:             "BTCDOM/USDT:USDT",
				BidAskSpread:       0.19,
				OrderBookImbalance: -0.91,
				RangePosition24h:   12,
				PriceChange24h:     -3.4,
			},
			{
				Symbol:             "BTC/USDT:USDT",
				BidAskSpread:       0.031,
				OrderBookImbalance: 0.42,
				RangePosition24h:   38.5,
				PriceChange24h:     -0.84,
			},
		})

		require.True(t, decision.SignalQualityKnown)
		require.InDelta(t, 0.031, decision.SignalBidAskSpreadPct, 1e-9)
		require.InDelta(t, 0.42, decision.SignalOrderBookImbalance, 1e-9)
		require.InDelta(t, 38.5, decision.SignalRangePosition24h, 1e-9)
		require.InDelta(t, -0.84, decision.SignalPriceChange24hPct, 1e-9)
	})
}

func TestCopyPreTradeTelemetryCopiesSignalQuality(t *testing.T) {
	source := &AITradingDecision{
		PreTradeRegime:               "trend",
		PreTradeExpectancy:           0.12,
		PreTradeExpectancySampleSize: 18,
		SignalQualityKnown:           true,
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
	require.True(t, target.SignalQualityKnown)
	require.InDelta(t, source.SignalBidAskSpreadPct, target.SignalBidAskSpreadPct, 1e-9)
	require.InDelta(t, source.SignalOrderBookImbalance, target.SignalOrderBookImbalance, 1e-9)
	require.InDelta(t, source.SignalRangePosition24h, target.SignalRangePosition24h, 1e-9)
	require.InDelta(t, source.SignalPriceChange24hPct, target.SignalPriceChange24hPct, 1e-9)
}

func TestApplyDecisionSignalQualityToCycleRecord(t *testing.T) {
	decision := &AITradingDecision{
		SignalQualityKnown:       true,
		SignalBidAskSpreadPct:    0.015,
		SignalOrderBookImbalance: 0.27,
		SignalRangePosition24h:   63.5,
		SignalPriceChange24hPct:  -1.9,
	}
	record := &CycleRecord{}

	applyDecisionSignalQualityToCycleRecord(record, decision)

	require.NotNil(t, record.BidAskSpreadPct)
	require.NotNil(t, record.OrderBookImbalance)
	require.NotNil(t, record.RangePosition24h)
	require.NotNil(t, record.PriceChange24hPct)
	require.InDelta(t, decision.SignalBidAskSpreadPct, *record.BidAskSpreadPct, 1e-9)
	require.InDelta(t, decision.SignalOrderBookImbalance, *record.OrderBookImbalance, 1e-9)
	require.InDelta(t, decision.SignalRangePosition24h, *record.RangePosition24h, 1e-9)
	require.InDelta(t, decision.SignalPriceChange24hPct, *record.PriceChange24hPct, 1e-9)
}

func TestApplyDecisionSignalQualityToCycleRecordSkipsUnknownSignalQuality(t *testing.T) {
	record := &CycleRecord{}

	applyDecisionSignalQualityToCycleRecord(record, &AITradingDecision{
		SignalBidAskSpreadPct:    0.015,
		SignalOrderBookImbalance: 0.27,
		SignalRangePosition24h:   63.5,
		SignalPriceChange24hPct:  -1.9,
	})

	require.Nil(t, record.BidAskSpreadPct)
	require.Nil(t, record.OrderBookImbalance)
	require.Nil(t, record.RangePosition24h)
	require.Nil(t, record.PriceChange24hPct)
}

func TestApplyDecisionSignalQualityToCycleRecordUsesTopRejectionWhenSignalUnknown(t *testing.T) {
	record := &CycleRecord{}
	decision := &AITradingDecision{
		CandidateFunnel: appautonomy.CandidateFunnelSnapshot{
			TopCandidateRejections: []appautonomy.CandidateRejection{
				{
					Symbol:             "WIF/USDT",
					Reason:             appautonomy.CandidateRejectSpreadTooWide,
					BidAskSpreadPct:    0.31,
					OrderBookImbalance: -0.18,
					RangePosition24h:   63.5,
					PriceChange24hPct:  -1.2,
				},
			},
		},
	}

	applyDecisionSignalQualityToCycleRecord(record, decision)

	require.NotNil(t, record.BidAskSpreadPct)
	require.NotNil(t, record.OrderBookImbalance)
	require.NotNil(t, record.RangePosition24h)
	require.NotNil(t, record.PriceChange24hPct)
	require.InDelta(t, 0.31, *record.BidAskSpreadPct, 1e-9)
	require.InDelta(t, -0.18, *record.OrderBookImbalance, 1e-9)
	require.InDelta(t, 63.5, *record.RangePosition24h, 1e-9)
	require.InDelta(t, -1.2, *record.PriceChange24hPct, 1e-9)
}
