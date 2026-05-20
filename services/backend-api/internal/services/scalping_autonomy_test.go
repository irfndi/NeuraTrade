package services

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/autonomous"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScalpingAutonomyCoordinator_SetStrategyMode_IgnoresInitialStageOverrideForNewShadowStrategy(t *testing.T) {
	t.Setenv("NEURATRADE_AUTONOMY_INITIAL_STAGE", "live")

	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "scalping-autonomy-shadow.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store := NewAutonomousRolloutStore(sqliteDB.DB)
	require.NoError(t, store.InitSchema(context.Background()))

	coordinator := NewScalpingAutonomyCoordinator(store, AIScalpingConfig{})
	state, err := coordinator.SetStrategyMode(context.Background(), "strategy-shadow", autonomous.ModeShadow)
	require.NoError(t, err)
	require.NotNil(t, state)

	assert.Equal(t, autonomous.StageShadow, state.CurrentStage)
	assert.Equal(t, autonomous.StatusActive, state.Status)
	assert.Empty(t, state.History)
}

func TestScalpingAutonomyCoordinator_SetStrategyMode_PromotesOnlyToRequestedStage(t *testing.T) {
	t.Setenv("NEURATRADE_AUTONOMY_INITIAL_STAGE", "live")

	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "scalping-autonomy-paper.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store := NewAutonomousRolloutStore(sqliteDB.DB)
	require.NoError(t, store.InitSchema(context.Background()))

	coordinator := NewScalpingAutonomyCoordinator(store, AIScalpingConfig{})
	state, err := coordinator.SetStrategyMode(context.Background(), "strategy-paper", autonomous.ModePaper)
	require.NoError(t, err)
	require.NotNil(t, state)
	require.Len(t, state.History, 1)

	assert.Equal(t, autonomous.StagePaper, state.CurrentStage)
	assert.Equal(t, autonomous.StageShadow, state.History[0].FromStage)
	assert.Equal(t, autonomous.StagePaper, state.History[0].ToStage)
}

func TestScalpingAutonomyCoordinator_SetStrategyMode_UsesOperatorTriggerOnRollback(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "scalping-autonomy-rollback.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store := NewAutonomousRolloutStore(sqliteDB.DB)
	require.NoError(t, store.InitSchema(context.Background()))

	coordinator := NewScalpingAutonomyCoordinator(store, AIScalpingConfig{})

	state, err := coordinator.SetStrategyMode(context.Background(), "strategy-live", autonomous.ModePaper)
	require.NoError(t, err)
	state.Metrics = scalpingLiveProofMetrics()
	require.NoError(t, store.SaveRolloutState(context.Background(), state))

	_, err = coordinator.SetStrategyMode(context.Background(), "strategy-live", autonomous.ModeLive)
	require.NoError(t, err)

	state, err = coordinator.SetStrategyMode(context.Background(), "strategy-live", autonomous.ModePaper)
	require.NoError(t, err)
	require.NotNil(t, state)
	require.NotEmpty(t, state.History)

	last := state.History[len(state.History)-1]
	assert.Equal(t, autonomous.StageLive, last.FromStage)
	assert.Equal(t, autonomous.StagePaper, last.ToStage)
	assert.Contains(t, last.Reason, string(autonomous.TriggerOperatorSetMode))
	assert.NotContains(t, last.Reason, string(autonomous.TriggerSafeMode))
}

