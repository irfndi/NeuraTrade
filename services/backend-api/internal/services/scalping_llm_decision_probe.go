package services

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/irfndi/neuratrade/internal/ai/llm"
	appautonomy "github.com/irfndi/neuratrade/internal/app/autonomy"
	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/shopspring/decimal"
)

type ScalpingLLMDecisionProbeOptions struct {
	Exchange       string
	Model          string
	Portfolio      TradingPortfolio
	RequireValid   bool
	RequireHealthy bool
	SignalHistory  []ScalpingLLMSignalSnapshot
}

type ScalpingLLMDecisionProbeResult struct {
	Exchange                string                      `json:"exchange"`
	ObservedAt              time.Time                   `json:"observed_at"`
	SignalCount             int                         `json:"signal_count"`
	SignalQualityCount      int                         `json:"signal_quality_count"`
	SignalQualityCoverage   decimal.Decimal             `json:"signal_quality_coverage"`
	SignalSnapshots         []ScalpingLLMSignalSnapshot `json:"signal_snapshots,omitempty"`
	Provider                string                      `json:"provider,omitempty"`
	Model                   string                      `json:"model,omitempty"`
	Decision                *AITradingDecision          `json:"decision,omitempty"`
	ContractValid           bool                        `json:"contract_valid"`
	ContractError           string                      `json:"contract_error,omitempty"`
	PreTradeGateAllowed     bool                        `json:"pre_trade_gate_allowed"`
	PreTradeGateReason      string                      `json:"pre_trade_gate_reason,omitempty"`
	PreTradeRegime          string                      `json:"pre_trade_regime,omitempty"`
	RuntimeDiagnostics      map[string]interface{}      `json:"runtime_diagnostics,omitempty"`
	LLMDegraded             bool                        `json:"llm_degraded"`
	ReasoningDiagnostics    []string                    `json:"reasoning_diagnostics,omitempty"`
	RawReasoningDiagnostics []string                    `json:"raw_reasoning_diagnostics,omitempty"`
	PaperTrade              *ScalpingLLMProbeTrade      `json:"paper_trade,omitempty"`
	PaperTradeError         string                      `json:"paper_trade_error,omitempty"`
	ObservedPaperTrade      *ScalpingLLMProbeTrade      `json:"observed_paper_trade,omitempty"`
}

type ScalpingLLMSignalSnapshot struct {
	Symbol             string          `json:"symbol"`
	Price              decimal.Decimal `json:"price"`
	High24h            float64         `json:"high_24h"`
	Low24h             float64         `json:"low_24h"`
	Volume24h          float64         `json:"volume_24h"`
	BidAskSpread       float64         `json:"spread_pct"`
	OrderBookImbalance float64         `json:"ob_imbalance"`
	PriceChange24h     float64         `json:"price_change_24h_pct"`
	RecentPriceChange  float64         `json:"recent_price_change_pct"`
	RecentChangeAgeSec float64         `json:"recent_change_age_sec"`
	RecentChangeKnown  bool            `json:"recent_change_known"`
	RangePosition24h   float64         `json:"range_pos_24h"`
	SuggestedAction    string          `json:"suggested_action,omitempty"`
	ConfidenceHint     float64         `json:"confidence_hint,omitempty"`
	CandidateScore     float64         `json:"candidate_score,omitempty"`
	ObservedAt         time.Time       `json:"observed_at"`
}

type ScalpingLLMProbeTrade struct {
	Symbol       string          `json:"symbol"`
	Side         string          `json:"side"`
	Notional     decimal.Decimal `json:"notional"`
	EntryPrice   decimal.Decimal `json:"entry_price"`
	ExitPrice    decimal.Decimal `json:"exit_price"`
	GrossPnL     decimal.Decimal `json:"gross_pnl"`
	Fees         decimal.Decimal `json:"fees"`
	NetPnL       decimal.Decimal `json:"net_pnl"`
	PnLPct       decimal.Decimal `json:"pnl_pct"`
	Outcome      string          `json:"outcome"`
	ExitReason   string          `json:"exit_reason"`
	ExitObserved bool            `json:"exit_observed"`
}

