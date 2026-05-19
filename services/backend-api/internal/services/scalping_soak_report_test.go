package services

import (
	"context"
	"fmt"
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
	require.False(t, report.LiveTrialReadiness.Ready)
	require.Equal(t, DefaultScalpingLiveTrialMinClosedTrades, report.LiveTrialReadiness.MinClosedTrades)
	require.Contains(t, report.LiveTrialReadiness.Reasons, "closed_trades_below_live_trial_minimum")
	require.Contains(t, report.LiveTrialReadiness.Reasons, "signal_quality_incomplete")
	require.Contains(t, report.LiveTrialReadiness.Reasons, "ai_provider_degraded")

	require.Equal(t, 1, report.AIProviderDegradation.DegradedCycles)
	require.Equal(t, 1, report.AIProviderDegradation.ByReason["ai_unavailable"])
	require.NotNil(t, report.BaselineComparison)
	require.True(t, report.BaselineComparison.DeltaNetPnL.Round(6).Equal(decimal.NewFromFloat(0.20)))
}

func TestBuildScalpingSoakReportSupportsLegacyTelemetryWithoutRecentMomentum(t *testing.T) {
	ctx := context.Background()
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "legacy-scalping-soak-report.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, sqliteDB.Close())
	})

	_, err = sqliteDB.Exec(ctx, `
		CREATE TABLE scalping_cycle_telemetry (
			id TEXT PRIMARY KEY,
			chat_id TEXT,
			exchange TEXT,
			order_id TEXT,
			cycle_at TIMESTAMP,
			symbol TEXT,
			action TEXT,
			regime TEXT,
			gate_block_code TEXT,
			rejection_counts TEXT,
			bid_ask_spread_pct REAL,
			order_book_imbalance REAL,
			range_position_24h REAL,
			price_change_24h_pct REAL,
			outcome TEXT,
			pnl NUMERIC,
			hold_duration_seconds INT
		)
	`)
	require.NoError(t, err)
	_, err = sqliteDB.Exec(ctx, `
		CREATE TABLE realized_pnl_journal (
			order_id TEXT,
			realized_pnl NUMERIC,
			fees NUMERIC
		)
	`)
	require.NoError(t, err)

	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	_, err = sqliteDB.Exec(ctx, `
		INSERT INTO scalping_cycle_telemetry (
			id, chat_id, exchange, order_id, cycle_at, symbol, action, regime,
			rejection_counts, bid_ask_spread_pct, order_book_imbalance,
			range_position_24h, price_change_24h_pct, outcome, pnl, hold_duration_seconds
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "legacy-cycle-1", "chat-legacy", "bitget", "", now, "BTC/USDT", "hold", "neutral",
		`{"no_directional_edge":1}`, 0.04, 0.22, 42.0, 0.3, "", "0", 0)
	require.NoError(t, err)

	report, err := BuildScalpingSoakReport(ctx, sqliteDB, ScalpingSoakReportFilter{
		ChatID:   "chat-legacy",
		Exchange: "bitget",
		Since:    now.Add(-time.Minute),
		Until:    now.Add(time.Minute),
	})
	require.NoError(t, err)

	require.Equal(t, 1, report.TotalCycles)
	require.Equal(t, 1, report.SignalQuality.KnownCycles)
	require.True(t, report.SignalQuality.Coverage.Equal(decimal.NewFromInt(1)))
	require.True(t, report.SignalQuality.AvgRecentPriceChangePct.Equal(decimal.Zero))
	require.Equal(t, 1, report.RejectionByReason["no_directional_edge"])
}

func TestBuildScalpingSoakReportUsesPnLSignAndSparseAverageDenominators(t *testing.T) {
	ctx := context.Background()
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "scalping-soak-report-sparse.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, sqliteDB.Close())
	})

	telemetryStore := NewScalpingTelemetryStore(sqliteDB, nil)
	require.NoError(t, telemetryStore.EnsureSchema(ctx))
	lifecycleStore, err := NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)

	now := time.Date(2026, 5, 11, 14, 0, 0, 0, time.UTC)
	insertSoakClosedTradeWithNullableHold(t, ctx, lifecycleStore, telemetryStore, now.Add(-2*time.Minute), CycleRecord{
		ID:                  "cycle-negative-labelled-win",
		ChatID:              "chat-1",
		Exchange:            "bitget",
		OrderID:             "ord-negative-labelled-win",
		CycleAt:             now.Add(-2 * time.Minute),
		Symbol:              "BTC/USDT",
		Action:              "buy",
		Regime:              "trend",
		BidAskSpreadPct:     floatPtr(0.02),
		RejectionCountsJSON: `{}`,
	}, decimal.NewFromFloat(-0.04), decimal.Zero, "win", "-0.04", nil)

	holdSeconds := 60
	insertSoakClosedTradeWithNullableHold(t, ctx, lifecycleStore, telemetryStore, now.Add(-1*time.Minute), CycleRecord{
		ID:                 "cycle-positive-labelled-loss",
		ChatID:             "chat-1",
		Exchange:           "bitget",
		OrderID:            "ord-positive-labelled-loss",
		CycleAt:            now.Add(-1 * time.Minute),
		Symbol:             "ETH/USDT",
		Action:             "sell",
		Regime:             "range",
		BidAskSpreadPct:    floatPtr(0.06),
		OrderBookImbalance: floatPtr(-0.5),
	}, decimal.NewFromFloat(0.06), decimal.Zero, "loss", "0.06", &holdSeconds)

	report, err := BuildScalpingSoakReport(ctx, sqliteDB, ScalpingSoakReportFilter{
		ChatID:   "chat-1",
		Exchange: "bitget",
		Since:    now.Add(-10 * time.Minute),
		Until:    now.Add(time.Minute),
	})
	require.NoError(t, err)

	require.Equal(t, 2, report.TradeSummary.ClosedTrades)
	require.Equal(t, 1, report.TradeSummary.Wins, "win count should follow net PnL sign, not stale outcome label")
	require.Equal(t, 1, report.TradeSummary.Losses, "loss count should follow net PnL sign, not stale outcome label")
	require.True(t, report.TradeSummary.ProfitFactor.Round(6).Equal(decimal.NewFromFloat(1.5)))
	require.True(t, report.TradeSummary.AvgHoldDurationSec.Round(6).Equal(decimal.NewFromInt(60)))
	require.True(t, report.SignalQuality.AvgBidAskSpreadPct.Round(6).Equal(decimal.NewFromFloat(0.04)))
	require.True(t, report.SignalQuality.AvgAbsOrderBookImbalance.Round(6).Equal(decimal.NewFromFloat(0.5)))
}

func TestBuildScalpingSoakReportCountsFirstTradeLossAsDrawdown(t *testing.T) {
	ctx := context.Background()
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "scalping-soak-report-first-loss.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, sqliteDB.Close())
	})

	telemetryStore := NewScalpingTelemetryStore(sqliteDB, nil)
	require.NoError(t, telemetryStore.EnsureSchema(ctx))
	lifecycleStore, err := NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)

	now := time.Date(2026, 5, 13, 1, 0, 0, 0, time.UTC)
	insertSoakClosedTrade(t, ctx, lifecycleStore, telemetryStore, now, CycleRecord{
		ID:                  "cycle-first-loss",
		ChatID:              "chat-1",
		Exchange:            "bitget",
		OrderID:             "ord-first-loss",
		CycleAt:             now,
		Symbol:              "BTC/USDT",
		Action:              "buy",
		Regime:              "neutral",
		BidAskSpreadPct:     floatPtr(0.02),
		RejectionCountsJSON: `{}`,
	}, decimal.NewFromFloat(-0.03), decimal.NewFromFloat(-0.01), "loss", "-0.04", 60)

	baseline := BrokenScalpingBaseline()
	report, err := BuildScalpingSoakReport(ctx, sqliteDB, ScalpingSoakReportFilter{
		ChatID:   "chat-1",
		Exchange: "bitget",
		Since:    now.Add(-time.Minute),
		Until:    now.Add(time.Minute),
		Baseline: &baseline,
	})
	require.NoError(t, err)

	require.True(t, report.TradeSummary.MaxDrawdown.Round(6).Equal(decimal.NewFromFloat(0.04)))
	require.True(t, report.TradeSummary.MaxDrawdownPct.Round(6).Equal(decimal.NewFromFloat(0.000833)))
}

func TestBuildScalpingSoakReportExplainsMissingDrawdownBaseline(t *testing.T) {
	ctx := context.Background()
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "scalping-soak-report-no-baseline.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, sqliteDB.Close())
	})

	telemetryStore := NewScalpingTelemetryStore(sqliteDB, nil)
	require.NoError(t, telemetryStore.EnsureSchema(ctx))
	lifecycleStore, err := NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)

	now := time.Date(2026, 5, 11, 14, 0, 0, 0, time.UTC)
	for i := 0; i < DefaultScalpingLiveTrialMinClosedTrades; i++ {
		action := "buy"
		if i%2 == 1 {
			action = "sell"
		}
		holdSeconds := 45
		grossPnL := decimal.NewFromFloat(0.20)
		outcome := "win"
		telemetryPnL := "0.19"
		if i == DefaultScalpingLiveTrialMinClosedTrades-1 {
			grossPnL = decimal.NewFromFloat(-0.01)
			outcome = "loss"
			telemetryPnL = "-0.02"
		}
		insertSoakClosedTradeWithNullableHold(t, ctx, lifecycleStore, telemetryStore, now.Add(time.Duration(i)*time.Second), CycleRecord{
			ID:                 fmt.Sprintf("cycle-profitable-%02d", i),
			ChatID:             "chat-1",
			Exchange:           "bitget",
			OrderID:            fmt.Sprintf("ord-profitable-%02d", i),
			CycleAt:            now.Add(time.Duration(i) * time.Second),
			Symbol:             "BTC/USDT",
			Action:             action,
			Regime:             "range",
			BidAskSpreadPct:    floatPtr(0.02),
			OrderBookImbalance: floatPtr(0.30),
			RangePosition24h:   floatPtr(35),
			PriceChange24hPct:  floatPtr(0.2),
		}, grossPnL, decimal.NewFromFloat(-0.01), outcome, telemetryPnL, &holdSeconds)
	}

	report, err := BuildScalpingSoakReport(ctx, sqliteDB, ScalpingSoakReportFilter{
		ChatID:   "chat-1",
		Exchange: "bitget",
		Since:    now.Add(-time.Minute),
		Until:    now.Add(time.Minute),
	})
	require.NoError(t, err)

	require.False(t, report.LiveTrialReadiness.Ready)
	require.Contains(t, report.LiveTrialReadiness.Reasons, "drawdown_baseline_missing")
	require.NotContains(t, report.LiveTrialReadiness.Reasons, "drawdown_not_observed")
}

func TestBuildScalpingSoakReportLiveReadinessAllowsSelectiveScalping(t *testing.T) {
	ctx := context.Background()
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "scalping-soak-report-selective.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, sqliteDB.Close())
	})

	telemetryStore := NewScalpingTelemetryStore(sqliteDB, nil)
	require.NoError(t, telemetryStore.EnsureSchema(ctx))
	lifecycleStore, err := NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)

	now := time.Date(2026, 5, 19, 4, 30, 0, 0, time.UTC)
	for i := 0; i < 80; i++ {
		insertSoakCycle(t, ctx, telemetryStore, CycleRecord{
			ID:                 fmt.Sprintf("cycle-hold-%02d", i),
			ChatID:             "chat-1",
			Exchange:           "bitget",
			CycleAt:            now.Add(time.Duration(i) * time.Second),
			Symbol:             "BTC/USDT",
			Action:             "hold",
			Regime:             "neutral",
			BidAskSpreadPct:    floatPtr(0.02),
			OrderBookImbalance: floatPtr(0.10),
			RangePosition24h:   floatPtr(50),
			PriceChange24hPct:  floatPtr(0.05),
		})
	}
	for i := 0; i < DefaultScalpingLiveTrialMinClosedTrades; i++ {
		action := "buy"
		if i%2 == 1 {
			action = "sell"
		}
		grossPnL := decimal.NewFromFloat(0.20)
		outcome := "win"
		telemetryPnL := "0.19"
		if i == 0 {
			grossPnL = decimal.NewFromFloat(-0.03)
			outcome = "loss"
			telemetryPnL = "-0.04"
		}
		insertSoakClosedTrade(t, ctx, lifecycleStore, telemetryStore, now.Add(time.Duration(100+i)*time.Second), CycleRecord{
			ID:                 fmt.Sprintf("cycle-trade-%02d", i),
			ChatID:             "chat-1",
			Exchange:           "bitget",
			OrderID:            fmt.Sprintf("ord-trade-%02d", i),
			CycleAt:            now.Add(time.Duration(100+i) * time.Second),
			Symbol:             "BTC/USDT",
			Action:             action,
			Regime:             "trend",
			BidAskSpreadPct:    floatPtr(0.02),
			OrderBookImbalance: floatPtr(0.30),
			RangePosition24h:   floatPtr(35),
			PriceChange24hPct:  floatPtr(0.2),
		}, grossPnL, decimal.NewFromFloat(-0.01), outcome, telemetryPnL, 60)
	}

	baseline := BrokenScalpingBaseline()
	report, err := BuildScalpingSoakReport(ctx, sqliteDB, ScalpingSoakReportFilter{
		ChatID:   "chat-1",
		Exchange: "bitget",
		Since:    now.Add(-time.Minute),
		Until:    now.Add(3 * time.Minute),
		Baseline: &baseline,
	})
	require.NoError(t, err)

	require.True(t, report.ActionSplit["hold"].GreaterThan(decimal.NewFromFloat(0.745)))
	require.True(t, report.LiveTrialReadiness.Ready)
	require.Empty(t, report.LiveTrialReadiness.Reasons)
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

func insertSoakClosedTradeWithNullableHold(
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
	holdDurationSeconds *int,
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
	var holdDurationArg any
	if holdDurationSeconds != nil {
		holdDurationArg = *holdDurationSeconds
	}
	_, err := telemetryStore.db.Exec(ctx, telemetryStore.bindQuery(`
		UPDATE scalping_cycle_telemetry
		SET outcome = ?,
			pnl = CAST(? AS NUMERIC),
			hold_duration_seconds = ?,
			closed_at = ?
		WHERE order_id = ?
	`), outcome, telemetryPnL, holdDurationArg, closedAt, record.OrderID)
	require.NoError(t, err)
}