func TestScalpingAutonomyCoordinator_SetStrategyMode_BlocksLiveWithoutPaperProof(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "scalping-autonomy-live-proof-block.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store := NewAutonomousRolloutStore(sqliteDB.DB)
	require.NoError(t, store.InitSchema(context.Background()))

	coordinator := NewScalpingAutonomyCoordinator(store, AIScalpingConfig{})
	state, err := coordinator.SetStrategyMode(context.Background(), "strategy-live-proof", autonomous.ModePaper)
	require.NoError(t, err)
	require.Equal(t, autonomous.StagePaper, state.CurrentStage)

	state, err = coordinator.SetStrategyMode(context.Background(), "strategy-live-proof", autonomous.ModeLive)
	require.Error(t, err)
	require.NotNil(t, state)
	require.Contains(t, err.Error(), "scalping live paper proof not met")
	require.Contains(t, err.Error(), "closed_trades_below_live_trial_minimum")
	require.Contains(t, err.Error(), "net_pnl_not_positive")
	require.Equal(t, autonomous.StagePaper, state.CurrentStage)

	persisted, getErr := store.GetRolloutState(context.Background(), "strategy-live-proof")
	require.NoError(t, getErr)
	require.NotNil(t, persisted)
	require.Equal(t, autonomous.StagePaper, persisted.CurrentStage)
}

func TestScalpingAutonomyCoordinator_SetStrategyMode_AllowsLiveWithPaperProof(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "scalping-autonomy-live-proof-pass.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store := NewAutonomousRolloutStore(sqliteDB.DB)
	require.NoError(t, store.InitSchema(context.Background()))

	coordinator := NewScalpingAutonomyCoordinator(store, AIScalpingConfig{})
	state, err := coordinator.SetStrategyMode(context.Background(), "strategy-live-proof-pass", autonomous.ModePaper)
	require.NoError(t, err)
	state.Metrics = scalpingLiveProofMetrics()
	require.NoError(t, store.SaveRolloutState(context.Background(), state))

	state, err = coordinator.SetStrategyMode(context.Background(), "strategy-live-proof-pass", autonomous.ModeLive)
	require.NoError(t, err)
	require.NotNil(t, state)
	require.Equal(t, autonomous.StageLive, state.CurrentStage)
	require.NotEmpty(t, state.History)
}

func TestScalpingAutonomyCoordinator_SetStrategyMode_CountsBreakEvenClosedTradesInLiveProof(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "scalping-autonomy-live-proof-breakeven.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store := NewAutonomousRolloutStore(sqliteDB.DB)
	require.NoError(t, store.InitSchema(context.Background()))

	coordinator := NewScalpingAutonomyCoordinator(store, AIScalpingConfig{})
	state, err := coordinator.SetStrategyMode(context.Background(), "strategy-live-proof-breakeven", autonomous.ModePaper)
	require.NoError(t, err)
	state.Metrics = autonomous.RolloutMetrics{
		TotalTrades:           DefaultScalpingLiveTrialMinClosedTrades,
		WinningTrades:         10,
		LosingTrades:          9,
		TotalPnL:              decimal.NewFromFloat(0.25),
		WinRate:               0.5,
		MaxDrawdown:           decimal.NewFromFloat(0.01),
		SignalQualityCoverage: decimal.NewFromInt(1),
		HoldRatio:             decimal.NewFromFloat(0.5),
		UptimePercent:         100,
		LastUpdated:           time.Now().UTC(),
	}
	require.NoError(t, store.SaveRolloutState(context.Background(), state))

	state, err = coordinator.SetStrategyMode(context.Background(), "strategy-live-proof-breakeven", autonomous.ModeLive)
	require.NoError(t, err)
	require.Equal(t, autonomous.StageLive, state.CurrentStage)
}

func TestScalpingAutonomyCoordinator_SetStrategyMode_BlocksLiveWithoutOperationalProofMetrics(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "scalping-autonomy-live-proof-operational.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store := NewAutonomousRolloutStore(sqliteDB.DB)
	require.NoError(t, store.InitSchema(context.Background()))

	coordinator := NewScalpingAutonomyCoordinator(store, AIScalpingConfig{})
	state, err := coordinator.SetStrategyMode(context.Background(), "strategy-live-proof-operational", autonomous.ModePaper)
	require.NoError(t, err)
	state.Metrics = scalpingLiveProofMetrics()
	state.Metrics.SignalQualityCoverage = decimal.Zero
	state.Metrics.AIProviderDegradedCycles = 1
	state.Metrics.HoldRatio = decimal.NewFromFloat(0.8)
	state.Metrics.OpenPositions = 1
	require.NoError(t, store.SaveRolloutState(context.Background(), state))

	state, err = coordinator.SetStrategyMode(context.Background(), "strategy-live-proof-operational", autonomous.ModeLive)

	require.Error(t, err)
	require.NotNil(t, state)
	require.Contains(t, err.Error(), "signal_quality_incomplete")
	require.Contains(t, err.Error(), "ai_provider_degraded")
	require.Contains(t, err.Error(), "hold_ratio_above_live_trial_maximum")
	require.Contains(t, err.Error(), "open_positions_unclosed")
	require.Equal(t, autonomous.StagePaper, state.CurrentStage)
}

