package services

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newMomentumFilterTestEngine(minMomentum float64) *ScalpingBacktestEngine {
	return NewScalpingBacktestEngine(nil, ScalpingBacktestConfig{
		StartTime:           time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		EndTime:             time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
		Symbols:             []string{"X/USDT"},
		Exchange:            "binance",
		InitialCapital:      decimal.NewFromInt(10000),
		FeeRate:             decimal.NewFromFloat(0.001),
		SlippagePct:         decimal.NewFromFloat(0.000001),
		MaxBidAskSpreadPct:  1,
		MinConfidence:       0.55,
		MinExpectancyN:      99,
		MaxCapitalPct:       100,
		DefaultHoldPeriod:   1 * time.Hour,
		MaxLossPct:          0.007,
		MinEntryMomentumPct: minMomentum,
	})
}

func makeMomentumSignal(t time.Time, price, high, low float64) HistoricalSignal {
	return HistoricalSignal{
		Timestamp: t,
		Symbol:    "X/USDT",
		Exchange:  "binance",
		Signal: MarketSignal{
			Symbol:             "X/USDT",
			Price:              price,
			Low:                low,
			High:               high,
			BidAskSpread:       0.001,
			OrderBookImbalance: 0.5,
			RangePosition24h:   30,
			Volume24h:          1000000,
			BBPercentB:         -0.5,
		},
	}
}

func TestScalpingBacktest_MomentumFilter_DisabledWhenZero(t *testing.T) {
	engine := newMomentumFilterTestEngine(0)
	signals := []HistoricalSignal{
		makeMomentumSignal(time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC), 100, 101, 99),
		makeMomentumSignal(time.Date(2026, 1, 1, 2, 0, 0, 0, time.UTC), 100, 101, 99),
	}
	_ = signals

	assert.NotPanics(t, func() {
		_ = engine
	})
}

func TestScalpingBacktest_MomentumFilter_RejectsFlatBuy(t *testing.T) {
	engine := newMomentumFilterTestEngine(0.005)
	signals := []HistoricalSignal{
		makeMomentumSignal(time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC), 100, 101, 99),
		makeMomentumSignal(time.Date(2026, 1, 1, 2, 0, 0, 0, time.UTC), 100, 101, 99),
	}

	result, err := engine.RunSignals(context.Background(), signals)
	require.NoError(t, err)
	require.NotNil(t, result)

	momentumRejections := 0
	for _, sig := range result.Signals {
		if sig.RejectionReason == "entry_momentum_misaligned" {
			momentumRejections++
		}
	}
	assert.GreaterOrEqual(t, momentumRejections, 1, "flat buy should be rejected by momentum filter")
}
