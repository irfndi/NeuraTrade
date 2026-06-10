package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"

	appautonomy "github.com/irfndi/neuratrade/internal/app/autonomy"
)

const shadowRecorderUserID = "00000000-0000-0000-0000-000000000000"

var ErrVariantNotFound = errors.New("shadow variant not found")

type shadowVariantRuntime struct {
	mu             sync.Mutex
	engine         *ShadowModeEngine
	lastRealized   decimal.Decimal
	tradeCount     int64
	winningTrades  int64
	rejectionCount int64
	gateRejections map[string]int64
	openDecisions  map[string]int64
	lastEntryPrice map[string]decimal.Decimal
	openedAt       map[string]time.Time
}

type ShadowEvaluationCoordinator struct {
	logger     *zap.Logger
	db         DBPool
	mirror     *ShadowDecisionMirror
	comparison *LiveShadowComparisonEngine
	variants   *ShadowVariantStore
	simulator  *PaperExecutionSimulator
	recorder   *PaperTradeRecorder

	mu       sync.RWMutex
	runtimes map[string]*shadowVariantRuntime

	// Periodic reconciliation for orphan paper_trades rows. The live
	// paper-trading path opens rows in recordPaperTradeOpen but never
	// closes them (the close path runs only in backfills), so positions
	// accumulate unboundedly. Without this loop, paper PnL diverges from
	// reality and a real-money rollout would silently leak.
	reconcilerStop chan struct{}
	reconcilerDone chan struct{}
}

func NewShadowEvaluationCoordinator(
	db DBPool,
	logger *zap.Logger,
	simulator *PaperExecutionSimulator,
	recorder *PaperTradeRecorder,
	seedVariants []ShadowVariantConfig,
) *ShadowEvaluationCoordinator {
	if logger == nil {
		logger = zap.NewNop()
	}
	if simulator == nil {
		simulator = NewPaperExecutionSimulator(DefaultPaperExecutionConfig())
	}
	store := NewShadowVariantStore(seedVariants)
	coordinator := &ShadowEvaluationCoordinator{
		logger:     logger,
		db:         db,
		mirror:     NewShadowDecisionMirror(logger),
		comparison: NewLiveShadowComparisonEngine(logger),
		variants:   store,
		simulator:  simulator,
		recorder:   recorder,
		runtimes:   make(map[string]*shadowVariantRuntime),
	}
	for _, variant := range store.List() {
		coordinator.runtimes[variant.VariantID] = newShadowVariantRuntime(logger)
	}
	return coordinator
}

func (c *ShadowEvaluationCoordinator) MirrorDecision(
	ctx context.Context,
	liveDecision *AITradingDecision,
	portfolio TradingPortfolio,
	policy appautonomy.ScalpingCyclePolicy,
) ([]ShadowMirroredDecision, error) {
	if c == nil {
		return nil, nil
	}
	variants := c.variants.List()
	results := make([]ShadowMirroredDecision, 0, len(variants))
	for _, variant := range variants {
		runtime := c.runtimeForVariant(variant.VariantID)
		mirrored := c.mirror.MirrorDecision(liveDecision, portfolio, policy, variant)
		decisionID, err := c.insertShadowDecision(ctx, mirrored)
		if err != nil {
			c.logger.Warn("shadow decision persistence failed", zap.String("variant_id", variant.VariantID), zap.Error(err))
		}
		c.recordRejection(runtime, mirrored)
		if err := c.executeShadowDecision(ctx, runtime, decisionID, mirrored, portfolio); err != nil {
			c.logger.Warn("shadow execution simulation failed", zap.String("variant_id", variant.VariantID), zap.Error(err))
		}
		results = append(results, mirrored)
	}
	return results, nil
}