func TestScalpingAutonomyCoordinator_SetStrategyMode_BlocksLiveWithStalePaperProof(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "scalping-autonomy-live-proof-stale.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store := NewAutonomousRolloutStore(sqliteDB.DB)
	require.NoError(t, store.InitSchema(context.Background()))

	coordinator := NewScalpingAutonomyCoordinator(store, AIScalpingConfig{})
	state, err := coordinator.SetStrategyMode(context.Background(), "strategy-live-proof-stale", autonomous.ModePaper)
	require.NoError(t, err)
	state.Metrics = scalpingLiveProofMetrics()
	state.Metrics.LastUpdated = time.Now().UTC().Add(-defaultScalpingLiveTrialProofMaxAge - time.Minute)
	require.NoError(t, store.SaveRolloutState(context.Background(), state))

	state, err = coordinator.SetStrategyMode(context.Background(), "strategy-live-proof-stale", autonomous.ModeLive)

	require.Error(t, err)
	require.NotNil(t, state)
	require.Contains(t, err.Error(), "paper_proof_stale")
	require.Equal(t, autonomous.StagePaper, state.CurrentStage)
}

func TestScalpingAutonomyCoordinator_SetStrategyMode_BlocksPersistedLiveWithoutProofMetrics(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "scalping-autonomy-live-proof-set-revalidate.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store := NewAutonomousRolloutStore(sqliteDB.DB)
	require.NoError(t, store.InitSchema(context.Background()))

	require.NoError(t, store.SaveRolloutState(context.Background(), &autonomous.RolloutState{
		StrategyID:   "strategy-live-proof-set-revalidate",
		CurrentStage: autonomous.StageLive,
		Status:       autonomous.StatusActive,
		EnteredAt:    time.Now().UTC(),
		Metrics:      autonomous.RolloutMetrics{},
	}))

	coordinator := NewScalpingAutonomyCoordinator(store, AIScalpingConfig{})
	state, err := coordinator.SetStrategyMode(context.Background(), "strategy-live-proof-set-revalidate", autonomous.ModeLive)

	require.Error(t, err)
	require.NotNil(t, state)
	require.Contains(t, err.Error(), "scalping live paper proof not met")
	require.Contains(t, err.Error(), "closed_trades_below_live_trial_minimum")
	require.Contains(t, err.Error(), "net_pnl_not_positive")
	require.Equal(t, autonomous.StageLive, state.CurrentStage)
}

func TestScalpingAutonomyCoordinator_SetStrategyMode_AllowsPersistedLiveWithProofMetrics(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "scalping-autonomy-live-proof-set-revalidate-pass.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store := NewAutonomousRolloutStore(sqliteDB.DB)
	require.NoError(t, store.InitSchema(context.Background()))

	require.NoError(t, store.SaveRolloutState(context.Background(), &autonomous.RolloutState{
		StrategyID:   "strategy-live-proof-set-revalidate-pass",
		CurrentStage: autonomous.StageLive,
		Status:       autonomous.StatusActive,
		EnteredAt:    time.Now().UTC(),
		Metrics:      scalpingLiveProofMetrics(),
	}))

	coordinator := NewScalpingAutonomyCoordinator(store, AIScalpingConfig{})
	state, err := coordinator.SetStrategyMode(context.Background(), "strategy-live-proof-set-revalidate-pass", autonomous.ModeLive)

	require.NoError(t, err)
	require.NotNil(t, state)
	require.Equal(t, autonomous.StageLive, state.CurrentStage)
}

