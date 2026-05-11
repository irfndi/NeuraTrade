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

func TestBuildScalpingSoakReportSummarizesAcceptanceMetrics(t *testing.T) {
	ctx := context.Background()
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "scalping-soak-report.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, sqliteDB.Close())
	})

	telemetryStore := NewScalpingTelemetryStore(sqliteDB, nil)
	require.NoError(t, telemetryStore.EnsureSchema(ctx))
	lifecycleStore, err := NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)

	now := time.Date(2026, 5, 11, 13, 30, 0, 0, time.UTC)
	insertSoakCycle(t, ctx, telemetryStore, CycleRecord{
		ID:                    "cycle-hold-ai",
		ChatID:                "chat-1",
		Exchange:              "bitget",
		CycleAt:               now.Add(-4 * time.Minute),
		Action:                "hold",
		Regime:                "neutral",
		GateBlockCode:         "ai_unavailable",
		RejectionCountsJSON:   `{"missing_orderbook_signal":2}`,
		BidAskSpreadPct:       floatPtr(0.02),
		OrderBookImbalance:    floatPtr(0.15),
		RangePosition24h:      floatPtr(40),
		PriceChange24hPct:     floatPtr(0.5),
		UniverseCount:         8,
		RankedCount:           8,
		ViableCount:           0,
		PolicyAdjustmentsJSON: `[]`,
	})

	insertSoakClosedTrade(t, ctx, lifecycleStore, telemetryStore, now.Add(-3*time.Minute), CycleRecord{
		ID:                  "cycle-buy-win",
		ChatID:              "chat-1",
		Exchange:            "bitget",
		OrderID:             "ord-win",
		CycleAt:             now.Add(-3 * time.Minute),
		Symbol:              "BTC/USDT",
		Action:              "buy",
		Regime:              "trend",
		BidAskSpreadPct:     floatPtr(0.03),
		OrderBookImbalance:  floatPtr(0.35),
		RangePosition24h:    floatPtr(55),
		PriceChange24hPct:   floatPtr(1.2),
		RejectionCountsJSON: `{}`,
	}, decimal.NewFromFloat(0.10), decimal.NewFromFloat(-0.02), "win", "0.08", 120)

	insertSoakClosedTrade(t, ctx, lifecycleStore, telemetryStore, now.Add(-2*time.Minute), CycleRecord{
		ID:                  "cycle-sell-loss",
		ChatID:              "chat-1",
		Exchange:            "bitget",
		OrderID:             "ord-loss",
		CycleAt:             now.Add(-2 * time.Minute),
		Symbol:              "ETH/USDT",
		Action:              "sell",
		Regime:              "neutral",
		BidAskSpreadPct:     floatPtr(0.04),
		OrderBookImbalance:  floatPtr(-0.25),
		RangePosition24h:    floatPtr(65),
		PriceChange24hPct:   floatPtr(-0.7),
		RejectionCountsJSON: `{}`,
	}, decimal.NewFromFloat(-0.05), decimal.NewFromFloat(-0.01), "loss", "-0.06", 180)

	insertSoakCycle(t, ctx, telemetryStore, CycleRecord{
		ID:                  "cycle-hold-spread",
		ChatID:              "chat-1",
		Exchange:            "bitget",
		CycleAt:             now.Add(-1 * time.Minute),
		Action:              "hold",
		Regime:              "range",
		RejectionCountsJSON: `{"spread_too_wide":1}`,
	})

	baseline := BrokenScalpingBaseline()
	report, err := BuildScalpingSoakReport(ctx, sqliteDB, ScalpingSoakReportFilter{
		ChatID:   "chat-1",
		Exchange: "bitget",
		Since:    now.Add(-10 * time.Minute),
		Until:    now.Add(time.Minute),
		Baseline: &baseline,
	})
	require.NoError(t, err)

	require.Equal(t, 4, report.TotalCycles)
	require.Equal(t, map[string]int{"hold": 2, "buy": 1, "sell": 1}, report.ActionBreakdown)
	require.True(t, report.ActionSplit["hold"].Equal(decimal.NewFromFloat(0.5)))
	require.Equal(t, 2, report.RegimeBreakdown["neutral"])
	require.Equal(t, 2, report.RejectionByReason["missing_orderbook_signal"])
	require.Equal(t, 1, report.RejectionByReason["spread_too_wide"])
	require.Equal(t, 1, report.GateBlockByCode["ai_unavailable"])

	require.Equal(t, 3, report.SignalQuality.KnownCycles)
	require.True(t, report.SignalQuality.Coverage.Equal(decimal.NewFromFloat(0.75)))
	require.Equal(t, 1, report.SignalQuality.MissingSignalQualityCycles)

	require.Equal(t, 2, report.TradeSummary.ClosedTrades)
	require.Equal(t, 1, report.TradeSummary.Wins)
	require.Equal(t, 1, report.TradeSummary.Losses)
	require.True(t, report.TradeSummary.WinRate.Equal(decimal.NewFromFloat(0.5)))
	require.True(t, report.TradeSummary.GrossPnL.Round(6).Equal(decimal.NewFromFloat(0.05)))
	require.True(t, report.TradeSummary.NetPnL.Round(6).Equal(decimal.NewFromFloat(0.02)))
	require.True(t, report.TradeSummary.Fees.Round(6).Equal(decimal.NewFromFloat(-0.03)))
	require.True(t, report.TradeSummary.AvgNetPnLPerTrade.Round(6).Equal(decimal.NewFromFloat(0.01)))
	require.True(t, report.TradeSummary.MaxDrawdown.Round(6).Equal(decimal.NewFromFloat(0.06)))
	require.True(t, report.TradeSummary.MaxDrawdownPct.Round(6).Equal(decimal.NewFromFloat(0.00125)))
	require.True(t, report.TradeSummary.ProfitFactor.Round(6).Equal(decimal.NewFromFloat(1.333333)))
	require.False(t, report.InsufficientTradeProof)

	require.Equal(t, 1, report.AIProviderDegradation.DegradedCycles)
	require.Equal(t, 1, report.AIProviderDegradation.ByReason["ai_unavailable"])
	require.NotNil(t, report.BaselineComparison)
	require.True(t, report.BaselineComparison.DeltaNetPnL.Round(6).Equal(decimal.NewFromFloat(0.20)))
}

