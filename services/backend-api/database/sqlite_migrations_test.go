package database_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSQLiteMigrationsRunFreshThroughPortfolioSnapshots(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 is required for sqlite migration smoke test")
	}

	dbPath := filepath.Join(t.TempDir(), "neuratrade.db")
	cmd := exec.Command("bash", "sqlite-migrate.sh", "run")
	cmd.Env = append(os.Environ(), "SQLITE_PATH="+dbPath)

	output, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "sqlite migrations failed\n%s", output)

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

func TestSQLiteMigrationsCreateFundingArbitrageOpportunities(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skipf("sqlite3 CLI not available: %v", err)
	}

	backendRoot, err := filepath.Abs("..")
	require.NoError(t, err)

	sourceDir := filepath.Join(backendRoot, "database", "sqlite_migrations")
	migrationsDir := filepath.Join(t.TempDir(), "sqlite_migrations")
	require.NoError(t, os.Mkdir(migrationsDir, 0o755))

	for _, name := range []string{
		"001_initial_schema.sql",
		"002_add_semantic_memory.sql",
		"003_add_missing_tables.sql",
		"004_add_ccxt_exchanges.sql",
		"005_add_blacklist_columns.sql",
		"006_add_blacklist_active.sql",
		"007_add_market_data.sql",
		"008_add_funding_rates.sql",
		"009_add_arbitrage_tables.sql",
		"010_add_multi_leg_arbitrage.sql",
		"011_create_funding_arbitrage_opportunities.sql",
	} {
		copySQLiteMigrationForTest(t, filepath.Join(sourceDir, name), filepath.Join(migrationsDir, name))
	}

	dbPath := filepath.Join(t.TempDir(), "neuratrade.db")
	cmd := exec.Command("bash", filepath.Join(backendRoot, "database", "sqlite-migrate.sh"), "run")
	cmd.Env = append(os.Environ(),
		"SQLITE_PATH="+dbPath,
		"SQLITE_MIGRATIONS_DIR="+migrationsDir,
	)
	output, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "sqlite migrations failed:\n%s", output)

	assertSQLiteMigrationScalar(
		t,
		dbPath,
		"SELECT type FROM sqlite_master WHERE name = 'funding_arbitrage_opportunities'",
		"table",
	)
	assertSQLiteMigrationScalar(
		t,
		dbPath,
		"SELECT COUNT(*) FROM schema_migrations WHERE filename = '011_create_funding_arbitrage_opportunities.sql'",
		"1",
	)
	assertSQLiteMigrationScalar(
		t,
		dbPath,
		"SELECT COUNT(*) FROM pragma_index_list('funding_arbitrage_opportunities') WHERE name = 'idx_funding_arbitrage_expires'",
		"1",
	)
	assertSQLiteMigrationScalar(
		t,
		dbPath,
		"SELECT COUNT(*) FROM pragma_table_info('funding_arbitrage_opportunities') WHERE name IN ('trading_pair_id', 'long_exchange_id', 'short_exchange_id') AND \"notnull\" = 1",
		"3",
	)
	assertSQLiteMigrationScalar(
		t,
		dbPath,
		"SELECT COUNT(*) FROM pragma_foreign_key_list('funding_arbitrage_opportunities') WHERE \"table\" = 'trading_pairs' AND \"from\" = 'trading_pair_id'",
		"1",
	)
	assertSQLiteMigrationScalar(
		t,
		dbPath,
		"SELECT COUNT(*) FROM sqlite_master WHERE name = 'funding_arbitrage_opportunities' AND sql LIKE '%CHECK (long_exchange_id <> short_exchange_id)%'",
		"1",
	)
}

func TestSQLiteMigrationsRepairLegacyFundingArbitrageOpportunities(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skipf("sqlite3 CLI not available: %v", err)
	}

	backendRoot, err := filepath.Abs("..")
	require.NoError(t, err)

	sourceDir := filepath.Join(backendRoot, "database", "sqlite_migrations")
	migrationsDir := filepath.Join(t.TempDir(), "sqlite_migrations")
	require.NoError(t, os.Mkdir(migrationsDir, 0o755))
	copySQLiteMigrationForTest(
		t,
		filepath.Join(sourceDir, "011_create_funding_arbitrage_opportunities.sql"),
		filepath.Join(migrationsDir, "011_create_funding_arbitrage_opportunities.sql"),
	)

	dbPath := filepath.Join(t.TempDir(), "neuratrade.db")
	legacySchema := strings.Join([]string{
		"CREATE TABLE funding_arbitrage_opportunities (",
		"  id TEXT PRIMARY KEY,",
		"  symbol TEXT NOT NULL,",
		"  buy_exchange_id INTEGER NOT NULL,",
		"  sell_exchange_id INTEGER NOT NULL,",
		"  funding_rate_buy REAL NOT NULL,",
		"  funding_rate_sell REAL NOT NULL,",
		"  rate_difference REAL NOT NULL,",
		"  apy REAL NOT NULL,",
		"  risk_score REAL NOT NULL,",
		"  is_active INTEGER DEFAULT 1,",
		"  expires_at TEXT NOT NULL,",
		"  detected_at TEXT NOT NULL,",
		"  created_at TEXT DEFAULT CURRENT_TIMESTAMP",
		");",
		"CREATE INDEX idx_funding_arbitrage_active ON funding_arbitrage_opportunities(is_active, detected_at DESC) WHERE is_active = 1;",
	}, "\n")
	cmd := exec.Command("sqlite3", dbPath, legacySchema)
	output, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "legacy schema setup failed:\n%s", output)

	cmd = exec.Command("bash", filepath.Join(backendRoot, "database", "sqlite-migrate.sh"), "run")
	cmd.Env = append(os.Environ(),
		"SQLITE_PATH="+dbPath,
		"SQLITE_MIGRATIONS_DIR="+migrationsDir,
	)
	output, err = cmd.CombinedOutput()
	require.NoErrorf(t, err, "sqlite migrations failed:\n%s", output)
	require.Contains(t, string(output), "repair funding_arbitrage_opportunities -> funding_arbitrage_opportunities_legacy_")

	assertSQLiteMigrationScalar(
		t,
		dbPath,
		"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name LIKE 'funding_arbitrage_opportunities_legacy_%'",
		"1",
	)
	assertSQLiteMigrationScalar(
		t,
		dbPath,
		"SELECT COUNT(*) FROM pragma_table_info('funding_arbitrage_opportunities') WHERE name = 'estimated_profit_percentage'",
		"1",
	)
	assertSQLiteMigrationScalar(
		t,
		dbPath,
		"SELECT tbl_name FROM sqlite_master WHERE type = 'index' AND name = 'idx_funding_arbitrage_profit'",
		"funding_arbitrage_opportunities",
	)
}

func assertSQLiteScalar(t *testing.T, dbPath, query, want string) {
	t.Helper()

	cmd := exec.Command("sqlite3", dbPath, query)
	output, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "sqlite query failed\nquery: %s\n%s", query, output)
	require.Equalf(t, want, strings.TrimSpace(string(output)), "sqlite query mismatch for %q", query)
}

func copySQLiteMigrationForTest(t *testing.T, src, dst string) {
	t.Helper()

	info, err := os.Stat(src)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))

	contents, err := os.ReadFile(src)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(dst, contents, info.Mode().Perm()))
}

func assertSQLiteMigrationScalar(t *testing.T, dbPath, query, want string) {
	t.Helper()

	assertSQLiteScalar(t, dbPath, query, want)
}