func TestScalpingAutonomyCoordinator_ValidateStrategyMode_DoesNotPromoteLive(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "scalping-autonomy-live-proof-validate.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store := NewAutonomousRolloutStore(sqliteDB.DB)
	require.NoError(t, store.InitSchema(context.Background()))

	coordinator := NewScalpingAutonomyCoordinator(store, AIScalpingConfig{})
	state, err := coordinator.SetStrategyMode(context.Background(), "strategy-live-proof-validate", autonomous.ModePaper)
	require.NoError(t, err)
	state.Metrics = scalpingLiveProofMetrics()
	require.NoError(t, store.SaveRolloutState(context.Background(), state))

	require.NoError(t, coordinator.ValidateStrategyMode(context.Background(), "strategy-live-proof-validate", autonomous.ModeLive))

	persisted, err := store.GetRolloutState(context.Background(), "strategy-live-proof-validate")
	require.NoError(t, err)
	require.Equal(t, autonomous.StagePaper, persisted.CurrentStage)
}

func TestScalpingAutonomyCoordinator_ValidateStrategyMode_BlocksPersistedLiveWithoutProofMetrics(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "scalping-autonomy-live-proof-revalidate.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store := NewAutonomousRolloutStore(sqliteDB.DB)
	require.NoError(t, store.InitSchema(context.Background()))

	require.NoError(t, store.SaveRolloutState(context.Background(), &autonomous.RolloutState{
		StrategyID:   "strategy-live-proof-revalidate",
		CurrentStage: autonomous.StageLive,
		Status:       autonomous.StatusActive,
		EnteredAt:    time.Now().UTC(),
		Metrics:      autonomous.RolloutMetrics{},
	}))

	coordinator := NewScalpingAutonomyCoordinator(store, AIScalpingConfig{})
	err = coordinator.ValidateStrategyMode(context.Background(), "strategy-live-proof-revalidate", autonomous.ModeLive)

	require.Error(t, err)
	require.Contains(t, err.Error(), "scalping live paper proof not met")
	require.Contains(t, err.Error(), "closed_trades_below_live_trial_minimum")
	require.Contains(t, err.Error(), "net_pnl_not_positive")
}

func TestScalpingAutonomyCoordinator_ValidateStrategyMode_AllowsPersistedLiveWithProofMetrics(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "scalping-autonomy-live-proof-revalidate-pass.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store := NewAutonomousRolloutStore(sqliteDB.DB)
	require.NoError(t, store.InitSchema(context.Background()))

	require.NoError(t, store.SaveRolloutState(context.Background(), &autonomous.RolloutState{
		StrategyID:   "strategy-live-proof-revalidate-pass",
		CurrentStage: autonomous.StageLive,
		Status:       autonomous.StatusActive,
		EnteredAt:    time.Now().UTC(),
		Metrics:      scalpingLiveProofMetrics(),
	}))

	coordinator := NewScalpingAutonomyCoordinator(store, AIScalpingConfig{})
	require.NoError(t, coordinator.ValidateStrategyMode(context.Background(), "strategy-live-proof-revalidate-pass", autonomous.ModeLive))
}

func scalpingLiveProofMetrics() autonomous.RolloutMetrics {
	return autonomous.RolloutMetrics{
		TotalTrades:           DefaultScalpingLiveTrialMinClosedTrades,
		WinningTrades:         DefaultScalpingLiveTrialMinClosedTrades - 1,
		LosingTrades:          1,
		TotalPnL:              decimal.NewFromFloat(0.25),
		WinRate:               0.95,
		MaxDrawdown:           decimal.NewFromFloat(0.01),
		SignalQualityCoverage: decimal.NewFromInt(1),
		HoldRatio:             decimal.NewFromFloat(0.5),
		UptimePercent:         100,
		LastUpdated:           time.Now().UTC(),
	}
}
