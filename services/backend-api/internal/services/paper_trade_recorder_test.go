package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/database"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestPaperTradeRecorder_RecordDirectClosedTrade(t *testing.T) {
	ctx := context.Background()

	// Set up SQLite with paper_trades migration (same pattern as paper_dry_run_validation_test.go).
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "paper-direct-closed.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqliteDB.Close()) })

	migrationPath := filepath.Join("..", "..", "database", "sqlite_migrations", "012_create_paper_trades_table.sql")
	migrationSQL, err := os.ReadFile(migrationPath)
	require.NoError(t, err)
	_, err = sqliteDB.Exec(ctx, string(migrationSQL))
	require.NoError(t, err)

	recorder := NewPaperTradeRecorder(sqliteDB, noopPaperDryRunLogger{})

	closedAt := time.Now()
	result, err := recorder.RecordDirectClosedTrade(ctx, &PaperTrade{
		UserID:     "test-user",
		StrategyID: "scalping",
		Exchange:   "bitget",
		Symbol:     "BTC/USDT:USDT",
		Side:       "buy",
		EntryPrice: decimal.NewFromInt(50000),
		ExitPrice:  decimal.NewFromInt(51000),
		Size:       decimal.NewFromFloat(0.01),
		Fees:       decimal.NewFromFloat(0.6),
		OpenedAt:   time.Now().Add(-time.Hour),
		ClosedAt:   &closedAt,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "closed", result.Status)
	require.True(t, result.ID > 0)
	// PnL = (51000 - 50000) * 0.1 - 0.6 = 10 - 0.6 = 9.4
	require.True(t, result.PnL.Equal(decimal.RequireFromString("9.4")), "expected PnL 9.4, got %s", result.PnL.String())

	// Verify persistence: query the row directly from SQLite.
	var status string
	err = sqliteDB.DB.QueryRowContext(ctx, "SELECT status FROM paper_trades WHERE id = ?", result.ID).Scan(&status)
	require.NoError(t, err)
	require.Equal(t, "closed", status)
}
