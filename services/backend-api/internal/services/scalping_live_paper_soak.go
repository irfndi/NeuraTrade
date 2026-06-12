package services

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/shopspring/decimal"
)

const (
	DefaultScalpingLivePaperSoakCycles      = 1
	MaxScalpingLivePaperSoakCycles          = 120
	MaxScalpingLivePaperSoakInterval        = time.Minute
	scalpingLivePaperSoakBaseTimeout        = 30 * time.Second
	scalpingLivePaperSoakPerCycleTimeout    = 30 * time.Second
	defaultScalpingLivePaperSoakExchange    = "bitget"
	defaultScalpingLivePaperSoakChatID      = "live-paper-probe"
	defaultScalpingLivePaperSoakOrderPrefix = "live-paper-probe"
)

type ScalpingLivePaperSoakOptions struct {
	Exchange          string
	Cycles            int
	Interval          time.Duration
	ChatID            string
	OrderPrefix       string
	RequireTrades     bool
	InitialCapital    decimal.Decimal
	FeeRate           decimal.Decimal
	HoldPeriod        time.Duration
	MaxPairsToAnalyze *int
	MaxCandidatePairs int
	OrderBookPairs    int
	Baseline          *ScalpingSoakBaseline
}

type ScalpingLivePaperSoakResult struct {
	Exchange               string                  `json:"exchange"`
	Cycles                 int                     `json:"cycles"`
	TotalSignals           int                     `json:"total_signals"`
	EligibleSignals        int                     `json:"eligible_signals"`
	TotalTrades            int                     `json:"total_trades"`
	OpenPositions          int                     `json:"open_positions"`
	WinningTrades          int                     `json:"winning_trades"`
	LosingTrades           int                     `json:"losing_trades"`
	NetPnL                 decimal.Decimal         `json:"net_pnl"`
	Fees                   decimal.Decimal         `json:"fees"`
	Report                 ScalpingSoakReport      `json:"report"`
	LastRejectionByReason  map[string]int          `json:"last_rejection_by_reason,omitempty"`
	LastGateSummary        []GateSummaryEntry      `json:"last_gate_summary,omitempty"`
	LastBacktestResult     *ScalpingBacktestResult `json:"-"`
	InsufficientTradeProof bool                    `json:"insufficient_trade_proof"`
}

func NormalizeScalpingLivePaperSoakCycles(cycles int) int {
	if cycles <= 0 {
		return DefaultScalpingLivePaperSoakCycles
	}
	if cycles > MaxScalpingLivePaperSoakCycles {
		return MaxScalpingLivePaperSoakCycles
	}
	return cycles
}

func NormalizeScalpingLivePaperSoakInterval(interval time.Duration) time.Duration {
	if interval <= 0 {
		return 0
	}
	if interval > MaxScalpingLivePaperSoakInterval {
		return MaxScalpingLivePaperSoakInterval
	}
	return interval
}

func ScalpingLivePaperSoakTimeout(cycles int, interval time.Duration) time.Duration {
	cycles = NormalizeScalpingLivePaperSoakCycles(cycles)
	interval = NormalizeScalpingLivePaperSoakInterval(interval)
	timeout := scalpingLivePaperSoakBaseTimeout + time.Duration(cycles)*scalpingLivePaperSoakPerCycleTimeout
	if cycles > 1 {
		timeout += time.Duration(cycles-1) * interval
	}
	if timeout < time.Minute {
		return time.Minute
	}
	return timeout
}

