package runtime

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/irfndi/neuratrade/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildIntegratedHandlers_WithoutSQLDBReturnsNilAndError(t *testing.T) {
	handlers, err := BuildIntegratedHandlers(Dependencies{})
	require.Error(t, err)
	require.Nil(t, handlers)
	assert.Contains(t, err.Error(), "sql db is nil")
}

func TestBuildIntegratedHandlers_WithSQLDBInitializesHandlers(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "autonomy-runtime.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	handlers, err := BuildIntegratedHandlers(Dependencies{SQLDB: sqliteDB.DB})
	require.NoError(t, err)
	require.NotNil(t, handlers)
}

func TestBuildLocalIntegratedHandlers_SetsDBForFallback(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "autonomy-runtime-fallback.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	handlers := BuildLocalIntegratedHandlers(Dependencies{SQLDB: sqliteDB.DB})
	require.NotNil(t, handlers)
	require.NoError(t, handlers.SetAutonomyStore(nil))
}

func TestEnsureAutonomySchema_WrapsInitErrors(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "autonomy-runtime-wrap.db"))
	require.NoError(t, err)
	require.NoError(t, sqliteDB.Close())

	err = EnsureAutonomySchema(context.Background(), sqliteDB.DB)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "init autonomy schema")
}
