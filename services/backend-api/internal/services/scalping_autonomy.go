package services

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/irfndi/neuratrade/internal/autonomous"
	"github.com/shopspring/decimal"
)

type scalpingAutonomyContextKey struct{}
type scalpingAutonomyEvalInputKey struct{}

// ScalpingAutonomyScope carries per-cycle gate context from quest runtime.
type ScalpingAutonomyScope struct {
	ChatID            string
	StrategyID        string
	Exchange          string
	MarketType        string
	Leverage          int
	SafeModeEnabled   bool
	KillSwitchEngaged bool
	ExchangeConnected bool
	ConnectionChecked bool
}

type scalpingAutonomyEvalInput struct {
	SafeModeEnabled   bool
	KillSwitchEngaged bool
	AvailableBudget   decimal.Decimal
	ExchangeConnected bool
}

func WithScalpingAutonomyScope(ctx context.Context, scope ScalpingAutonomyScope) context.Context {
	return context.WithValue(ctx, scalpingAutonomyContextKey{}, scope)
}

func scalpingAutonomyScopeFromContext(ctx context.Context) (ScalpingAutonomyScope, bool) {
	raw := ctx.Value(scalpingAutonomyContextKey{})
	scope, ok := raw.(ScalpingAutonomyScope)
	if !ok {
		return ScalpingAutonomyScope{}, false
	}
	if strings.TrimSpace(scope.StrategyID) == "" && strings.TrimSpace(scope.ChatID) != "" {
		scope.StrategyID = ScalpingStrategyID(scope.ChatID)
	}
	return scope, true
}

func scalpingChatIDFromContext(ctx context.Context) string {
	scope, ok := scalpingAutonomyScopeFromContext(ctx)
	if !ok {
		return ""
	}
	return strings.TrimSpace(scope.ChatID)
}

func scalpingExchangeFromContext(ctx context.Context) string {
	scope, ok := scalpingAutonomyScopeFromContext(ctx)
	if !ok {
		return ""
	}
	return strings.TrimSpace(scope.Exchange)
}

func scalpingLeverageFromContext(ctx context.Context) int {
	scope, ok := scalpingAutonomyScopeFromContext(ctx)
	if !ok {
		return 0
	}
	if scope.Leverage <= 0 {
		return 0
	}
	return scope.Leverage
}

func scalpingMarketTypeFromContext(ctx context.Context) string {
	scope, ok := scalpingAutonomyScopeFromContext(ctx)
	if !ok {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(scope.MarketType))
}

func withScalpingAutonomyEvalInput(ctx context.Context, input scalpingAutonomyEvalInput) context.Context {
	return context.WithValue(ctx, scalpingAutonomyEvalInputKey{}, input)
}

func scalpingAutonomyEvalInputFromContext(ctx context.Context) (scalpingAutonomyEvalInput, bool) {
	raw := ctx.Value(scalpingAutonomyEvalInputKey{})
	input, ok := raw.(scalpingAutonomyEvalInput)
	if !ok {
		return scalpingAutonomyEvalInput{}, false
	}
	return input, true
}

// ScalpingStrategyID returns the canonical strategy key for per-chat rollout state.
func ScalpingStrategyID(chatID string) string {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return ""
	}
	return fmt.Sprintf("scalping:%s:default", chatID)
}

type scalpingPolicyValidator struct{}

func (v *scalpingPolicyValidator) ValidateProposal(ctx context.Context, proposal *autonomous.StrategyProposal) (bool, string, error) {
	if proposal == nil {
		return false, "proposal_nil", nil
	}
	if strings.TrimSpace(proposal.StrategyID) == "" {
		return false, "strategy_id_required", nil
	}
	if strings.TrimSpace(proposal.Symbol) == "" {
		return false, "symbol_required", nil
	}
	side := strings.ToLower(strings.TrimSpace(proposal.Side))
	if side != "buy" && side != "sell" {
		return false, "side_must_be_buy_or_sell", nil
	}
	if proposal.Confidence <= 0 {
		return false, "confidence_must_be_positive", nil
	}
	if proposal.MaxDrawdown.GreaterThan(decimal.NewFromFloat(25)) {
		return false, "max_drawdown_policy_exceeded", nil
	}
	return true, "", nil
}

