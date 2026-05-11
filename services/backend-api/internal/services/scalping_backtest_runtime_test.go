package services

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/database"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

const (
	scalpingRuntimeDBPathEnv   = "NEURATRADE_SCALPING_BACKTEST_DB_PATH"
	scalpingRuntimeSymbolsEnv  = "NEURATRADE_SCALPING_BACKTEST_SYMBOLS"
	scalpingRuntimeExchangeEnv = "NEURATRADE_SCALPING_BACKTEST_EXCHANGE"
	scalpingRuntimeLimitEnv    = "NEURATRADE_SCALPING_BACKTEST_SYMBOL_LIMIT"
)

func TestScalpingBacktestEngine_RunAgainstRuntimeSQLite(t *testing.T) {
	sqliteDB, config := prepareRuntimeSQLiteBacktest(t)
	result := runPreparedRuntimeSQLiteScalpingBacktest(t, sqliteDB, config)

	t.Logf(
		"runtime scalping backtest: signals=%d eligible=%d trades=%d wins=%d losses=%d win_rate=%s pnl=%s return=%s profit_factor=%s sharpe=%s drawdown=%s symbols=%d rejections=%v gates=%v",
		result.Summary.TotalSignals,
		result.Summary.EligibleSignals,
		result.Summary.TotalTrades,
		result.Summary.WinningTrades,
		result.Summary.LosingTrades,
		result.Summary.WinRate.StringFixed(4),
		result.Summary.TotalPnL.StringFixed(8),
		result.Summary.TotalReturnPct.StringFixed(4),
		result.Summary.ProfitFactor.StringFixed(4),
		result.Summary.SharpeRatio.StringFixed(4),
		result.Summary.MaxDrawdownPct.StringFixed(4),
		len(result.Config.Symbols),
		result.Summary.RejectionByReason,
		result.GateSummary,
	)

	require.Greater(t, result.Summary.TotalSignals, 0, "runtime DB should produce historical signals")
	require.Greater(t, result.Summary.EligibleSignals, 0, "runtime DB should produce gate-eligible signals")
	require.Greater(t, result.Summary.TotalTrades, 0, "runtime DB should produce simulated trades")
	require.NotEmpty(t, result.GateSummary, "runtime DB should populate gate diagnostics")
}

func BenchmarkScalpingBacktestEngine_RuntimeSQLite(b *testing.B) {
	sqliteDB, config := prepareRuntimeSQLiteBacktest(b)
	result := runPreparedRuntimeSQLiteScalpingBacktest(b, sqliteDB, config)
	require.Greater(b, result.Summary.TotalTrades, 0)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result = runPreparedRuntimeSQLiteScalpingBacktest(b, sqliteDB, config)
		require.Greater(b, result.Summary.TotalTrades, 0)
	}
}

