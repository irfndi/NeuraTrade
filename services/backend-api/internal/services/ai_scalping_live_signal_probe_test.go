package services

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	appautonomy "github.com/irfndi/neuratrade/internal/app/autonomy"
	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

const (
	scalpingLiveSignalProbeEnv        = "NEURATRADE_SCALPING_LIVE_SIGNAL_PROBE"
	scalpingLivePaperProbeEnv         = "NEURATRADE_SCALPING_LIVE_PAPER_PROBE"
	scalpingLiveSignalProbeExchange   = "NEURATRADE_SCALPING_LIVE_SIGNAL_PROBE_EXCHANGE"
	scalpingLiveSignalProbeRequireEnv = "NEURATRADE_SCALPING_LIVE_SIGNAL_PROBE_REQUIRE_VIABLE"
	scalpingLivePaperProbeRequireEnv  = "NEURATRADE_SCALPING_LIVE_PAPER_PROBE_REQUIRE_TRADES"
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
	funnel := appautonomy.BuildCandidateFunnel(candidateSignalsFromMarketSignals(signals), policy)

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

	now := time.Now().UTC()
	historicalSignals := make([]HistoricalSignal, 0, len(signals))
	symbols := make([]string, 0, len(signals))
	for i, signal := range signals {
		symbols = append(symbols, signal.Symbol)
		historicalSignals = append(historicalSignals, HistoricalSignal{
			Timestamp: now.Add(time.Duration(i) * time.Millisecond),
			Symbol:    signal.Symbol,
			Exchange:  exchange,
			Signal:    signal,
		})
	}

	engine := NewScalpingBacktestEngine(nil, ScalpingBacktestConfig{
		StartTime:          now.Add(-time.Second),
		EndTime:            now.Add(time.Second),
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
	})
	result, err := engine.RunSignals(ctx, historicalSignals)
	require.NoError(t, err)

	fees := decimal.Zero
	for _, trade := range result.Trades {
		fees = fees.Add(trade.Fees)
	}
	require.Len(t, result.Trades, result.Summary.TotalTrades, "trade slice length should match summary total trades")
	require.True(t, fees.GreaterThanOrEqual(decimal.Zero), "aggregate fees should never be negative")
	require.LessOrEqual(
		t,
		result.Summary.WinningTrades+result.Summary.LosingTrades,
		result.Summary.TotalTrades,
		"win/loss counts should not exceed total trades",
	)
	t.Logf(
		"live paper scalping probe: exchange=%s signals=%d eligible=%d paper_trades=%d wins=%d losses=%d win_rate=%s net_pnl=%s fees=%s profit_factor=%s drawdown=%s rejections=%v gates=%v",
		exchange,
		result.Summary.TotalSignals,
		result.Summary.EligibleSignals,
		result.Summary.TotalTrades,
		result.Summary.WinningTrades,
		result.Summary.LosingTrades,
		result.Summary.WinRate.StringFixed(4),
		result.Summary.TotalPnL.StringFixed(8),
		fees.StringFixed(8),
		result.Summary.ProfitFactor.StringFixed(4),
		result.Summary.MaxDrawdownPct.StringFixed(4),
		result.Summary.RejectionByReason,
		result.GateSummary,
	)

	require.Greater(t, result.Summary.TotalSignals, 0, "live paper probe should evaluate public signals")
	if strings.TrimSpace(os.Getenv(scalpingLivePaperProbeRequireEnv)) != "" {
		require.Greater(t, result.Summary.TotalTrades, 0, "live paper probe should produce no-order paper trades")
	}
}
