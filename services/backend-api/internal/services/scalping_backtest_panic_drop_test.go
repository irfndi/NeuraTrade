package services

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newPanicDropTestEngine(enabled bool, minDropPct float64) *ScalpingBacktestEngine {
	return NewScalpingBacktestEngine(nil, ScalpingBacktestConfig{
		StartTime:            time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		EndTime:              time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
		Symbols:              []string{"X/USDT"},
		Exchange:             "binance",
		InitialCapital:       decimal.NewFromInt(10000),
		FeeRate:              decimal.NewFromFloat(0.001),
		SlippagePct:          decimal.NewFromFloat(0.000001),
		MaxBidAskSpreadPct:   1,
		MinConfidence:        0.55,
		MinExpectancyN:       99,
		MaxCapitalPct:        100,
		DefaultHoldPeriod:    1 * time.Hour,
		MaxLossPct:           0.007,
		EnablePanicDropEntry: enabled,
		MinPanicDropPct:      minDropPct,
	})
}

// makePanicDropSignal builds a signal with BB%B NOT oversold (so BB%B cases won't fire)
// but with a large RecentPriceChange drop. When EnablePanicDropEntry is true, the panic
// drop case should fire (action=buy). When false, the signal should be rejected.
func makePanicDropSignal(t time.Time, recentChangePct float64, known bool) HistoricalSignal {
	return HistoricalSignal{
		Timestamp: t,
		Symbol:    "X/USDT",
		Exchange:  "binance",
		Signal: MarketSignal{
			Symbol:             "X/USDT",
			Price:              100,
			Low:                99,
			High:               101,
			BidAskSpread:       0.001,
			OrderBookImbalance: 0.5,
			RangePosition24h:   30,
			Volume24h:          1000000,
			BBPercentB:         0.5, // NOT oversold — BB%B cases won't fire
			RecentPriceChange:  recentChangePct,
			RecentChangeKnown:  known,
		},
	}
}

func TestScalpingBacktest_PanicDrop_FiresOnLargeDrop(t *testing.T) {
	engine := newPanicDropTestEngine(true, 0.02)
	signals := []HistoricalSignal{
		makePanicDropSignal(time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC), -0.05, true), // 5% drop
	}

	result, err := engine.RunSignals(context.Background(), signals)
	require.NoError(t, err)
	require.NotNil(t, result)

	// The panic drop case should have fired (action=buy), so the signal
	// should NOT be rejected by the BB%B / no-action logic.
	require.GreaterOrEqual(t, len(result.Signals), 1)
	sig := result.Signals[0]
	assert.NotEqual(t, "no_action", sig.RejectionReason,
		"panic drop with 5%% drop and BB%%b=0.5 should fire, not be rejected as no_action")
}

func TestScalpingBacktest_PanicDrop_DisabledByDefault(t *testing.T) {
	engine := newPanicDropTestEngine(false, 0.02)
	signals := []HistoricalSignal{
		makePanicDropSignal(time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC), -0.05, true),
	}

	result, err := engine.RunSignals(context.Background(), signals)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.GreaterOrEqual(t, len(result.Signals), 1)
	sig := result.Signals[0]
	// When panic drop is disabled, BB%B=0.5 (not oversold) should reject the signal.
	assert.Equal(t, "no_directional_edge", sig.RejectionReason,
		"panic drop disabled + BB%%b=0.5 should be rejected as no_directional_edge")
}

func TestScalpingBacktest_PanicDrop_RejectsSmallDrop(t *testing.T) {
	engine := newPanicDropTestEngine(true, 0.02) // threshold 2%
	signals := []HistoricalSignal{
		makePanicDropSignal(time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC), -0.01, true), // only 1% drop
	}

	result, err := engine.RunSignals(context.Background(), signals)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.GreaterOrEqual(t, len(result.Signals), 1)
	sig := result.Signals[0]
	// 1% drop is below the 2% threshold, and BB%B=0.5 won't fire either.
	assert.Equal(t, "no_directional_edge", sig.RejectionReason,
		"panic drop with 1%% drop below 2%% threshold should be rejected")
}

func TestScalpingBacktest_PanicDrop_RejectsUnknownChange(t *testing.T) {
	engine := newPanicDropTestEngine(true, 0.02)
	signals := []HistoricalSignal{
		makePanicDropSignal(time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC), 0, false), // unknown
	}

	result, err := engine.RunSignals(context.Background(), signals)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.GreaterOrEqual(t, len(result.Signals), 1)
	sig := result.Signals[0]
	// RecentChangeKnown=false should prevent the panic drop case from firing.
	assert.Equal(t, "no_directional_edge", sig.RejectionReason,
		"panic drop with RecentChangeKnown=false should be rejected")
}
