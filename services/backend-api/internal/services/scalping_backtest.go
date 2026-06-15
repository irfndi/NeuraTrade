package services

import (
	"context"
	"fmt"
	"math"
	randv2 "math/rand/v2"
	"os"
	"sort"
	"strings"
	"time"

	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"

	"github.com/google/uuid"
	appautonomy "github.com/irfndi/neuratrade/internal/app/autonomy"
	internaldb "github.com/irfndi/neuratrade/internal/database"
	"github.com/shopspring/decimal"
)

const (
	DefaultScalpingBacktestSlippage         = 0.001
	DefaultScalpingBacktestNoise            = 0.0
	DefaultScalpingBacktestHoldPeriod       = 4 * time.Hour
	DefaultScalpingBacktestMaxCapitalPct    = 5.0
	DefaultScalpingBacktestSpreadMultiplier = 8
	// maxProfitFactorNoLosses represents an effectively unbounded profit
	// factor when a report has winning trades and no losing trades.
	maxProfitFactorNoLosses = 999

	// backtestSpreadMultiplier scales the intra-candle high-low range to
	// approximate a bid-ask spread. The factor 8 was derived empirically from
	// typical crypto market microstructure where the observable range is
	// roughly 8x the effective spread on liquid pairs.
	// Used as the default when SpreadMultiplier is not set in config.
	backtestSpreadMultiplier = DefaultScalpingBacktestSpreadMultiplier
)

// envBacktestSymbols is the operator override for the scalping backtest
// default universe. Set this (comma-separated) to run a backtest over a
// custom symbol set without touching NEURATRADE_PAPER_SYMBOLS, which is
// for paper-trading strategies and intentionally separate.
const envBacktestSymbols = "NEURATRADE_BACKTEST_SYMBOLS"

func defaultScalpingBacktestUniverse() []string {
	if raw := os.Getenv(envBacktestSymbols); raw != "" {
		parts := strings.Split(raw, ",")
		seen := make(map[string]struct{}, len(parts))
		symbols := make([]string, 0, len(parts))
		for _, p := range parts {
			s := strings.ToUpper(strings.TrimSpace(p))
			if s == "" {
				continue
			}
			if _, ok := seen[s]; ok {
				continue
			}
			seen[s] = struct{}{}
			symbols = append(symbols, s)
		}
		if len(symbols) > 0 {
			return symbols
		}
	}
	return []string{"BTC/USDT", "ETH/USDT", "SOL/USDT", "BNB/USDT", "XRP/USDT"}
}

type ScalpingCyclePolicy = appautonomy.ScalpingCyclePolicy

type MarketSignal = aiMarketSignal

type ScalpingBacktestConfig struct {
	StartTime             time.Time
	EndTime               time.Time
	Symbols               []string
	Exchange              string
	InitialCapital        decimal.Decimal
	FeeRate               decimal.Decimal
	SlippagePct           decimal.Decimal
	NoisePct              float64
	MaxBidAskSpreadPct    float64
	MinConfidence         float64
	MinExpectancyN        int
	MinExpectancyEdge     float64
	MaxCapitalPct         float64
	DefaultHoldPeriod     time.Duration
	MaxHoldCandles        int
	EntryCutoffTime       time.Time
	RequireRecentMomentum bool
	MinRecentMomentumPct  float64
	SpreadMultiplier      float64
	DeterministicFallback DeterministicFallbackConfig
	RegimeHighBand        float64
	RegimeLowBand         float64
	// AsymmetricExit configures asymmetric stop-loss / take-profit levels
	// (and optional breakeven / trailing-stop behaviour) for the backtest.
	// When UseAsymmetricExits is false, the legacy symmetric 0.8%/1.2%
	// defaults are used — matching the pre-asymmetric live path.
	AsymmetricExit AsymmetricExitConfig
	// Mode selects the decision pipeline used during the backtest. The
	// default "deterministic" runs buildDecisionFromSignal alone. The "ai"
	// mode additionally computes SuggestedAction/ConfidenceHint/CandidateScore
	// hints (mirroring AIScalpingService.signalsWithDecisionHints) and
	// records them in ScalpingBacktestSignal.Hints. No live LLM is called —
	// backtests are offline replays, so the AI mode is a "shadow" that
	// records what hints the AI path would have consumed.
	Mode string

	// MaxLossPct caps per-trade loss as a percentage of entry notional.
	// When non-zero the backtest engine force-closes any open position
	// whose unrealized loss breaches this threshold.
	MaxLossPct float64
	// EnablePanicDropEntry toggles the panic-drop entry filter. When true,
	// the engine considers entries triggered by sharp drawdowns.
	EnablePanicDropEntry bool
	// MinPanicDropPct is the minimum drawdown (in percent) required to
	// qualify as a panic-drop signal when EnablePanicDropEntry is true.
	MinPanicDropPct float64
	// MinEntryMomentumPct is the minimum recent momentum (in percent)
	// required to qualify an entry alongside trailing-stop support.
	MinEntryMomentumPct float64
}

type ScalpingBacktestResult struct {
	RunID       string
	Config      ScalpingBacktestConfig
	StartTime   time.Time
	EndTime     time.Time
	Mode        string
	Summary     ScalpingBacktestSummary
	Signals     []ScalpingBacktestSignal
	Trades      []ScalpingBacktestTrade
	GateSummary []GateSummaryEntry
}

type ScalpingBacktestSummary struct {
	TotalSignals           int                        `json:"total_signals"`
	EligibleSignals        int                        `json:"eligible_signals"`
	RejectedSignals        int                        `json:"rejected_signals"`
	TotalTrades            int                        `json:"total_trades"`
	OpenPositions          int                        `json:"open_positions"`
	WinningTrades          int                        `json:"winning_trades"`
	LosingTrades           int                        `json:"losing_trades"`
	WinRate                decimal.Decimal            `json:"win_rate"`
	TotalPnL               decimal.Decimal            `json:"total_pnl"`
	TotalReturnPct         decimal.Decimal            `json:"total_return_pct"`
	ProfitFactor           decimal.Decimal            `json:"profit_factor"`
	SharpeRatio            decimal.Decimal            `json:"sharpe_ratio"`
	MaxDrawdownPct         decimal.Decimal            `json:"max_drawdown_pct"`
	AvgWin                 decimal.Decimal            `json:"avg_win"`
	AvgLoss                decimal.Decimal            `json:"avg_loss"`
	AvgHoldDuration        time.Duration              `json:"avg_hold_duration"`
	RegimeBreakdown        map[string]int             `json:"regime_breakdown"`
	SymbolBreakdown        map[string]int             `json:"symbol_breakdown"`
	RejectionByReason      map[string]int             `json:"rejection_by_reason"`
	GateRejectByName       map[string]int             `json:"gate_reject_by_name"`
	RegimeWinRateBreakdown map[string]decimal.Decimal `json:"regime_win_rate_breakdown"`
}

type ScalpingBacktestSignal struct {
	Timestamp        time.Time
	Symbol           string
	Exchange         string
	Signal           MarketSignal
	Decision         *AITradingDecision
	Regime           string
	RegimeVolatility string
	FunnelStage      string
	RejectionReason  string
	GateResults      map[string]bool
	// Hints is populated only when the backtest ran in Mode="ai". It captures
	// the SuggestedAction/ConfidenceHint/CandidateScore the AI scalping path
	// would have consumed for this signal, so operators can post-hoc
	// compare deterministic decisions against the hints the LLM would have
	// used. Nil in Mode="deterministic" runs.
	Hints *SignalHints
}

// SignalHints is the sidecar metadata the AI scalping path consumes per
// signal. Mirrors the fields set by AIScalpingService.signalsWithDecisionHints
// (SuggestedAction/ConfidenceHint/CandidateScore). Recorded in backtest
// results so operators can compare the deterministic decision against the
// hints without re-running the offline replay against a live LLM.
type SignalHints struct {
	SuggestedAction string  `json:"suggested_action"`
	ConfidenceHint  float64 `json:"confidence_hint"`
	CandidateScore  float64 `json:"candidate_score"`
}

type ScalpingBacktestTrade struct {
	Symbol        string
	Exchange      string
	Side          string
	Size          decimal.Decimal
	Notional      decimal.Decimal
	EntryPrice    decimal.Decimal
	ExitPrice     decimal.Decimal
	EntryTime     time.Time
	ExitTime      time.Time
	PnL           decimal.Decimal
	PnLPct        decimal.Decimal
	Fees          decimal.Decimal
	Outcome       string
	ExitReason    string
	RegimeAtEntry string
	RegimeAtExit  string
}

type GateResult struct {
	Allowed bool
	Reason  string
}

type GateStats struct {
	PassCount         int
	RejectCount       int
	RejectionReasons  map[string]int
	BreakdownBySymbol map[string]int
	BreakdownByRegime map[string]int
}

type GateSummaryEntry struct {
	GateName            string         `json:"gate_name"`
	PassCount           int            `json:"pass_count"`
	RejectCount         int            `json:"reject_count"`
	TopRejectionReasons []ReasonCount  `json:"top_rejection_reasons"`
	BreakdownBySymbol   map[string]int `json:"breakdown_by_symbol"`
	BreakdownByRegime   map[string]int `json:"breakdown_by_regime"`
}

type ReasonCount struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

type HistoricalSignal struct {
	Timestamp time.Time
	Symbol    string
	Exchange  string
	Signal    MarketSignal
}

type scalpingOHLCVPoint struct {
	symbol    string
	exchange  string
	open      float64
	high      float64
	low       float64
	close     float64
	volume    float64
	timestamp time.Time
}

type SignalEvaluation struct {
	Signal          HistoricalSignal
	Decision        *AITradingDecision
	Regime          string
	FunnelStage     string
	RejectionReason string
	GateResults     map[string]GateResult
	Allowed         bool
	// Hints is the AI-path sidecar (SuggestedAction/ConfidenceHint/CandidateScore)
	// populated only when the backtest ran in Mode="ai". Nil in Mode="deterministic".
	Hints *SignalHints
}

type SimulatedPosition struct {
	Symbol      string
	Side        string
	Size        decimal.Decimal
	Notional    decimal.Decimal
	EntryPrice  decimal.Decimal
	EntryTime   time.Time
	StopLoss    decimal.Decimal
	TakeProfit  decimal.Decimal
	RegimeEntry string
	Signal      MarketSignal
	Decision    *AITradingDecision
	HoldCandles int
}

type SimulatedTrade struct {
	Trade ScalpingBacktestTrade
}

type ScalpingBacktestEngine struct {
	db                    DBPool
	config                ScalpingBacktestConfig
	capital               decimal.Decimal
	positions             map[string]*SimulatedPosition
	tradeHistory          []ScalpingBacktestTrade
	signalHistory         []ScalpingBacktestSignal
	gateStats             map[string]*GateStats
	policy                ScalpingCyclePolicy
	marketDataPriceColumn scalpingMarketDataPriceColumn
}