func (v *scalpingPolicyValidator) IsSafeModeEnabled(ctx context.Context) (bool, error) {
	input, ok := scalpingAutonomyEvalInputFromContext(ctx)
	if !ok {
		return false, nil
	}
	return input.SafeModeEnabled, nil
}

func (v *scalpingPolicyValidator) IsKillSwitchEngaged(ctx context.Context) (bool, error) {
	input, ok := scalpingAutonomyEvalInputFromContext(ctx)
	if !ok {
		return false, nil
	}
	return input.KillSwitchEngaged, nil
}

type scalpingRiskManager struct{}

func (r *scalpingRiskManager) GetAvailableBudget(ctx context.Context, strategyID string) (decimal.Decimal, error) {
	input, ok := scalpingAutonomyEvalInputFromContext(ctx)
	if !ok {
		return decimal.Zero, fmt.Errorf("autonomy budget context unavailable")
	}
	if input.AvailableBudget.LessThan(decimal.Zero) {
		return decimal.Zero, nil
	}
	return input.AvailableBudget, nil
}

func (r *scalpingRiskManager) CheckRiskLimits(ctx context.Context, proposal *autonomous.StrategyProposal) (bool, string, error) {
	if proposal == nil {
		return false, "proposal_nil", nil
	}
	if proposal.MaxDrawdown.GreaterThan(decimal.NewFromFloat(25)) {
		return false, "proposal_max_drawdown_exceeds_risk_cap", nil
	}
	if rawSize, ok := proposal.Parameters["size_pct"]; ok {
		size := readFloatMetric(rawSize)
		if size <= 0 || size > 100 {
			return false, "size_pct_out_of_bounds", nil
		}
	}
	budget, err := r.GetAvailableBudget(ctx, proposal.StrategyID)
	if err != nil {
		return false, "budget_unavailable", err
	}
	if budget.LessThanOrEqual(decimal.Zero) {
		return false, "budget_depleted", nil
	}
	return true, "", nil
}

type scalpingExchangeConnector struct{}

func (e *scalpingExchangeConnector) IsConnected(ctx context.Context, exchange string) (bool, error) {
	input, ok := scalpingAutonomyEvalInputFromContext(ctx)
	if !ok {
		return false, fmt.Errorf("exchange connectivity context unavailable")
	}
	return input.ExchangeConnected, nil
}

func (e *scalpingExchangeConnector) CancelAllOrders(ctx context.Context, strategyID, exchange string) error {
	log.Printf(
		"[AUTONOMY] rollback cancel-all-orders is not wired for strategy=%s exchange=%s; continuing best-effort rollback",
		strings.TrimSpace(strategyID),
		strings.TrimSpace(exchange),
	)
	return nil
}

func (e *scalpingExchangeConnector) FlattenPositions(ctx context.Context, strategyID, exchange string) error {
	log.Printf(
		"[AUTONOMY] rollback flatten-positions is not wired for strategy=%s exchange=%s; continuing best-effort rollback",
		strings.TrimSpace(strategyID),
		strings.TrimSpace(exchange),
	)
	return nil
}

// ScalpingAutonomyCoordinator wires proposal validation, rollout gating, and rollback checks.
type ScalpingAutonomyCoordinator struct {
	store           *AutonomousRolloutStore
	proposal        *autonomous.StrategyProposalEngine
	rollout         *autonomous.StagedRolloutManager
	gate            *autonomous.LiveTradingGate
	rollback        *autonomous.AutoRollbackEngine
	defaultExchange string
	lastRollback    map[string]*autonomous.RollbackEvent
	rollbackMutex   sync.RWMutex
}