func RunPublicScalpingLivePaperSoak(
	ctx context.Context,
	db DBPool,
	options ScalpingLivePaperSoakOptions,
) (*ScalpingLivePaperSoakResult, error) {
	if isNilDBPool(db) {
		return nil, fmt.Errorf("live paper scalping soak requires database")
	}

	exchange := strings.TrimSpace(options.Exchange)
	if exchange == "" {
		exchange = defaultScalpingLivePaperSoakExchange
	}
	cycles := NormalizeScalpingLivePaperSoakCycles(options.Cycles)
	interval := NormalizeScalpingLivePaperSoakInterval(options.Interval)
	initialCapital := options.InitialCapital
	if !initialCapital.GreaterThan(decimal.Zero) {
		initialCapital = decimal.NewFromFloat(48)
	}
	feeRate := options.FeeRate
	if !feeRate.GreaterThan(decimal.Zero) {
		feeRate = decimal.NewFromFloat(0.0006)
	}
	holdPeriod := options.HoldPeriod
	if holdPeriod <= 0 {
		holdPeriod = DefaultScalpingBacktestHoldPeriod
	}
	if resolved := resolveSoakDefaultHoldPeriod(); resolved != DefaultScalpingBacktestHoldPeriod {
		holdPeriod = resolved
	}
	chatID := strings.TrimSpace(options.ChatID)
	if chatID == "" {
		chatID = defaultScalpingLivePaperSoakChatID
	}
	orderPrefix := strings.TrimSpace(options.OrderPrefix)
	if orderPrefix == "" {
		orderPrefix = defaultScalpingLivePaperSoakOrderPrefix
	}

	ccxtSvc := ccxt.NewNativeCCXTService(15*time.Second, 1)
	if err := ccxtSvc.Initialize(ctx); err != nil {
		return nil, fmt.Errorf("initialize native ccxt service: %w", err)
	}
	defer func() {
		_ = ccxtSvc.Close()
	}()

	defaults := resolveScalpingLivePaperSoakConfig(exchange, options)

	svc := &AIScalpingService{
		config:             defaults,
		ccxtService:        ccxtSvc,
		symbolGuards:       make(map[string]symbolExecutionGuard),
		signalObservations: make(map[string][]scalpingSignalObservation),
	}

	soak := &ScalpingLivePaperSoakResult{
		Exchange: exchange,
		Cycles:   cycles,
	}
	historicalSignals := make([]HistoricalSignal, 0, cycles*defaults.MaxPairsToAnalyze)
	nextSignalTime := time.Now().UTC()

	for cycle := 0; cycle < cycles; cycle++ {
		if cycle > 0 && interval > 0 {
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("wait between live paper soak cycles: %w", ctx.Err())
			case <-time.After(interval):
			}
		}

		cycleStartTime := time.Now().UTC()
		if cycleStartTime.Before(nextSignalTime) {
			cycleStartTime = nextSignalTime
		}
		signals, err := gatherPublicScalpingLivePaperSoakSignals(ctx, svc, exchange, cycleStartTime)
		if err != nil {
			return nil, err
		}
		historicalSignals = append(historicalSignals, signals...)
		nextSignalTime = signals[len(signals)-1].Timestamp.Add(time.Millisecond)
	}

	result, fees, err := runPublicScalpingLivePaperSoakSignals(ctx, defaults, exchange, initialCapital, feeRate, holdPeriod, historicalSignals)
	if err != nil {
		return nil, err
	}
	soak.LastBacktestResult = result
	soak.LastRejectionByReason = result.Summary.RejectionByReason
	soak.LastGateSummary = result.GateSummary
	soak.TotalSignals = result.Summary.TotalSignals
	soak.EligibleSignals = result.Summary.EligibleSignals
	soak.TotalTrades = result.Summary.TotalTrades
	soak.OpenPositions = result.Summary.OpenPositions
	soak.WinningTrades = result.Summary.WinningTrades
	soak.LosingTrades = result.Summary.LosingTrades
	soak.Fees = fees
	soak.NetPnL = result.Summary.TotalPnL

	report, err := PersistScalpingPaperBacktestSoakReport(ctx, db, result, ScalpingPaperSoakPersistenceOptions{
		ChatID:      chatID,
		Exchange:    exchange,
		Baseline:    options.Baseline,
		OrderPrefix: orderPrefix,
	})
	if err != nil {
		return nil, fmt.Errorf("persist live paper scalping soak: %w", err)
	}
	if report.TotalCycles != soak.TotalSignals {
		return nil, fmt.Errorf("persisted cycle mismatch: got %d want %d", report.TotalCycles, soak.TotalSignals)
	}
	if report.TradeSummary.ClosedTrades != soak.TotalTrades {
		return nil, fmt.Errorf("persisted trade mismatch: got %d want %d", report.TradeSummary.ClosedTrades, soak.TotalTrades)
	}
	if !report.TradeSummary.NetPnL.Round(8).Equal(soak.NetPnL.Round(8)) {
		return nil, fmt.Errorf("persisted net pnl mismatch: got %s want %s", report.TradeSummary.NetPnL.String(), soak.NetPnL.String())
	}
	if result.Summary.OpenPositions > 0 {
		report.LiveTrialReadiness.Ready = false
		report.LiveTrialReadiness.Reasons = appendScalpingReadinessReason(report.LiveTrialReadiness.Reasons, "open_positions_unclosed")
	}

	soak.Report = report
	soak.InsufficientTradeProof = report.InsufficientTradeProof
	if options.RequireTrades && soak.TotalTrades == 0 {
		return nil, fmt.Errorf("live paper scalping soak produced no paper trades")
	}
	return soak, nil
}

func resolveScalpingLivePaperSoakConfig(exchange string, options ScalpingLivePaperSoakOptions) AIScalpingConfig {
	base := DefaultAIScalpingConfig()
	base.Exchange = exchange
	defaults := ResolveAIScalpingConfigFromEnv(base)
	defaults.Exchange = exchange
	if options.MaxPairsToAnalyze != nil {
		defaults.MaxPairsToAnalyze = clampInt(*options.MaxPairsToAnalyze, 1, 64)
	}
	if options.MaxCandidatePairs > 0 {
		defaults.MaxCandidatePairs = clampInt(options.MaxCandidatePairs, defaults.MaxPairsToAnalyze, 2000)
	}
	if defaults.MaxCandidatePairs < defaults.MaxPairsToAnalyze {
		defaults.MaxCandidatePairs = defaults.MaxPairsToAnalyze
	}
	if options.OrderBookPairs > 0 {
		defaults.OrderBookPairs = clampInt(options.OrderBookPairs, 1, defaults.MaxPairsToAnalyze)
		if defaults.OrderBookPairs > defaultOrderBookPairsBase {
			defaults.AutoExpandOrderBooks = false
		}
	}
	if defaults.OrderBookPairs > defaults.MaxPairsToAnalyze {
		defaults.OrderBookPairs = defaults.MaxPairsToAnalyze
	}
	if defaults.OrderBookPairs <= 0 {
		defaults.OrderBookPairs = clampInt(defaultOrderBookPairsBase, 1, defaults.MaxPairsToAnalyze)
	}
	defaults.AutoExecute = false
	defaults.EnforceFutures = false
	return defaults
}