func RunPublicScalpingLLMDecisionProbe(
	ctx context.Context,
	llmClient llm.Client,
	options ScalpingLLMDecisionProbeOptions,
) (*ScalpingLLMDecisionProbeResult, error) {
	if llmClient == nil {
		return nil, fmt.Errorf("scalping LLM decision probe requires llm client")
	}
	exchange := strings.TrimSpace(options.Exchange)
	if exchange == "" {
		exchange = defaultScalpingLivePaperSoakExchange
	}

	ccxtSvc := ccxt.NewNativeCCXTService(15*time.Second, 1)
	if err := ccxtSvc.Initialize(ctx); err != nil {
		return nil, fmt.Errorf("initialize native ccxt service: %w", err)
	}
	defer func() {
		_ = ccxtSvc.Close()
	}()

	cfg := DefaultAIScalpingConfig()
	cfg.Exchange = exchange
	cfg.Model = strings.TrimSpace(options.Model)
	cfg.MaxPairsToAnalyze = 8
	cfg.MaxCandidatePairs = 24
	cfg.OrderBookPairs = 8
	cfg.AutoExpandOrderBooks = true
	cfg.AutoExecute = false
	cfg.EnforceFutures = false
	cfg.Timeout = clampProbeTimeout(cfg.Timeout)

	svc := NewAIScalpingService(cfg, llmClient, nil, ccxtSvc, nil, nil)
	svc.seedScalpingSignalObservationHistory(options.SignalHistory)
	return runScalpingLLMDecisionProbeWithService(ctx, svc, options)
}

func (s *AIScalpingService) seedScalpingSignalObservationHistory(history []ScalpingLLMSignalSnapshot) {
	if s == nil || len(history) == 0 {
		return
	}
	seeded := make(map[string][]scalpingSignalObservation)
	for _, snapshot := range history {
		symbol := normalizeSymbolForComparison(snapshot.Symbol)
		if symbol == "" || snapshot.ObservedAt.IsZero() || !snapshot.Price.GreaterThan(decimal.Zero) {
			continue
		}
		price := snapshot.Price.InexactFloat64()
		if isNonFiniteFloat(price) || price <= 0 {
			continue
		}
		seeded[symbol] = append(seeded[symbol], scalpingSignalObservation{
			At:    snapshot.ObservedAt.UTC(),
			Price: price,
		})
	}
	if len(seeded) == 0 {
		return
	}
	for symbol := range seeded {
		sort.Slice(seeded[symbol], func(i, j int) bool {
			return seeded[symbol][i].At.Before(seeded[symbol][j].At)
		})
	}

	s.signalObservationMu.Lock()
	defer s.signalObservationMu.Unlock()
	if s.signalObservations == nil {
		s.signalObservations = make(map[string][]scalpingSignalObservation)
	}
	for symbol, observations := range seeded {
		s.signalObservations[symbol] = append(s.signalObservations[symbol], observations...)
	}
}

