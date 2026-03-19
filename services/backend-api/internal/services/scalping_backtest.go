package services

import (
	"context"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	appautonomy "github.com/irfndi/neuratrade/internal/app/autonomy"
	"github.com/shopspring/decimal"
)

const (
	DefaultScalpingBacktestSlippage         = 0.001
	DefaultScalpingBacktestHoldPeriod       = 5 * time.Minute
	DefaultScalpingBacktestMaxCapitalPct    = 5.0
	DefaultScalpingBacktestSpreadMultiplier = 8

	// backtestSpreadMultiplier scales the intra-candle high-low range to
	// approximate a bid-ask spread. The factor 8 was derived empirically from
	// typical crypto market microstructure where the observable range is
	// roughly 8x the effective spread on liquid pairs.
	// Used as the default when SpreadMultiplier is not set in config.
	backtestSpreadMultiplier = DefaultScalpingBacktestSpreadMultiplier
)

func defaultScalpingBacktestUniverse() []string {
	return []string{"BTC/USDT", "ETH/USDT", "SOL/USDT", "BNB/USDT", "XRP/USDT"}
}

type ScalpingCyclePolicy = appautonomy.ScalpingCyclePolicy

type MarketSignal = aiMarketSignal

type ScalpingBacktestConfig struct {
	StartTime          time.Time
	EndTime            time.Time
	Symbols            []string
	Exchange           string
	InitialCapital     decimal.Decimal
	FeeRate            decimal.Decimal
	SlippagePct        decimal.Decimal
	MaxBidAskSpreadPct float64
	MinConfidence      float64
	MinExpectancyN     int
	MinExpectancyEdge  float64
	MaxCapitalPct      float64
	DefaultHoldPeriod  time.Duration
	SpreadMultiplier   float64
}

type ScalpingBacktestResult struct {
	RunID       string
	Config      ScalpingBacktestConfig
	StartTime   time.Time
	EndTime     time.Time
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
	Signal           MarketSignal
	Regime           string
	RegimeVolatility string
	FunnelStage      string
	RejectionReason  string
	GateResults      map[string]bool
}

type ScalpingBacktestTrade struct {
	Symbol        string
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
}

type SimulatedPosition struct {
	Symbol      string
	Side        string
	Size        decimal.Decimal
	Notional    decimal.Decimal
	EntryPrice  decimal.Decimal
	EntryTime   time.Time
	RegimeEntry string
	Signal      MarketSignal
	Decision    *AITradingDecision
}

type SimulatedTrade struct {
	Trade ScalpingBacktestTrade
}

type ScalpingBacktestEngine struct {
	db            DBPool
	config        ScalpingBacktestConfig
	capital       decimal.Decimal
	positions     map[string]*SimulatedPosition
	tradeHistory  []ScalpingBacktestTrade
	signalHistory []ScalpingBacktestSignal
	gateStats     map[string]*GateStats
	policy        ScalpingCyclePolicy
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

	e.capital = e.config.InitialCapital
	e.positions = make(map[string]*SimulatedPosition)
	e.tradeHistory = make([]ScalpingBacktestTrade, 0)
	e.signalHistory = make([]ScalpingBacktestSignal, 0)
	e.gateStats = make(map[string]*GateStats)

	runID := uuid.NewString()
	historicalSignals, err := e.loadHistoricalSignals(ctx, e.config.StartTime, e.config.EndTime)
	if err != nil {
		return nil, err
	}
	if len(historicalSignals) == 0 {
		return nil, fmt.Errorf("no historical signals found in range")
	}

	for _, signal := range historicalSignals {
		evaluation, evalErr := e.evaluateSignal(ctx, signal)
		if evalErr != nil {
			return nil, evalErr
		}

		recorded := ScalpingBacktestSignal{
			Timestamp:        signal.Timestamp,
			Symbol:           signal.Symbol,
			Signal:           signal.Signal,
			Regime:           evaluation.Regime,
			RegimeVolatility: e.classifyRegimeVolatility(signal.Signal),
			FunnelStage:      evaluation.FunnelStage,
			RejectionReason:  evaluation.RejectionReason,
			GateResults:      toGateBoolMap(evaluation.GateResults),
		}
		e.signalHistory = append(e.signalHistory, recorded)

		if !evaluation.Allowed || evaluation.Decision == nil {
			continue
		}

		trade, simErr := e.simulateExecution(ctx, signal, evaluation.Decision)
		if simErr != nil {
			return nil, fmt.Errorf("simulate execution for %s at %s: %w", signal.Symbol, signal.Timestamp.Format(time.RFC3339), simErr)
		}
		e.tradeHistory = append(e.tradeHistory, trade.Trade)
		e.capital = e.capital.Add(trade.Trade.PnL)
	}

	result := &ScalpingBacktestResult{
		RunID:       runID,
		Config:      e.config,
		StartTime:   e.config.StartTime,
		EndTime:     e.config.EndTime,
		Summary:     e.calculateSummary(),
		Signals:     append([]ScalpingBacktestSignal(nil), e.signalHistory...),
		Trades:      append([]ScalpingBacktestTrade(nil), e.tradeHistory...),
		GateSummary: e.buildGateSummary(),
	}

	return result, nil
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
		log.Printf("[BACKTEST] OHLCV signal load failed (falling back to market_data): %v", err)
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
	}
	funnel := appautonomy.BuildCandidateFunnel([]appautonomy.CandidateSignal{candidate}, e.policy)

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
		eval.FunnelStage = "eligible"
	}
	return eval, nil
}