func gatherPublicScalpingLivePaperSoakSignals(
	ctx context.Context,
	svc *AIScalpingService,
	exchange string,
	startTime time.Time,
) ([]HistoricalSignal, error) {
	signals, err := svc.gatherMarketSignals(ctx)
	if err != nil {
		return nil, fmt.Errorf("gather live scalping market signals: %w", err)
	}
	if len(signals) == 0 {
		return nil, fmt.Errorf("live paper scalping soak gathered no market signals")
	}
	if startTime.IsZero() {
		startTime = time.Now().UTC()
	}
	startTime = startTime.UTC()
	historicalSignals := make([]HistoricalSignal, 0, len(signals))
	for i, signal := range signals {
		historicalSignals = append(historicalSignals, HistoricalSignal{
			Timestamp: startTime.Add(time.Duration(i) * time.Millisecond),
			Symbol:    signal.Symbol,
			Exchange:  exchange,
			Signal:    signal,
		})
	}
	return historicalSignals, nil
}

func runPublicScalpingLivePaperSoakSignals(
	ctx context.Context,
	defaults AIScalpingConfig,
	exchange string,
	initialCapital decimal.Decimal,
	feeRate decimal.Decimal,
	holdPeriod time.Duration,
	historicalSignals []HistoricalSignal,
) (*ScalpingBacktestResult, decimal.Decimal, error) {
	if len(historicalSignals) == 0 {
		return nil, decimal.Zero, fmt.Errorf("live paper scalping soak gathered no market signals")
	}
	symbols := make([]string, 0, len(historicalSignals))
	seenSymbols := make(map[string]struct{}, len(historicalSignals))
	startTime := historicalSignals[0].Timestamp
	endTime := historicalSignals[0].Timestamp
	for _, signal := range historicalSignals {
		key := normalizeSymbolForComparison(signal.Symbol)
		if key != "" {
			if _, exists := seenSymbols[key]; !exists {
				seenSymbols[key] = struct{}{}
				symbols = append(symbols, signal.Symbol)
			}
		}
		if signal.Timestamp.Before(startTime) {
			startTime = signal.Timestamp
		}
		if signal.Timestamp.After(endTime) {
			endTime = signal.Timestamp
		}
	}

	fallbackConfig := defaults.DeterministicFallback.Normalized()
	entryCutoffTime := endTime.Add(-holdPeriod)
	if entryCutoffTime.Before(startTime) {
		entryCutoffTime = time.Time{}
	}
	engine := NewScalpingBacktestEngine(nil, ScalpingBacktestConfig{
		StartTime:             startTime.Add(-time.Second),
		EndTime:               endTime.Add(time.Second),
		Symbols:               symbols,
		Exchange:              exchange,
		InitialCapital:        initialCapital,
		FeeRate:               feeRate,
		SlippagePct:           decimal.NewFromFloat(DefaultScalpingBacktestSlippage),
		MaxBidAskSpreadPct:    math.Min(defaults.MaxBidAskSpreadPct, fallbackConfig.MaxBidAskSpread),
		MinConfidence:         defaults.MinConfidence,
		MinExpectancyN:        defaults.MinExpectancyN,
		MinExpectancyEdge:     defaults.MinExpectancyEdge,
		MaxCapitalPct:         defaults.MaxCapitalPct,
		DefaultHoldPeriod:     holdPeriod,
		EntryCutoffTime:       entryCutoffTime,
		RequireRecentMomentum: resolveSoakRequireRecentMomentum(),
		MinRecentMomentumPct:  0.05,
		DeterministicFallback: fallbackConfig,
	})
	result, err := engine.RunSignals(ctx, historicalSignals)
	if err != nil {
		return nil, decimal.Zero, fmt.Errorf("run live paper scalping signals: %w", err)
	}

	fees := decimal.Zero
	for _, trade := range result.Trades {
		fees = fees.Add(trade.Fees)
	}
	return result, fees, nil
}

func resolveSoakRequireRecentMomentum() bool {
	if v, ok := getEnvBool("NEURATRADE_SCALPING_REQUIRE_RECENT_MOMENTUM"); ok {
		return v
	}
	return true
}

func resolveSoakDefaultHoldPeriod() time.Duration {
	if v := getEnvInt("NEURATRADE_SCALPING_DEFAULT_HOLD_PERIOD_SECONDS"); v > 0 {
		return time.Duration(v) * time.Second
	}
	return DefaultScalpingBacktestHoldPeriod
}
