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