func NewScalpingBacktestEngine(db DBPool, config ScalpingBacktestConfig) *ScalpingBacktestEngine {
	normalized := normalizeScalpingBacktestConfig(config)
	policyCfg := appautonomy.DefaultScalpingPolicyConfig()
	policyCfg.MaxBidAskSpreadPct = normalized.MaxBidAskSpreadPct
	policy := appautonomy.EvaluateScalpingPolicy(appautonomy.ScalpingCycleInput{
		TotalValue:         normalized.InitialCapital,
		BaseMinConfidence:  normalized.MinConfidence,
		BaseMaxCapitalPct:  normalized.MaxCapitalPct,
		PhaseMinConfidence: normalized.MinConfidence,
		PhaseMaxCapitalPct: normalized.MaxCapitalPct,
	}, policyCfg)

	return &ScalpingBacktestEngine{
		db:            db,
		config:        normalized,
		capital:       normalized.InitialCapital,
		positions:     make(map[string]*SimulatedPosition),
		tradeHistory:  make([]ScalpingBacktestTrade, 0),
		signalHistory: make([]ScalpingBacktestSignal, 0),
		gateStats:     make(map[string]*GateStats),
		policy:        policy,
	}
}

func (e *ScalpingBacktestEngine) Run(ctx context.Context) (*ScalpingBacktestResult, error) {
	if e == nil {
		return nil, fmt.Errorf("scalping backtest engine is nil")
	}
	if err := e.validateConfig(); err != nil {
		return nil, err
	}

	historicalSignals, err := e.loadHistoricalSignals(ctx, e.config.StartTime, e.config.EndTime)
	if err != nil {
		return nil, fmt.Errorf("load historical scalping signals: %w", err)
	}
	result, err := e.RunSignals(ctx, historicalSignals)
	if err != nil {
		return nil, fmt.Errorf("run historical scalping signals: %w", err)
	}
	return result, nil
}

func (e *ScalpingBacktestEngine) RunSignals(ctx context.Context, historicalSignals []HistoricalSignal) (*ScalpingBacktestResult, error) {
	if e == nil {
		return nil, fmt.Errorf("scalping backtest engine is nil")
	}
	if err := e.validateConfig(); err != nil {
		return nil, err
	}

	e.capital = e.config.InitialCapital
	e.positions = make(map[string]*SimulatedPosition)
	e.tradeHistory = make([]ScalpingBacktestTrade, 0)
	e.signalHistory = make([]ScalpingBacktestSignal, 0)
	e.gateStats = make(map[string]*GateStats)

	runID := uuid.NewString()
	if len(historicalSignals) == 0 {
		return nil, fmt.Errorf("no historical signals found in range")
	}

	signals := append([]HistoricalSignal(nil), historicalSignals...)
	sort.SliceStable(signals, func(i, j int) bool {
		if !signals[i].Timestamp.Equal(signals[j].Timestamp) {
			return signals[i].Timestamp.Before(signals[j].Timestamp)
		}
		if signals[i].Symbol != signals[j].Symbol {
			return signals[i].Symbol < signals[j].Symbol
		}
		return signals[i].Exchange < signals[j].Exchange
	})
	lastSignalAtByPosition := make(map[string]time.Time, len(signals))
	lastPriceBySymbol := make(map[string]float64)
	for _, signal := range signals {
		positionKey := simulatedPositionKey(signal.Exchange, signal.Symbol)
		if signal.Timestamp.After(lastSignalAtByPosition[positionKey]) {
			lastSignalAtByPosition[positionKey] = signal.Timestamp
		}
		lastPriceBySymbol[signal.Symbol] = signal.Signal.Price
	}

	for _, signal := range signals {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("run scalping backtest signals: %w", err)
		}
		positionKey := simulatedPositionKey(signal.Exchange, signal.Symbol)
		if position, ok := e.positions[positionKey]; ok {
			if !signal.Timestamp.Equal(position.EntryTime) {
				position.HoldCandles++
			}
			markPrice := decimal.NewFromFloat(signal.Signal.Price)
			candleLow := decimal.NewFromFloat(signal.Signal.Low)
			candleHigh := decimal.NewFromFloat(signal.Signal.High)

			slTPExit := false
			switch strings.ToLower(strings.TrimSpace(position.Side)) {
			case "buy":
				if position.TakeProfit.GreaterThan(decimal.Zero) && markPrice.GreaterThanOrEqual(position.TakeProfit) {
					slTPExit = true
				} else if position.StopLoss.GreaterThan(decimal.Zero) && candleLow.LessThanOrEqual(position.StopLoss) {
					slTPExit = true
				}
			case "sell":
				if position.TakeProfit.GreaterThan(decimal.Zero) && markPrice.LessThanOrEqual(position.TakeProfit) {
					slTPExit = true
				} else if position.StopLoss.GreaterThan(decimal.Zero) && candleHigh.GreaterThanOrEqual(position.StopLoss) {
					slTPExit = true
				}
			}
			if slTPExit || e.positionCanCloseAt(signal.Timestamp, position) {
				trade := e.closeSimulatedPosition(signal, position)
				e.tradeHistory = append(e.tradeHistory, trade)
				e.capital = e.capital.Add(trade.PnL)
				delete(e.positions, positionKey)
			}
		}

		evaluation, evalErr := e.evaluateSignal(ctx, signal)
		if evalErr != nil {
			return nil, fmt.Errorf("evaluate scalping backtest signal: %w", evalErr)
		}
		if evaluation.Allowed && !e.config.EntryCutoffTime.IsZero() && signal.Timestamp.After(e.config.EntryCutoffTime) {
			evaluation.Allowed = false
			evaluation.Decision = nil
			evaluation.FunnelStage = "entry_cutoff"
			evaluation.RejectionReason = "entry_cutoff_window"
		}
		if evaluation.Allowed && !e.hasObservableClose(signal, lastSignalAtByPosition[positionKey]) {
			evaluation.Allowed = false
			evaluation.Decision = nil
			evaluation.FunnelStage = "entry_close_unobserved"
			evaluation.RejectionReason = "entry_without_close_signal"
		}

		recorded := ScalpingBacktestSignal{
			Timestamp:        signal.Timestamp,
			Symbol:           signal.Symbol,
			Exchange:         signal.Exchange,
			Signal:           signal.Signal,
			Decision:         evaluation.Decision,
			Regime:           evaluation.Regime,
			RegimeVolatility: e.classifyRegimeVolatility(signal.Signal),
			FunnelStage:      evaluation.FunnelStage,
			RejectionReason:  evaluation.RejectionReason,
			GateResults:      toGateBoolMap(evaluation.GateResults),
			Hints:            evaluation.Hints,
		}
		e.signalHistory = append(e.signalHistory, recorded)

		if !evaluation.Allowed || evaluation.Decision == nil {
			continue
		}

		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("run scalping backtest signals: %w", err)
		}
		if _, exists := e.positions[positionKey]; exists {
			continue
		}
		position, simErr := e.openSimulatedPosition(ctx, signal, evaluation.Decision)
		if simErr != nil {
			return nil, fmt.Errorf("simulate execution for %s at %s: %w", signal.Symbol, signal.Timestamp.Format(time.RFC3339), simErr)
		}
		e.positions[positionKey] = position
	}

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("run scalping backtest signals (end-of-run sweep): %w", err)
	}

	e.sweepRemainingPositions(lastPriceBySymbol, signals)

	result := &ScalpingBacktestResult{
		RunID:       runID,
		Config:      e.config,
		Mode:        e.config.Mode,
		StartTime:   e.config.StartTime,
		EndTime:     e.config.EndTime,
		Summary:     e.calculateSummary(),
		Signals:     append([]ScalpingBacktestSignal(nil), e.signalHistory...),
		Trades:      append([]ScalpingBacktestTrade(nil), e.tradeHistory...),
		GateSummary: e.buildGateSummary(),
	}

	return result, nil
}

func (e *ScalpingBacktestEngine) hasObservableClose(signal HistoricalSignal, lastSignalAt time.Time) bool {
	if lastSignalAt.IsZero() {
		return false
	}
	holdFor := e.config.DefaultHoldPeriod
	if holdFor <= 0 {
		holdFor = DefaultScalpingBacktestHoldPeriod
	}
	return !lastSignalAt.Before(signal.Timestamp.Add(holdFor))
}

func (e *ScalpingBacktestEngine) sweepRemainingPositions(lastPriceBySymbol map[string]float64, signals []HistoricalSignal) {
	for positionKey, position := range e.positions {
		parts := strings.SplitN(positionKey, "|", 2)
		exchange := ""
		if len(parts) == 2 {
			exchange = parts[0]
		}
		closePrice := lastPriceBySymbol[position.Symbol]
		closeSignal := HistoricalSignal{
			Timestamp: e.config.EndTime,
			Symbol:    position.Symbol,
			Exchange:  exchange,
			Signal:    MarketSignal{Price: closePrice, OrderBookImbalance: position.Signal.OrderBookImbalance, ConfidenceHint: position.Signal.ConfidenceHint},
		}
		if closeSignal.Timestamp.IsZero() && len(signals) > 0 {
			closeSignal.Timestamp = signals[len(signals)-1].Timestamp
		}
		trade := e.closeSimulatedPosition(closeSignal, position)
		trade.ExitReason = "force_close_end_of_run"
		e.tradeHistory = append(e.tradeHistory, trade)
		e.capital = e.capital.Add(trade.PnL)
		delete(e.positions, positionKey)
	}
}

func (e *ScalpingBacktestEngine) loadHistoricalSignals(ctx context.Context, startTime, endTime time.Time) ([]HistoricalSignal, error) {
	if isNilDBPool(e.db) {
		return nil, fmt.Errorf("database connection is nil")
	}

	symbolFilter := make(map[string]struct{})
	for _, symbol := range e.config.Symbols {
		norm := normalizeSymbolForComparison(symbol)
		if norm == "" {
			continue
		}
		symbolFilter[norm] = struct{}{}
	}

	if len(symbolFilter) == 0 {
		for _, symbol := range defaultScalpingBacktestUniverse() {
			symbolFilter[normalizeSymbolForComparison(symbol)] = struct{}{}
		}
	}

	signals, err := e.loadSignalsFromOHLCV(ctx, startTime, endTime, symbolFilter)
	if err != nil {
		zaplogrus.Warnf("[BACKTEST] OHLCV signal load failed (falling back to market_data): %v", err)
	}
	if err == nil && len(signals) > 0 {
		return signals, nil
	}

	return e.loadSignalsFromMarketData(ctx, startTime, endTime, symbolFilter)
}

