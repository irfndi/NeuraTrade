package services

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	appautonomy "github.com/irfndi/neuratrade/internal/app/autonomy"
	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/stretchr/testify/require"
)

const (
	scalpingLiveSignalProbeEnv        = "NEURATRADE_SCALPING_LIVE_SIGNAL_PROBE"
	scalpingLiveSignalProbeExchange   = "NEURATRADE_SCALPING_LIVE_SIGNAL_PROBE_EXCHANGE"
	scalpingLiveSignalProbeRequireEnv = "NEURATRADE_SCALPING_LIVE_SIGNAL_PROBE_REQUIRE_VIABLE"
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
