package autonomy

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestEvaluateScalpingPolicy_MicroTierRelaxesBootstrapFloor(t *testing.T) {
	policy := EvaluateScalpingPolicy(ScalpingCycleInput{
		TotalValue:         46.93,
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

func TestBuildCandidateFunnel_CapturesStructuredRejectReasons(t *testing.T) {
	policy := EvaluateScalpingPolicy(ScalpingCycleInput{
		TotalValue:        46.93,
		BaseMinConfidence: 0.65,
		BaseMaxCapitalPct: 5.0,
	}, DefaultScalpingPolicyConfig())

	snapshot := BuildCandidateFunnel([]CandidateSignal{
		{
			Symbol:             "PLUME/USDT",
			Price:              1.0,
			High24h:            1.1,
			Low24h:             0.9,
			Volume24h:          500000,
			BidAskSpread:       0.36,
			OrderBookImbalance: -0.30,
			RangePosition24h:   60,
		},
		{
			Symbol:             "OPN/USDT",
			Price:              1.0,
			High24h:            1.2,
			Low24h:             0.8,
			Volume24h:          100,
			BidAskSpread:       0.19,
			OrderBookImbalance: -0.20,
			RangePosition24h:   56,
		},
		{
			Symbol:             "WIF/USDT",
			Price:              1.0,
			High24h:            1.1,
			Low24h:             0.9,
			Volume24h:          1000000,
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