func (e *ScalpingBacktestEngine) evaluateSignal(ctx context.Context, signal HistoricalSignal) (*SignalEvaluation, error) {
	regime := e.classifyRegime(signal.Signal)

	candidate := appautonomy.CandidateSignal{
		Symbol:             signal.Symbol,
		Price:              decimal.NewFromFloat(signal.Signal.Price),
		High24h:            decimal.NewFromFloat(signal.Signal.High24h),
		Low24h:             decimal.NewFromFloat(signal.Signal.Low24h),
		Volume24h:          decimal.NewFromFloat(signal.Signal.Volume24h),
		BidAskSpread:       signal.Signal.BidAskSpread,
		OrderBookImbalance: signal.Signal.OrderBookImbalance,
		RangePosition24h:   signal.Signal.RangePosition24h,
		PriceChange24hPct:  signal.Signal.PriceChange24h,
	}
	funnel := appautonomy.BuildCandidateFunnel([]appautonomy.CandidateSignal{candidate}, e.policy, appautonomy.DefaultScalpingPolicyConfig())

	eval := &SignalEvaluation{
		Signal:      signal,
		Regime:      regime,
		FunnelStage: "gate_evaluation",
	}

	if funnel.CandidateViableCount <= 0 {
		eval.Allowed = false
		eval.FunnelStage = "candidate_rejected"
		if len(funnel.TopCandidateRejections) > 0 {
			eval.RejectionReason = strings.TrimSpace(funnel.TopCandidateRejections[0].Reason)
		}
		if eval.RejectionReason == "" {
			eval.RejectionReason = "candidate_funnel_block"
		}
		return eval, nil
	}

	decision := e.buildDecisionFromSignal(ctx, signal.Signal)
	if decision == nil || decision.Action == "hold" {
		eval.Allowed = false
		eval.FunnelStage = "deterministic_hold"
		eval.RejectionReason = "no_directional_edge"
		return eval, nil
	}

	// Mode="ai" records the hints the AI scalping path would have consumed
	// for this signal. No live LLM is invoked (backtests are offline
	// replays); hints are computed deterministically from the same
	// DeterministicFallback config the AI service uses. Hints are only
	// persisted when the candidate survives the live gate cascade — the
	// AI hint path's signalsWithDecisionHints also skips rejected
	// candidates, so a rejected backtest candidate must not carry a hint.
	eval.Decision = decision
	eval.GateResults = e.evaluateGates(signal.Signal, decision)
	sortedGates := make([]string, 0, len(eval.GateResults))
	for gateName := range eval.GateResults {
		sortedGates = append(sortedGates, gateName)
	}
	sort.Strings(sortedGates)
	for _, gateName := range sortedGates {
		gateResult := eval.GateResults[gateName]
		e.updateGateStats(gateName, gateResult, signal.Symbol, regime)
		if !gateResult.Allowed && eval.RejectionReason == "" {
			eval.RejectionReason = gateResult.Reason
			eval.FunnelStage = "gate_rejected"
		}
	}

	eval.Allowed = eval.RejectionReason == ""
	if eval.Allowed {
		// Hints are computed deterministically from the same fallback config
		// the AI path uses; no LLM is invoked. Exposing them for both modes
		// lets CLI paper-trading loops read the action side directly.
		eval.Hints = e.computeSignalHints(signal.Signal, decision)
		eval.FunnelStage = "eligible"
	}
	return eval, nil
}

// backtestExitLevels returns symmetric (0.8% / 1.2%) or asymmetric
// stop-loss / take-profit levels depending on the config. It delegates to
// the live-trading asymmetricExitLevels when UseAsymmetricExits is set.
func backtestExitLevels(price float64, action string, leverage int, cfg AsymmetricExitConfig) (decimal.Decimal, decimal.Decimal) {
	if cfg.UseAsymmetricExits {
		return asymmetricExitLevels(price, action, leverage, cfg)
	}
	return defaultExitLevelsWithLeverage(price, action, leverage)
}

func (e *ScalpingBacktestEngine) openSimulatedPosition(ctx context.Context, signal HistoricalSignal, decision *AITradingDecision) (*SimulatedPosition, error) {
	_ = ctx
	if decision == nil {
		return nil, fmt.Errorf("decision is nil")
	}
	if decision.Action != "buy" && decision.Action != "sell" {
		return nil, fmt.Errorf("unsupported decision action: %s", decision.Action)
	}
	if signal.Signal.Price <= 0 {
		return nil, fmt.Errorf("invalid signal price")
	}

	entryPriceRaw := decimal.NewFromFloat(signal.Signal.Price)
	slippage := e.config.SlippagePct
	one := decimal.NewFromInt(1)

	entryPrice := entryPriceRaw
	if decision.Action == "buy" {
		entryPrice = entryPriceRaw.Mul(one.Add(slippage))
	} else {
		entryPrice = entryPriceRaw.Mul(one.Sub(slippage))
	}

	if e.config.NoisePct > 0 {
		noiseFactor := randv2.Float64()*e.config.NoisePct*2 - e.config.NoisePct
		entryPrice = entryPrice.Mul(one.Add(decimal.NewFromFloat(noiseFactor)))
	}

	sizePercent := clampFloat(decision.SizePercent, 0, e.config.MaxCapitalPct)
	if sizePercent <= 0 {
		sizePercent = math.Min(e.config.MaxCapitalPct, 1)
	}
	remainingCapital := e.capital
	for _, pos := range e.positions {
		remainingCapital = remainingCapital.Sub(pos.Notional)
	}
	if remainingCapital.LessThanOrEqual(decimal.Zero) {
		return nil, fmt.Errorf("insufficient capital")
	}
	notional := remainingCapital.Mul(decimal.NewFromFloat(sizePercent / 100))
	if notional.LessThanOrEqual(decimal.Zero) {
		return nil, fmt.Errorf("insufficient capital")
	}

	quantity := decimal.Zero
	if !entryPrice.IsZero() {
		quantity = notional.Div(entryPrice)
	}
	if quantity.LessThanOrEqual(decimal.Zero) {
		return nil, fmt.Errorf("calculated quantity is non-positive")
	}

	stopLoss := decision.StopLoss
	takeProfit := decision.TakeProfit
	if stopLoss == nil || takeProfit == nil {
		sl, tp := backtestExitLevels(signal.Signal.Price, decision.Action, 1, e.config.AsymmetricExit)
		if stopLoss == nil {
			stopLoss = &sl
		}
		if takeProfit == nil {
			takeProfit = &tp
		}
	}

	var risk, reward decimal.Decimal
	if decision.Action == "buy" {
		if stopLoss.GreaterThan(decimal.Zero) {
			risk = entryPrice.Sub(*stopLoss)
		}
		reward = takeProfit.Sub(entryPrice)
	} else {
		if stopLoss.GreaterThan(decimal.Zero) {
			risk = stopLoss.Sub(entryPrice)
		}
		reward = entryPrice.Sub(*takeProfit)
	}
	if risk.LessThan(decimal.Zero) || reward.LessThanOrEqual(decimal.Zero) {
		return nil, fmt.Errorf("invalid stop loss / take profit shape")
	}

	position := &SimulatedPosition{
		Symbol:      signal.Symbol,
		Side:        decision.Action,
		Size:        quantity,
		Notional:    notional,
		EntryPrice:  entryPrice,
		EntryTime:   signal.Timestamp,
		StopLoss:    *stopLoss,
		TakeProfit:  *takeProfit,
		RegimeEntry: e.classifyRegime(signal.Signal),
		Signal:      signal.Signal,
		Decision:    decision,
	}

	return position, nil
}

func (e *ScalpingBacktestEngine) closeSimulatedPosition(signal HistoricalSignal, position *SimulatedPosition) ScalpingBacktestTrade {
	markPrice := decimal.NewFromFloat(signal.Signal.Price)
	candleLow := decimal.NewFromFloat(signal.Signal.Low)
	candleHigh := decimal.NewFromFloat(signal.Signal.High)
	exitPrice := markPrice
	exitReason := "mark_to_market"

	maxCandles := e.config.MaxHoldCandles
	if maxCandles <= 0 {
		maxCandles = 200
	}

	side := strings.ToLower(strings.TrimSpace(position.Side))

	switch side {
	case "buy":
		if position.TakeProfit.GreaterThan(decimal.Zero) && markPrice.GreaterThanOrEqual(position.TakeProfit) {
			exitPrice = position.TakeProfit
			exitReason = "take_profit"
		}
	case "sell":
		if position.TakeProfit.GreaterThan(decimal.Zero) && markPrice.LessThanOrEqual(position.TakeProfit) {
			exitPrice = position.TakeProfit
			exitReason = "take_profit"
		}
	}

	if exitReason == "mark_to_market" {
		maxLossPct := decimal.NewFromFloat(0.015)
		switch side {
		case "buy":
			maxLossPrice := position.EntryPrice.Mul(decimal.NewFromInt(1).Sub(maxLossPct))
			if candleLow.GreaterThan(decimal.Zero) && candleLow.LessThanOrEqual(maxLossPrice) {
				exitPrice = maxLossPrice
				exitReason = "max_loss"
			}
		case "sell":
			maxLossPrice := position.EntryPrice.Mul(decimal.NewFromInt(1).Add(maxLossPct))
			if candleHigh.GreaterThan(decimal.Zero) && candleHigh.GreaterThanOrEqual(maxLossPrice) {
				exitPrice = maxLossPrice
				exitReason = "max_loss"
			}
		}
	}

	if exitReason == "mark_to_market" {
		switch side {
		case "buy":
			if position.StopLoss.GreaterThan(decimal.Zero) && candleLow.LessThanOrEqual(position.StopLoss) {
				exitPrice = position.StopLoss
				exitReason = "stop_loss"
			}
		case "sell":
			if position.StopLoss.GreaterThan(decimal.Zero) && candleHigh.GreaterThanOrEqual(position.StopLoss) {
				exitPrice = position.StopLoss
				exitReason = "stop_loss"
			}
		}
	}

	if exitReason == "mark_to_market" && position.HoldCandles >= maxCandles {
		exitReason = "time_stop"
	}

	one := decimal.NewFromInt(1)
	slippage := e.config.SlippagePct
	if strings.EqualFold(position.Side, "buy") {
		exitPrice = exitPrice.Mul(one.Sub(slippage))
	} else {
		exitPrice = exitPrice.Mul(one.Add(slippage))
	}

	if e.config.NoisePct > 0 {
		noiseFactor := randv2.Float64()*e.config.NoisePct*2 - e.config.NoisePct
		exitPrice = exitPrice.Mul(one.Add(decimal.NewFromFloat(noiseFactor)))
	}

	var grossPnL decimal.Decimal
	if strings.EqualFold(position.Side, "buy") {
		grossPnL = exitPrice.Sub(position.EntryPrice).Mul(position.Size)
	} else {
		grossPnL = position.EntryPrice.Sub(exitPrice).Mul(position.Size)
	}

	fees := position.Notional.Mul(e.config.FeeRate).Mul(decimal.NewFromInt(2))
	netPnL := grossPnL.Sub(fees)
	pnlPct := decimal.Zero
	if !position.Notional.IsZero() {
		pnlPct = netPnL.Div(position.Notional).Mul(decimal.NewFromInt(100))
	}

	return ScalpingBacktestTrade{
		Symbol:        position.Symbol,
		Exchange:      signal.Exchange,
		Side:          position.Side,
		Size:          position.Size,
		Notional:      position.Notional,
		EntryPrice:    position.EntryPrice,
		ExitPrice:     exitPrice,
		EntryTime:     position.EntryTime,
		ExitTime:      signal.Timestamp,
		PnL:           netPnL,
		PnLPct:        pnlPct,
		Fees:          fees,
		Outcome:       outcomeFromPnL(netPnL),
		ExitReason:    exitReason,
		RegimeAtEntry: position.RegimeEntry,
		RegimeAtExit:  e.classifyRegime(signal.Signal),
	}
}

func (e *ScalpingBacktestEngine) positionCanCloseAt(timestamp time.Time, position *SimulatedPosition) bool {
	if position == nil || !timestamp.After(position.EntryTime) {
		return false
	}
	maxCandles := e.config.MaxHoldCandles
	if maxCandles <= 0 {
		maxCandles = 200
	}
	return position.HoldCandles >= maxCandles
}