func runScalpingLLMDecisionProbeWithService(
	ctx context.Context,
	svc *AIScalpingService,
	options ScalpingLLMDecisionProbeOptions,
) (*ScalpingLLMDecisionProbeResult, error) {
	if svc == nil {
		return nil, fmt.Errorf("scalping LLM decision probe requires scalping service")
	}

	portfolio := options.Portfolio
	if !walletBasis(portfolio).GreaterThan(decimal.Zero) {
		portfolio = defaultScalpingLLMProbePortfolio(svc.config)
	} else {
		portfolio = applyScalpingLLMProbePortfolioDefaults(portfolio, svc.config)
	}

	signals, err := svc.gatherMarketSignals(ctx)
	if err != nil {
		return nil, fmt.Errorf("gather live scalping market signals: %w", err)
	}
	if len(signals) == 0 {
		return nil, fmt.Errorf("scalping LLM decision probe gathered no market signals")
	}
	observedAt := time.Now().UTC()
	decisionSignals := svc.signalsWithDecisionHints(ctx, signals, portfolio)

	decision, err := svc.getAIDecision(ctx, decisionSignals, portfolio)
	if err != nil {
		return nil, fmt.Errorf("get scalping LLM decision: %w", err)
	}
	normalizeProbeDecision(decision)
	annotateDecisionSignalTelemetry(decision, decisionSignals)
	rawReasoningDiagnostics := scalpingProbeReasoningDiagnostics(decision, decisionSignals, svc.config.MaxBidAskSpreadPct)

	result := &ScalpingLLMDecisionProbeResult{
		Exchange:                svc.config.Exchange,
		ObservedAt:              observedAt,
		SignalCount:             len(decisionSignals),
		SignalQualityCount:      countSignalsWithOrderBookQuality(decisionSignals),
		SignalSnapshots:         scalpingLLMSignalSnapshots(decisionSignals, observedAt),
		Decision:                decision,
		ContractValid:           true,
		PreTradeGateAllowed:     true,
		RuntimeDiagnostics:      svc.RuntimeDiagnostics(),
		Model:                   strings.TrimSpace(svc.config.Model),
		SignalQualityCoverage:   decimal.NewFromInt(int64(countSignalsWithOrderBookQuality(decisionSignals))).Div(decimal.NewFromInt(int64(len(decisionSignals)))),
		RawReasoningDiagnostics: rawReasoningDiagnostics,
	}

	if provider, ok := stringFromRuntimeDiagnostic(result.RuntimeDiagnostics, "last_successful_provider"); ok {
		result.Provider = provider
	}
	if result.Provider == "" {
		if provider, ok := stringFromRuntimeDiagnostic(result.RuntimeDiagnostics, "last_provider"); ok {
			result.Provider = provider
		}
	}
	result.LLMDegraded = scalpingProbeRuntimeDegraded(result.RuntimeDiagnostics)

	if validationErr := svc.validateDecision(ctx, decision, decisionSignals); validationErr != nil {
		if isDecisionContractValidationError(decision, validationErr) {
			result.ContractValid = false
			result.ContractError = validationErr.Error()
		} else {
			result.PreTradeGateAllowed = false
			result.PreTradeGateReason = validationErr.Error()
		}
	}
	result.ReasoningDiagnostics = scalpingProbeReasoningDiagnostics(decision, decisionSignals, svc.config.MaxBidAskSpreadPct)
	if result.ContractValid && result.PreTradeGateAllowed {
		gate := svc.evaluatePreTradeGate(ctx, decision, decisionSignals)
		result.PreTradeGateAllowed = gate.Allowed
		result.PreTradeGateReason = gate.Reason
		result.PreTradeRegime = gate.Regime
		if gate.Allowed && isActionableScalpingProbeDecision(decision) {
			trade, tradeErr := simulateScalpingLLMProbePaperTrade(ctx, svc, decision, decisionSignals, portfolio)
			if tradeErr != nil {
				result.PaperTradeError = tradeErr.Error()
			} else {
				result.PaperTrade = trade
			}
		}
	}

	if options.RequireHealthy && result.LLMDegraded {
		return result, fmt.Errorf("scalping LLM decision probe degraded: %s", runtimeDiagnosticString(result.RuntimeDiagnostics, "last_error"))
	}
	if options.RequireValid && !result.ContractValid {
		return result, fmt.Errorf("scalping LLM decision contract invalid: %s", result.ContractError)
	}
	return result, nil
}

