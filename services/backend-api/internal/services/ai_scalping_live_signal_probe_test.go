package services

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appautonomy "github.com/irfndi/neuratrade/internal/app/autonomy"
	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

const (
	scalpingLiveSignalProbeEnv        = "NEURATRADE_SCALPING_LIVE_SIGNAL_PROBE"
	scalpingLivePaperProbeEnv         = "NEURATRADE_SCALPING_LIVE_PAPER_PROBE"
	scalpingLiveSignalProbeExchange   = "NEURATRADE_SCALPING_LIVE_SIGNAL_PROBE_EXCHANGE"
	scalpingLiveSignalProbeRequireEnv = "NEURATRADE_SCALPING_LIVE_SIGNAL_PROBE_REQUIRE_VIABLE"
	scalpingLivePaperProbeRequireEnv  = "NEURATRADE_SCALPING_LIVE_PAPER_PROBE_REQUIRE_TRADES"
	scalpingLivePaperProbeCyclesEnv   = "NEURATRADE_SCALPING_LIVE_PAPER_PROBE_CYCLES"
	scalpingLivePaperProbeIntervalEnv = "NEURATRADE_SCALPING_LIVE_PAPER_PROBE_INTERVAL_MS"
)

func TestAIScalpingService_LiveSignalProbe(t *testing.T) {
	if strings.TrimSpace(os.Getenv(scalpingLiveSignalProbeEnv)) == "" {
		t.Skipf("set %s=1 to fetch public exchange ticker/orderbook data", scalpingLiveSignalProbeEnv)
	}

	exchange := strings.TrimSpace(os.Getenv(scalpingLiveSignalProbeExchange))
	if exchange == "" {
		exchange = "bitget"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ccxtSvc := ccxt.NewNativeCCXTService(15*time.Second, 1)
	require.NoError(t, ccxtSvc.Initialize(ctx))
	t.Cleanup(func() {
		require.NoError(t, ccxtSvc.Close())
	})

	defaults := DefaultAIScalpingConfig()
	defaults.Exchange = exchange
	defaults.MaxPairsToAnalyze = 8
	defaults.MaxCandidatePairs = 24
	defaults.OrderBookPairs = 8
	defaults.AutoExpandOrderBooks = true
	defaults.AutoExecute = false
	defaults.EnforceFutures = false

	svc := &AIScalpingService{
		config:       defaults,
		ccxtService:  ccxtSvc,
		symbolGuards: make(map[string]symbolExecutionGuard),
	}

	signals, err := svc.gatherMarketSignals(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, signals)

	policy := svc.scalpingCyclePolicy(ctx, TradingPortfolio{
		USDTBalance: 48,
		TotalValue:  48,
	})
	funnel := appautonomy.BuildCandidateFunnel(candidateSignalsFromMarketSignals(signals), policy, appautonomy.ApplyScalpingPolicyConfigFromEnv(appautonomy.DefaultScalpingPolicyConfig()))

	withOrderbook := 0
	for _, signal := range signals {
		if signal.BidAskSpread > 0 {
			withOrderbook++
		}
	}
	t.Logf(
		"live scalping signal probe: exchange=%s signals=%d with_orderbook=%d universe=%d ranked=%d viable=%d rejections=%v",
		exchange,
		len(signals),
		withOrderbook,
		funnel.CandidateUniverseCount,
		funnel.CandidateRankedCount,
		funnel.CandidateViableCount,
		funnel.RejectionCounts,
	)
	for _, rejection := range funnel.TopCandidateRejections {
		t.Logf(
			"top rejection: symbol=%s reason=%s confidence=%.4f spread=%.4f imbalance=%.4f range=%.2f change24h=%.4f",
			rejection.Symbol,
			rejection.Reason,
			rejection.EstimatedConfidence,
			rejection.BidAskSpreadPct,
			rejection.OrderBookImbalance,
			rejection.RangePosition24h,
			rejection.PriceChange24hPct,
		)
	}

	require.Greater(t, withOrderbook, 0, "live probe should collect at least one orderbook-backed signal")
	if strings.TrimSpace(os.Getenv(scalpingLiveSignalProbeRequireEnv)) != "" {
		require.Greater(t, funnel.CandidateViableCount, 0, "live probe should produce a viable scalping candidate")
	}
}

func TestAIScalpingService_LivePaperSignalProbe(t *testing.T) {
	if strings.TrimSpace(os.Getenv(scalpingLivePaperProbeEnv)) == "" {
		t.Skipf("set %s=1 to fetch public exchange data and run no-order paper simulation", scalpingLivePaperProbeEnv)
	}

	exchange := strings.TrimSpace(os.Getenv(scalpingLiveSignalProbeExchange))
	if exchange == "" {
		exchange = "bitget"
	}

	cycles := NormalizeScalpingLivePaperSoakCycles(getEnvInt(scalpingLivePaperProbeCyclesEnv))
	interval := NormalizeScalpingLivePaperSoakInterval(time.Duration(getEnvInt(scalpingLivePaperProbeIntervalEnv)) * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), ScalpingLivePaperSoakTimeout(cycles, interval))
	defer cancel()

	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "live-paper-soak.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, sqliteDB.Close())
	})

	baseline := BrokenScalpingBaseline()
	requireTrades := strings.TrimSpace(os.Getenv(scalpingLivePaperProbeRequireEnv)) != ""
	result, err := RunPublicScalpingLivePaperSoak(ctx, sqliteDB, ScalpingLivePaperSoakOptions{
		Exchange:       exchange,
		Cycles:         cycles,
		Interval:       interval,
		ChatID:         "live-paper-probe",
		OrderPrefix:    "live-paper-probe",
		RequireTrades:  requireTrades,
		InitialCapital: decimal.NewFromInt(48),
		FeeRate:        decimal.NewFromFloat(0.0006),
		Baseline:       &baseline,
	})
	require.NoError(t, err)
	require.NotNil(t, result.LastBacktestResult)
	require.Equal(t, result.TotalSignals, result.Report.TotalCycles)
	require.Equal(t, result.TotalTrades, result.Report.TradeSummary.ClosedTrades)
	require.Equal(t, result.WinningTrades, result.Report.TradeSummary.Wins)
	require.Equal(t, result.LosingTrades, result.Report.TradeSummary.Losses)
	require.True(t, result.Report.TradeSummary.NetPnL.Round(8).Equal(result.NetPnL.Round(8)))
	require.True(t, result.Report.TradeSummary.Fees.Round(8).Equal(result.Fees.Round(8)))
	if requireTrades {
		require.False(t, result.InsufficientTradeProof)
	}

	t.Logf(
		"live paper scalping probe: exchange=%s cycles=%d signals=%d eligible=%d paper_trades=%d wins=%d losses=%d win_rate=%s net_pnl=%s fees=%s profit_factor=%s drawdown=%s rejections=%v gates=%v persisted_cycles=%d persisted_trades=%d persisted_net_pnl=%s persisted_signal_quality=%s",
		exchange,
		cycles,
		result.TotalSignals,
		result.EligibleSignals,
		result.TotalTrades,
		result.WinningTrades,
		result.LosingTrades,
		result.Report.TradeSummary.WinRate.StringFixed(4),
		result.NetPnL.StringFixed(8),
		result.Fees.StringFixed(8),
		result.Report.TradeSummary.ProfitFactor.StringFixed(4),
		result.Report.TradeSummary.MaxDrawdownPct.StringFixed(4),
		result.LastRejectionByReason,
		result.LastGateSummary,
		result.Report.TotalCycles,
		result.Report.TradeSummary.ClosedTrades,
		result.Report.TradeSummary.NetPnL.StringFixed(8),
		result.Report.SignalQuality.Coverage.StringFixed(4),
	)

	require.Greater(t, result.TotalSignals, 0, "live paper probe should evaluate public signals")
	if requireTrades {
		require.Greater(t, result.TotalTrades, 0, "live paper probe should produce no-order paper trades")
	}
}