func simulatedPositionKey(exchange, symbol string) string {
	return strings.ToLower(strings.TrimSpace(exchange)) + "|" + normalizeSymbolForComparison(symbol)
}

func (e *ScalpingBacktestEngine) classifyRegime(signal MarketSignal) string {
	if signal.BidAskSpread > e.config.MaxBidAskSpreadPct {
		return "illiquid"
	}
	imbalance := math.Abs(signal.OrderBookImbalance)
	if imbalance >= 0.25 && signal.RangePosition24h > e.configRegimeLowBand()+5 && signal.RangePosition24h < e.configRegimeHighBand()-5 {
		return "trend"
	}
	if imbalance < 0.10 {
		return "chop"
	}
	return "neutral"
}

func (e *ScalpingBacktestEngine) classifyRegimeVolatility(signal MarketSignal) string {
	if signal.BidAskSpread > e.config.MaxBidAskSpreadPct*2 {
		return "high"
	}
	if signal.BidAskSpread > e.config.MaxBidAskSpreadPct {
		return "elevated"
	}
	return "normal"
}

func (e *ScalpingBacktestEngine) evaluateGates(signal MarketSignal, decision *AITradingDecision) map[string]GateResult {
	results := map[string]GateResult{}
	fallback := e.config.DeterministicFallback.Normalized()

	spreadAllowed := signal.BidAskSpread >= 0 &&
		(signal.BidAskSpread <= e.config.MaxBidAskSpreadPct ||
			scalpingReversalBuyCandidate(signal, fallback) ||
			scalpingSellWindowCandidate(signal, fallback))
	results["spread"] = GateResult{Allowed: spreadAllowed, Reason: gateReason(spreadAllowed, "spread_too_wide")}

	imbalanceAllowed := math.Abs(signal.OrderBookImbalance) >= 0.05
	results["imbalance"] = GateResult{Allowed: imbalanceAllowed, Reason: gateReason(imbalanceAllowed, "imbalance_too_weak")}

	confidenceAllowed := decision != nil && decision.Confidence >= e.config.MinConfidence
	results["confidence"] = GateResult{Allowed: confidenceAllowed, Reason: gateReason(confidenceAllowed, "confidence_below_floor")}

	expectancyAllowed := true
	expectancyReason := ""
	if decision != nil {
		expectancy, samples, hasScoped := e.scopedExpectancy(signal.Symbol, decision.Action)
		if samples >= e.config.MinExpectancyN && expectancy < e.config.MinExpectancyEdge {
			expectancyAllowed = false
			expectancyReason = "expectancy_below_min_edge"
		} else if samples < e.config.MinExpectancyN {
			expectancyReason = "expectancy_insufficient_samples"
		} else if !hasScoped {
			expectancyReason = "expectancy_global_fallback"
		}
	}
	results["expectancy"] = GateResult{Allowed: expectancyAllowed, Reason: expectancyReason}

	riskRewardAllowed := false
	riskRewardReason := "risk_reward_invalid"
	if decision != nil {
		stopLoss := decision.StopLoss
		takeProfit := decision.TakeProfit
		if stopLoss == nil || takeProfit == nil {
			sl, tp := defaultExitLevels(signal.Price, decision.Action)
			if stopLoss == nil {
				stopLoss = &sl
			}
			if takeProfit == nil {
				takeProfit = &tp
			}
		}
		entry := decimal.NewFromFloat(signal.Price)
		risk := decimal.Zero
		reward := decimal.Zero
		switch strings.ToLower(strings.TrimSpace(decision.Action)) {
		case "buy":
			if stopLoss.GreaterThan(decimal.Zero) {
				risk = entry.Sub(*stopLoss)
			}
			reward = takeProfit.Sub(entry)
		case "sell":
			if stopLoss.GreaterThan(decimal.Zero) {
				risk = stopLoss.Sub(entry)
			}
			reward = entry.Sub(*takeProfit)
		}
		if risk.GreaterThanOrEqual(decimal.Zero) && reward.GreaterThan(decimal.Zero) {
			if risk.IsZero() {
				riskRewardAllowed = true
				riskRewardReason = ""
			} else {
				ratio := reward.Div(risk)
				riskRewardAllowed = ratio.GreaterThanOrEqual(decimal.NewFromFloat(minRiskRewardRatio))
				if riskRewardAllowed {
					riskRewardReason = ""
				} else {
					riskRewardReason = "insufficient_risk_reward_ratio"
				}
			}
		}
	}
	results["risk_reward"] = GateResult{Allowed: riskRewardAllowed, Reason: riskRewardReason}

	return results
}

func (e *ScalpingBacktestEngine) calculateSummary() ScalpingBacktestSummary {
	summary := ScalpingBacktestSummary{
		TotalSignals:           len(e.signalHistory),
		TotalTrades:            len(e.tradeHistory),
		OpenPositions:          len(e.positions),
		RegimeBreakdown:        map[string]int{},
		SymbolBreakdown:        map[string]int{},
		RejectionByReason:      map[string]int{},
		GateRejectByName:       map[string]int{},
		RegimeWinRateBreakdown: map[string]decimal.Decimal{},
	}

	for _, sig := range e.signalHistory {
		summary.RegimeBreakdown[sig.Regime]++
		summary.SymbolBreakdown[sig.Symbol]++
		if strings.TrimSpace(sig.RejectionReason) == "" {
			summary.EligibleSignals++
		} else {
			summary.RejectedSignals++
			summary.RejectionByReason[sig.RejectionReason]++
		}
		for gateName, passed := range sig.GateResults {
			if !passed {
				summary.GateRejectByName[gateName]++
			}
		}
	}

	if len(e.tradeHistory) == 0 {
		return summary
	}

	totalPnL := decimal.Zero
	grossProfit := decimal.Zero
	grossLoss := decimal.Zero
	totalWin := decimal.Zero
	totalLoss := decimal.Zero
	totalHold := time.Duration(0)
	equity := e.config.InitialCapital
	peak := equity
	maxDrawdownPct := decimal.Zero
	returns := make([]float64, 0, len(e.tradeHistory))
	regimeWins := map[string]int{}
	regimeTotals := map[string]int{}

	for _, trade := range e.tradeHistory {
		totalPnL = totalPnL.Add(trade.PnL)
		totalHold += trade.ExitTime.Sub(trade.EntryTime)
		regimeTotals[trade.RegimeAtEntry]++

		if trade.PnL.GreaterThan(decimal.Zero) {
			summary.WinningTrades++
			grossProfit = grossProfit.Add(trade.PnL)
			totalWin = totalWin.Add(trade.PnL)
			regimeWins[trade.RegimeAtEntry]++
		} else if trade.PnL.LessThan(decimal.Zero) {
			summary.LosingTrades++
			loss := trade.PnL.Abs()
			grossLoss = grossLoss.Add(loss)
			totalLoss = totalLoss.Add(loss)
		}

		equity = equity.Add(trade.PnL)
		if equity.GreaterThan(peak) {
			peak = equity
		}
		if peak.GreaterThan(decimal.Zero) {
			drawdown := peak.Sub(equity).Div(peak).Mul(decimal.NewFromInt(100))
			if drawdown.GreaterThan(maxDrawdownPct) {
				maxDrawdownPct = drawdown
			}
		}

		ret, _ := trade.PnLPct.Float64()
		returns = append(returns, ret)
	}

	summary.TotalPnL = totalPnL
	if e.config.InitialCapital.GreaterThan(decimal.Zero) {
		summary.TotalReturnPct = totalPnL.Div(e.config.InitialCapital).Mul(decimal.NewFromInt(100))
	}

	if summary.TotalTrades > 0 {
		summary.WinRate = decimal.NewFromInt(int64(summary.WinningTrades)).
			Div(decimal.NewFromInt(int64(summary.TotalTrades))).
			Mul(decimal.NewFromInt(100))
		summary.AvgHoldDuration = totalHold / time.Duration(summary.TotalTrades)
	}

	if summary.WinningTrades > 0 {
		summary.AvgWin = totalWin.Div(decimal.NewFromInt(int64(summary.WinningTrades)))
	}
	if summary.LosingTrades > 0 {
		summary.AvgLoss = totalLoss.Div(decimal.NewFromInt(int64(summary.LosingTrades)))
	}

	if grossLoss.GreaterThan(decimal.Zero) {
		summary.ProfitFactor = grossProfit.Div(grossLoss)
	} else if grossProfit.GreaterThan(decimal.Zero) {
		summary.ProfitFactor = decimal.NewFromInt(maxProfitFactorNoLosses)
	}

	summary.MaxDrawdownPct = maxDrawdownPct
	summary.SharpeRatio = decimal.NewFromFloat(calculateBacktestSharpe(returns))

	for regime, total := range regimeTotals {
		if total <= 0 {
			continue
		}
		wins := regimeWins[regime]
		rate := decimal.NewFromInt(int64(wins)).Div(decimal.NewFromInt(int64(total))).Mul(decimal.NewFromInt(100))
		summary.RegimeWinRateBreakdown[regime] = rate
	}

	return summary
}

func (e *ScalpingBacktestEngine) validateConfig() error {
	if e.config.Mode != "" && e.config.Mode != "deterministic" && e.config.Mode != "ai" {
		return fmt.Errorf("invalid mode %q (expected 'deterministic' or 'ai')", e.config.Mode)
	}
	if e.config.StartTime.IsZero() || e.config.EndTime.IsZero() {
		return fmt.Errorf("start_time and end_time are required")
	}
	if !e.config.StartTime.Before(e.config.EndTime) {
		return fmt.Errorf("start_time must be before end_time")
	}
	if e.config.InitialCapital.LessThanOrEqual(decimal.Zero) {
		return fmt.Errorf("initial capital must be positive")
	}
	return nil
}

func normalizeScalpingBacktestConfig(config ScalpingBacktestConfig) ScalpingBacktestConfig {
	defaults := DefaultAIScalpingConfig()
	if config.InitialCapital.LessThanOrEqual(decimal.Zero) {
		config.InitialCapital = decimal.NewFromInt(10000)
	}
	if config.FeeRate.LessThanOrEqual(decimal.Zero) {
		config.FeeRate = decimal.NewFromFloat(defaultFallbackRoundTripFeePct).Div(decimal.NewFromInt(200))
	}
	if config.SlippagePct.LessThanOrEqual(decimal.Zero) {
		config.SlippagePct = decimal.NewFromFloat(DefaultScalpingBacktestSlippage)
	}
	if config.MaxBidAskSpreadPct <= 0 {
		config.MaxBidAskSpreadPct = appautonomy.DefaultScalpingMaxBidAskSpreadPct
	}
	if config.MinConfidence <= 0 {
		config.MinConfidence = defaults.MinConfidence
	}
	if config.MinExpectancyN <= 0 {
		config.MinExpectancyN = defaults.MinExpectancyN
	}
	if config.MaxCapitalPct <= 0 {
		config.MaxCapitalPct = DefaultScalpingBacktestMaxCapitalPct
	}
	if config.DefaultHoldPeriod <= 0 {
		config.DefaultHoldPeriod = DefaultScalpingBacktestHoldPeriod
	}
	if config.MaxHoldCandles <= 0 {
		config.MaxHoldCandles = 200
	}
	if config.SpreadMultiplier <= 0 {
		config.SpreadMultiplier = backtestSpreadMultiplier
	}
	if config.Mode == "" {
		config.Mode = "deterministic"
	}
	config.DeterministicFallback = config.DeterministicFallback.Normalized()
	config.AsymmetricExit = config.AsymmetricExit.Normalized()
	if len(config.Symbols) == 0 {
		config.Symbols = defaultScalpingBacktestUniverse()
	}
	return config
}