func scalpingLLMSignalSnapshots(signals []aiMarketSignal, observedAt time.Time) []ScalpingLLMSignalSnapshot {
	snapshots := make([]ScalpingLLMSignalSnapshot, 0, len(signals))
	for _, signal := range signals {
		if strings.TrimSpace(signal.Symbol) == "" || signal.Price <= 0 {
			continue
		}
		snapshots = append(snapshots, ScalpingLLMSignalSnapshot{
			Symbol:             normalizeSymbolForComparison(signal.Symbol),
			Price:              decimal.NewFromFloat(signal.Price),
			High24h:            signal.High24h,
			Low24h:             signal.Low24h,
			Volume24h:          signal.Volume24h,
			BidAskSpread:       signal.BidAskSpread,
			OrderBookImbalance: signal.OrderBookImbalance,
			PriceChange24h:     signal.PriceChange24h,
			RecentPriceChange:  signal.RecentPriceChange,
			RecentChangeAgeSec: signal.RecentChangeAgeSec,
			RecentChangeKnown:  signal.RecentChangeKnown,
			RangePosition24h:   signal.RangePosition24h,
			SuggestedAction:    signal.SuggestedAction,
			ConfidenceHint:     signal.ConfidenceHint,
			CandidateScore:     signal.CandidateScore,
			ObservedAt:         observedAt,
		})
	}
	return snapshots
}

func isActionableScalpingProbeDecision(decision *AITradingDecision) bool {
	if decision == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(decision.Action)) {
	case "buy", "sell":
		return true
	default:
		return false
	}
}

func scalpingProbeReasoningDiagnostics(decision *AITradingDecision, signals []aiMarketSignal, maxSpreadPct float64) []string {
	if decision == nil {
		return nil
	}
	diagnostics := scalpingPctUnitReasoningDiagnostics(decision.Reasoning, signals)
	diagnostics = append(diagnostics, scalpingHintReasoningDiagnostics(decision.Reasoning, signals)...)
	if strings.EqualFold(strings.TrimSpace(decision.Action), "hold") {
		diagnostics = append(diagnostics, scalpingHoldSpreadReasoningDiagnostics(decision.Reasoning, signals, maxSpreadPct)...)
	}
	return diagnostics
}

func normalizeContradictoryHoldSpreadReasoning(decision *AITradingDecision, signals []aiMarketSignal, maxSpreadPct float64) {
	if decision == nil || !strings.EqualFold(strings.TrimSpace(decision.Action), "hold") {
		return
	}
	if len(scalpingHoldSpreadReasoningDiagnostics(decision.Reasoning, signals, maxSpreadPct)) == 0 {
		return
	}
	decision.Reasoning = "Holding because no analyzed setup cleared the effective confidence and risk gates; liquidity was not used as a blanket rejection reason."
}

func normalizeDiagnosticHoldReasoning(decision *AITradingDecision, signals []aiMarketSignal) {
	if decision == nil || !strings.EqualFold(strings.TrimSpace(decision.Action), "hold") {
		return
	}
	if len(scalpingPctUnitReasoningDiagnostics(decision.Reasoning, signals)) == 0 &&
		len(scalpingHintReasoningDiagnostics(decision.Reasoning, signals)) == 0 {
		return
	}
	decision.Reasoning = "Holding because no analyzed setup cleared the effective confidence and risk gates."
}

func scalpingHoldSpreadReasoningDiagnostics(reason string, signals []aiMarketSignal, maxSpreadPct float64) []string {
	reasoning := strings.ToLower(strings.TrimSpace(reason))
	if reasoning == "" || !strings.Contains(reasoning, "spread") {
		return nil
	}
	if !holdReasoningClaimsWideSpread(reasoning) {
		return nil
	}
	threshold := maxSpreadPct
	thresholdName := "liquidity"
	if holdReasoningScopesSpreadToBuySafetyGate(reasoning) {
		threshold = scalpingRecentBuyMaxSpreadPct
		thresholdName = "buy safety"
	}
	if threshold <= 0 {
		threshold = appautonomy.DefaultScalpingMaxBidAskSpreadPct
	}
	diagnostics := make([]string, 0, 1)
	for _, signal := range signals {
		if signal.BidAskSpread <= 0 || signal.BidAskSpread > threshold {
			continue
		}
		if holdReasoningClaimsWideSpreadForSignal(reasoning, signal) {
			diagnostics = append(diagnostics, fmt.Sprintf("hold reasoning cites wide spread while %s spread %.3f%% is within %.3f%% %s threshold", signal.Symbol, signal.BidAskSpread, threshold, thresholdName))
		}
	}
	return diagnostics
}