func (c *ShadowEvaluationCoordinator) RecordShadowOutcome(
	ctx context.Context,
	shadowDecision *AITradingDecision,
	marketPrices map[string]decimal.Decimal,
) {
	if c == nil {
		return
	}
	variants := c.variants.List()
	for _, variant := range variants {
		runtime := c.runtimeForVariant(variant.VariantID)
		runtime.mu.Lock()
		runtime.engine.UpdatePrices(marketPrices)
		if shadowDecision == nil {
			runtime.mu.Unlock()
			continue
		}
		action := strings.ToLower(strings.TrimSpace(shadowDecision.Action))
		symbol := strings.TrimSpace(shadowDecision.Symbol)
		if action != "sell" || symbol == "" {
			runtime.mu.Unlock()
			continue
		}
		decisionID, ok := runtime.openDecisions[symbol]
		if !ok {
			runtime.mu.Unlock()
			continue
		}
		portfolioSnapshot := runtime.engine.GetPortfolio()
		exitPrice := marketPrices[symbol]
		if !exitPrice.GreaterThan(decimal.Zero) {
			exitPrice = runtime.lastEntryPrice[symbol]
		}
		sellOK := true
		if exitPrice.GreaterThan(decimal.Zero) && portfolioSnapshot.Positions != nil {
			if pos, hasPos := portfolioSnapshot.Positions[symbol]; hasPos && pos.Quantity.GreaterThan(decimal.Zero) {
				if _, execErr := runtime.engine.ExecuteTrade(
					ctx, symbol, "sell", pos.Quantity, exitPrice,
				); execErr != nil {
					c.logger.Warn("shadow sell execution failed",
						zap.String("variant_id", variant.VariantID),
						zap.String("symbol", symbol),
						zap.Error(execErr),
					)
					sellOK = false
				}
			}
		}
		var realized decimal.Decimal
		if sellOK {
			portfolioSnapshot = runtime.engine.GetPortfolio()
			realized = portfolioSnapshot.RealizedPNL.Sub(runtime.lastRealized)
			runtime.lastRealized = portfolioSnapshot.RealizedPNL
			if realized.GreaterThan(decimal.Zero) {
				runtime.winningTrades++
			}
			delete(runtime.openDecisions, symbol)
			delete(runtime.lastEntryPrice, symbol)
			delete(runtime.openedAt, symbol)
		}
		runtime.mu.Unlock()
		if sellOK {
			if err := c.insertShadowOutcome(ctx, decisionID, exitPrice, realized); err != nil {
				c.logger.Warn("shadow outcome persistence failed", zap.String("variant_id", variant.VariantID), zap.Error(err))
			}
		}
	}
}

const defaultShadowMaxPositionAge = 4 * time.Hour

// CloseStaleShadowPositions force-closes shadow positions that have been open
// longer than maxAge. This prevents memory leaks in openDecisions when the
// live system never sells a symbol that shadow entered. Call from periodic
// reconciliation or diagnostic sweeps.
func (c *ShadowEvaluationCoordinator) CloseStaleShadowPositions(
	ctx context.Context,
	marketPrices map[string]decimal.Decimal,
	maxAge time.Time,
) {
	if c == nil {
		return
	}
	if maxAge.IsZero() {
		maxAge = time.Now().UTC().Add(-defaultShadowMaxPositionAge)
	}
	variants := c.variants.List()
	for _, variant := range variants {
		runtime := c.runtimeForVariant(variant.VariantID)
		runtime.mu.Lock()
		runtime.engine.UpdatePrices(marketPrices)
		var stale []string
		for symbol, openedAt := range runtime.openedAt {
			if openedAt.Before(maxAge) {
				stale = append(stale, symbol)
			}
		}
		if len(stale) == 0 {
			runtime.mu.Unlock()
			continue
		}
		type staleOutcome struct {
			decisionID int64
			exitPrice  decimal.Decimal
			realized   decimal.Decimal
			symbol     string
			age        time.Duration
		}
		var outcomes []staleOutcome
		for _, symbol := range stale {
			decisionID := runtime.openDecisions[symbol]
			openedAt := runtime.openedAt[symbol]
			exitPrice := marketPrices[symbol]
			if !exitPrice.GreaterThan(decimal.Zero) {
				exitPrice = runtime.lastEntryPrice[symbol]
			}
			var realized decimal.Decimal
			if exitPrice.GreaterThan(decimal.Zero) {
				portfolioSnapshot := runtime.engine.GetPortfolio()
				if portfolioSnapshot.Positions != nil {
					if pos, hasPos := portfolioSnapshot.Positions[symbol]; hasPos && pos.Quantity.GreaterThan(decimal.Zero) {
						_, _ = runtime.engine.ExecuteTrade(
							context.Background(), symbol, "sell",
							pos.Quantity, exitPrice,
						)
					}
				}
				portfolioSnapshot = runtime.engine.GetPortfolio()
				realized = portfolioSnapshot.RealizedPNL.Sub(runtime.lastRealized)
				runtime.lastRealized = portfolioSnapshot.RealizedPNL
				if realized.GreaterThan(decimal.Zero) {
					runtime.winningTrades++
				}
			}
			delete(runtime.openDecisions, symbol)
			delete(runtime.lastEntryPrice, symbol)
			delete(runtime.openedAt, symbol)
			if decisionID > 0 && exitPrice.GreaterThan(decimal.Zero) {
				outcomes = append(outcomes, staleOutcome{
					decisionID: decisionID,
					exitPrice:  exitPrice,
					realized:   realized,
					symbol:     symbol,
					age:        time.Since(openedAt),
				})
			}
		}
		runtime.mu.Unlock()
		for _, o := range outcomes {
			c.logger.Info("shadow stale position closed",
				zap.String("variant_id", variant.VariantID),
				zap.String("symbol", o.symbol),
				zap.Duration("age", o.age),
			)
			if persistErr := c.insertShadowOutcome(ctx, o.decisionID, o.exitPrice, o.realized); persistErr != nil {
				c.logger.Warn("shadow stale outcome persistence failed",
					zap.String("variant_id", variant.VariantID),
					zap.String("symbol", o.symbol),
					zap.Error(persistErr),
				)
			}
		}
	}
}