func NewScalpingAutonomyCoordinator(store *AutonomousRolloutStore, config AIScalpingConfig) *ScalpingAutonomyCoordinator {
	if store == nil {
		return nil
	}
	proposalCfg := autonomous.DefaultStrategyProposalConfig()
	if config.MinConfidence > 0 {
		proposalCfg.MinConfidence = clampFloat(config.MinConfidence, 0.05, 0.99)
	}
	gateCfg := autonomous.DefaultGateConfig()
	gateCfg.CacheDuration = 0

	proposalEngine := autonomous.NewStrategyProposalEngine(proposalCfg, &scalpingPolicyValidator{}, &scalpingRiskManager{})
	rolloutManager := autonomous.NewStagedRolloutManager(store, nil)
	liveGate := autonomous.NewLiveTradingGate(gateCfg, &scalpingPolicyValidator{}, &scalpingRiskManager{}, &scalpingExchangeConnector{}, rolloutManager)
	rollbackCfg := autonomous.DefaultRollbackConfig()
	rollbackCfg.GracefulRollback = false
	rollbackEngine := autonomous.NewAutoRollbackEngine(rollbackCfg, store, rolloutManager, nil, &scalpingExchangeConnector{})

	return &ScalpingAutonomyCoordinator{
		store:           store,
		proposal:        proposalEngine,
		rollout:         rolloutManager,
		gate:            liveGate,
		rollback:        rollbackEngine,
		defaultExchange: resolveDefaultScalpingExchange(config.Exchange),
		lastRollback:    make(map[string]*autonomous.RollbackEvent),
	}
}

func (c *ScalpingAutonomyCoordinator) EvaluatePreExecution(
	ctx context.Context,
	scope ScalpingAutonomyScope,
	decision *AITradingDecision,
	portfolio TradingPortfolio,
	maxCapitalPct decimal.Decimal,
) (*autonomous.GateState, *autonomous.RolloutState, error) {
	if c == nil {
		return nil, nil, nil
	}
	if decision == nil {
		return nil, nil, fmt.Errorf("decision is nil")
	}
	if strings.EqualFold(strings.TrimSpace(decision.Action), "hold") {
		return nil, nil, nil
	}

	strategyID := strings.TrimSpace(scope.StrategyID)
	if strategyID == "" {
		strategyID = ScalpingStrategyID(scope.ChatID)
	}
	if strategyID == "" {
		return nil, nil, fmt.Errorf("autonomy strategy_id is required")
	}
	if strings.TrimSpace(scope.Exchange) == "" {
		scope.Exchange = strings.TrimSpace(c.defaultExchange)
	}
	if strings.TrimSpace(scope.Exchange) == "" {
		return nil, nil, fmt.Errorf("autonomy exchange is required")
	}
	if !scope.ConnectionChecked {
		scope.ExchangeConnected = false
	}

	rolloutState, err := c.ensureRolloutState(ctx, strategyID)
	if err != nil {
		return nil, nil, err
	}

	expectedReturn, maxDrawdown := estimateDecisionReturnAndRisk(decision)
	availableBudget := resolveAvailableBudget(portfolio, maxCapitalPct)
	evalInput := scalpingAutonomyEvalInput{
		SafeModeEnabled:   scope.SafeModeEnabled,
		KillSwitchEngaged: scope.KillSwitchEngaged,
		AvailableBudget:   availableBudget,
		ExchangeConnected: scope.ExchangeConnected,
	}
	evalCtx := withScalpingAutonomyEvalInput(ctx, evalInput)

	proposal, err := c.proposal.GenerateProposal(
		evalCtx,
		strategyID,
		decision.Symbol,
		scope.Exchange,
		decision.Action,
		decision.Confidence,
		decision.Reasoning,
		map[string]any{
			"size_pct":        decision.SizePercent,
			"max_capital_pct": maxCapitalPct,
			"risk_drawdown":   portfolio.RiskDrawdown,
		},
		expectedReturn,
		maxDrawdown,
	)
	if err != nil {
		return nil, rolloutState, fmt.Errorf("build strategy proposal: %w", err)
	}
	if err := c.proposal.ValidateProposal(evalCtx, proposal); err != nil {
		return nil, rolloutState, fmt.Errorf("validate strategy proposal: %w", err)
	}

	c.gate.ClearCache(strategyID)
	gateState, err := c.gate.Evaluate(evalCtx, strategyID)
	if err != nil {
		return nil, rolloutState, fmt.Errorf("evaluate live gate: %w", err)
	}
	return gateState, rolloutState, nil
}