func scalpingPctUnitReasoningDiagnostics(reason string, signals []aiMarketSignal) []string {
	reasoning := strings.ToLower(strings.TrimSpace(reason))
	if reasoning == "" {
		return nil
	}
	diagnostics := make([]string, 0, 1)
	for _, signal := range signals {
		if !reasoningMentionsSignal(reasoning, signal) {
			continue
		}
		if signal.RecentChangeKnown {
			if citation, ok := pctUnitInflationCitation(reasoning, signal.RecentPriceChange); ok {
				diagnostics = append(diagnostics, fmt.Sprintf("reasoning appears to scale recent_price_change_pct for %s by 100 (cited %s while signal is %.5f%%)", signal.Symbol, citation, signal.RecentPriceChange))
			}
		}
		if citation, ok := pctUnitInflationCitation(reasoning, signal.PriceChange24h); ok {
			diagnostics = append(diagnostics, fmt.Sprintf("reasoning appears to scale price_change_24h_pct for %s by 100 (cited %s while signal is %.5f%%)", signal.Symbol, citation, signal.PriceChange24h))
		}
	}
	return diagnostics
}

func scalpingHintReasoningDiagnostics(reason string, signals []aiMarketSignal) []string {
	reasoning := strings.ToLower(strings.TrimSpace(reason))
	if reasoning == "" || !reasoningClaimsHintPresent(reasoning) {
		return nil
	}
	diagnostics := make([]string, 0, 1)
	for _, signal := range signals {
		if !reasoningMentionsSignal(reasoning, signal) {
			continue
		}
		if reasoningClaimsHintFieldPresent(reasoning, "confidence_hint", "confidence hint") && signal.ConfidenceHint <= 0 {
			diagnostics = append(diagnostics, fmt.Sprintf("reasoning cites absent confidence_hint for %s", signal.Symbol))
		}
		if reasoningClaimsHintFieldPresent(reasoning, "candidate_score", "candidate score") && signal.CandidateScore <= 0 {
			diagnostics = append(diagnostics, fmt.Sprintf("reasoning cites absent candidate_score for %s", signal.Symbol))
		}
		if reasoningClaimsHintFieldPresent(reasoning, "suggested_action", "suggested action") && strings.TrimSpace(signal.SuggestedAction) == "" {
			diagnostics = append(diagnostics, fmt.Sprintf("reasoning cites absent suggested_action for %s", signal.Symbol))
		}
	}
	return diagnostics
}

func reasoningClaimsHintPresent(reasoning string) bool {
	return reasoningClaimsHintFieldPresent(reasoning, "confidence_hint", "confidence hint") ||
		reasoningClaimsHintFieldPresent(reasoning, "candidate_score", "candidate score") ||
		reasoningClaimsHintFieldPresent(reasoning, "suggested_action", "suggested action")
}

func reasoningClaimsHintFieldPresent(reasoning string, variants ...string) bool {
	reasoning = strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(reasoning))), " ")
	if reasoning == "" {
		return false
	}
	for _, variant := range variants {
		variant = strings.ToLower(strings.TrimSpace(variant))
		if variant == "" || !strings.Contains(reasoning, variant) {
			continue
		}
		negated := []string{
			"no " + variant,
			"without " + variant,
			variant + " absent",
			variant + " is absent",
			variant + " missing",
			variant + " is missing",
			variant + " not provided",
			variant + " is not provided",
			variant + " not present",
			variant + " is not present",
		}
		negatedMatch := false
		for _, phrase := range negated {
			if strings.Contains(reasoning, phrase) {
				negatedMatch = true
				break
			}
		}
		if !negatedMatch {
			return true
		}
	}
	return false
}

