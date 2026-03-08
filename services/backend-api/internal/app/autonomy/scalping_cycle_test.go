package autonomy

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestEvaluateScalpingPolicy_MicroTierRelaxesBootstrapFloor(t *testing.T) {
	policy := EvaluateScalpingPolicy(ScalpingCycleInput{
		TotalValue:         decimal.NewFromFloat(46.93),
		BaseMinConfidence:  0.65,
		BaseMaxCapitalPct:  5.0,
		Phase:              "bootstrap",
		PhaseMinConfidence: 0.75,
		PhaseMaxCapitalPct: 1.0,
	}, DefaultScalpingPolicyConfig())

	require.Equal(t, AccountTierMicro, policy.AccountTier)
	require.InDelta(t, 0.65, policy.EffectiveMinConfidence, 0.0001)
	require.InDelta(t, 0.50, policy.EffectiveMaxCapitalPct, 0.0001)
	require.Equal(t, 1, policy.MaxConcurrentPositions)
}

func TestEvaluateScalpingPolicy_NoFillRecoveryAdjustments(t *testing.T) {
	config := DefaultScalpingPolicyConfig()

	baseInput := ScalpingCycleInput{
		TotalValue:        decimal.NewFromInt(1_000),
		BaseMinConfidence: 0.85,
		BaseMaxCapitalPct: 0.10,
	}

	testCases := []struct {
		name            string
		noFillMinutes   float64
		expectedMinConf float64
		expectedMaxCap  float64
	}{
		{
			name:            "at_1x_recovery_window",
			noFillMinutes:   float64(config.NoFillRecoveryMinutes),
			expectedMinConf: 0.75,
			expectedMaxCap:  0.50,
		},
		{
			name:            "at_2x_recovery_window",
			noFillMinutes:   float64(2 * config.NoFillRecoveryMinutes),
			expectedMinConf: config.NoFillMinConfidenceFloor,
			expectedMaxCap:  1.00,
		},
		{
			name:            "at_3x_recovery_window",
			noFillMinutes:   float64(3 * config.NoFillRecoveryMinutes),
			expectedMinConf: config.NoFillMinConfidenceFloor,
			expectedMaxCap:  config.NoFillMaxCapitalPctCap,
		},
	}

	var prevMinConfidence float64
	var prevMaxCapitalPct float64
	for i, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			input := baseInput
			input.NoFillMinutes = tc.noFillMinutes

			policy := EvaluateScalpingPolicy(input, config)

			require.Contains(t, policy.PolicyAdjustments, "controlled_no_fill_recovery")
			require.InDelta(t, tc.expectedMinConf, policy.EffectiveMinConfidence, 0.0001)
			require.InDelta(t, tc.expectedMaxCap, policy.EffectiveMaxCapitalPct, 0.0001)
			if i > 0 {
				require.LessOrEqual(t, policy.EffectiveMinConfidence, prevMinConfidence)
				require.GreaterOrEqual(t, policy.EffectiveMaxCapitalPct, prevMaxCapitalPct)
			}

			prevMinConfidence = policy.EffectiveMinConfidence
			prevMaxCapitalPct = policy.EffectiveMaxCapitalPct
		})
	}
}

func TestEvaluateScalpingPolicy_LossStreakAndNegativeExpectancyTightening(t *testing.T) {
	config := DefaultScalpingPolicyConfig()

	baseline := EvaluateScalpingPolicy(ScalpingCycleInput{
		TotalValue:        decimal.NewFromInt(1_000),
		BaseMinConfidence: 0.70,
		BaseMaxCapitalPct: 5.0,
	}, config)

	withLossAndNegativeExpectancy := EvaluateScalpingPolicy(ScalpingCycleInput{
		TotalValue:        decimal.NewFromInt(1_000),
		BaseMinConfidence: 0.70,
		BaseMaxCapitalPct: 5.0,
		ConsecutiveLosses: 3,
		RiskExpectancy:    -0.01,
		RiskSampleSize:    12,
	}, config)

	require.Greater(t, withLossAndNegativeExpectancy.EffectiveMinConfidence, baseline.EffectiveMinConfidence)
	require.Less(t, withLossAndNegativeExpectancy.EffectiveMaxCapitalPct, baseline.EffectiveMaxCapitalPct)
	require.Contains(t, withLossAndNegativeExpectancy.PolicyAdjustments, "loss_streak_confidence_tightening")
	require.Contains(t, withLossAndNegativeExpectancy.PolicyAdjustments, "negative_expectancy_cap")
}