func (c *ScalpingAutonomyCoordinator) RecordExecutionResult(
	ctx context.Context,
	scope ScalpingAutonomyScope,
	decision *AITradingDecision,
	portfolio TradingPortfolio,
	executionErr error,
) error {
	if c == nil {
		return nil
	}
	if decision == nil || strings.EqualFold(strings.TrimSpace(decision.Action), "hold") {
		return nil
	}

	strategyID := strings.TrimSpace(scope.StrategyID)
	if strategyID == "" {
		strategyID = ScalpingStrategyID(scope.ChatID)
	}
	if strategyID == "" {
		return fmt.Errorf("autonomy strategy_id is required")
	}

	state, err := c.rollout.GetRolloutState(ctx, strategyID)
	if err != nil {
		return fmt.Errorf("load rollout state: %w", err)
	}
	if state == nil {
		state, err = c.ensureRolloutState(ctx, strategyID)
		if err != nil {
			return fmt.Errorf("ensure rollout state: %w", err)
		}
	}

	metrics := mergeRolloutMetrics(state.Metrics, portfolio, executionErr)
	if err := c.rollout.UpdateMetrics(ctx, strategyID, metrics); err != nil {
		return fmt.Errorf("update rollout metrics: %w", err)
	}

	rollbackEvent, err := c.rollback.Evaluate(ctx, strategyID, metrics)
	if err != nil {
		return fmt.Errorf("evaluate rollback trigger: %w", err)
	}
	if rollbackEvent != nil {
		c.rollbackMutex.Lock()
		defer c.rollbackMutex.Unlock()
		c.lastRollback[strategyID] = rollbackEvent
	}

	return nil
}

func (c *ScalpingAutonomyCoordinator) GetChatRolloutState(ctx context.Context, chatID string) (*autonomous.RolloutState, error) {
	if c == nil || c.store == nil {
		return nil, nil
	}
	return c.store.GetChatRolloutState(ctx, chatID)
}

func (c *ScalpingAutonomyCoordinator) SetStrategyMode(
	ctx context.Context,
	strategyID string,
	mode autonomous.StrategyMode,
) (*autonomous.RolloutState, error) {
	if c == nil || c.store == nil || c.rollout == nil {
		return nil, fmt.Errorf("autonomy coordinator is unavailable")
	}
	strategyID = strings.TrimSpace(strategyID)
	if strategyID == "" {
		return nil, fmt.Errorf("strategy_id is required")
	}

	targetStage, err := stageForMode(mode)
	if err != nil {
		return nil, err
	}

	state, err := c.rollout.GetRolloutState(ctx, strategyID)
	if err != nil {
		return nil, fmt.Errorf("get rollout state: %w", err)
	}
	if state == nil {
		state, err = c.rollout.InitializeRollout(ctx, strategyID, autonomous.DefaultPromotionCriteria())
		if err != nil {
			return nil, fmt.Errorf("initialize rollout state: %w", err)
		}
	}
	if targetStage == autonomous.StageLive && state.CurrentStage != autonomous.StageLive {
		if failures := scalpingLivePaperProofFailures(state); len(failures) > 0 {
			return state, fmt.Errorf("scalping live paper proof not met: %s", strings.Join(failures, ", "))
		}
	}
	for state.CurrentStage != targetStage {
		switch {
		case state.CurrentStage == autonomous.StageShadow && targetStage != autonomous.StageShadow:
			state, err = c.rollout.ForcePromote(ctx, strategyID, "operator_set_strategy_mode")
		case state.CurrentStage == autonomous.StagePaper && targetStage == autonomous.StageLive:
			state, err = c.rollout.ForcePromote(ctx, strategyID, "operator_set_strategy_mode")
		default:
			state, err = c.rollout.Rollback(ctx, strategyID, autonomous.TriggerOperatorSetMode, "operator_set_strategy_mode")
		}
		if err != nil {
			return nil, fmt.Errorf("set strategy mode to %s: %w", mode, err)
		}
	}

	if state.Status == autonomous.StatusPaused || state.Status == autonomous.StatusRolledBack {
		state.Status = autonomous.StatusActive
		state.EnteredAt = time.Now().UTC()
		if err := c.store.SaveRolloutState(ctx, state); err != nil {
			return nil, fmt.Errorf("persist active rollout status: %w", err)
		}
	}
	return state, nil
}