func prepareRuntimeSQLiteBacktest(tb testing.TB) (*database.SQLiteDB, ScalpingBacktestConfig) {
	tb.Helper()

	sourcePath := strings.TrimSpace(os.Getenv(scalpingRuntimeDBPathEnv))
	if sourcePath == "" {
		tb.Skipf("set %s to an existing runtime SQLite database to run this validation", scalpingRuntimeDBPathEnv)
	}

	dbPath := copyRuntimeSQLiteDB(tb, sourcePath)
	sqliteDB, err := database.NewSQLiteConnection(dbPath)
	require.NoError(tb, err)
	tb.Cleanup(func() {
		require.NoError(tb, sqliteDB.Close())
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	start, end, rowCount := runtimeMarketDataBounds(tb, ctx, sqliteDB.DB)
	require.Greater(tb, rowCount, 0, "runtime DB should contain market_data rows")

	symbols := runtimeBacktestSymbols(tb, ctx, sqliteDB.DB)
	require.NotEmpty(tb, symbols, "runtime DB should contain market_data symbols")

	exchange := strings.TrimSpace(os.Getenv(scalpingRuntimeExchangeEnv))
	if exchange == "" {
		exchange = "bitget"
	}

	defaults := DefaultAIScalpingConfig()
	config := ScalpingBacktestConfig{
		StartTime:          start,
		EndTime:            end,
		Symbols:            symbols,
		Exchange:           exchange,
		InitialCapital:     decimal.NewFromFloat(48),
		FeeRate:            decimal.NewFromFloat(0.0006),
		SlippagePct:        decimal.NewFromFloat(DefaultScalpingBacktestSlippage),
		MaxBidAskSpreadPct: defaults.MaxBidAskSpreadPct,
		MinConfidence:      defaults.MinConfidence,
		MinExpectancyN:     defaults.MinExpectancyN,
		MinExpectancyEdge:  defaults.MinExpectancyEdge,
		MaxCapitalPct:      defaults.MaxCapitalPct,
		DefaultHoldPeriod:  DefaultScalpingBacktestHoldPeriod,
		SpreadMultiplier:   DefaultScalpingBacktestSpreadMultiplier,
	}

	return sqliteDB, normalizeScalpingBacktestConfig(config)
}

func runPreparedRuntimeSQLiteScalpingBacktest(tb testing.TB, sqliteDB *database.SQLiteDB, config ScalpingBacktestConfig) *ScalpingBacktestResult {
	tb.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	engine := NewScalpingBacktestEngine(sqliteDB, config)
	result, err := engine.Run(ctx)
	require.NoError(tb, err)
	return result
}

func copyRuntimeSQLiteDB(tb testing.TB, sourcePath string) string {
	tb.Helper()

	sourceDB, err := sql.Open("sqlite3", sourcePath)
	require.NoError(tb, err)
	defer sourceDB.Close()

	targetPath := filepath.Join(tb.TempDir(), "runtime-snapshot-"+filepath.Base(sourcePath))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err = sourceDB.ExecContext(ctx, "VACUUM INTO ?", targetPath)
	require.NoError(tb, err)

	return targetPath
}

func runtimeMarketDataBounds(tb testing.TB, ctx context.Context, db *sql.DB) (time.Time, time.Time, int) {
	tb.Helper()

	var startRaw, endRaw string
	var count int
	err := db.QueryRowContext(ctx, `SELECT MIN(timestamp), MAX(timestamp), COUNT(*) FROM market_data`).Scan(&startRaw, &endRaw, &count)
	require.NoError(tb, err)

	return parseRuntimeSQLiteTime(tb, startRaw), parseRuntimeSQLiteTime(tb, endRaw), count
}

func runtimeBacktestSymbols(tb testing.TB, ctx context.Context, db *sql.DB) []string {
	tb.Helper()

	if rawSymbols := strings.TrimSpace(os.Getenv(scalpingRuntimeSymbolsEnv)); rawSymbols != "" {
		parts := strings.Split(rawSymbols, ",")
		symbols := make([]string, 0, len(parts))
		for _, part := range parts {
			if symbol := strings.TrimSpace(part); symbol != "" {
				symbols = append(symbols, symbol)
			}
		}
		return symbols
	}

	limit := 12
	if rawLimit := strings.TrimSpace(os.Getenv(scalpingRuntimeLimitEnv)); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		require.NoError(tb, err)
		require.Positive(tb, parsed)
		limit = parsed
	}

	rows, err := db.QueryContext(ctx, `
		SELECT tp.symbol
		FROM market_data md
		JOIN trading_pairs tp ON tp.id = md.trading_pair_id
		GROUP BY tp.symbol
		ORDER BY COUNT(*) DESC, tp.symbol ASC
		LIMIT ?
	`, limit)
	require.NoError(tb, err)
	defer rows.Close()

	symbols := make([]string, 0, limit)
	for rows.Next() {
		var symbol string
		require.NoError(tb, rows.Scan(&symbol))
		symbols = append(symbols, symbol)
	}
	require.NoError(tb, rows.Err())
	return symbols
}

func parseRuntimeSQLiteTime(tb testing.TB, raw string) time.Time {
	tb.Helper()

	raw = strings.TrimSpace(raw)
	require.NotEmpty(tb, raw)

	layouts := []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999-07:00",
		"2006-01-02 15:04:05-07:00",
		"2006-01-02 15:04:05.999999999Z07:00",
		"2006-01-02 15:04:05.999999Z07:00",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		parsed, err := time.Parse(layout, raw)
		if err == nil {
			return parsed
		}
	}
	tb.Fatalf("unsupported SQLite timestamp %q", raw)
	return time.Time{}
}