func (c *ShadowEvaluationCoordinator) CompareLiveVsShadow(
	ctx context.Context,
	start time.Time,
	end time.Time,
) (LiveShadowComparisonReport, error) {
	if c == nil {
		return LiveShadowComparisonReport{}, nil
	}
	if end.IsZero() {
		end = time.Now().UTC()
	}
	if start.IsZero() {
		start = end.Add(-24 * time.Hour)
	}
	live := c.liveMetricsSnapshot()
	variantMetrics := c.shadowMetricsSnapshot()
	return c.comparison.BuildReport(start, end, live, variantMetrics), nil
}

func (c *ShadowEvaluationCoordinator) GetShadowDiagnostics(_ context.Context) map[string]interface{} {
	variants := c.variants.List()
	variantStats := make([]map[string]interface{}, 0, len(variants))
	for _, variant := range variants {
		runtime := c.runtimeForVariant(variant.VariantID)
		runtime.mu.Lock()
		stats := runtime.engine.GetStats()
		rejectionCount := runtime.rejectionCount
		tradeCount := runtime.tradeCount
		runtime.mu.Unlock()
		stats["variant_id"] = variant.VariantID
		stats["variant_name"] = variant.Name
		stats["description"] = variant.Description
		stats["policy_overrides"] = variant.PolicyOverrides
		stats["shadow_rejection_count"] = rejectionCount
		stats["shadow_trade_count"] = tradeCount
		variantStats = append(variantStats, stats)
	}
	return map[string]interface{}{
		"generated_at": time.Now().UTC().Format(time.RFC3339),
		"variants":     variantStats,
	}
}

func (c *ShadowEvaluationCoordinator) ListVariants() []ShadowVariantConfig {
	if c == nil {
		return nil
	}
	return c.variants.List()
}

func (c *ShadowEvaluationCoordinator) UpsertVariant(config ShadowVariantConfig) (ShadowVariantConfig, error) {
	if c == nil {
		return ShadowVariantConfig{}, fmt.Errorf("shadow coordinator is nil")
	}
	variant, err := c.variants.Upsert(config)
	if err != nil {
		return ShadowVariantConfig{}, err
	}
	c.mu.Lock()
	if _, ok := c.runtimes[variant.VariantID]; !ok {
		c.runtimes[variant.VariantID] = newShadowVariantRuntime(c.logger)
	}
	c.mu.Unlock()
	return variant, nil
}

func (c *ShadowEvaluationCoordinator) DeleteVariant(variantID string) bool {
	if c == nil {
		return false
	}
	if !c.variants.Delete(variantID) {
		return false
	}
	key := strings.TrimSpace(strings.ToLower(variantID))
	c.mu.Lock()
	delete(c.runtimes, key)
	c.mu.Unlock()
	return true
}