// ValidateStrategyMode checks whether a requested strategy mode transition is
// allowed without mutating rollout state.
func (c *ScalpingAutonomyCoordinator) ValidateStrategyMode(
	ctx context.Context,
	strategyID string,
	mode autonomous.StrategyMode,
) error {
	if c == nil || c.rollout == nil {
		return fmt.Errorf("autonomy coordinator is unavailable")
	}
	strategyID = strings.TrimSpace(strategyID)
	if strategyID == "" {
		return fmt.Errorf("strategy_id is required")
	}
	targetStage, err := stageForMode(mode)
	if err != nil {
		return err
	}
	if targetStage != autonomous.StageLive {
		return nil
	}
	state, err := c.rollout.GetRolloutState(ctx, strategyID)
	if err != nil {
		return fmt.Errorf("get rollout state: %w", err)
	}
	if state != nil && state.CurrentStage == autonomous.StageLive {
		return nil
	}
	if failures := scalpingLivePaperProofFailures(state); len(failures) > 0 {
		return fmt.Errorf("scalping live paper proof not met: %s", strings.Join(failures, ", "))
	}
	return nil
}

func scalpingLivePaperProofFailures(state *autonomous.RolloutState) []string {
	if state == nil {
		return []string{"rollout_state_missing"}
	}
	failures := make([]string, 0, 8)
	if state.CurrentStage != autonomous.StagePaper || state.Status != autonomous.StatusActive {
		failures = append(failures, fmt.Sprintf("strategy_not_active_paper (stage: %s, status: %s)", state.CurrentStage, state.Status))
	}
	metrics := state.Metrics
	closedOutcomeTrades := metrics.WinningTrades + metrics.LosingTrades
	closedTrades := metrics.TotalTrades
	if closedOutcomeTrades > closedTrades {
		closedTrades = closedOutcomeTrades
	}
	if closedTrades < DefaultScalpingLiveTrialMinClosedTrades {
		failures = append(failures, "closed_trades_below_live_trial_minimum")
	}
	if metrics.WinningTrades == 0 {
		failures = append(failures, "no_winning_trades")
	}
	if metrics.LosingTrades == 0 {
		failures = append(failures, "no_losing_trades")
	}
	if !metrics.TotalPnL.GreaterThan(decimal.Zero) {
		failures = append(failures, "net_pnl_not_positive")
	}
	if closedTrades == 0 || !metrics.TotalPnL.Div(decimal.NewFromInt(int64(max(closedTrades, 1)))).GreaterThan(decimal.Zero) {
		failures = append(failures, "avg_net_pnl_not_positive")
	}
	if !metrics.MaxDrawdown.GreaterThan(decimal.Zero) {
		failures = append(failures, "drawdown_not_observed")
	}
	if metrics.WinRate <= 0 {
		failures = append(failures, "win_rate_not_positive")
	}
	return failures
}

