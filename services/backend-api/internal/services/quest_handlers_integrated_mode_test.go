package services

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/irfndi/neuratrade/internal/autonomous"
	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegratedQuestHandlersResolveOperationalModePrefersStoredState(t *testing.T) {
	testCases := []struct {
		name          string
		opModeService *OperationalModeService
		quest         *Quest
		chatID        string
		expected      OperationalMode
	}{
		{
			name: "stored_live_overrides_dry_metadata",
			opModeService: &OperationalModeService{
				config: DefaultOperationalModeConfig(),
				states: map[string]*OperationalModeState{
					"chat-live": {
						ChatID: "chat-live",
						Mode:   OpModeLive,
					},
				},
			},
			quest:    &Quest{Metadata: map[string]string{"dry_run": "true"}},
			chatID:   "chat-live",
			expected: OpModeLive,
		},
		{
			name: "default_service_mode_stays_dry",
			opModeService: &OperationalModeService{
				config: DefaultOperationalModeConfig(),
				states: map[string]*OperationalModeState{},
			},
			quest:    &Quest{Metadata: map[string]string{"dry_run": "false"}},
			chatID:   "chat-default",
			expected: OpModeDry,
		},
		{
			name: "unsupported_stored_mode_is_treated_as_dry",
			opModeService: &OperationalModeService{
				config: DefaultOperationalModeConfig(),
				states: map[string]*OperationalModeState{
					"chat-paper": {
						ChatID: "chat-paper",
						Mode:   ModePaper,
					},
				},
			},
			quest:    &Quest{Metadata: map[string]string{"dry_run": "false"}},
			chatID:   "chat-paper",
			expected: OpModeDry,
		},
		{
			name:          "metadata_dry_run_without_service",
			opModeService: nil,
			quest:         &Quest{Metadata: map[string]string{"dry_run": "true"}},
			chatID:        "chat-meta-dry",
			expected:      OpModeDry,
		},
		{
			name:          "metadata_paper_trading_without_service",
			opModeService: nil,
			quest:         &Quest{Metadata: map[string]string{"paper_trading": "true"}},
			chatID:        "chat-meta-paper",
			expected:      OpModeDry,
		},
		{
			name:          "no_service_or_metadata_defaults_live",
			opModeService: nil,
			quest:         &Quest{Metadata: map[string]string{}},
			chatID:        "chat-live-default",
			expected:      OpModeLive,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			handlers := &IntegratedQuestHandlers{opModeService: tc.opModeService}
			assert.Equal(t, tc.expected, handlers.resolveOperationalMode(tc.chatID, tc.quest))
		})
	}
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

type syncFailureBalanceFetcher struct{}

func (syncFailureBalanceFetcher) FetchBalance(context.Context, string) (*ccxt.BalanceResponse, error) {
	return &ccxt.BalanceResponse{}, nil
}

func TestIntegratedQuestHandlersExecuteAIScalping_HoldsOnModeSyncFailure(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "rollout-sync-fail.db"))
	require.NoError(t, err)

	store := NewAutonomousRolloutStore(sqliteDB.DB)
	require.NoError(t, store.InitSchema(context.Background()))

	handlers := &IntegratedQuestHandlers{
		ccxtService:         syncFailureBalanceFetcher{},
		opModeService:       &OperationalModeService{config: DefaultOperationalModeConfig(), states: map[string]*OperationalModeState{}},
		autonomyCoordinator: NewScalpingAutonomyCoordinator(store, AIScalpingConfig{}),
	}

	require.NoError(t, sqliteDB.Close())

	quest := &Quest{
		Metadata:   map[string]string{"chat_id": "1082762347"},
		Checkpoint: map[string]interface{}{},
	}

	err = handlers.executeAIScalping(context.Background(), quest, "1082762347")
	require.NoError(t, err)
	assert.Equal(t, "hold", quest.Checkpoint["status"])
	assert.Equal(t, "failed to synchronize scalping rollout mode", quest.Checkpoint["runtime_entry_gate_reason"])
	assert.NotEmpty(t, quest.Checkpoint["autonomy_mode_sync_error"])
}