func (c *ShadowEvaluationCoordinator) VariantDiagnostics(ctx context.Context, variantID string) (map[string]interface{}, error) {
	if c == nil {
		return map[string]interface{}{}, nil
	}
	variant, ok := c.variants.Get(variantID)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrVariantNotFound, variantID)
	}
	report, _ := c.CompareLiveVsShadow(ctx, time.Now().UTC().Add(-24*time.Hour), time.Now().UTC())
	runtime := c.runtimeForVariant(variant.VariantID)
	runtime.mu.Lock()
	result := map[string]interface{}{
		"variant":        variant,
		"portfolio":      runtime.engine.GetPortfolio(),
		"stats":          runtime.engine.GetStats(),
		"trade_count":    runtime.tradeCount,
		"rejections":     runtime.rejectionCount,
		"gate_reasons":   copyGateMap(runtime.gateRejections),
		"comparison_24h": map[string]interface{}{},
	}
	runtime.mu.Unlock()
	for _, cmp := range report.Comparisons {
		if cmp.VariantID == variant.VariantID {
			result["comparison_24h"] = cmp
			break
		}
	}
	return result, nil
}

func (c *ShadowEvaluationCoordinator) PersistComparisonSnapshot(
	ctx context.Context,
	start time.Time,
	end time.Time,
) error {
	report, err := c.CompareLiveVsShadow(ctx, start, end)
	if err != nil {
		return err
	}
	for _, item := range report.Comparisons {
		if err := c.insertComparisonSnapshot(ctx, report.WindowStart, report.WindowEnd, item); err != nil {
			c.logger.Warn("live-shadow comparison persistence failed", zap.String("variant_id", item.VariantID), zap.Error(err))
		}
	}
	return nil
}

func newShadowVariantRuntime(logger *zap.Logger) *shadowVariantRuntime {
	config := DefaultShadowModeConfig()
	config.Enabled = true
	engine := NewShadowModeEngine(config, logger)
	engine.Enable()
	return &shadowVariantRuntime{
		engine:         engine,
		gateRejections: make(map[string]int64),
		openDecisions:  make(map[string]int64),
		lastEntryPrice: make(map[string]decimal.Decimal),
		openedAt:       make(map[string]time.Time),
	}
}

