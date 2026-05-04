package database_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSQLiteMigrationsCreateFundingArbitrageOpportunities(t *testing.T) {
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
}

func copySQLiteMigrationForTest(t *testing.T, sourcePath, targetPath string) {
	t.Helper()

	contents, err := os.ReadFile(sourcePath)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(targetPath, contents, 0o644))
}

func assertSQLiteMigrationScalar(t *testing.T, dbPath, query, want string) {
	t.Helper()

	cmd := exec.Command("sqlite3", dbPath, query)
	output, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "sqlite query failed\nquery: %s\n%s", query, output)
	require.Equalf(t, want, strings.TrimSpace(string(output)), "sqlite query mismatch for %q", query)
}
