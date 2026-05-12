package services

import (
	"context"
	"os"
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
				envFeaturePaperTrading: "true",
				envFeatureRealTrading:  "false",
			},
			expected: ModePaper,
			ok:       true,
		},
		{
			name: "plural paper enabled and real disabled",
			env: map[string]string{
				envFeaturesPaperTrading: "true",
				envFeaturesRealTrading:  "false",
			},
			expected: ModePaper,
			ok:       true,
		},
		{
			name: "real disabled without paper falls back to dry",
			env: map[string]string{
				envFeatureRealTrading: "false",
			},
			expected: OpModeDry,
			ok:       true,
		},
		{
			name: "invalid real trading value is treated as disabled",
			env: map[string]string{
				envFeatureRealTrading: "flase",
			},
			expected: OpModeDry,
			ok:       true,
		},
		{
			name: "paper aliases prefer non-live when conflicting",
			env: map[string]string{
				envFeaturesPaperTrading: "false",
				envFeaturePaperTrading:  "true",
			},
			expected: ModePaper,
			ok:       true,
		},
		{
			name: "real aliases prefer disabled when conflicting",
			env: map[string]string{
				envFeaturesRealTrading: "true",
				envFeatureRealTrading:  "false",
			},
			expected: OpModeDry,
			ok:       true,
		},
		{
			name: "paper and real both enabled does not override persisted state",
			env: map[string]string{
				envFeaturePaperTrading: "true",
				envFeatureRealTrading:  "true",
			},
			ok: false,
		},
		{
			name: "unset env does not override persisted state",
			env:  map[string]string{},
			ok:   false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			unsetRuntimeModeEnv(t)
			for key, value := range tc.env {
				t.Setenv(key, value)
			}

			mode, ok := runtimeModeOverrideFromEnv()
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.expected, mode)
		})
	}
}

func unsetRuntimeModeEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{envFeaturePaperTrading, envFeaturesPaperTrading, envFeatureRealTrading, envFeaturesRealTrading} {
		previous, ok := os.LookupEnv(key)
		require.NoError(t, os.Unsetenv(key))
		t.Cleanup(func() {
			if ok {
				require.NoError(t, os.Setenv(key, previous))
				return
			}
			require.NoError(t, os.Unsetenv(key))
		})
	}
}