func (c *ShadowEvaluationCoordinator) runtimeForVariant(variantID string) *shadowVariantRuntime {
	key := strings.TrimSpace(strings.ToLower(variantID))
	c.mu.RLock()
	runtime, ok := c.runtimes[key]
	c.mu.RUnlock()
	if ok {
		return runtime
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if runtime, ok = c.runtimes[key]; ok {
		return runtime
	}
	runtime = newShadowVariantRuntime(c.logger)
	c.runtimes[key] = runtime
	return runtime
}

func (c *ShadowEvaluationCoordinator) recordRejection(runtime *shadowVariantRuntime, mirrored ShadowMirroredDecision) {
	if runtime == nil {
		return
	}
	if mirrored.GateAllowed {
		return
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	runtime.rejectionCount++
	code := strings.TrimSpace(mirrored.GateCode)
	if code == "" {
		code = "unknown"
	}
	runtime.gateRejections[code]++
}

func (c *ShadowEvaluationCoordinator) executeShadowDecision(
	ctx context.Context,
	runtime *shadowVariantRuntime,
	decisionID int64,
	mirrored ShadowMirroredDecision,
	portfolio TradingPortfolio,
) error {
	if runtime == nil || !mirrored.GateAllowed {
		return nil
	}
	action := strings.ToLower(strings.TrimSpace(mirrored.ShadowAction))
	if action != "buy" && action != "sell" {
		return nil
	}
	if mirrored.EntryPrice == nil || !mirrored.EntryPrice.GreaterThan(decimal.Zero) {
		return nil
	}
	wallet := walletBasis(portfolio)
	if !wallet.GreaterThan(decimal.Zero) {
		wallet = decimal.NewFromInt(1000)
	}
	notional := wallet.Mul(decimal.NewFromFloat(mirrored.SizePercent)).Div(decimal.NewFromInt(100))
	if !notional.GreaterThan(decimal.Zero) {
		return nil
	}
	quantity := notional.Div(*mirrored.EntryPrice)
	if !quantity.GreaterThan(decimal.Zero) {
		return nil
	}
	order, err := c.simulator.CreateOrder(PaperOrderRequest{
		UserID:   shadowRecorderUserID,
		Exchange: "shadow",
		Symbol:   mirrored.Symbol,
		Type:     PaperOrderTypeMarket,
		Side:     toPaperSide(action),
		Size:     quantity,
	})
	if err != nil {
		return err
	}
	filled, err := c.simulator.SimulateFill(ctx, order, *mirrored.EntryPrice)
	if err != nil {
		return err
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if filled.Status == PaperOrderStatusRejected || filled.Status == PaperOrderStatusCancelled || filled.Status == PaperOrderStatusExpired {
		runtime.rejectionCount++
		runtime.gateRejections["shadow_execution_rejected"]++
		return nil
	}
	if !filled.FilledSize.GreaterThan(decimal.Zero) {
		return nil
	}
	_, err = runtime.engine.ExecuteTrade(ctx, mirrored.Symbol, action, filled.FilledSize, filled.AvgFillPrice)
	if err != nil {
		return err
	}
	runtime.tradeCount++
	switch action {
	case "buy":
		runtime.openDecisions[mirrored.Symbol] = decisionID
		runtime.lastEntryPrice[mirrored.Symbol] = filled.AvgFillPrice
		runtime.openedAt[mirrored.Symbol] = time.Now().UTC()
	}
	// Sell-side cleanup (realized PnL, winningTrades, outcome persistence, and
	// openDecisions deletion) is owned exclusively by RecordShadowOutcome so that
	// every close produces exactly one shadow_outcomes row.
	c.recordPaperTradeOpen(ctx, mirrored, filled)
	return nil
}

func (c *ShadowEvaluationCoordinator) recordPaperTradeOpen(ctx context.Context, mirrored ShadowMirroredDecision, order *PaperOrder) {
	if c.recorder == nil || order == nil {
		return
	}
	if !order.FilledSize.GreaterThan(decimal.Zero) {
		return
	}
	_, err := c.recorder.RecordOpenTrade(ctx, &PaperTrade{
		UserID:     shadowRecorderUserID,
		StrategyID: mirrored.VariantID,
		Exchange:   order.Exchange,
		Symbol:     order.Symbol,
		Side:       string(order.Side),
		EntryPrice: order.AvgFillPrice,
		Size:       order.FilledSize,
		Fees:       decimal.Zero,
		CostBasis:  order.AvgFillPrice.Mul(order.FilledSize),
	})
	if err != nil {
		c.logger.Debug("paper trade recorder open skipped", zap.Error(err))
	}
}

func toPaperSide(action string) PaperOrderSide {
	if strings.EqualFold(strings.TrimSpace(action), "sell") {
		return PaperOrderSideSell
	}
	return PaperOrderSideBuy
}

func (c *ShadowEvaluationCoordinator) insertShadowDecision(ctx context.Context, mirrored ShadowMirroredDecision) (int64, error) {
	if isNilDBPool(c.db) {
		return 0, nil
	}
	query := `
		INSERT INTO shadow_decisions (
			variant_id, live_decision_id, symbol, action, confidence, size_pct,
			entry_price, stop_loss, take_profit, gate_allowed, gate_reason, gate_code
		) VALUES ($1, NULL, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id
	`
	var id int64
	err := c.db.QueryRow(
		ctx,
		query,
		mirrored.VariantID,
		strings.TrimSpace(mirrored.Symbol),
		strings.ToLower(strings.TrimSpace(mirrored.ShadowAction)),
		mirrored.Confidence,
		mirrored.SizePercent,
		decimalValueOrZero(mirrored.EntryPrice),
		decimalValueOrZero(mirrored.StopLoss),
		decimalValueOrZero(mirrored.TakeProfit),
		mirrored.GateAllowed,
		strings.TrimSpace(mirrored.GateReason),
		strings.TrimSpace(mirrored.GateCode),
	).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

func (c *ShadowEvaluationCoordinator) insertShadowOutcome(
	ctx context.Context,
	decisionID int64,
	exitPrice decimal.Decimal,
	realizedPnL decimal.Decimal,
) error {
	if isNilDBPool(c.db) || decisionID <= 0 {
		return nil
	}
	query := `
		INSERT INTO shadow_outcomes (
			shadow_decision_id, exit_price, realized_pnl, max_favor, max_adverse, closed_at
		) VALUES ($1, $2, $3, $4, $5, $6)
	`
	_, err := c.db.Exec(ctx, query, decisionID, exitPrice, realizedPnL, decimal.Zero, decimal.Zero, time.Now().UTC())
	return err
}

func (c *ShadowEvaluationCoordinator) insertComparisonSnapshot(
	ctx context.Context,
	windowStart time.Time,
	windowEnd time.Time,
	comparison LiveShadowVariantComparison,
) error {
	if isNilDBPool(c.db) {
		return nil
	}
	query := `
		INSERT INTO live_shadow_comparisons (
			variant_id, comparison_window_start, comparison_window_end,
			live_pnl, shadow_pnl, pnl_divergence,
			live_win_rate, shadow_win_rate,
			live_trade_count, shadow_trade_count,
			live_rejection_count, shadow_rejection_count
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`
	_, err := c.db.Exec(
		ctx,
		query,
		comparison.VariantID,
		windowStart.UTC(),
		windowEnd.UTC(),
		comparison.LivePnL,
		comparison.ShadowPnL,
		comparison.PnLDivergence,
		comparison.LiveWinRate.InexactFloat64(),
		comparison.ShadowWinRate.InexactFloat64(),
		comparison.LiveTradeCount,
		comparison.ShadowTradeCount,
		comparison.LiveRejectionCount,
		comparison.ShadowRejectionCount,
	)
	return err
}

func (c *ShadowEvaluationCoordinator) liveMetricsSnapshot() LiveShadowMetrics {
	perf := GetScalpingPerformance().GetPerformance()
	return LiveShadowMetrics{
		PnL:            metricDecimal(perf["total_pnl"]),
		WinRate:        decimal.NewFromFloat(readFloatMetric(perf["win_rate"]) * 100),
		TradeCount:     int64(readIntMetric(perf["total_trades"])),
		RejectionCount: int64(readIntMetric(perf["rejected_trades"])),
	}
}

func (c *ShadowEvaluationCoordinator) shadowMetricsSnapshot() []ShadowVariantMetrics {
	variants := c.variants.List()
	result := make([]ShadowVariantMetrics, 0, len(variants))
	for _, variant := range variants {
		runtime := c.runtimeForVariant(variant.VariantID)
		runtime.mu.Lock()
		portfolio := runtime.engine.GetPortfolio()
		tradeCount := runtime.tradeCount
		winningTrades := runtime.winningTrades
		rejectionCount := runtime.rejectionCount
		gateRejections := copyGateMap(runtime.gateRejections)
		runtime.mu.Unlock()
		winRate := decimal.Zero
		if tradeCount > 0 {
			winRate = decimal.NewFromInt(winningTrades).Div(decimal.NewFromInt(tradeCount)).Mul(decimal.NewFromInt(100))
		}
		result = append(result, ShadowVariantMetrics{
			VariantID:       variant.VariantID,
			VariantName:     variant.Name,
			PnL:             portfolio.RealizedPNL,
			WinRate:         winRate,
			TradeCount:      tradeCount,
			RejectionCount:  rejectionCount,
			EntryTimingBps:  decimal.Zero,
			ExitTimingBps:   decimal.Zero,
			OpportunityCost: decimal.Zero,
			GateRejections:  gateRejections,
		})
	}
	return result
}

func copyGateMap(source map[string]int64) map[string]int64 {
	if len(source) == 0 {
		return map[string]int64{}
	}
	result := make(map[string]int64, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func metricDecimal(raw interface{}) decimal.Decimal {
	value, err := decimalFromAny(raw)
	if err != nil {
		return decimal.Zero
	}
	return value
}

// ReconciliationConfig controls the periodic orphan-paper-trade closer
// run by Start(). Defaults are conservative: scan every 10 minutes,
// force-close any paper_trade row that's been open for more than 4 hours.
// The 4h window matches defaultShadowMaxPositionAge for shadow decisions.
type ReconciliationConfig struct {
	// Interval between sweeps. Must be > 0; default 10m.
	Interval time.Duration
	// MaxAge is the cutoff for "stale" rows. Must be > 0; default 4h.
	MaxAge time.Duration
	// ExitPriceFunc returns the exit price to use when closing a stale
	// row. If nil, the row's entry price is reused (which produces
	// zero realized PnL). A nil function is the safe default for
	// paper-only systems; live-trading callers should pass a price feed.
	ExitPriceFunc func(trade *PaperTrade) decimal.Decimal
}

func (c *ReconciliationConfig) applyDefaults() {
	if c.Interval <= 0 {
		c.Interval = 10 * time.Minute
	}
	if c.MaxAge <= 0 {
		c.MaxAge = defaultShadowMaxPositionAge
	}
}

// Start launches the periodic orphan-paper-trade reconciler. Idempotent:
// calling Start twice is a no-op. The reconciler closes paper_trades
// rows that have been 'open' for longer than cfg.MaxAge. The legacy
// CloseStaleShadowPositions loop (which only manages the in-memory
// shadow decision state) is run on the same tick.
//
// This is required for real-money readiness: without it, paper-trade
// rows accumulate forever and inflate cost-basis calculations.
func (c *ShadowEvaluationCoordinator) Start(ctx context.Context, cfg ReconciliationConfig) {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.reconcilerStop != nil {
		c.mu.Unlock()
		return
	}
	cfg.applyDefaults()
	stop := make(chan struct{})
	done := make(chan struct{})
	c.reconcilerStop = stop
	c.reconcilerDone = done
	c.mu.Unlock()

	go c.reconcileLoop(ctx, cfg, stop, done)
}

// Stop signals the periodic reconciler to exit and waits for it. Safe
// to call multiple times. Safe to call before Start (no-op).
func (c *ShadowEvaluationCoordinator) Stop() {
	if c == nil {
		return
	}
	c.mu.Lock()
	stop := c.reconcilerStop
	done := c.reconcilerDone
	c.reconcilerStop = nil
	c.reconcilerDone = nil
	c.mu.Unlock()
	if stop == nil {
		return
	}
	close(stop)
	if done != nil {
		<-done
	}
}

func (c *ShadowEvaluationCoordinator) reconcileLoop(ctx context.Context, cfg ReconciliationConfig, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()
	c.runReconcileOnce(ctx, cfg) // run once immediately
	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case <-ticker.C:
			c.runReconcileOnce(ctx, cfg)
		}
	}
}

func (c *ShadowEvaluationCoordinator) runReconcileOnce(ctx context.Context, cfg ReconciliationConfig) {
	cutoff := time.Now().UTC().Add(-cfg.MaxAge)
	c.CloseStaleShadowPositions(ctx, nil, cutoff)

	if c.recorder == nil {
		return
	}
	stale, err := c.recorder.ListStaleOpenTrades(ctx, cutoff)
	if err != nil {
		c.logger.Warn("paper trade reconciler: list stale failed",
			zap.Error(err),
		)
		return
	}
	if len(stale) == 0 {
		return
	}
	closed := 0
	for _, t := range stale {
		exitPrice := t.EntryPrice
		if cfg.ExitPriceFunc != nil {
			if p := cfg.ExitPriceFunc(t); p.GreaterThan(decimal.Zero) {
				exitPrice = p
			}
		}
		// Calculate PnL against the chosen exit price.
		var pnl decimal.Decimal
		if t.Side == "buy" {
			pnl = exitPrice.Sub(t.EntryPrice).Mul(t.Size)
		} else {
			pnl = t.EntryPrice.Sub(exitPrice).Mul(t.Size)
		}
		if _, err := c.recorder.RecordCloseTrade(ctx, t.ID, exitPrice, decimal.Zero, time.Now().UTC()); err != nil {
			c.logger.Warn("paper trade reconciler: close failed",
				zap.Int64("trade_id", t.ID),
				zap.String("symbol", t.Symbol),
				zap.String("strategy", t.StrategyID),
				zap.Error(err),
			)
			continue
		}
		closed++
		_ = pnl // PnL is recomputed inside RecordCloseTrade; logged there
	}
	c.logger.Info("paper trade reconciler: closed stale rows",
		zap.Int("stale", len(stale)),
		zap.Int("closed", closed),
		zap.Duration("max_age", cfg.MaxAge),
	)
}