func stageForMode(mode autonomous.StrategyMode) (autonomous.RolloutStage, error) {
	switch mode {
	case autonomous.ModeShadow:
		return autonomous.StageShadow, nil
	case autonomous.ModePaper:
		return autonomous.StagePaper, nil
	case autonomous.ModeLive:
		return autonomous.StageLive, nil
	default:
		return "", fmt.Errorf("unsupported strategy mode: %s", mode)
	}
}

func (c *ScalpingAutonomyCoordinator) LastRollback(strategyID string) *autonomous.RollbackEvent {
	if c == nil {
		return nil
	}
	strategyID = strings.TrimSpace(strategyID)
	if strategyID == "" {
		return nil
	}
	c.rollbackMutex.RLock()
	defer c.rollbackMutex.RUnlock()
	event, ok := c.lastRollback[strategyID]
	if !ok || event == nil {
		return nil
	}
	copyEvent := *event
	return &copyEvent
}

func (c *ScalpingAutonomyCoordinator) ensureRolloutState(ctx context.Context, strategyID string) (*autonomous.RolloutState, error) {
	state, err := c.rollout.GetRolloutState(ctx, strategyID)
	if err != nil {
		return nil, fmt.Errorf("get rollout state: %w", err)
	}
	if state != nil {
		return state, nil
	}

	criteria := autonomous.DefaultPromotionCriteria()
	state, err = c.rollout.InitializeRollout(ctx, strategyID, criteria)
	if err != nil {
		return nil, fmt.Errorf("initialize rollout state: %w", err)
	}

	targetStage := defaultAutonomyInitialStage()
	if targetStage != autonomous.StageShadow {
		for state.CurrentStage != targetStage {
			state, err = c.rollout.Promote(ctx, strategyID, "initial_stage_override")
			if errors.Is(err, autonomous.ErrPromotionCriteriaNotMet) {
				state, err = c.rollout.ForcePromote(ctx, strategyID, "initial_stage_override")
			}
			if err != nil {
				return nil, fmt.Errorf("promote initial rollout stage to %s: %w", targetStage, err)
			}
			if state.CurrentStage == autonomous.StageLive {
				break
			}
		}
	}
	return state, nil
}

func defaultAutonomyInitialStage() autonomous.RolloutStage {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv("NEURATRADE_AUTONOMY_INITIAL_STAGE")))
	switch raw {
	case "shadow":
		return autonomous.StageShadow
	case "paper":
		return autonomous.StagePaper
	case "live":
		return autonomous.StageLive
	default:
		return autonomous.StageShadow
	}
}

func resolveDefaultScalpingExchange(configExchange string) string {
	if trimmed := strings.ToLower(strings.TrimSpace(configExchange)); trimmed != "" {
		return trimmed
	}
	if trimmed := strings.ToLower(strings.TrimSpace(os.Getenv("NEURATRADE_SCALPING_EXCHANGE"))); trimmed != "" {
		return trimmed
	}
	if trimmed := strings.ToLower(strings.TrimSpace(os.Getenv("DEFAULT_EXCHANGE"))); trimmed != "" {
		return trimmed
	}
	return ""
}

func resolveAvailableBudget(portfolio TradingPortfolio, maxCapitalPct decimal.Decimal) decimal.Decimal {
	pctDec := maxCapitalPct
	if pctDec.LessThanOrEqual(decimal.Zero) {
		pctDec = decimal.NewFromInt(1)
	}
	maxPct := decimal.NewFromInt(100)
	if pctDec.GreaterThan(maxPct) {
		pctDec = maxPct
	}
	// Budget gating uses deployable quote balance only. Unlike walletBasis(), it
	// intentionally excludes TotalValue / unrealized-PnL fallbacks so autonomy
	// does not size fresh entries off paper equity that may not be free to deploy.
	usdtDec := portfolio.USDTBalanceDecimal
	if !usdtDec.GreaterThan(decimal.Zero) {
		usdtDec = decimalFromBalanceFloat(portfolio.USDTBalance)
	}
	budget := usdtDec.Mul(pctDec).Div(maxPct)
	if budget.LessThan(decimal.Zero) {
		return decimal.Zero
	}
	return budget
}