func pctUnitInflationCitation(reasoning string, value float64) (string, bool) {
	value = math.Abs(value)
	if value < 0.01 || value >= 1 {
		return "", false
	}
	inflated := value * 100
	formats := []string{
		fmt.Sprintf("%.5f%%", inflated),
		fmt.Sprintf("%.4f%%", inflated),
		fmt.Sprintf("%.3f%%", inflated),
		fmt.Sprintf("%.2f%%", inflated),
		fmt.Sprintf("%.1f%%", inflated),
	}
	for _, candidate := range formats {
		candidate = strings.TrimSuffix(candidate, "%")
		candidate = strings.TrimRight(strings.TrimRight(candidate, "0"), ".") + "%"
		if containsStandalonePctCitation(reasoning, candidate) {
			return candidate, true
		}
	}
	return "", false
}

func containsStandalonePctCitation(reasoning, candidate string) bool {
	if reasoning == "" || candidate == "" {
		return false
	}
	searchFrom := 0
	for {
		idx := strings.Index(reasoning[searchFrom:], candidate)
		if idx < 0 {
			return false
		}
		start := searchFrom + idx
		end := start + len(candidate)
		if hasPctCitationBoundary(reasoning, start, end) {
			return true
		}
		searchFrom = end
	}
}

func hasPctCitationBoundary(reasoning string, start, end int) bool {
	if start > 0 {
		prev := rune(reasoning[start-1])
		if unicode.IsDigit(prev) || prev == '.' {
			return false
		}
	}
	if end < len(reasoning) {
		next := rune(reasoning[end])
		if unicode.IsDigit(next) || next == '.' {
			return false
		}
	}
	return true
}

func reasoningMentionsSignal(reasoning string, signal aiMarketSignal) bool {
	symbol := strings.ToLower(strings.TrimSpace(signal.Symbol))
	base := strings.ToLower(strings.TrimSpace(strings.Split(symbol, "/")[0]))
	return (symbol != "" && strings.Contains(reasoning, symbol)) ||
		(base != "" && containsScalpingSymbolToken(reasoning, base)) ||
		strings.Contains(reasoning, "all signals") ||
		strings.Contains(reasoning, "all symbols")
}

func holdReasoningScopesSpreadToBuySafetyGate(reasoning string) bool {
	reasoning = strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(reasoning))), " ")
	if reasoning == "" {
		return false
	}
	return strings.Contains(reasoning, "buy safety gate") ||
		strings.Contains(reasoning, "buy-safety gate") ||
		strings.Contains(reasoning, "buy gate")
}

func containsScalpingSymbolToken(text, symbol string) bool {
	symbol = strings.ToLower(strings.TrimSpace(symbol))
	if symbol == "" {
		return false
	}
	for start := 0; ; {
		index := strings.Index(text[start:], symbol)
		if index < 0 {
			return false
		}
		index += start
		end := index + len(symbol)
		if scalpingSymbolBoundary(text, index-1) && scalpingSymbolBoundary(text, end) {
			return true
		}
		start = end
	}
}

func scalpingSymbolBoundary(text string, index int) bool {
	if index < 0 || index >= len(text) {
		return true
	}
	return !unicode.IsLetter(rune(text[index])) && !unicode.IsDigit(rune(text[index]))
}

