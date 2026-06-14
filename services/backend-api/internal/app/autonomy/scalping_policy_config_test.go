package autonomy

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestApplyScalpingPolicyConfigFromEnv_ReadsNewThresholdFields(t *testing.T) {
	t.Setenv("NEURATRADE_SCALPING_STRONG_IMBALANCE_FLOOR", "0.05")
	t.Setenv("NEURATRADE_SCALPING_NEUTRAL_IMBALANCE_FLOOR", "0.03")
	t.Setenv("NEURATRADE_SCALPING_BUY_RANGE_MAX", "70.0")
	t.Setenv("NEURATRADE_SCALPING_SELL_RANGE_MIN", "20.0")
	t.Setenv("NEURATRADE_SCALPING_CONTINUATION_RANGE_BUFFER", "8.0")
	t.Setenv("NEURATRADE_SCALPING_BREAKDOWN_SELL_RANGE_MIN", "20.0")
	t.Setenv("NEURATRADE_SCALPING_CONFIDENCE_BASE", "0.20")
	t.Setenv("NEURATRADE_SCALPING_CONFIDENCE_IMBALANCE_W", "0.30")
	t.Setenv("NEURATRADE_SCALPING_CONFIDENCE_LIQUIDITY_W", "0.30")
	t.Setenv("NEURATRADE_SCALPING_CONFIDENCE_RANGE_W", "0.30")
	t.Setenv("NEURATRADE_SCALPING_CONFIDENCE_VOLUME_W", "0.30")
	t.Setenv("NEURATRADE_SCALPING_CONFIDENCE_VOLUME_LOG_BASE", "5.0")

	got := ApplyScalpingPolicyConfigFromEnv(DefaultScalpingPolicyConfig()).Normalized()

	checks := []struct {
		name string
		got  float64
		want float64
	}{
		{"StrongImbalanceFloor", got.StrongImbalanceFloor, 0.05},
		{"NeutralImbalanceFloor", got.NeutralImbalanceFloor, 0.03},
		{"BuyRangeMax", got.BuyRangeMax, 70.0},
		{"SellRangeMin", got.SellRangeMin, 20.0},
		{"ContinuationRangeBuffer", got.ContinuationRangeBuffer, 8.0},
		{"BreakdownSellRangeMin", got.BreakdownSellRangeMin, 20.0},
		{"ConfidenceBase", got.ConfidenceBase, 0.20},
		{"ConfidenceImbalanceW", got.ConfidenceImbalanceW, 0.30},
		{"ConfidenceLiquidityW", got.ConfidenceLiquidityW, 0.30},
		{"ConfidenceRangeW", got.ConfidenceRangeW, 0.30},
		{"ConfidenceVolumeW", got.ConfidenceVolumeW, 0.30},
		{"ConfidenceVolumeLogBase", got.ConfidenceVolumeLogBase, 5.0},
	}
	for _, c := range checks {
		require.InDelta(t, c.want, c.got, 0.000001, c.name)
	}
}

func TestApplyScalpingPolicyConfigFromEnv_PreservesDefaultsWhenUnset(t *testing.T) {
	for _, env := range []string{
		"NEURATRADE_SCALPING_STRONG_IMBALANCE_FLOOR",
		"NEURATRADE_SCALPING_NEUTRAL_IMBALANCE_FLOOR",
		"NEURATRADE_SCALPING_BUY_RANGE_MAX",
		"NEURATRADE_SCALPING_SELL_RANGE_MIN",
		"NEURATRADE_SCALPING_CONTINUATION_RANGE_BUFFER",
		"NEURATRADE_SCALPING_BREAKDOWN_SELL_RANGE_MIN",
		"NEURATRADE_SCALPING_CONFIDENCE_BASE",
		"NEURATRADE_SCALPING_CONFIDENCE_IMBALANCE_W",
		"NEURATRADE_SCALPING_CONFIDENCE_LIQUIDITY_W",
		"NEURATRADE_SCALPING_CONFIDENCE_RANGE_W",
		"NEURATRADE_SCALPING_CONFIDENCE_VOLUME_W",
		"NEURATRADE_SCALPING_CONFIDENCE_VOLUME_LOG_BASE",
	} {
		t.Setenv(env, "")
	}

	def := DefaultScalpingPolicyConfig()
	got := ApplyScalpingPolicyConfigFromEnv(def).Normalized()

	require.InDelta(t, def.StrongImbalanceFloor, got.StrongImbalanceFloor, 0.000001, "StrongImbalanceFloor")
	require.InDelta(t, def.NeutralImbalanceFloor, got.NeutralImbalanceFloor, 0.000001, "NeutralImbalanceFloor")
	require.InDelta(t, def.BuyRangeMax, got.BuyRangeMax, 0.000001, "BuyRangeMax")
	require.InDelta(t, def.SellRangeMin, got.SellRangeMin, 0.000001, "SellRangeMin")
	require.InDelta(t, def.ContinuationRangeBuffer, got.ContinuationRangeBuffer, 0.000001, "ContinuationRangeBuffer")
	require.InDelta(t, def.BreakdownSellRangeMin, got.BreakdownSellRangeMin, 0.000001, "BreakdownSellRangeMin")
	require.InDelta(t, def.ConfidenceBase, got.ConfidenceBase, 0.000001, "ConfidenceBase")
	require.InDelta(t, def.ConfidenceImbalanceW, got.ConfidenceImbalanceW, 0.000001, "ConfidenceImbalanceW")
	require.InDelta(t, def.ConfidenceLiquidityW, got.ConfidenceLiquidityW, 0.000001, "ConfidenceLiquidityW")
	require.InDelta(t, def.ConfidenceRangeW, got.ConfidenceRangeW, 0.000001, "ConfidenceRangeW")
	require.InDelta(t, def.ConfidenceVolumeW, got.ConfidenceVolumeW, 0.000001, "ConfidenceVolumeW")
	require.InDelta(t, def.ConfidenceVolumeLogBase, got.ConfidenceVolumeLogBase, 0.000001, "ConfidenceVolumeLogBase")
}

