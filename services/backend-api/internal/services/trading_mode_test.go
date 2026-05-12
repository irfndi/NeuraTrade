package services

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/irfndi/neuratrade/internal/database"
	"github.com/irfndi/neuratrade/internal/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOperationalModeService_SQLitePersistence(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "trading-mode.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	logger := logging.NewStandardLogger("error", "development")
	service := NewOperationalModeService(sqliteDB, DefaultOperationalModeConfig(), logger)
	ctx := context.Background()

	confirmations, err := service.AddConfirmation(ctx, "chat-1", "tester")
	require.NoError(t, err)
	assert.Equal(t, 1, confirmations)

	confirmations, err = service.AddConfirmation(ctx, "chat-1", "tester")
	require.NoError(t, err)
	assert.Equal(t, 2, confirmations)

	err = service.SetMode(ctx, "chat-1", OpModeLive, "tester")
	require.NoError(t, err)
	assert.Equal(t, OpModeLive, service.GetMode("chat-1"))

	reloaded := NewOperationalModeService(sqliteDB, DefaultOperationalModeConfig(), logger)
	assert.Equal(t, OpModeLive, reloaded.GetMode("chat-1"))
}

func TestOperationalModeService_DryAndPaperHelpersRemainDistinct(t *testing.T) {
	logger := logging.NewStandardLogger("error", "development")
	service := NewOperationalModeService(nil, DefaultOperationalModeConfig(), logger)
	ctx := context.Background()

	require.NoError(t, service.SetMode(ctx, "chat-dry", OpModeDry, "tester"))
	require.NoError(t, service.SetMode(ctx, "chat-paper", ModePaper, "tester"))

	assert.True(t, service.IsDry("chat-dry"))
	assert.False(t, service.IsPaper("chat-dry"))
	assert.False(t, service.IsDry("chat-paper"))
	assert.True(t, service.IsPaper("chat-paper"))
}

func TestOperationalModeService_GetModeInfo_UsesDistinctDryAndPaperLabels(t *testing.T) {
	logger := logging.NewStandardLogger("error", "development")
	service := NewOperationalModeService(nil, DefaultOperationalModeConfig(), logger)
	ctx := context.Background()

	require.NoError(t, service.SetMode(ctx, "chat-dry", OpModeDry, "tester"))
	require.NoError(t, service.SetMode(ctx, "chat-paper", ModePaper, "tester"))

	dryInfo := service.GetModeInfo("chat-dry")
	paperInfo := service.GetModeInfo("chat-paper")

	assert.Contains(t, dryInfo, "DRY MODE (Shadow/No Order Execution)")
	assert.Contains(t, dryInfo, "Strategy runs stay in shadow observation mode")
	assert.Contains(t, paperInfo, "PAPER MODE (Simulated Orders)")
	assert.Contains(t, paperInfo, "Orders are simulated through the autonomy paper stage")
}

func TestRuntimeModeOverrideFromEnv_HonorsSingularAndPluralAliases(t *testing.T) {
	testCases := []struct {
		name     string
		env      map[string]string
		expected OperationalMode
		ok       bool
	}{
		{
			name: "singular paper enabled and real disabled",
			env: map[string]string{
				"FEATURE_PAPER_TRADING": "true",
				"FEATURE_REAL_TRADING":  "false",
			},
			expected: ModePaper,
			ok:       true,
		},
		{
			name: "plural paper enabled and real disabled",
			env: map[string]string{
				"FEATURES_PAPER_TRADING": "true",
				"FEATURES_REAL_TRADING":  "false",
			},
			expected: ModePaper,
			ok:       true,
		},
		{
			name: "real disabled without paper falls back to dry",
			env: map[string]string{
				"FEATURE_REAL_TRADING": "false",
			},
			expected: OpModeDry,
			ok:       true,
		},
		{
			name: "unset env does not override persisted state",
			env:  map[string]string{},
			ok:   false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			for _, key := range []string{"FEATURE_PAPER_TRADING", "FEATURES_PAPER_TRADING", "FEATURE_REAL_TRADING", "FEATURES_REAL_TRADING"} {
				t.Setenv(key, "")
			}
			for key, value := range tc.env {
				t.Setenv(key, value)
			}

			mode, ok := runtimeModeOverrideFromEnv()
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.expected, mode)
		})
	}
}