func holdReasoningClaimsWideSpread(reasoning string) bool {
	reasoning = strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(reasoning))), " ")
	if reasoning == "" {
		return false
	}
	phrases := []string{
		"wide spread",
		"spread >",
		"spread>",
		"spread above",
		"spread is above",
		"spread was above",
		"spread remains above",
		"spread greater than",
		"spread is greater than",
		"spread wider than",
		"spread is wider than",
		"spread too wide",
		"spread is too wide",
		"spread exceeds",
		"spread exceeded",
		"spread over",
		"spread beyond",
	}
	for _, phrase := range phrases {
		if strings.Contains(reasoning, phrase) {
			return true
		}
	}
	return false
}

func holdReasoningClaimsWideSpreadForSignal(reasoning string, signal aiMarketSignal) bool {
	for _, clause := range splitScalpingReasoningClauses(reasoning) {
		if !holdReasoningClaimsWideSpread(clause) {
			continue
		}
		if strings.Contains(clause, "all signals") || strings.Contains(clause, "all symbols") {
			return true
		}
		if reasoningMentionsSpecificSignal(clause, signal) {
			return true
		}
	}
	return false
}

func splitScalpingReasoningClauses(reasoning string) []string {
	return strings.FieldsFunc(reasoning, func(r rune) bool {
		switch r {
		case '.', ';', ',', '\n':
			return true
		default:
			return false
		}
	})
}

func reasoningMentionsSpecificSignal(reasoning string, signal aiMarketSignal) bool {
	symbol := strings.ToLower(strings.TrimSpace(signal.Symbol))
	base := strings.ToLower(strings.TrimSpace(strings.Split(symbol, "/")[0]))
	return (symbol != "" && strings.Contains(reasoning, symbol)) ||
		(base != "" && containsScalpingSymbolToken(reasoning, base))
}

func simulateScalpingLLMProbePaperTrade(
	ctx context.Context,
	svc *AIScalpingService,
	decision *AITradingDecision,
	signals []aiMarketSignal,
	portfolio TradingPortfolio,
) (*ScalpingLLMProbeTrade, error) {
	if svc == nil {
		return nil, fmt.Errorf("scalping LLM paper trade simulation requires scalping service")
	}
	if !isActionableScalpingProbeDecision(decision) {
		return nil, fmt.Errorf("scalping LLM paper trade simulation requires buy/sell decision")
	}
	signal, ok := scalpingProbeSignalForDecision(decision, signals)
	if !ok {
		return nil, fmt.Errorf("decision symbol %q not found in probe signals", decision.Symbol)
	}
	capital := walletBasis(portfolio)
	if !capital.GreaterThan(decimal.Zero) {
		capital = decimal.NewFromFloat(48)
	}
	now := time.Now().UTC()
	engine := NewScalpingBacktestEngine(nil, ScalpingBacktestConfig{
		StartTime:          now.Add(-time.Second),
		EndTime:            now.Add(time.Second),
		Symbols:            []string{signal.Symbol},
		Exchange:           strings.TrimSpace(svc.config.Exchange),
		InitialCapital:     capital,
		FeeRate:            decimal.NewFromFloat(0.0006),
		SlippagePct:        decimal.NewFromFloat(DefaultScalpingBacktestSlippage),
		MaxBidAskSpreadPct: svc.config.MaxBidAskSpreadPct,
		MinConfidence:      svc.config.MinConfidence,
		MinExpectancyN:     svc.config.MinExpectancyN,
		MinExpectancyEdge:  svc.config.MinExpectancyEdge,
		MaxCapitalPct:      svc.config.MaxCapitalPct,
		DefaultHoldPeriod:  DefaultScalpingBacktestHoldPeriod,
	})
	if strings.TrimSpace(engine.config.Exchange) == "" {
		engine.config.Exchange = defaultScalpingLivePaperSoakExchange
	}
	position, err := engine.openSimulatedPosition(ctx, HistoricalSignal{
		Timestamp: now,
		Symbol:    signal.Symbol,
		Exchange:  engine.config.Exchange,
		Signal:    signal,
	}, decision)
	if err != nil {
		return nil, fmt.Errorf("open scalping LLM paper trade: %w", err)
	}
	return &ScalpingLLMProbeTrade{
		Symbol:       position.Symbol,
		Side:         position.Side,
		Notional:     position.Notional,
		EntryPrice:   position.EntryPrice,
		Outcome:      "open",
		ExitReason:   "exit_unobserved",
		ExitObserved: false,
	}, nil
}