// computeSignalHints derives the AI-path sidecar metadata (SuggestedAction,
// ConfidenceHint, CandidateScore) from a deterministic decision. Mirrors the
// field set produced by AIScalpingService.signalsWithDecisionHints so a
// backtest result recorded with Mode="ai" can be diffed against what the
// live AI scalping path would have consumed. Returns nil for non-actionable
// decisions (hold / nil) and for buy decisions that the live AI path would
// reject via scalpingBuySignalRejectionReason (momentum below buy floor,
// fee-fragile spread, or range position above the buy ceiling).
func (e *ScalpingBacktestEngine) computeSignalHints(signal MarketSignal, decision *AITradingDecision) *SignalHints {
	if decision == nil {
		return nil
	}
	action := strings.ToLower(strings.TrimSpace(decision.Action))
	if action != "buy" && action != "sell" {
		return nil
	}
	if action == "buy" && scalpingBacktestBuyRejectionReason(signal) != "" {
		return nil
	}
	fallback := e.config.DeterministicFallback.Normalized()

	effectiveMaxSpread := fallback.MaxBidAskSpread
	if e.config.MaxBidAskSpreadPct > 0 {
		effectiveMaxSpread = math.Max(effectiveMaxSpread, e.config.MaxBidAskSpreadPct)
	}
	effectiveMaxSpread = math.Max(effectiveMaxSpread, 0.0001)
	liquidityScore := clampFloat(1-(signal.BidAskSpread/effectiveMaxSpread), 0, 1)
	volumeBasis := math.Max(signal.Volume24h, 0)
	volumeScore := clampFloat(math.Log10(volumeBasis+1)/fallback.VolumeLogScale, 0, 1)
	// Reuse the range-alignment value computed by buildDecisionFromSignal so
	// the AI-mode score reflects the exact same per-branch logic the
	// deterministic path applied for this action (reversal, sell-window,
	// proximity-adjusted, blowoff, and dual-proximity variants are all
	// captured by reading decision.RangeAlignment instead of re-deriving).
	score := math.Abs(signal.OrderBookImbalance)*fallback.ImbalanceWeight +
		liquidityScore*fallback.LiquidityWeight +
		decision.RangeAlignment*fallback.RangeWeight +
		volumeScore*fallback.VolumeWeight

	return &SignalHints{
		SuggestedAction: action,
		ConfidenceHint:  decision.Confidence,
		CandidateScore:  score,
	}
}

// scalpingBacktestBuyRejectionReason mirrors AIScalpingService.scalpingBuySignalRejectionReason
// for the backtest engine: returns a non-empty reason when a buy decision
// would be suppressed by the live AI hint path's momentum/spread/range gates
// that buildDecisionFromSignal does not enforce when RequireRecentMomentum=false.
// When RequireRecentMomentum=true, buildDecisionFromSignal already applies
// these gates itself and returns nil before reaching computeSignalHints.
func scalpingBacktestBuyRejectionReason(signal MarketSignal) string {
	if scalpingReversalBuyCandidate(signal, DefaultDeterministicFallbackConfig()) {
		return ""
	}
	buyMomentumMin := scalpingRecentBuyMinTrendPct
	if !signal.RecentChangeKnown {
		if signal.RangePosition24h > scalpingNoRecentBuyMaxRangePct {
			return fmt.Sprintf("buy hint rejected without recent momentum confirmation above deep-low range ceiling on %s (range_pos_24h=%.1f%%, required<=%.1f%%)", signal.Symbol, signal.RangePosition24h, scalpingNoRecentBuyMaxRangePct)
		}
		return ""
	}
	switch {
	case signal.RecentPriceChange < buyMomentumMin:
		return fmt.Sprintf("buy hint rejected without recent momentum confirmation on %s (recent_price_change=%.4f%%, required>=%.4f%%)", signal.Symbol, signal.RecentPriceChange, buyMomentumMin)
	case signal.BidAskSpread > scalpingRecentBuyMaxSpreadPct:
		return fmt.Sprintf("buy hint rejected with fee-fragile spread on %s (spread=%.4f%%, required<=%.4f%%)", signal.Symbol, signal.BidAskSpread, scalpingRecentBuyMaxSpreadPct)
	case signal.PriceChange24h < scalpingRecentBuyMinTrendPct:
		return fmt.Sprintf("buy hint rejected without positive 24h trend on %s (price_change_24h=%.4f%%, required>=%.4f%%)", signal.Symbol, signal.PriceChange24h, scalpingRecentBuyMinTrendPct)
	case signal.RangePosition24h > scalpingRecentBuyMaxRangePct:
		return fmt.Sprintf("buy hint rejected above recent-buy range ceiling on %s (range_pos_24h=%.1f%%, required<=%.1f%%)", signal.Symbol, signal.RangePosition24h, scalpingRecentBuyMaxRangePct)
	default:
		return ""
	}
}

func (e *ScalpingBacktestEngine) buildDecisionFromSignal(ctx context.Context, signal MarketSignal) (decision *AITradingDecision) {
	var rejectionReason string
	defer func() {
		if decision == nil {
			zaplogrus.Infof("[SCALPING-BACKTEST] reject %s on %s (spread=%.4f%%, imbalance=%.4f, ADX=%.1f, ATR=%.2f, BB%%b=%.4f, range=%.1f%%, trend_24h=%.4f%%, recent=%.4f%%)",
				rejectionReason, signal.Symbol, signal.BidAskSpread, signal.OrderBookImbalance, signal.ADX, signal.ATRRatio, signal.BBPercentB, signal.RangePosition24h, signal.PriceChange24h, signal.RecentPriceChange)
		}
	}()
	_ = ctx
	fallback := e.config.DeterministicFallback.Normalized()
	if signal.Price <= 0 || strings.TrimSpace(signal.Symbol) == "" {
		rejectionReason = "invalid_signal"
		return nil
	}

	imbalance := math.Abs(signal.OrderBookImbalance)
	reversalBuy := scalpingReversalBuyCandidate(signal, fallback)
	sellWindow := scalpingSellWindowCandidate(signal, fallback)
	if signal.BidAskSpread <= 0 || (signal.BidAskSpread > e.config.MaxBidAskSpreadPct && !reversalBuy && !sellWindow) {
		rejectionReason = "spread_too_wide"
		return nil
	}
	if signal.ADX > fallback.ADXMaxPct {
		rejectionReason = "adx_too_high"
		return nil
	}
	if signal.ATRRatio > fallback.ATRRatioMax {
		rejectionReason = "atr_too_high"
		return nil
	}
	effectiveMinImbalance := math.Min(fallback.MinImbalance, 0.20)
	if e.config.RequireRecentMomentum {
		effectiveMinImbalance = fallback.MinImbalance
	}
	if imbalance < effectiveMinImbalance && !scalpingBlowoffSellTrendConfirmed(signal, fallback) && !reversalBuy && !sellWindow {
		rejectionReason = "imbalance_too_weak"
		return nil
	}

	momentumPct := fallbackMomentumPct(signal)
	buyMomentumMin := fallback.BuyMinPriceChangePct
	sellMomentumMax := fallback.SellMaxPriceChangePct
	if e.config.RequireRecentMomentum {
		if !signal.RecentChangeKnown {
			rejectionReason = "recent_momentum_required"
			return nil
		}
		momentumPct = signal.RecentPriceChange
		minRecentMomentum := e.config.MinRecentMomentumPct
		if minRecentMomentum <= 0 {
			minRecentMomentum = 0.05
		}
		buyMomentumMin = math.Max(buyMomentumMin, minRecentMomentum)
		sellMomentumMax = math.Min(sellMomentumMax, -minRecentMomentum)
	}
	action := ""
	rangeAlignment := 0.0
	momentumAligned := false
	switch {
	case reversalBuy:
		action = "buy"
		momentumAligned = true
		rangeAlignment = clampFloat((fallback.ReversalBuyMaxRangePct-signal.RangePosition24h)/math.Max(fallback.ReversalBuyMaxRangePct, 1), 0, 1)
	case sellWindow:
		action = "sell"
		momentumAligned = true
		rangeAlignment = clampFloat((signal.RangePosition24h-fallback.SellWindowMinRangePct)/math.Max(100-fallback.SellWindowMinRangePct, 1), 0, 1)
	case signal.BBPercentB < fallback.BBEntryMaxPct && imbalance > 0.05 && signal.RangePosition24h <= fallback.BuyRangeMax:
		action = "buy"
		momentumAligned = momentumPct >= buyMomentumMin
		rangeAlignment = clampFloat((fallback.BuyRangeMax-signal.RangePosition24h)/math.Max(fallback.BuyRangeMax, 1), 0, 1)
	case signal.BBPercentB > fallback.BBExitMinPct && imbalance > 0.05 && signal.RangePosition24h >= fallback.SellRangeMin:
		action = "sell"
		momentumAligned = momentumPct <= sellMomentumMax
		rangeAlignment = clampFloat((signal.RangePosition24h-fallback.SellRangeMin)/math.Max(100-fallback.SellRangeMin, 1), 0, 1)
	case signal.BBPercentB < fallback.BBEntryMaxPct && imbalance > 0.05 && signal.RangePosition24h <= fallback.BuyRangeMax+5:
		action = "buy"
		momentumAligned = momentumPct >= buyMomentumMin
		rangeAlignment = clampFloat((fallback.BuyRangeMax+5-signal.RangePosition24h)/math.Max(fallback.BuyRangeMax+5, 1), 0, 1)
	case signal.BBPercentB > fallback.BBExitMinPct && imbalance > 0.05 && signal.RangePosition24h >= fallback.SellRangeMin-5:
		action = "sell"
		momentumAligned = momentumPct <= sellMomentumMax
		rangeAlignment = clampFloat((signal.RangePosition24h-(fallback.SellRangeMin-5))/math.Max(100-(fallback.SellRangeMin-5), 1), 0, 1)
	case scalpingBlowoffSellTrendConfirmed(signal, fallback):
		action = "sell"
		momentumAligned = true
		rangeAlignment = clampFloat(
			(signal.RangePosition24h-fallback.BlowoffSellRangeMin)/math.Max(fallback.BlowoffSellRangeMax-fallback.BlowoffSellRangeMin, 1),
			0,
			1,
		)
	case signal.BBPercentB < fallback.BBEntryMaxPct && imbalance > math.Max(effectiveMinImbalance, 0.20) && momentumPct > buyMomentumMin && signal.RangePosition24h <= fallback.SellRangeMin+5:
		action = "buy"
		momentumAligned = true
		rangeAlignment = clampFloat((fallback.SellRangeMin+5-signal.RangePosition24h)/math.Max(fallback.SellRangeMin+5-fallback.BuyRangeMax, 1), 0, 1)
	case signal.BBPercentB > fallback.BBExitMinPct && imbalance > math.Max(effectiveMinImbalance, 0.20) && momentumPct < sellMomentumMax && signal.RangePosition24h >= fallback.BuyRangeMax-5:
		action = "sell"
		momentumAligned = true
		rangeAlignment = clampFloat((signal.RangePosition24h-(fallback.BuyRangeMax-5))/math.Max(fallback.SellRangeMin-(fallback.BuyRangeMax-5), 1), 0, 1)
	default:
		rejectionReason = "no_directional_edge"
		return nil
	}

	if !momentumAligned {
		rejectionReason = "momentum_not_aligned"
		return nil
	}
	if e.config.RequireRecentMomentum && action == "buy" && !reversalBuy && signal.BidAskSpread > fallback.RecentBuyMaxSpreadPct {
		rejectionReason = "recent_buy_spread_too_wide"
		return nil
	}
	if e.config.RequireRecentMomentum && action == "buy" && !reversalBuy && signal.PriceChange24h < fallback.RecentBuyMinTrendPct {
		rejectionReason = "recent_buy_trend_too_low"
		return nil
	}
	if e.config.RequireRecentMomentum && action == "buy" && !reversalBuy && signal.RangePosition24h > fallback.RecentBuyMaxRangePct {
		rejectionReason = "recent_buy_range_too_high"
		return nil
	}
	if e.config.RequireRecentMomentum && action == "sell" && !scalpingBlowoffSellTrendConfirmed(signal, fallback) &&
		!sellWindow && signal.RangePosition24h < fallback.RecentSellMinRangePct {
		rejectionReason = "recent_sell_range_too_low"
		return nil
	}

	liquidityScore := clampFloat(1-(signal.BidAskSpread/math.Max(e.config.MaxBidAskSpreadPct, 0.0001)), 0, 1)
	volumeScore := clampFloat(math.Log10(math.Max(signal.Volume24h, 0)+1)/fallback.VolumeLogScale, 0, 1)
	score := imbalance*fallback.ImbalanceWeight +
		liquidityScore*fallback.LiquidityWeight +
		rangeAlignment*fallback.RangeWeight +
		volumeScore*fallback.VolumeWeight
	confidence := clampFloat(
		fallback.BaseConfidence+score*fallback.ConfidenceScale,
		fallback.MinConfidence,
		fallback.MaxConfidence,
	)

	if confidence < e.config.MinConfidence {
		rejectionReason = "confidence_below_floor"
		return nil
	}

	risk, reward := fallbackRiskRewardPct(signal)
	projectedNetEdgePct := fallbackProjectedNetEdgePct(signal.BidAskSpread, reward)
	requiredNetEdgePct := fallbackRequiredNetEdgePct(TradingPortfolio{AccountTier: e.policy.AccountTier}, e.config.MinExpectancyEdge)
	if projectedNetEdgePct.LessThan(requiredNetEdgePct) {
		rejectionReason = "expectancy_below_min_edge"
		return nil
	}

	price := decimal.NewFromFloat(signal.Price)
	one := decimal.NewFromInt(1)
	stopLoss := decimal.Zero
	takeProfit := decimal.Zero
	if action == "sell" {
		if risk.GreaterThan(decimal.Zero) {
			stopLoss = price.Mul(one.Add(risk))
		}
		takeProfit = price.Mul(one.Sub(reward))
	} else {
		if risk.GreaterThan(decimal.Zero) {
			stopLoss = price.Mul(one.Sub(risk))
		}
		takeProfit = price.Mul(one.Add(reward))
	}
	sizePct := clampFloat(e.config.MaxCapitalPct*fallback.SizeFraction, fallback.MinSizePct, e.config.MaxCapitalPct)

	return &AITradingDecision{
		Action:          action,
		Symbol:          signal.Symbol,
		SizePercent:     sizePct,
		Confidence:      confidence,
		Reasoning:       "deterministic backtest candidate",
		ReasonCategory:  reasonCategoryDeterministicFallback,
		ConfidenceKnown: true,
		StopLoss:        &stopLoss,
		TakeProfit:      &takeProfit,
		RangeAlignment:  rangeAlignment,
	}
}

