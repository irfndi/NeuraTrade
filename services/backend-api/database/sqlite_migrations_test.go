package database_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSQLiteMigrationsRunFreshThroughPortfolioSnapshots(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 is required for sqlite migration smoke test")
	}

	dbPath := filepath.Join(t.TempDir(), "neuratrade.db")
	cmd := exec.Command("bash", "sqlite-migrate.sh", "run")
	cmd.Env = append(os.Environ(), "SQLITE_PATH="+dbPath)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sqlite migrations failed: %v\n%s", err, output)
	}

	assertSQLiteScalar(t, dbPath,
		"SELECT type FROM sqlite_master WHERE name = 'ohlcv_candles';",
		"view",
	)
	assertSQLiteScalar(t, dbPath,
		"SELECT COUNT(*) FROM schema_migrations WHERE filename = '080_create_scalping_portfolio_snapshots.sql';",
		"1",
	)
	assertSQLiteScalar(t, dbPath,
		"SELECT type FROM sqlite_master WHERE name = 'scalping_portfolio_snapshots';",
		"table",
	)
}

func assertSQLiteScalar(t *testing.T, dbPath, query, want string) {
	t.Helper()

	cmd := exec.Command("sqlite3", dbPath, query)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sqlite query failed: %v\nquery: %s\n%s", err, query, output)
	}

	if got := strings.TrimSpace(string(output)); got != want {
		t.Fatalf("sqlite query mismatch for %q: got %q, want %q", query, got, want)
	}
}
