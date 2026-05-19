package services

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/irfndi/neuratrade/internal/autonomous"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestRecordScalpingLiveTrialProofPersistsRolloutMetrics(t *testing.T) {
	ctx := context.Background()
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "scalping-live-proof.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, sqliteDB.Close())
	})

	result := liveReadyScalpingSoakResultForRolloutProof()
	state, err := RecordScalpingLiveTrialProof(ctx, sqliteDB.DB, "scalping:chat-1:default", result)

	require.NoError(t, err)
	require.NotNil(t, state)
	require.Equal(t, autonomous.StagePaper, state.CurrentStage)
	require.Equal(t, autonomous.StatusActive, state.Status)
	require.Equal(t, result.Report.TradeSummary.ClosedTrades, state.Metrics.TotalTrades)
	require.Equal(t, result.Report.TradeSummary.Wins, state.Metrics.WinningTrades)
	require.Equal(t, result.Report.TradeSummary.Losses, state.Metrics.LosingTrades)
	require.True(t, state.Metrics.TotalPnL.Equal(result.Report.TradeSummary.NetPnL))
	require.Equal(t, result.Report.TradeSummary.WinRate.InexactFloat64(), state.Metrics.WinRate)
	require.True(t, state.Metrics.MaxDrawdown.Equal(result.Report.TradeSummary.MaxDrawdownPct))
	require.True(t, state.Metrics.SignalQualityCoverage.Equal(decimal.NewFromInt(1)))
	require.Zero(t, state.Metrics.AIProviderDegradedCycles)
	require.True(t, state.Metrics.HoldRatio.Equal(decimal.NewFromFloat(0.5)))
	require.Zero(t, state.Metrics.OpenPositions)

	store := NewAutonomousRolloutStore(sqliteDB.DB)
	persisted, err := store.GetRolloutState(ctx, "scalping:chat-1:default")
	require.NoError(t, err)
	require.NotNil(t, persisted)
	require.True(t, persisted.Metrics.SignalQualityCoverage.Equal(decimal.NewFromInt(1)))
	require.True(t, persisted.Metrics.HoldRatio.Equal(decimal.NewFromFloat(0.5)))
}

func TestRecordScalpingLiveTrialProofPromotesExistingNonLiveStateToActivePaper(t *testing.T) {
	ctx := context.Background()
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "scalping-live-proof-existing.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, sqliteDB.Close())
	})

	store := NewAutonomousRolloutStore(sqliteDB.DB)
	require.NoError(t, store.InitSchema(ctx))
	require.NoError(t, store.SaveRolloutState(ctx, &autonomous.RolloutState{
		StrategyID:        "scalping:chat-1:default",
		CurrentStage:      autonomous.StageShadow,
		Status:            autonomous.StatusPaused,
		PromotionCriteria: autonomous.DefaultPromotionCriteria(),
	}))

	result := liveReadyScalpingSoakResultForRolloutProof()
	state, err := RecordScalpingLiveTrialProof(ctx, sqliteDB.DB, "scalping:chat-1:default", result)

	require.NoError(t, err)
	require.NotNil(t, state)
	require.Equal(t, autonomous.StagePaper, state.CurrentStage)
	require.Equal(t, autonomous.StatusActive, state.Status)
	require.NoError(t, NewScalpingAutonomyCoordinator(store, AIScalpingConfig{}).
		ValidateStrategyMode(ctx, "scalping:chat-1:default", autonomous.ModeLive))
}

func TestRecordScalpingLiveTrialProofRejectsNotReadyResult(t *testing.T) {
	ctx := context.Background()
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "scalping-live-proof-not-ready.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, sqliteDB.Close())
	})

	result := liveReadyScalpingSoakResultForRolloutProof()
	result.Report.LiveTrialReadiness.Ready = false
	result.Report.LiveTrialReadiness.Reasons = []string{"closed_trades_below_live_trial_minimum"}

	_, err = RecordScalpingLiveTrialProof(ctx, sqliteDB.DB, "scalping:chat-1:default", result)

	require.ErrorContains(t, err, "scalping live trial proof not ready")
	require.ErrorContains(t, err, "closed_trades_below_live_trial_minimum")
}

func TestRecordScalpingLiveTrialProofRejectsInconsistentReadyMetrics(t *testing.T) {
	ctx := context.Background()
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "scalping-live-proof-inconsistent.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, sqliteDB.Close())
	})

	result := liveReadyScalpingSoakResultForRolloutProof()
	result.Report.SignalQuality.Coverage = decimal.Zero

	_, err = RecordScalpingLiveTrialProof(ctx, sqliteDB.DB, "scalping:chat-1:default", result)

	require.ErrorContains(t, err, "scalping live trial proof metrics invalid")
	require.ErrorContains(t, err, "signal_quality_incomplete")
}

func liveReadyScalpingSoakResultForRolloutProof() *ScalpingLivePaperSoakResult {
	return &ScalpingLivePaperSoakResult{
		OpenPositions: 0,
		Report: ScalpingSoakReport{
			ActionSplit: map[string]decimal.Decimal{
				"hold": decimal.NewFromFloat(0.5),
			},
			SignalQuality: ScalpingSignalQualitySoakStats{
				Coverage: decimal.NewFromInt(1),
			},
			TradeSummary: ScalpingSoakTradeSummary{
				ClosedTrades:      DefaultScalpingLiveTrialMinClosedTrades,
				Wins:              DefaultScalpingLiveTrialMinClosedTrades - 2,
				Losses:            2,
				WinRate:           decimal.NewFromFloat(0.9),
				NetPnL:            decimal.NewFromFloat(0.25),
				AvgNetPnLPerTrade: decimal.NewFromFloat(0.0125),
				MaxDrawdown:       decimal.NewFromFloat(0.05),
				MaxDrawdownPct:    decimal.NewFromFloat(0.001),
			},
			AIProviderDegradation: ScalpingAIDegradationSoakStats{
				DegradedCycles: 0,
			},
			LiveTrialReadiness: ScalpingLiveTrialReadiness{
				Ready:           true,
				MinClosedTrades: DefaultScalpingLiveTrialMinClosedTrades,
			},
		},
	}
}