func estimateDecisionReturnAndRisk(decision *AITradingDecision) (decimal.Decimal, decimal.Decimal) {
	if decision == nil || decision.EntryPrice == nil || decision.StopLoss == nil || decision.TakeProfit == nil {
		return decimal.Zero, decimal.Zero
	}
	entry := *decision.EntryPrice
	if entry.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero, decimal.Zero
	}

	action := strings.ToLower(strings.TrimSpace(decision.Action))
	takeProfit := *decision.TakeProfit
	stopLoss := *decision.StopLoss

	expectedReturn := decimal.Zero
	maxDrawdown := decimal.Zero
	switch action {
	case "buy":
		expectedReturn = takeProfit.Sub(entry).Div(entry).Mul(decimal.NewFromInt(100))
		maxDrawdown = entry.Sub(stopLoss).Div(entry).Mul(decimal.NewFromInt(100))
	case "sell":
		expectedReturn = entry.Sub(takeProfit).Div(entry).Mul(decimal.NewFromInt(100))
		maxDrawdown = stopLoss.Sub(entry).Div(entry).Mul(decimal.NewFromInt(100))
	}

	if expectedReturn.LessThan(decimal.Zero) {
		expectedReturn = expectedReturn.Abs()
	}
	if maxDrawdown.LessThan(decimal.Zero) {
		maxDrawdown = maxDrawdown.Abs()
	}
	return expectedReturn, maxDrawdown
}

func mergeRolloutMetrics(
	previous autonomous.RolloutMetrics,
	portfolio TradingPortfolio,
	executionErr error,
) autonomous.RolloutMetrics {
	metrics := previous
	perf := GetScalpingPerformance().GetPerformance()

	if pnlRaw, ok := perf["total_pnl"]; ok {
		if pnlValue, err := decimalFromAny(pnlRaw); err == nil {
			metrics.TotalPnL = pnlValue
		}
	}
	if drawdownPct := portfolio.RiskDrawdown * 100; drawdownPct > 0 {
		drawdown := decimal.NewFromFloat(drawdownPct)
		if drawdown.GreaterThan(metrics.MaxDrawdown) {
			metrics.MaxDrawdown = drawdown
		}
	}

	winningTrades := readIntMetric(perf["profitable_trades"])
	losingTrades := readIntMetric(perf["losing_trades"])
	if winningTrades > metrics.WinningTrades {
		metrics.WinningTrades = winningTrades
	}
	if losingTrades > metrics.LosingTrades {
		metrics.LosingTrades = losingTrades
	}

	perfTotal := readIntMetric(perf["total_trades"])
	if perfTotal > metrics.TotalTrades {
		metrics.TotalTrades = perfTotal
	}
	closedOutcomes := metrics.WinningTrades + metrics.LosingTrades
	if closedOutcomes > metrics.TotalTrades {
		metrics.TotalTrades = closedOutcomes
	}

	if executionErr != nil {
		metrics.RejectionCount++
	}
	if metrics.TotalTrades > 0 {
		metrics.WinRate = float64(metrics.WinningTrades) / float64(metrics.TotalTrades)
		metrics.UptimePercent = 100
	}
	metrics.LastUpdated = time.Now().UTC()
	return metrics
}

func decimalFromAny(v interface{}) (decimal.Decimal, error) {
	switch value := v.(type) {
	case decimal.Decimal:
		return value, nil
	case string:
		return decimal.NewFromString(strings.TrimSpace(value))
	case float64:
		return decimal.NewFromFloat(value), nil
	case float32:
		return decimal.NewFromFloat(float64(value)), nil
	case int:
		return decimal.NewFromInt(int64(value)), nil
	case int64:
		return decimal.NewFromInt(value), nil
	default:
		asText := strings.TrimSpace(fmt.Sprint(v))
		if asText == "" {
			return decimal.Zero, fmt.Errorf("empty decimal value")
		}
		return decimal.NewFromString(asText)
	}
}