func TestScalpingPolicyConfig_Normalized_ClampsAndFallsBack(t *testing.T) {
	cfg := ScalpingPolicyConfig{
		StrongImbalanceFloor:    1.5,
		NeutralImbalanceFloor:   -0.5,
		BuyRangeMax:             150.0,
		SellRangeMin:            -10.0,
		ContinuationRangeBuffer: 80.0,
		BreakdownSellRangeMin:   -10.0,
		ConfidenceBase:          1.5,
		ConfidenceImbalanceW:    20.0,
		ConfidenceLiquidityW:    -0.1,
		ConfidenceRangeW:        20.0,
		ConfidenceVolumeW:       -0.1,
		ConfidenceVolumeLogBase: 500.0,
	}
	got := cfg.Normalized()

	require.InDelta(t, 1.0, got.StrongImbalanceFloor, 0.000001, "StrongImbalanceFloor > 1 clamps to 1")
	require.InDelta(t, 0.10, got.NeutralImbalanceFloor, 0.000001, "NeutralImbalanceFloor <= 0 falls back to default 0.10")
	require.InDelta(t, 100.0, got.BuyRangeMax, 0.000001, "BuyRangeMax > 100 clamps to 100")
	require.InDelta(t, 55.0, got.SellRangeMin, 0.000001, "SellRangeMin <= 0 falls back to default 55.0")
	require.InDelta(t, 80.0, got.ContinuationRangeBuffer, 0.000001, "ContinuationRangeBuffer in [0,100] stays as-is")
	require.InDelta(t, 20.0, got.BreakdownSellRangeMin, 0.000001, "BreakdownSellRangeMin <= 0 falls back to default 20.0")
	require.InDelta(t, 1.0, got.ConfidenceBase, 0.000001, "ConfidenceBase > 1 clamps to 1")
	require.InDelta(t, 10.0, got.ConfidenceImbalanceW, 0.000001, "ConfidenceImbalanceW > 10 clamps to 10")
	require.InDelta(t, 0.20, got.ConfidenceLiquidityW, 0.000001, "ConfidenceLiquidityW <= 0 falls back to default 0.20")
	require.InDelta(t, 10.0, got.ConfidenceRangeW, 0.000001, "ConfidenceRangeW > 10 clamps to 10")
	require.InDelta(t, 0.10, got.ConfidenceVolumeW, 0.000001, "ConfidenceVolumeW <= 0 falls back to default 0.10")
	require.InDelta(t, 100.0, got.ConfidenceVolumeLogBase, 0.000001, "ConfidenceVolumeLogBase > 100 clamps to 100")
}

func TestEvaluateCandidateSignal_RespectsLoosenedImbalanceFloor(t *testing.T) {
	signal := CandidateSignal{
		Symbol:             "XRP/USDT",
		Price:              decimal.NewFromFloat(1.0),
		High24h:            decimal.NewFromFloat(1.2),
		Low24h:             decimal.NewFromFloat(0.8),
		Volume24h:          decimal.NewFromInt(1_500_000),
		BidAskSpread:       0.06,
		OrderBookImbalance: 0.08,
		RangePosition24h:   50,
		PriceChange24hPct:  -0.8,
	}
	policy := EvaluateScalpingPolicy(ScalpingCycleInput{
		TotalValue:        decimal.NewFromInt(48),
		BaseMinConfidence: 0.55,
		BaseMaxCapitalPct: 5.0,
	}, DefaultScalpingPolicyConfig())

	defaultCfg := DefaultScalpingPolicyConfig()
	_, viableA, rejectionA := evaluateCandidateSignal(signal, policy, defaultCfg)
	require.False(t, viableA, "default 0.10 neutral floor should reject 0.08 imbalance (imbalance < neutral floor)")
	require.Equal(t, CandidateRejectNoDirectionalEdge, rejectionA.Reason)

	loosened := defaultCfg
	loosened.NeutralImbalanceFloor = 0.05
	_, _, rejectionB := evaluateCandidateSignal(signal, policy, loosened)
	require.NotEqual(t, CandidateRejectNoDirectionalEdge, rejectionB.Reason,
		"loosened 0.05 neutral floor should NOT reject 0.08 imbalance as no_directional_edge")
}
