package services

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/database"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestPersistScalpingPaperBacktestSoakReportBuildsAcceptanceMetrics(t *testing.T) {
	ctx := context.Background()
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "paper-soak-report.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, sqliteDB.Close())
	})

	now := time.Date(2026, 5, 12, 3, 15, 0, 0, time.UTC)
	engine := newRunSignalsTestEngine(now)
	result, err := engine.RunSignals(ctx, []HistoricalSignal{
		runSignalsTestSignal(now, "AAA/USDT", 100, 0.50, 35),
		runSignalsTestSignal(now.Add(30*time.Second), "BBB/USDT", 50, -0.45, 65),
		runSignalsTestSignal(now.Add(60*time.Second), "AAA/USDT", 102, 0.50, 35),
		runSignalsTestSignal(now.Add(90*time.Second), "BBB/USDT", 49, -0.45, 65),
		runSignalsTestSignal(now.Add(120*time.Second), "CCC/USDT", 25, 0.01, 50),
	})
	require.NoError(t, err)
	require.Equal(t, 2, result.Summary.TotalTrades)
	require.NotEmpty(t, result.Summary.RejectionByReason)
	var rejectionReason string
	for reason, count := range result.Summary.RejectionByReason {
		rejectionReason = reason
		require.Equal(t, 1, count)
		break
	}
	require.NotEmpty(t, rejectionReason)

	baseline := BrokenScalpingBaseline()
	report, err := PersistScalpingPaperBacktestSoakReport(ctx, sqliteDB, result, ScalpingPaperSoakPersistenceOptions{
		ChatID:      "paper-soak-chat",
		Exchange:    "bitget",
		Baseline:    &baseline,
		OrderPrefix: "paper-soak-test",
	})
	require.NoError(t, err)

	fees := decimal.Zero
	for _, trade := range result.Trades {
		fees = fees.Add(trade.Fees)
	}

	require.Equal(t, result.Summary.TotalSignals, report.TotalCycles)
	require.Equal(t, result.Summary.TotalTrades, report.TradeSummary.ClosedTrades)
	require.Equal(t, result.Summary.WinningTrades, report.TradeSummary.Wins)
	require.Equal(t, result.Summary.LosingTrades, report.TradeSummary.Losses)
	require.Equal(t, 1, report.ActionBreakdown["buy"])
	require.Equal(t, 1, report.ActionBreakdown["sell"])
	require.Equal(t, 3, report.ActionBreakdown["hold"])
	require.Equal(t, 1, report.RejectionByReason[rejectionReason])
	require.NotContains(t, report.RejectionByReason, "")
	require.True(t, report.SignalQuality.Coverage.Equal(decimal.NewFromInt(1)))
	require.True(t, report.TradeSummary.NetPnL.Round(8).Equal(result.Summary.TotalPnL.Round(8)))
	require.True(t, report.TradeSummary.Fees.Round(8).Equal(fees.Round(8)))
	require.True(t, report.TradeSummary.ProfitFactor.IsZero())
	require.True(t, report.TradeSummary.ProfitFactorUnbounded)
	require.False(t, report.InsufficientTradeProof)
	require.NotNil(t, report.BaselineComparison)

	secondReport, err := PersistScalpingPaperBacktestSoakReport(ctx, sqliteDB, result, ScalpingPaperSoakPersistenceOptions{
		ChatID:      "paper-soak-chat",
		Exchange:    "bitget",
		OrderPrefix: "paper-soak-test",
	})
	require.NoError(t, err)
	require.Equal(t, result.Summary.TotalTrades*2, secondReport.TradeSummary.ClosedTrades)
}

func TestPersistScalpingPaperBacktestSoakReportRejectsInvalidInputs(t *testing.T) {
	ctx := context.Background()
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "paper-soak-report-invalid.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, sqliteDB.Close())
	})

	_, err = PersistScalpingPaperBacktestSoakReport(ctx, nil, &ScalpingBacktestResult{}, ScalpingPaperSoakPersistenceOptions{})
	require.ErrorContains(t, err, "requires database")

	_, err = PersistScalpingPaperBacktestSoakReport(ctx, sqliteDB, nil, ScalpingPaperSoakPersistenceOptions{})
	require.ErrorContains(t, err, "requires backtest result")

	_, err = PersistScalpingPaperBacktestSoakReport(ctx, sqliteDB, &ScalpingBacktestResult{}, ScalpingPaperSoakPersistenceOptions{})
	require.ErrorContains(t, err, "requires signals")
}
