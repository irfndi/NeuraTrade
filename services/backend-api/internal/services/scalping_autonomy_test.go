package services

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/irfndi/neuratrade/internal/autonomous"
	"github.com/irfndi/neuratrade/internal/database"
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