func TestEvaluateScalpingPolicy_UsesInclusiveTierThresholdsAndStandardConcurrentCap(t *testing.T) {
	cfg := DefaultScalpingPolicyConfig()

	require.Equal(t, AccountTierMicro, ResolveAccountTier(cfg.MicroAccountMaxValue, cfg))
	require.Equal(t, AccountTierSmall, ResolveAccountTier(cfg.SmallAccountMaxValue, cfg))

	policy := EvaluateScalpingPolicy(ScalpingCycleInput{
		TotalValue:        decimal.NewFromInt(1_000),
		BaseMinConfidence: 0.70,
		BaseMaxCapitalPct: 5.0,
	}, cfg)

	require.Equal(t, AccountTierSmall, policy.AccountTier)
	require.Equal(t, DefaultMaxConcurrentPositions, policy.MaxConcurrentPositions)
}

func TestNextUnblockCondition_Mappings(t *testing.T) {
	policy := ScalpingCyclePolicy{
		EffectiveMinConfidence: 0.72,
		MaxConcurrentPositions: 2,
	}

	testCases := []struct {
		name     string
		reason   string
		expected string
	}{
		{name: "confidence", reason: CandidateRejectConfidenceBelowThreshold, expected: "confidence"},
		{name: "spread", reason: CandidateRejectSpreadTooWide, expected: "spread"},
		{name: "orderbook", reason: CandidateRejectMissingOrderbookSignal, expected: "orderbook"},
		{name: "expectancy", reason: CandidateRejectPreTradeExpectancy, expected: "expectancy"},
		{name: "rollout", reason: CandidateRejectRolloutShadow, expected: "strategy-mode"},
		{name: "rollout_paper", reason: CandidateRejectRolloutPaper, expected: "paper"},
		{name: "max_concurrent", reason: CandidateRejectMaxConcurrentPositions, expected: "reduce open positions"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			next := NextUnblockCondition(tc.reason, policy)
			require.NotEmpty(t, next)
			require.Contains(t, strings.ToLower(next), strings.ToLower(tc.expected))
		})
	}
}

func TestBuildCandidateFunnel_CapturesStructuredRejectReasons(t *testing.T) {
	policy := EvaluateScalpingPolicy(ScalpingCycleInput{
		TotalValue:        decimal.NewFromFloat(46.93),
		BaseMinConfidence: 0.65,
		BaseMaxCapitalPct: 5.0,
	}, DefaultScalpingPolicyConfig())

	snapshot := BuildCandidateFunnel([]CandidateSignal{
		{
			Symbol:             "PLUME/USDT",
			Price:              decimal.NewFromFloat(1.0),
			High24h:            decimal.NewFromFloat(1.1),
			Low24h:             decimal.NewFromFloat(0.9),
			Volume24h:          decimal.NewFromInt(500000),
			BidAskSpread:       0.36,
			OrderBookImbalance: -0.30,
			RangePosition24h:   60,
		},
		{
			Symbol:             "OPN/USDT",
			Price:              decimal.NewFromFloat(1.0),
			High24h:            decimal.NewFromFloat(1.2),
			Low24h:             decimal.NewFromFloat(0.8),
			Volume24h:          decimal.NewFromInt(100),
			BidAskSpread:       0.19,
			OrderBookImbalance: -0.20,
			RangePosition24h:   56,
		},
		{
			Symbol:             "WIF/USDT",
			Price:              decimal.NewFromFloat(1.0),
			High24h:            decimal.NewFromFloat(1.1),
			Low24h:             decimal.NewFromFloat(0.9),
			Volume24h:          decimal.NewFromInt(1000000),
			BidAskSpread:       0.08,
			OrderBookImbalance: 0.02,
			RangePosition24h:   40,
		},
	}, policy)

	require.Equal(t, 3, snapshot.CandidateUniverseCount)
	require.Equal(t, 3, snapshot.CandidateRankedCount)
	require.Equal(t, 0, snapshot.CandidateViableCount)
	require.Len(t, snapshot.TopCandidateRejections, 3)
	reasons := []string{
		snapshot.TopCandidateRejections[0].Reason,
		snapshot.TopCandidateRejections[1].Reason,
		snapshot.TopCandidateRejections[2].Reason,
	}
	require.Contains(t, reasons, CandidateRejectSpreadTooWide)
	require.Contains(t, reasons, CandidateRejectConfidenceBelowThreshold)
	require.Contains(t, reasons, CandidateRejectMissingOrderbookSignal)
}

