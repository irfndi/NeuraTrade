package services

import (
	"context"
	"fmt"
	"strings"
	"time"

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
}

type ScalpingLLMDecisionProbeResult struct {
	Exchange              string                 `json:"exchange"`
	SignalCount           int                    `json:"signal_count"`
	SignalQualityCount    int                    `json:"signal_quality_count"`
	SignalQualityCoverage decimal.Decimal        `json:"signal_quality_coverage"`
	Provider              string                 `json:"provider,omitempty"`
	Model                 string                 `json:"model,omitempty"`
	Decision              *AITradingDecision     `json:"decision,omitempty"`
	ContractValid         bool                   `json:"contract_valid"`
	ContractError         string                 `json:"contract_error,omitempty"`
	PreTradeGateAllowed   bool                   `json:"pre_trade_gate_allowed"`
	PreTradeGateReason    string                 `json:"pre_trade_gate_reason,omitempty"`
	PreTradeRegime        string                 `json:"pre_trade_regime,omitempty"`
	RuntimeDiagnostics    map[string]interface{} `json:"runtime_diagnostics,omitempty"`
	LLMDegraded           bool                   `json:"llm_degraded"`
	ReasoningDiagnostics  []string               `json:"reasoning_diagnostics,omitempty"`
	PaperTrade            *ScalpingLLMProbeTrade `json:"paper_trade,omitempty"`
	PaperTradeError       string                 `json:"paper_trade_error,omitempty"`
}

type ScalpingLLMProbeTrade struct {
	Symbol     string          `json:"symbol"`
	Side       string          `json:"side"`
	Notional   decimal.Decimal `json:"notional"`
	EntryPrice decimal.Decimal `json:"entry_price"`
	ExitPrice  decimal.Decimal `json:"exit_price"`
	GrossPnL   decimal.Decimal `json:"gross_pnl"`
	Fees       decimal.Decimal `json:"fees"`
	NetPnL     decimal.Decimal `json:"net_pnl"`
	PnLPct     decimal.Decimal `json:"pnl_pct"`
	Outcome    string          `json:"outcome"`
	ExitReason string          `json:"exit_reason"`
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
	return runScalpingLLMDecisionProbeWithService(ctx, svc, options)
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

	decision, err := svc.getAIDecision(ctx, signals, portfolio)
	if err != nil {
		return nil, fmt.Errorf("get scalping LLM decision: %w", err)
	}
	normalizeProbeDecision(decision)
	reasoningDiagnostics := scalpingProbeReasoningDiagnostics(decision, signals, svc.config.MaxBidAskSpreadPct)

	result := &ScalpingLLMDecisionProbeResult{
		Exchange:              svc.config.Exchange,
		SignalCount:           len(signals),
		SignalQualityCount:    countSignalsWithOrderBookQuality(signals),
		Decision:              decision,
		ContractValid:         true,
		PreTradeGateAllowed:   true,
		RuntimeDiagnostics:    svc.RuntimeDiagnostics(),
		Model:                 strings.TrimSpace(svc.config.Model),
		SignalQualityCoverage: decimal.NewFromInt(int64(countSignalsWithOrderBookQuality(signals))).Div(decimal.NewFromInt(int64(len(signals)))),
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

	if validationErr := svc.validateDecision(decision, signals); validationErr != nil {
		result.ContractValid = false
		result.ContractError = validationErr.Error()
	}
	result.ReasoningDiagnostics = reasoningDiagnostics
	if result.ContractValid {
		gate := svc.evaluatePreTradeGate(ctx, decision, signals)
		result.PreTradeGateAllowed = gate.Allowed
		result.PreTradeGateReason = gate.Reason
		result.PreTradeRegime = gate.Regime
		if gate.Allowed && isActionableScalpingProbeDecision(decision) {
			trade, tradeErr := simulateScalpingLLMProbePaperTrade(ctx, svc, decision, signals, portfolio)
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
	if decision == nil || !strings.EqualFold(strings.TrimSpace(decision.Action), "hold") {
		return nil
	}
	return scalpingHoldSpreadReasoningDiagnostics(decision.Reasoning, signals, maxSpreadPct)
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

func scalpingHoldSpreadReasoningDiagnostics(reason string, signals []aiMarketSignal, maxSpreadPct float64) []string {
	reasoning := strings.ToLower(strings.TrimSpace(reason))
	if reasoning == "" || !strings.Contains(reasoning, "spread") {
		return nil
	}
	claimsWideSpread := strings.Contains(reasoning, ">") ||
		strings.Contains(reasoning, "above") ||
		strings.Contains(reasoning, "greater than") ||
		strings.Contains(reasoning, "wider than") ||
		strings.Contains(reasoning, "too wide") ||
		strings.Contains(reasoning, "wide spread")
	if !claimsWideSpread {
		return nil
	}
	threshold := maxSpreadPct
	if threshold <= 0 {
		threshold = appautonomy.DefaultScalpingMaxBidAskSpreadPct
	}
	diagnostics := make([]string, 0, 1)
	for _, signal := range signals {
		if signal.BidAskSpread <= 0 || signal.BidAskSpread > threshold {
			continue
		}
		symbol := strings.ToLower(strings.TrimSpace(signal.Symbol))
		base := strings.ToLower(strings.TrimSpace(strings.Split(symbol, "/")[0]))
		if symbol != "" && strings.Contains(reasoning, symbol) || base != "" && strings.Contains(reasoning, base) || strings.Contains(reasoning, "all signals") {
			diagnostics = append(diagnostics, fmt.Sprintf("hold reasoning cites wide spread while %s spread %.3f%% is within %.3f%% threshold", signal.Symbol, signal.BidAskSpread, threshold))
		}
	}
	return diagnostics
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
	trade, err := engine.simulateExecution(ctx, HistoricalSignal{
		Timestamp: now,
		Symbol:    signal.Symbol,
		Exchange:  engine.config.Exchange,
		Signal:    signal,
	}, decision)
	if err != nil {
		return nil, fmt.Errorf("simulate scalping LLM paper trade: %w", err)
	}
	result := trade.Trade
	return &ScalpingLLMProbeTrade{
		Symbol:     result.Symbol,
		Side:       result.Side,
		Notional:   result.Notional,
		EntryPrice: result.EntryPrice,
		ExitPrice:  result.ExitPrice,
		GrossPnL:   paperSoakGrossPnL(result),
		Fees:       result.Fees,
		NetPnL:     result.PnL,
		PnLPct:     result.PnLPct,
		Outcome:    result.Outcome,
		ExitReason: result.ExitReason,
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