func computeExpectancy(wins, losses int, winSum, lossSum decimal.Decimal) float64 {
	sample := wins + losses
	if sample == 0 {
		return 0
	}
	avgWin := decimal.Zero
	avgLoss := decimal.Zero
	if wins > 0 {
		avgWin = winSum.Div(decimal.NewFromInt(int64(wins)))
	}
	if losses > 0 {
		avgLoss = lossSum.Div(decimal.NewFromInt(int64(losses)))
	}
	winRate := float64(wins) / float64(sample)
	expectancyDec := decimal.NewFromFloat(winRate).Mul(avgWin).
		Sub(decimal.NewFromFloat(1 - winRate).Mul(avgLoss))
	expectancy, _ := expectancyDec.Float64()
	return expectancy
}

func (e *ScalpingBacktestEngine) scopedExpectancy(symbol, action string) (expectancy float64, sample int, scoped bool) {
	var wins int
	var losses int
	var winSum decimal.Decimal
	var lossSum decimal.Decimal

	for _, trade := range e.tradeHistory {
		if !strings.EqualFold(normalizeSymbolForComparison(trade.Symbol), normalizeSymbolForComparison(symbol)) {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(trade.Side), strings.TrimSpace(action)) {
			continue
		}
		if trade.PnL.GreaterThan(decimal.Zero) {
			wins++
			winSum = winSum.Add(trade.PnL)
		} else if trade.PnL.LessThan(decimal.Zero) {
			losses++
			lossSum = lossSum.Add(trade.PnL.Abs())
		}
	}

	sample = wins + losses
	if sample >= e.config.MinExpectancyN {
		return computeExpectancy(wins, losses, winSum, lossSum), sample, true
	}

	wins = 0
	losses = 0
	winSum = decimal.Zero
	lossSum = decimal.Zero
	for _, trade := range e.tradeHistory {
		if !strings.EqualFold(strings.TrimSpace(trade.Side), strings.TrimSpace(action)) {
			continue
		}
		if trade.PnL.GreaterThan(decimal.Zero) {
			wins++
			winSum = winSum.Add(trade.PnL)
		} else if trade.PnL.LessThan(decimal.Zero) {
			losses++
			lossSum = lossSum.Add(trade.PnL.Abs())
		}
	}
	sample = wins + losses
	if sample == 0 {
		return 0, 0, false
	}
	return computeExpectancy(wins, losses, winSum, lossSum), sample, false
}

func (e *ScalpingBacktestEngine) loadSignalsFromOHLCV(
	ctx context.Context,
	startTime time.Time,
	endTime time.Time,
	symbolFilter map[string]struct{},
) ([]HistoricalSignal, error) {
	tradingPairIDs, err := e.resolveTradingPairIDs(ctx, symbolFilter)
	if err != nil {
		return nil, fmt.Errorf("resolve trading pairs for ohlcv signals: %w", err)
	}
	if len(tradingPairIDs) == 0 {
		return nil, nil
	}

	query := `
		SELECT tp.symbol, COALESCE(ce.ccxt_id, e.ccxt_id, e.name), od.open, od.high, od.low, od.close, od.volume, od.timestamp
		FROM ohlcv_candles od
		JOIN trading_pairs tp ON tp.id = od.trading_pair_id
		JOIN exchanges e ON e.id = od.exchange_id
		LEFT JOIN ccxt_exchanges ce ON ce.exchange_id = e.id
		WHERE od.timestamp >= $1 AND od.timestamp <= $2
	`
	args := []any{startTime, endTime}
	query, args = appendScalpingBacktestFilters(query, args, "od.trading_pair_id", tradingPairIDs, strings.TrimSpace(strings.ToLower(e.config.Exchange)))
	query += " ORDER BY od.timestamp ASC"

	rows, err := e.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("load ohlcv signals: %w", err)
	}
	defer rows.Close()

	points := make([]scalpingOHLCVPoint, 0)
	for rows.Next() {
		var p scalpingOHLCVPoint
		if scanErr := rows.Scan(&p.symbol, &p.exchange, &p.open, &p.high, &p.low, &p.close, &p.volume, &p.timestamp); scanErr != nil {
			return nil, fmt.Errorf("scan ohlcv signal row: %w", scanErr)
		}
		if p.close <= 0 {
			continue
		}
		points = append(points, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ohlcv signals: %w", err)
	}

	return buildHistoricalSignalsFromOHLCV(points, e.config.SpreadMultiplier), nil
}

