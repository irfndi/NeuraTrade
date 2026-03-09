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

func TestIntegratedQuestHandlersResolveOperationalModePrefersStoredState(t *testing.T) {
	opModeService := &OperationalModeService{
		config: DefaultOperationalModeConfig(),
		states: map[string]*OperationalModeState{
			"chat-live": {
				ChatID: "chat-live",
				Mode:   OpModeLive,
			},
		},
	}

	handlers := &IntegratedQuestHandlers{opModeService: opModeService}
	quest := &Quest{Metadata: map[string]string{"dry_run": "true"}}

	assert.Equal(t, OpModeLive, handlers.resolveOperationalMode("chat-live", quest))
	assert.Equal(t, OpModeDry, handlers.resolveOperationalMode("chat-dry", &Quest{Metadata: map[string]string{"dry_run": "true"}}))
	assert.Equal(t, OpModeDry, handlers.resolveOperationalMode("chat-default", &Quest{Metadata: map[string]string{"dry_run": "false"}}))
}

func TestIntegratedQuestHandlersSyncScalpingStrategyModeFollowsOperatorMode(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "rollout-sync.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store := NewAutonomousRolloutStore(sqliteDB.DB)
	require.NoError(t, store.InitSchema(context.Background()))

	handlers := &IntegratedQuestHandlers{
		autonomyCoordinator: NewScalpingAutonomyCoordinator(store, AIScalpingConfig{}),
	}

	ctx := context.Background()
	require.NoError(t, handlers.syncScalpingStrategyMode(ctx, "1082762347", OpModeLive))

	state, err := store.GetRolloutState(ctx, ScalpingStrategyID("1082762347"))
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, autonomous.StageLive, state.CurrentStage)
	assert.Equal(t, autonomous.StatusActive, state.Status)

	require.NoError(t, handlers.syncScalpingStrategyMode(ctx, "1082762347", OpModeDry))

	state, err = store.GetRolloutState(ctx, ScalpingStrategyID("1082762347"))
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, autonomous.StageShadow, state.CurrentStage)
	assert.Equal(t, autonomous.StatusActive, state.Status)
}