func scalpingProbeSignalForDecision(decision *AITradingDecision, signals []aiMarketSignal) (aiMarketSignal, bool) {
	if decision == nil {
		return aiMarketSignal{}, false
	}
	target := normalizeSymbolForComparison(decision.Symbol)
	if target == "" {
		return aiMarketSignal{}, false
	}
	for _, signal := range signals {
		if normalizeSymbolForComparison(signal.Symbol) == target {
			return signal, true
		}
	}
	return aiMarketSignal{}, false
}

func defaultScalpingLLMProbePortfolio(cfg AIScalpingConfig) TradingPortfolio {
	capital := decimal.NewFromFloat(48)
	return applyScalpingLLMProbePortfolioDefaults(TradingPortfolio{
		USDTBalance:        capital.InexactFloat64(),
		USDTBalanceDecimal: capital,
		TotalValue:         capital.InexactFloat64(),
		TotalValueDecimal:  capital,
		StrategyPhase:      "probe",
		RecoveryEntryOK:    true,
	}, cfg)
}

func applyScalpingLLMProbePortfolioDefaults(portfolio TradingPortfolio, cfg AIScalpingConfig) TradingPortfolio {
	if strings.TrimSpace(portfolio.StrategyPhase) == "" {
		portfolio.StrategyPhase = "probe"
	}
	if strings.TrimSpace(portfolio.AccountTier) == "" {
		portfolio.AccountTier = appautonomy.AccountTierMicro
	}
	if portfolio.PhaseMinConfidence <= 0 {
		portfolio.PhaseMinConfidence = cfg.MinConfidence
	}
	if portfolio.EffectiveMinConfidence <= 0 {
		portfolio.EffectiveMinConfidence = cfg.MinConfidence
	}
	if portfolio.PhaseMaxCapitalPct <= 0 {
		portfolio.PhaseMaxCapitalPct = cfg.MaxCapitalPct
	}
	if portfolio.EffectiveMaxCapitalPct <= 0 {
		portfolio.EffectiveMaxCapitalPct = cfg.MaxCapitalPct
	}
	portfolio.RecoveryEntryOK = true
	return portfolio
}

func clampProbeTimeout(timeout time.Duration) time.Duration {
	if timeout <= 0 {
		return 30 * time.Second
	}
	if timeout > 2*time.Minute {
		return 2 * time.Minute
	}
	return timeout
}

func normalizeProbeDecision(decision *AITradingDecision) {
	if decision == nil {
		return
	}
	decision.Action = strings.ToLower(strings.TrimSpace(decision.Action))
	decision.Symbol = normalizeSymbolForComparison(decision.Symbol)
}

func countSignalsWithOrderBookQuality(signals []aiMarketSignal) int {
	count := 0
	for _, signal := range signals {
		if signal.BidAskSpread > 0 {
			count++
		}
	}
	return count
}

func scalpingProbeRuntimeDegraded(runtime map[string]interface{}) bool {
	if strings.TrimSpace(runtimeDiagnosticString(runtime, "last_error")) != "" {
		return true
	}
	return strings.TrimSpace(runtimeDiagnosticString(runtime, "last_success_at")) == ""
}

func runtimeDiagnosticString(runtime map[string]interface{}, key string) string {
	value, _ := stringFromRuntimeDiagnostic(runtime, key)
	return value
}

func stringFromRuntimeDiagnostic(runtime map[string]interface{}, key string) (string, bool) {
	if runtime == nil {
		return "", false
	}
	value, ok := runtime[key]
	if !ok || value == nil {
		return "", false
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	return text, text != ""
}