func (e *ScalpingBacktestEngine) loadSignalsFromMarketData(
	ctx context.Context,
	startTime time.Time,
	endTime time.Time,
	symbolFilter map[string]struct{},
) ([]HistoricalSignal, error) {
	tradingPairIDs, err := e.resolveTradingPairIDs(ctx, symbolFilter)
	if err != nil {
		return nil, fmt.Errorf("resolve trading pairs for market data signals: %w", err)
	}
	if len(tradingPairIDs) == 0 {
		return nil, nil
	}

	priceColumn := e.marketDataPriceColumn
	if priceColumn == scalpingMarketDataPriceColumnUnknown {
		priceColumn = scalpingMarketDataPriceColumnPrice
	}
	query := buildScalpingMarketDataSignalQuery(priceColumn)
	args := []any{startTime, endTime}
	query, args = appendScalpingBacktestFilters(query, args, "md.trading_pair_id", tradingPairIDs, strings.TrimSpace(strings.ToLower(e.config.Exchange)))
	query += " ORDER BY md.timestamp ASC"

	rows, err := e.db.Query(ctx, query, args...)
	if err != nil && priceColumn == scalpingMarketDataPriceColumnPrice && isMissingMarketDataPriceColumnError(err) {
		e.marketDataPriceColumn = scalpingMarketDataPriceColumnLastPrice
		query = buildScalpingMarketDataSignalQuery(e.marketDataPriceColumn)
		args = []any{startTime, endTime}
		query, args = appendScalpingBacktestFilters(query, args, "md.trading_pair_id", tradingPairIDs, strings.TrimSpace(strings.ToLower(e.config.Exchange)))
		query += " ORDER BY md.timestamp ASC"
		rows, err = e.db.Query(ctx, query, args...)
	}
	if err != nil {
		return nil, fmt.Errorf("load market_data fallback signals: %w", err)
	}
	if e.marketDataPriceColumn == scalpingMarketDataPriceColumnUnknown {
		e.marketDataPriceColumn = priceColumn
	}
	defer rows.Close()

	signals := make([]HistoricalSignal, 0)
	lastPriceBySeries := map[string]float64{}
	for rows.Next() {
		var symbol string
		var exchange string
		var price float64
		var bid float64
		var ask float64
		var high24 float64
		var low24 float64
		var volume24 float64
		var ts time.Time

		if scanErr := rows.Scan(&symbol, &exchange, &price, &bid, &ask, &high24, &low24, &volume24, &ts); scanErr != nil {
			return nil, fmt.Errorf("scan market_data signal row: %w", scanErr)
		}
		if price <= 0 {
			continue
		}

		spread := 0.0
		if bid > 0 && ask > 0 {
			spread = ((ask - bid) / price) * 100
		}
		if spread <= 0 && high24 > low24 {
			spread = estimateEffectiveSpreadPct(high24, low24, price, e.config.SpreadMultiplier)
		}

		imbalance := 0.0
		seriesKey := scalpingSeriesKey(symbol, exchange)
		if last, ok := lastPriceBySeries[seriesKey]; ok && price > 0 {
			imbalance = clampFloat((price-last)/price, -1, 1)
		}
		lastPriceBySeries[seriesKey] = price

		rangePos := 50.0
		if high24 > low24 {
			rangePos = clampFloat(((price-low24)/(high24-low24))*100, 0, 100)
		}

		priceChange24h := 0.0
		if low24 > 0 {
			priceChange24h = ((price - low24) / low24) * 100
		}

		signals = append(signals, HistoricalSignal{
			Timestamp: ts,
			Symbol:    symbol,
			Exchange:  exchange,
			Signal: MarketSignal{
				Symbol:             symbol,
				Price:              price,
				High24h:            high24,
				Low24h:             low24,
				Volume24h:          math.Max(volume24, 0),
				BidAskSpread:       math.Max(spread, 0),
				OrderBookImbalance: imbalance,
				PriceChange24h:     priceChange24h,
				RangePosition24h:   rangePos,
			},
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate market_data signals: %w", err)
	}

	return signals, nil
}

type scalpingMarketDataPriceColumn int

const (
	scalpingMarketDataPriceColumnUnknown scalpingMarketDataPriceColumn = iota
	scalpingMarketDataPriceColumnPrice
	scalpingMarketDataPriceColumnLastPrice
)

func buildScalpingMarketDataSignalQuery(priceColumn scalpingMarketDataPriceColumn) string {
	priceExpr := "md.price"
	highExpr := "COALESCE(md.high_24h, 0)"
	lowExpr := "COALESCE(md.low_24h, 0)"
	if priceColumn == scalpingMarketDataPriceColumnLastPrice {
		priceExpr = "md.last_price"
		highExpr = "CASE WHEN COALESCE(md.ask, 0) > 0 THEN md.ask ELSE md.last_price END"
		lowExpr = "CASE WHEN COALESCE(md.bid, 0) > 0 THEN md.bid ELSE md.last_price END"
	}

	return fmt.Sprintf(`
		SELECT tp.symbol, COALESCE(ce.ccxt_id, e.ccxt_id, e.name), %s,
			COALESCE(md.bid, 0), COALESCE(md.ask, 0),
			%s, %s, COALESCE(md.volume_24h, 0),
			md.timestamp
		FROM market_data md
		JOIN trading_pairs tp ON tp.id = md.trading_pair_id
		JOIN exchanges e ON e.id = md.exchange_id
		LEFT JOIN ccxt_exchanges ce ON ce.exchange_id = e.id
		WHERE md.timestamp >= $1 AND md.timestamp <= $2
	`, priceExpr, highExpr, lowExpr)
}

func isMissingMarketDataPriceColumnError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no such column: md.price") ||
		strings.Contains(msg, "column md.price does not exist")
}

func buildHistoricalSignalsFromOHLCV(points []scalpingOHLCVPoint, spreadMultiplier float64) []HistoricalSignal {
	if len(points) == 0 {
		return nil
	}

	bySymbol := make(map[string][]scalpingOHLCVPoint)
	for _, point := range points {
		norm := scalpingSeriesKey(point.symbol, point.exchange)
		bySymbol[norm] = append(bySymbol[norm], point)
	}

	signals := make([]HistoricalSignal, 0, len(points))
	multiplier := spreadMultiplier
	if multiplier <= 0 {
		multiplier = backtestSpreadMultiplier
	}
	for _, series := range bySymbol {
		sort.Slice(series, func(i, j int) bool {
			return series[i].timestamp.Before(series[j].timestamp)
		})
		windowMetrics := compute24hWindowMetrics(series)
		adxValues := computeADX(series, 14)
		atrValues := computeATR(series, 14)
		atrSMA := computeSMA(atrValues, 20)
		bbUpper, _, bbLower := computeBollingerBands(series, 20, 2.0)

		var prevClose float64
		for i, point := range series {
			priceChange24h := 0.0
			if windowMetrics[i].HasReferenceClose && windowMetrics[i].ReferenceClose24h > 0 {
				priceChange24h = ((point.close - windowMetrics[i].ReferenceClose24h) / windowMetrics[i].ReferenceClose24h) * 100
			}

			atrRatio := 0.0
			if atrSMA[i] > 0 {
				atrRatio = atrValues[i] / atrSMA[i]
			}

			bbPctB := 0.5
			bbRange := bbUpper[i] - bbLower[i]
			if bbRange > 0 {
				bbPctB = (point.close - bbLower[i]) / bbRange
			}

			signals = append(signals, mapPointToHistoricalSignal(point, windowMetrics[i], priceChange24h, multiplier, prevClose, adxValues[i], atrRatio, bbPctB, windowMetrics[i].Volume24h, 0))
			prevClose = point.close
		}
	}

	sort.Slice(signals, func(i, j int) bool {
		if signals[i].Timestamp.Equal(signals[j].Timestamp) {
			if signals[i].Symbol == signals[j].Symbol {
				return signals[i].Exchange < signals[j].Exchange
			}
			return signals[i].Symbol < signals[j].Symbol
		}
		return signals[i].Timestamp.Before(signals[j].Timestamp)
	})

	return signals
}

type scalping24hWindowMetrics struct {
	High24h           float64
	Low24h            float64
	Volume24h         float64
	ReferenceClose24h float64
	HasReferenceClose bool
}

func compute24hWindowMetrics(series []scalpingOHLCVPoint) []scalping24hWindowMetrics {
	if len(series) == 0 {
		return nil
	}

	metrics := make([]scalping24hWindowMetrics, len(series))
	start := 0
	runningVolume := 0.0
	highDeque := make([]int, 0, len(series))
	lowDeque := make([]int, 0, len(series))
	for i, point := range series {
		windowStart := point.timestamp.Add(-24 * time.Hour)
		runningVolume += math.Max(point.volume, 0)

		for len(highDeque) > 0 && series[highDeque[len(highDeque)-1]].high <= point.high {
			highDeque = highDeque[:len(highDeque)-1]
		}
		highDeque = append(highDeque, i)

		for len(lowDeque) > 0 && series[lowDeque[len(lowDeque)-1]].low >= point.low {
			lowDeque = lowDeque[:len(lowDeque)-1]
		}
		lowDeque = append(lowDeque, i)

		for start <= i && series[start].timestamp.Before(windowStart) {
			runningVolume -= math.Max(series[start].volume, 0)
			// Any entry equal to start can only survive at the deque front; less-extreme
			// candidates were already evicted during insertion when a newer point dominated them.
			if len(highDeque) > 0 && highDeque[0] == start {
				highDeque = highDeque[1:]
			}
			if len(lowDeque) > 0 && lowDeque[0] == start {
				lowDeque = lowDeque[1:]
			}
			start++
		}

		metrics[i] = scalping24hWindowMetrics{
			High24h:   series[highDeque[0]].high,
			Low24h:    series[lowDeque[0]].low,
			Volume24h: runningVolume,
		}
		if start <= i {
			referenceIdx := start
			if series[start].timestamp.After(windowStart) && start > 0 {
				referenceIdx = start - 1
			}
			if referenceIdx >= 0 && series[referenceIdx].close > 0 {
				metrics[i].ReferenceClose24h = series[referenceIdx].close
				metrics[i].HasReferenceClose = true
			}
		}
	}

	return metrics
}

func mapPointToHistoricalSignal(point scalpingOHLCVPoint, metrics scalping24hWindowMetrics, priceChange24h float64, spreadMultiplier float64, prevClose float64, adx float64, atrRatio float64, bbPctB float64, volume24h float64, recentTrades int) HistoricalSignal {
	imbalance := 0.0
	if point.high > point.low {
		imbalance = clampFloat((point.close-point.open)/(point.high-point.low), -1, 1)
	}

	rangePos := 50.0
	if metrics.High24h > metrics.Low24h {
		rangePos = clampFloat(((point.close-metrics.Low24h)/(metrics.High24h-metrics.Low24h))*100, 0, 100)
	}

	recentChangeKnown := false
	recentPriceChange := 0.0
	if prevClose > 0 {
		recentChangeKnown = true
		recentPriceChange = ((point.close - prevClose) / prevClose) * 100
	}

	return HistoricalSignal{
		Timestamp: point.timestamp,
		Symbol:    point.symbol,
		Exchange:  point.exchange,
		Signal: MarketSignal{
			Symbol:             point.symbol,
			Price:              point.close,
			High24h:            metrics.High24h,
			Low24h:             metrics.Low24h,
			Volume24h:          metrics.Volume24h,
			BidAskSpread:       estimateEffectiveSpreadPct(point.high, point.low, point.close, spreadMultiplier),
			OrderBookImbalance: imbalance,
			PriceChange24h:     priceChange24h,
			RangePosition24h:   rangePos,
			RecentPriceChange:  recentPriceChange,
			RecentChangeKnown:  recentChangeKnown,
			ADX:                adx,
			ATRRatio:           atrRatio,
			BBPercentB:         bbPctB,
			Low:                point.low,
			High:               point.high,
		},
	}
}

func estimateEffectiveSpreadPct(high, low, price, spreadMultiplier float64) float64 {
	if price <= 0 || high <= low {
		return 0
	}
	multiplier := spreadMultiplier
	if multiplier <= 0 {
		multiplier = backtestSpreadMultiplier
	}
	return math.Max(((high-low)/price)*100/multiplier, 0)
}

func computeATR(series []scalpingOHLCVPoint, period int) []float64 {
	n := len(series)
	result := make([]float64, n)
	if n < 2 || period <= 0 {
		return result
	}

	trValues := make([]float64, n)
	for i := 1; i < n; i++ {
		hl := series[i].high - series[i].low
		hpc := math.Abs(series[i].high - series[i-1].close)
		lpc := math.Abs(series[i].low - series[i-1].close)
		trValues[i] = math.Max(hl, math.Max(hpc, lpc))
	}

	if n-1 < period {
		return result
	}
	sum := 0.0
	for i := 1; i <= period; i++ {
		sum += trValues[i]
	}
	result[period] = sum / float64(period)
	for i := period + 1; i < n; i++ {
		result[i] = (result[i-1]*float64(period-1) + trValues[i]) / float64(period)
	}

	return result
}

func computeSMA(values []float64, period int) []float64 {
	n := len(values)
	result := make([]float64, n)
	if n < period || period <= 0 {
		return result
	}

	sum := 0.0
	for i := 0; i < period; i++ {
		sum += values[i]
	}
	result[period-1] = sum / float64(period)
	for i := period; i < n; i++ {
		sum += values[i] - values[i-period]
		result[i] = sum / float64(period)
	}

	return result
}

func computeStdDev(values []float64, period int) []float64 {
	n := len(values)
	result := make([]float64, n)
	if n < period || period <= 0 {
		return result
	}

	sum := 0.0
	sumSq := 0.0
	for i := 0; i < period; i++ {
		sum += values[i]
		sumSq += values[i] * values[i]
	}
	mean := sum / float64(period)
	variance := sumSq/float64(period) - mean*mean
	if variance < 0 {
		variance = 0
	}
	result[period-1] = math.Sqrt(variance)

	for i := period; i < n; i++ {
		sum += values[i] - values[i-period]
		sumSq += values[i]*values[i] - values[i-period]*values[i-period]
		mean = sum / float64(period)
		variance = sumSq/float64(period) - mean*mean
		if variance < 0 {
			variance = 0
		}
		result[i] = math.Sqrt(variance)
	}

	return result
}

func computeBollingerBands(series []scalpingOHLCVPoint, period int, stdDevMult float64) (upper []float64, middle []float64, lower []float64) {
	n := len(series)
	upper = make([]float64, n)
	middle = make([]float64, n)
	lower = make([]float64, n)
	if n < period || period <= 0 {
		return upper, middle, lower
	}

	closes := make([]float64, n)
	for i, p := range series {
		closes[i] = p.close
	}

	sma := computeSMA(closes, period)
	stdDev := computeStdDev(closes, period)

	for i := period - 1; i < n; i++ {
		middle[i] = sma[i]
		upper[i] = sma[i] + stdDevMult*stdDev[i]
		lower[i] = sma[i] - stdDevMult*stdDev[i]
	}

	return upper, middle, lower
}

func computeADX(series []scalpingOHLCVPoint, period int) []float64 {
	n := len(series)
	result := make([]float64, n)
	if n < 2*period+1 || period <= 0 {
		return result
	}

	tr := make([]float64, n)
	plusDM := make([]float64, n)
	minusDM := make([]float64, n)

	for i := 1; i < n; i++ {
		hl := series[i].high - series[i].low
		hpc := math.Abs(series[i].high - series[i-1].close)
		lpc := math.Abs(series[i].low - series[i-1].close)
		tr[i] = math.Max(hl, math.Max(hpc, lpc))

		upMove := series[i].high - series[i-1].high
		downMove := series[i-1].low - series[i].low
		if upMove > downMove && upMove > 0 {
			plusDM[i] = upMove
		}
		if downMove > upMove && downMove > 0 {
			minusDM[i] = downMove
		}
	}

	smoothTR := make([]float64, n)
	smoothPlusDM := make([]float64, n)
	smoothMinusDM := make([]float64, n)

	sumTR := 0.0
	sumPDM := 0.0
	sumMDM := 0.0
	for i := 1; i <= period; i++ {
		sumTR += tr[i]
		sumPDM += plusDM[i]
		sumMDM += minusDM[i]
	}
	smoothTR[period] = sumTR
	smoothPlusDM[period] = sumPDM
	smoothMinusDM[period] = sumMDM

	for i := period + 1; i < n; i++ {
		smoothTR[i] = smoothTR[i-1] - smoothTR[i-1]/float64(period) + tr[i]
		smoothPlusDM[i] = smoothPlusDM[i-1] - smoothPlusDM[i-1]/float64(period) + plusDM[i]
		smoothMinusDM[i] = smoothMinusDM[i-1] - smoothMinusDM[i-1]/float64(period) + minusDM[i]
	}

	plusDI := make([]float64, n)
	minusDI := make([]float64, n)
	for i := period; i < n; i++ {
		if smoothTR[i] > 0 {
			plusDI[i] = 100 * smoothPlusDM[i] / smoothTR[i]
			minusDI[i] = 100 * smoothMinusDM[i] / smoothTR[i]
		}
	}

	dx := make([]float64, n)
	for i := period; i < n; i++ {
		denom := plusDI[i] + minusDI[i]
		if denom > 0 {
			dx[i] = 100 * math.Abs(plusDI[i]-minusDI[i]) / denom
		}
	}

	adxStart := 2 * period
	if adxStart >= n {
		return result
	}
	sumDX := 0.0
	for i := adxStart - period + 1; i <= adxStart; i++ {
		sumDX += dx[i]
	}
	result[adxStart] = sumDX / float64(period)
	for i := adxStart + 1; i < n; i++ {
		result[i] = (result[i-1]*float64(period-1) + dx[i]) / float64(period)
	}

	return result
}

func appendScalpingBacktestFilters(baseQuery string, args []any, tradingPairColumn string, tradingPairIDs []int, exchangeFilter string) (string, []any) {
	query := baseQuery
	if len(tradingPairIDs) > 0 {
		placeholders := make([]string, len(tradingPairIDs))
		for i, id := range tradingPairIDs {
			placeholders[i] = fmt.Sprintf("$%d", len(args)+1)
			args = append(args, id)
		}
		query += fmt.Sprintf(" AND %s IN (%s)", tradingPairColumn, strings.Join(placeholders, ", "))
	}
	if exchangeFilter != "" {
		query += fmt.Sprintf(" AND LOWER(COALESCE(ce.ccxt_id, e.ccxt_id, e.name)) = $%d", len(args)+1)
		args = append(args, exchangeFilter)
	}
	return query, args
}

func (e *ScalpingBacktestEngine) resolveTradingPairIDs(ctx context.Context, symbolFilter map[string]struct{}) ([]int, error) {
	if len(symbolFilter) == 0 {
		return nil, nil
	}

	normalizedSymbols := make([]string, 0, len(symbolFilter))
	for symbol := range symbolFilter {
		normalized := normalizeSymbolForComparison(symbol)
		if normalized == "" {
			continue
		}
		normalizedSymbols = append(normalizedSymbols, normalized)
	}
	if len(normalizedSymbols) == 0 {
		return nil, nil
	}
	placeholders := make([]string, 0, len(normalizedSymbols))
	args := make([]any, 0, len(normalizedSymbols))
	for i, symbol := range normalizedSymbols {
		placeholders = append(placeholders, fmt.Sprintf("$%d", i+1))
		args = append(args, symbol)
	}
	query := fmt.Sprintf(`SELECT id
		FROM trading_pairs
		WHERE %s IN (%s)`, normalizedTradingPairSymbolExpr(e.db), strings.Join(placeholders, ", "))
	rows, err := e.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query trading pairs: %w", err)
	}
	defer rows.Close()

	ids := make([]int, 0, len(normalizedSymbols))
	for rows.Next() {
		var id int
		if scanErr := rows.Scan(&id); scanErr != nil {
			return nil, fmt.Errorf("scan trading pair row: %w", scanErr)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate trading pair rows: %w", err)
	}

	return ids, nil
}

func normalizedTradingPairSymbolExpr(db DBPool) string {
	if isSQLiteTradingPairDB(db) {
		return `UPPER(REPLACE(
			CASE
				WHEN INSTR(symbol, ':') > 0 THEN SUBSTR(symbol, 1, INSTR(symbol, ':') - 1)
				ELSE symbol
			END,
			'-',
			'/'
		))`
	}
	return `UPPER(REPLACE(
		CASE
			WHEN POSITION(':' IN symbol) > 0 THEN SUBSTRING(symbol FROM 1 FOR POSITION(':' IN symbol) - 1)
			ELSE symbol
		END,
		'-',
		'/'
	))`
}

// sqlitePoolProbe is implemented by adapters that wrap an in-process
// *internaldb.SQLiteDB. The services package cannot import
// internal/api/handlers (where the production HTTP adapter lives), so
// it asks via this marker interface instead. Adding this method to
// an adapter is the only change required to make a wrapper visible
// to isSQLiteTradingPairDB.
type sqlitePoolProbe interface {
	IsSQLitePool() bool
}

func isSQLiteTradingPairDB(db DBPool) bool {
	switch v := db.(type) {
	case *internaldb.SQLiteDB:
		return true
	case readOnlyDBPoolAdapter:
		if sqlite, ok := v.pool.(*internaldb.SQLiteDB); ok && sqlite != nil {
			return true
		}
		return false
	case sqlitePoolProbe:
		return v.IsSQLitePool()
	default:
		return false
	}
}

func scalpingSeriesKey(symbol, exchange string) string {
	normSymbol := normalizeSymbolForComparison(symbol)
	normExchange := strings.TrimSpace(strings.ToLower(exchange))
	if normExchange == "" {
		return normSymbol
	}
	return normSymbol + "@" + normExchange
}

func (e *ScalpingBacktestEngine) updateGateStats(gateName string, result GateResult, symbol, regime string) {
	stats, ok := e.gateStats[gateName]
	if !ok {
		stats = &GateStats{
			RejectionReasons:  map[string]int{},
			BreakdownBySymbol: map[string]int{},
			BreakdownByRegime: map[string]int{},
		}
		e.gateStats[gateName] = stats
	}

	if result.Allowed {
		stats.PassCount++
		return
	}
	stats.RejectCount++
	reason := strings.TrimSpace(result.Reason)
	if reason == "" {
		reason = "rejected"
	}
	stats.RejectionReasons[reason]++
	stats.BreakdownBySymbol[symbol]++
	stats.BreakdownByRegime[regime]++
}

func (e *ScalpingBacktestEngine) buildGateSummary() []GateSummaryEntry {
	entries := make([]GateSummaryEntry, 0, len(e.gateStats))
	for gateName, stats := range e.gateStats {
		reasons := make([]ReasonCount, 0, len(stats.RejectionReasons))
		for reason, count := range stats.RejectionReasons {
			reasons = append(reasons, ReasonCount{Reason: reason, Count: count})
		}
		sort.Slice(reasons, func(i, j int) bool {
			if reasons[i].Count == reasons[j].Count {
				return reasons[i].Reason < reasons[j].Reason
			}
			return reasons[i].Count > reasons[j].Count
		})

		if len(reasons) > 5 {
			reasons = reasons[:5]
		}

		entries = append(entries, GateSummaryEntry{
			GateName:            gateName,
			PassCount:           stats.PassCount,
			RejectCount:         stats.RejectCount,
			TopRejectionReasons: reasons,
			BreakdownBySymbol:   cloneMap(stats.BreakdownBySymbol),
			BreakdownByRegime:   cloneMap(stats.BreakdownByRegime),
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].GateName < entries[j].GateName
	})
	return entries
}

func calculateBacktestSharpe(returns []float64) float64 {
	if len(returns) < 2 {
		return 0
	}
	mean := 0.0
	for _, r := range returns {
		mean += r
	}
	mean /= float64(len(returns))

	variance := 0.0
	for _, r := range returns {
		d := r - mean
		variance += d * d
	}
	variance /= float64(len(returns) - 1)
	std := math.Sqrt(math.Max(variance, 0))
	if std == 0 {
		return 0
	}
	return mean / std
}

func toGateBoolMap(input map[string]GateResult) map[string]bool {
	output := make(map[string]bool, len(input))
	for name, result := range input {
		output[name] = result.Allowed
	}
	return output
}

func gateReason(passed bool, reason string) string {
	if passed {
		return ""
	}
	return reason
}

func outcomeFromPnL(pnl decimal.Decimal) string {
	if pnl.GreaterThan(decimal.Zero) {
		return "win"
	}
	if pnl.LessThan(decimal.Zero) {
		return "loss"
	}
	return "breakeven"
}

func cloneMap(input map[string]int) map[string]int {
	if len(input) == 0 {
		return map[string]int{}
	}
	output := make(map[string]int, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func (e *ScalpingBacktestEngine) configRegimeHighBand() float64 {
	if e.config.RegimeHighBand > 0 {
		return e.config.RegimeHighBand
	}
	return 85
}

func (e *ScalpingBacktestEngine) configRegimeLowBand() float64 {
	if e.config.RegimeLowBand > 0 {
		return e.config.RegimeLowBand
	}
	return 15
}
