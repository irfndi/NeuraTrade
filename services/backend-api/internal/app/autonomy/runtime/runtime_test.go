package runtime

import (
	"path/filepath"
	"testing"

	"github.com/irfndi/neuratrade/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildIntegratedHandlers_WithoutSQLDBReturnsFallbackAndError(t *testing.T) {
	handlers, err := BuildIntegratedHandlers(Dependencies{})
	require.Error(t, err)
	require.NotNil(t, handlers)
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