func (e *ScalpingBacktestEngine) simulateExecution(ctx context.Context, signal HistoricalSignal, decision *AITradingDecision) (*SimulatedTrade, error) {
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

	sizePercent := clampFloat(decision.SizePercent, 0, e.config.MaxCapitalPct)
	if sizePercent <= 0 {
		sizePercent = math.Min(e.config.MaxCapitalPct, 1)
	}
	notional := e.capital.Mul(decimal.NewFromFloat(sizePercent / 100))
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
		sl, tp := defaultExitLevels(signal.Signal.Price, decision.Action)
		if stopLoss == nil {
			stopLoss = &sl
		}
		if takeProfit == nil {
			takeProfit = &tp
		}
	}

	var risk, reward decimal.Decimal
	if decision.Action == "buy" {
		risk = entryPrice.Sub(*stopLoss)
		reward = takeProfit.Sub(entryPrice)
	} else {
		risk = stopLoss.Sub(entryPrice)
		reward = entryPrice.Sub(*takeProfit)
	}
	if risk.LessThanOrEqual(decimal.Zero) || reward.LessThanOrEqual(decimal.Zero) {
		return nil, fmt.Errorf("invalid stop loss / take profit shape")
	}

	position := &SimulatedPosition{
		Symbol:      signal.Symbol,
		Side:        decision.Action,
		Size:        quantity,
		Notional:    notional,
		EntryPrice:  entryPrice,
		EntryTime:   signal.Timestamp,
		RegimeEntry: e.classifyRegime(signal.Signal),
		Signal:      signal.Signal,
		Decision:    decision,
	}

	edgeScore := decision.Confidence - 0.50
	edgeScore += math.Abs(signal.Signal.OrderBookImbalance) * 0.50
	if e.config.MaxBidAskSpreadPct > 0 {
		edgeScore -= (signal.Signal.BidAskSpread / e.config.MaxBidAskSpreadPct) * 0.25
	}
	if strings.EqualFold(position.RegimeEntry, "chop") {
		edgeScore -= 0.15
	}
	win := edgeScore >= 0

	exitPrice := *stopLoss
	exitReason := "stop_loss"
	if win {
		exitPrice = *takeProfit
		exitReason = "take_profit"
	}
	if decision.Action == "buy" {
		exitPrice = exitPrice.Mul(one.Sub(slippage))
	} else {
		exitPrice = exitPrice.Mul(one.Add(slippage))
	}

	var grossPnL decimal.Decimal
	if decision.Action == "buy" {
		grossPnL = exitPrice.Sub(entryPrice).Mul(quantity)
	} else {
		grossPnL = entryPrice.Sub(exitPrice).Mul(quantity)
	}

	fees := notional.Mul(e.config.FeeRate).Mul(decimal.NewFromInt(2))
	netPnL := grossPnL.Sub(fees)
	pnlPct := decimal.Zero
	if !notional.IsZero() {
		pnlPct = netPnL.Div(notional).Mul(decimal.NewFromInt(100))
	}

	holdFor := e.config.DefaultHoldPeriod
	if holdFor <= 0 {
		holdFor = DefaultScalpingBacktestHoldPeriod
	}

	trade := ScalpingBacktestTrade{
		Symbol:        signal.Symbol,
		Side:          decision.Action,
		Size:          quantity,
		Notional:      notional,
		EntryPrice:    entryPrice,
		ExitPrice:     exitPrice,
		EntryTime:     signal.Timestamp,
		ExitTime:      signal.Timestamp.Add(holdFor),
		PnL:           netPnL,
		PnLPct:        pnlPct,
		Fees:          fees,
		Outcome:       outcomeFromPnL(netPnL),
		ExitReason:    exitReason,
		RegimeAtEntry: position.RegimeEntry,
		RegimeAtExit:  position.RegimeEntry,
	}

	return &SimulatedTrade{Trade: trade}, nil
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

	spreadAllowed := signal.BidAskSpread >= 0 && signal.BidAskSpread <= e.config.MaxBidAskSpreadPct
	results["spread"] = GateResult{Allowed: spreadAllowed, Reason: gateReason(spreadAllowed, "spread_too_wide")}

	imbalanceAllowed := math.Abs(signal.OrderBookImbalance) >= 0.10
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
			risk = entry.Sub(*stopLoss)
			reward = takeProfit.Sub(entry)
		case "sell":
			risk = stopLoss.Sub(entry)
			reward = entry.Sub(*takeProfit)
		}
		if risk.GreaterThan(decimal.Zero) && reward.GreaterThan(decimal.Zero) {
			ratio := reward.Div(risk)
			riskRewardAllowed = ratio.GreaterThanOrEqual(decimal.NewFromFloat(minRiskRewardRatio))
			if riskRewardAllowed {
				riskRewardReason = ""
			} else {
				riskRewardReason = "insufficient_risk_reward_ratio"
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
		summary.ProfitFactor = decimal.NewFromInt(999)
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
	if config.FeeRate.IsNegative() {
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
	if config.SpreadMultiplier <= 0 {
		config.SpreadMultiplier = backtestSpreadMultiplier
	}
	if len(config.Symbols) == 0 {
		config.Symbols = defaultScalpingBacktestUniverse()
	}
	return config
}

func (e *ScalpingBacktestEngine) buildDecisionFromSignal(ctx context.Context, signal MarketSignal) *AITradingDecision {
	_ = ctx
	fallback := DefaultDeterministicFallbackConfig().Normalized()
	if signal.Price <= 0 || strings.TrimSpace(signal.Symbol) == "" {
		return nil
	}

	imbalance := math.Abs(signal.OrderBookImbalance)
	if signal.BidAskSpread <= 0 || signal.BidAskSpread > e.config.MaxBidAskSpreadPct {
		return nil
	}
	if imbalance < 0.10 {
		return nil
	}

	action := ""
	rangeAlignment := 0.0
	switch {
	case signal.OrderBookImbalance >= 0.10 && signal.RangePosition24h <= 45:
		action = "buy"
		rangeAlignment = clampFloat((45-signal.RangePosition24h)/45.0, 0, 1)
	case signal.OrderBookImbalance <= -0.10 && signal.RangePosition24h >= 55:
		action = "sell"
		rangeAlignment = clampFloat((signal.RangePosition24h-55)/45.0, 0, 1)
	default:
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
		return nil
	}

	stopLoss, takeProfit := defaultExitLevels(signal.Price, action)
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

	query := `
		SELECT tp.symbol, COALESCE(ce.ccxt_id, e.ccxt_id, e.name), md.price,
			COALESCE(md.bid, 0), COALESCE(md.ask, 0),
			COALESCE(md.high_24h, 0), COALESCE(md.low_24h, 0), COALESCE(md.volume_24h, 0),
			md.timestamp
		FROM market_data md
		JOIN trading_pairs tp ON tp.id = md.trading_pair_id
		JOIN exchanges e ON e.id = md.exchange_id
		LEFT JOIN ccxt_exchanges ce ON ce.exchange_id = e.id
		WHERE md.timestamp >= $1 AND md.timestamp <= $2
	`
	args := []any{startTime, endTime}
	query, args = appendScalpingBacktestFilters(query, args, "md.trading_pair_id", tradingPairIDs, strings.TrimSpace(strings.ToLower(e.config.Exchange)))
	query += " ORDER BY md.timestamp ASC"

	rows, err := e.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("load market_data fallback signals: %w", err)
	}
	defer rows.Close()

	signals := make([]HistoricalSignal, 0)
	lastPriceBySymbol := map[string]float64{}
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
		norm := normalizeSymbolForComparison(symbol)
		if last, ok := lastPriceBySymbol[norm]; ok && price > 0 {
			imbalance = clampFloat((price-last)/price, -1, 1)
		}
		lastPriceBySymbol[norm] = price

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

func buildHistoricalSignalsFromOHLCV(points []scalpingOHLCVPoint, spreadMultiplier float64) []HistoricalSignal {
	if len(points) == 0 {
		return nil
	}

	bySymbol := make(map[string][]scalpingOHLCVPoint)
	for _, point := range points {
		norm := normalizeSymbolForComparison(point.symbol)
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

		for i, point := range series {
			priceChange24h := 0.0
			if i > 0 && series[i-1].close > 0 {
				priceChange24h = ((point.close - series[i-1].close) / series[i-1].close) * 100
			}

			signals = append(signals, mapPointToHistoricalSignal(point, windowMetrics[i], priceChange24h, multiplier))
		}
	}

	sort.Slice(signals, func(i, j int) bool {
		if signals[i].Timestamp.Equal(signals[j].Timestamp) {
			return signals[i].Symbol < signals[j].Symbol
		}
		return signals[i].Timestamp.Before(signals[j].Timestamp)
	})

	return signals
}

type scalping24hWindowMetrics struct {
	High24h   float64
	Low24h    float64
	Volume24h float64
}

func compute24hWindowMetrics(series []scalpingOHLCVPoint) []scalping24hWindowMetrics {
	if len(series) == 0 {
		return nil
	}

	metrics := make([]scalping24hWindowMetrics, len(series))
	for i, point := range series {
		windowStart := point.timestamp.Add(-24 * time.Hour)
		metrics[i] = scalping24hWindowMetrics{
			High24h: point.high,
			Low24h:  point.low,
		}
		for j := i; j >= 0; j-- {
			if series[j].timestamp.Before(windowStart) {
				break
			}
			if series[j].high > metrics[i].High24h {
				metrics[i].High24h = series[j].high
			}
			if series[j].low < metrics[i].Low24h {
				metrics[i].Low24h = series[j].low
			}
			metrics[i].Volume24h += math.Max(series[j].volume, 0)
		}
	}

	return metrics
}

func mapPointToHistoricalSignal(point scalpingOHLCVPoint, metrics scalping24hWindowMetrics, priceChange24h float64, spreadMultiplier float64) HistoricalSignal {
	imbalance := 0.0
	if point.high > point.low {
		imbalance = clampFloat((point.close-point.open)/(point.high-point.low), -1, 1)
	}

	rangePos := 50.0
	if metrics.High24h > metrics.Low24h {
		rangePos = clampFloat(((point.close-metrics.Low24h)/(metrics.High24h-metrics.Low24h))*100, 0, 100)
	}

	return HistoricalSignal{
		Timestamp: point.timestamp,
		Symbol:    point.symbol,
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

	rows, err := e.db.Query(ctx, `SELECT id, symbol FROM trading_pairs WHERE is_active = true`)
	if err != nil {
		return nil, fmt.Errorf("query trading pairs: %w", err)
	}
	defer rows.Close()

	ids := make([]int, 0, len(symbolFilter))
	for rows.Next() {
		var id int
		var symbol string
		if scanErr := rows.Scan(&id, &symbol); scanErr != nil {
			return nil, fmt.Errorf("scan trading pair row: %w", scanErr)
		}
		if _, ok := symbolFilter[normalizeSymbolForComparison(symbol)]; ok {
			ids = append(ids, id)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate trading pair rows: %w", err)
	}

	return ids, nil
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
	return 85
}

func (e *ScalpingBacktestEngine) configRegimeLowBand() float64 {
	return 15
}