func TestScalpingSoakReport_RunAgainstRuntimeSQLite(t *testing.T) {
	sqliteDB, _ := prepareRuntimeSQLiteBacktest(t)

	ctx := context.Background()
	telemetryStore := NewScalpingTelemetryStore(sqliteDB, nil)
	require.NoError(t, telemetryStore.EnsureSchema(ctx))
	_, err := NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)

	baseline := BrokenScalpingBaseline()
	report, err := BuildScalpingSoakReport(ctx, sqliteDB, ScalpingSoakReportFilter{
		Exchange: "bitget",
		Since:    time.Unix(0, 0).UTC(),
		Until:    time.Now().UTC().Add(24 * time.Hour),
		Baseline: &baseline,
	})
	require.NoError(t, err)
	t.Logf(
		"runtime scalping soak report: cycles=%d actions=%v regimes=%v trades=%d wins=%d losses=%d win_rate=%s gross_pnl=%s net_pnl=%s fees=%s avg_net_pnl=%s drawdown=%s signal_quality_coverage=%s rejections=%v gates=%v ai_degraded=%v baseline_delta_net_pnl=%s",
		report.TotalCycles,
		report.ActionBreakdown,
		report.RegimeBreakdown,
		report.TradeSummary.ClosedTrades,
		report.TradeSummary.Wins,
		report.TradeSummary.Losses,
		report.TradeSummary.WinRate.StringFixed(4),
		report.TradeSummary.GrossPnL.StringFixed(8),
		report.TradeSummary.NetPnL.StringFixed(8),
		report.TradeSummary.Fees.StringFixed(8),
		report.TradeSummary.AvgNetPnLPerTrade.StringFixed(8),
		report.TradeSummary.MaxDrawdown.StringFixed(8),
		report.SignalQuality.Coverage.StringFixed(4),
		report.RejectionByReason,
		report.GateBlockByCode,
		report.AIProviderDegradation.ByReason,
		report.BaselineComparison.DeltaNetPnL.StringFixed(8),
	)
}

func insertSoakCycle(t *testing.T, ctx context.Context, store *ScalpingTelemetryStore, record CycleRecord) {
	t.Helper()
	_, err := store.InsertCycleRecord(ctx, record)
	require.NoError(t, err)
}

func insertSoakClosedTrade(
	t *testing.T,
	ctx context.Context,
	lifecycleStore *TradingLifecycleStore,
	telemetryStore *ScalpingTelemetryStore,
	closedAt time.Time,
	record CycleRecord,
	grossPnL decimal.Decimal,
	fees decimal.Decimal,
	outcome string,
	telemetryPnL string,
	holdDurationSeconds int,
) {
	t.Helper()
	insertSoakCycle(t, ctx, telemetryStore, record)
	require.NoError(t, lifecycleStore.RecordClosedOrder(ctx, LifecycleCloseRecord{
		OrderID:     record.OrderID,
		ChatID:      record.ChatID,
		Exchange:    record.Exchange,
		Symbol:      record.Symbol,
		Side:        record.Action,
		MarketType:  "futures",
		Filled:      decimal.NewFromFloat(0.01),
		EntryPrice:  decimal.NewFromFloat(100),
		ExitPrice:   decimal.NewFromFloat(101),
		RealizedPnL: grossPnL,
		Fees:        fees,
		Source:      "autonomous_scalping",
		ClosedAt:    closedAt,
	}))
	require.NoError(t, telemetryStore.UpdateCycleOutcome(ctx, record.OrderID, ScalpingOutcomeRecord{
		Outcome:             outcome,
		PnL:                 telemetryPnL,
		HoldDurationSeconds: holdDurationSeconds,
		ClosedAt:            closedAt,
	}))
}
