package scalping

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type mockOrderBookMetrics struct {
	spread       decimal.Decimal
	imbalance    decimal.Decimal
	midPrice     decimal.Decimal
	bestBid      decimal.Decimal
	bestAsk      decimal.Decimal
	liquidity    decimal.Decimal
	bidDepth1Pct decimal.Decimal
	askDepth1Pct decimal.Decimal
}

func (m mockOrderBookMetrics) GetSpreadPct() decimal.Decimal      { return m.spread }
func (m mockOrderBookMetrics) GetImbalance1Pct() decimal.Decimal  { return m.imbalance }
func (m mockOrderBookMetrics) GetMidPrice() decimal.Decimal       { return m.midPrice }
func (m mockOrderBookMetrics) GetBestBid() decimal.Decimal        { return m.bestBid }
func (m mockOrderBookMetrics) GetBestAsk() decimal.Decimal        { return m.bestAsk }
func (m mockOrderBookMetrics) GetLiquidityScore() decimal.Decimal { return m.liquidity }
func (m mockOrderBookMetrics) GetBidDepth1Pct() decimal.Decimal   { return m.bidDepth1Pct }
func (m mockOrderBookMetrics) GetAskDepth1Pct() decimal.Decimal   { return m.askDepth1Pct }

func TestScalpingSignalComposer_DirectionalSellSignalsOutweighLiquidityBias(t *testing.T) {
	composer := NewScalpingComposer(nil)

	signal, err := composer.ComposeSignal(context.Background(), OHLCVData{
		Exchange:  "bitget",
		Symbol:    "DOGE/USDT",
		Timeframe: "1m",
		Candles: []OHLCVCandle{
			testCandle(0, "1.0000"),
			testCandle(1, "0.9960"),
			testCandle(2, "0.9910"),
			testCandle(3, "0.9850"),
			testCandle(4, "0.9780"),
		},
	}, mockOrderBookMetrics{
		spread:    decimal.RequireFromString("0.04"),
		imbalance: decimal.RequireFromString("-0.31"),
		midPrice:  decimal.RequireFromString("0.9780"),
		bestBid:   decimal.RequireFromString("0.9778"),
		bestAsk:   decimal.RequireFromString("0.9782"),
		liquidity: decimal.RequireFromString("80"),
	})

	require.NoError(t, err)
	require.Equal(t, DirectionSell, signal.Direction)
	require.True(t, signal.Confidence.GreaterThan(decimal.RequireFromString("0.45")))
	require.True(t, signal.AttributionWeights["trend"].IsNegative())
	require.True(t, signal.AttributionWeights["imbalance"].IsNegative())
}

func TestScalpingSignalComposer_BalancedComponentsHoldWhenMarginTooSmall(t *testing.T) {
	composer := NewScalpingComposer(nil)

	signal, err := composer.ComposeSignal(context.Background(), OHLCVData{
		Exchange:  "bitget",
		Symbol:    "ETH/USDT",
		Timeframe: "1m",
		Candles: []OHLCVCandle{
			testCandle(0, "100.00"),
			testCandle(1, "100.20"),
			testCandle(2, "100.10"),
			testCandle(3, "100.25"),
			testCandle(4, "100.15"),
		},
	}, mockOrderBookMetrics{
		spread:    decimal.RequireFromString("0.12"),
		imbalance: decimal.RequireFromString("-0.06"),
		midPrice:  decimal.RequireFromString("100.15"),
		bestBid:   decimal.RequireFromString("100.14"),
		bestAsk:   decimal.RequireFromString("100.16"),
		liquidity: decimal.RequireFromString("55"),
	})

	require.NoError(t, err)
	require.Equal(t, DirectionHold, signal.Direction)
	require.True(t, signal.Confidence.IsZero())
}

func testCandle(offsetMinutes int, close string) OHLCVCandle {
	price := decimal.RequireFromString(close)
	return OHLCVCandle{
		Timestamp: time.Unix(1700000000+int64(offsetMinutes*60), 0).UTC(),
		Open:      price,
		High:      price.Mul(decimal.RequireFromString("1.001")),
		Low:       price.Mul(decimal.RequireFromString("0.999")),
		Close:     price,
		Volume:    decimal.RequireFromString("1000"),
	}
}