func TestEvaluateCandidateSignal_RejectsInvalidMetrics(t *testing.T) {
	policy := EvaluateScalpingPolicy(ScalpingCycleInput{
		TotalValue:        decimal.NewFromInt(500),
		BaseMinConfidence: 0.65,
		BaseMaxCapitalPct: 5.0,
	}, DefaultScalpingPolicyConfig())

	testCases := []struct {
		name   string
		signal CandidateSignal
	}{
		{
			name: "nan_spread",
			signal: CandidateSignal{
				Symbol:             "ADA/USDT",
				Price:              decimal.NewFromInt(1),
				Volume24h:          decimal.NewFromInt(1_000),
				BidAskSpread:       math.NaN(),
				OrderBookImbalance: 0.25,
				RangePosition24h:   30,
			},
		},
		{
			name: "negative_volume",
			signal: CandidateSignal{
				Symbol:             "ADA/USDT",
				Price:              decimal.NewFromInt(1),
				Volume24h:          decimal.NewFromInt(-1),
				BidAskSpread:       0.05,
				OrderBookImbalance: 0.25,
				RangePosition24h:   30,
			},
		},
		{
			name: "infinite_imbalance",
			signal: CandidateSignal{
				Symbol:             "ADA/USDT",
				Price:              decimal.NewFromInt(1),
				Volume24h:          decimal.NewFromInt(1_000),
				BidAskSpread:       0.05,
				OrderBookImbalance: math.Inf(1),
				RangePosition24h:   30,
			},
		},
		{
			name: "nan_range_position",
			signal: CandidateSignal{
				Symbol:             "ADA/USDT",
				Price:              decimal.NewFromInt(1),
				Volume24h:          decimal.NewFromInt(1_000),
				BidAskSpread:       0.05,
				OrderBookImbalance: 0.25,
				RangePosition24h:   math.NaN(),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ranked, viable, rejection := evaluateCandidateSignal(tc.signal, policy)
			require.False(t, ranked)
			require.False(t, viable)
			require.Equal(t, CandidateRejectMissingOrderbookSignal, rejection.Reason)
		})
	}
}

func TestEvaluateProgressBlock_AfterTwoHoursWithoutAttempt(t *testing.T) {
	state := EvaluateProgressBlock(
		time.Date(2026, time.March, 7, 10, 0, 0, 0, time.UTC),
		time.Date(2026, time.March, 7, 12, 30, 0, 0, time.UTC),
		DefaultScalpingPolicyConfig(),
	)

	require.True(t, state.Blocked)
	require.Contains(t, state.Reason, "2h0m0s")
}

func TestBuildRolloutShadowGate(t *testing.T) {
	snapshot := BuildRolloutShadowGate("shadow", "active", "")

	require.False(t, snapshot.Allowed)
	require.Equal(t, CandidateRejectRolloutShadow, snapshot.BlockCode)
	require.Equal(t, "shadow", snapshot.RolloutStageCurrent)
	require.Equal(t, "active", snapshot.RolloutStatusCurrent)
	require.Contains(t, snapshot.RolloutGateReason, "strategy_not_live")
}

func TestBuildRolloutShadowGate_PaperStageUsesPaperBlockCode(t *testing.T) {
	snapshot := BuildRolloutShadowGate("paper", "active", "")

	require.False(t, snapshot.Allowed)
	require.Equal(t, CandidateRejectRolloutPaper, snapshot.BlockCode)
}
