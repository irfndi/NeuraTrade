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
