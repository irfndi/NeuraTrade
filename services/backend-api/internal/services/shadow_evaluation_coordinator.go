package services

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"

	appautonomy "github.com/irfndi/neuratrade/internal/app/autonomy"
)

const shadowRecorderUserID = "00000000-0000-0000-0000-000000000000"

type shadowVariantRuntime struct {
	engine         *ShadowModeEngine
	lastRealized   decimal.Decimal
	tradeCount     int64
	winningTrades  int64
	rejectionCount int64
	gateRejections map[string]int64
	openDecisions  map[string]int64
	lastEntryPrice map[string]decimal.Decimal
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
		runtime.engine.UpdatePrices(marketPrices)
		if shadowDecision == nil {
			continue
		}
		action := strings.ToLower(strings.TrimSpace(shadowDecision.Action))
		symbol := strings.TrimSpace(shadowDecision.Symbol)
		if action != "sell" || symbol == "" {
			continue
		}
		decisionID, ok := runtime.openDecisions[symbol]
		if !ok {
			continue
		}
		portfolioSnapshot := runtime.engine.GetPortfolio()
		realized := portfolioSnapshot.RealizedPNL.Sub(runtime.lastRealized)
		runtime.lastRealized = portfolioSnapshot.RealizedPNL
		exitPrice := marketPrices[symbol]
		if !exitPrice.GreaterThan(decimal.Zero) {
			exitPrice = runtime.lastEntryPrice[symbol]
		}
		if err := c.insertShadowOutcome(ctx, decisionID, exitPrice, realized); err != nil {
			c.logger.Warn("shadow outcome persistence failed", zap.String("variant_id", variant.VariantID), zap.Error(err))
		}
		if realized.GreaterThan(decimal.Zero) {
			runtime.winningTrades++
		}
		delete(runtime.openDecisions, symbol)
		delete(runtime.lastEntryPrice, symbol)
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
	report := c.comparison.BuildReport(start, end, live, variantMetrics)
	for _, item := range report.Comparisons {
		if err := c.insertComparisonSnapshot(ctx, report.WindowStart, report.WindowEnd, item); err != nil {
			c.logger.Warn("live-shadow comparison persistence failed", zap.String("variant_id", item.VariantID), zap.Error(err))
		}
	}
	return report, nil
}

func (c *ShadowEvaluationCoordinator) GetShadowDiagnostics(ctx context.Context) map[string]interface{} {
	report, _ := c.CompareLiveVsShadow(ctx, time.Now().UTC().Add(-24*time.Hour), time.Now().UTC())
	variants := c.variants.List()
	variantStats := make([]map[string]interface{}, 0, len(variants))
	for _, variant := range variants {
		runtime := c.runtimeForVariant(variant.VariantID)
		stats := runtime.engine.GetStats()
		stats["variant_id"] = variant.VariantID
		stats["variant_name"] = variant.Name
		stats["description"] = variant.Description
		stats["policy_overrides"] = variant.PolicyOverrides
		stats["shadow_rejection_count"] = runtime.rejectionCount
		stats["shadow_trade_count"] = runtime.tradeCount
		variantStats = append(variantStats, stats)
	}
	return map[string]interface{}{
		"generated_at": time.Now().UTC().Format(time.RFC3339),
		"variants":     variantStats,
		"comparison":   report,
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
		return nil, fmt.Errorf("shadow variant %q not found", variantID)
	}
	report, _ := c.CompareLiveVsShadow(ctx, time.Now().UTC().Add(-24*time.Hour), time.Now().UTC())
	runtime := c.runtimeForVariant(variant.VariantID)
	result := map[string]interface{}{
		"variant":        variant,
		"portfolio":      runtime.engine.GetPortfolio(),
		"stats":          runtime.engine.GetStats(),
		"trade_count":    runtime.tradeCount,
		"rejections":     runtime.rejectionCount,
		"gate_reasons":   runtime.gateRejections,
		"comparison_24h": map[string]interface{}{},
	}
	for _, cmp := range report.Comparisons {
		if cmp.VariantID == variant.VariantID {
			result["comparison_24h"] = cmp
			break
		}
	}
	return result, nil
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
	if action == "buy" {
		runtime.openDecisions[mirrored.Symbol] = decisionID
		runtime.lastEntryPrice[mirrored.Symbol] = filled.AvgFillPrice
	}
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
		portfolio := runtime.engine.GetPortfolio()
		winRate := decimal.Zero
		if runtime.tradeCount > 0 {
			winRate = decimal.NewFromInt(runtime.winningTrades).Div(decimal.NewFromInt(runtime.tradeCount)).Mul(decimal.NewFromInt(100))
		}
		result = append(result, ShadowVariantMetrics{
			VariantID:       variant.VariantID,
			VariantName:     variant.Name,
			PnL:             portfolio.RealizedPNL,
			WinRate:         winRate,
			TradeCount:      runtime.tradeCount,
			RejectionCount:  runtime.rejectionCount,
			EntryTimingBps:  decimal.Zero,
			ExitTimingBps:   decimal.Zero,
			OpportunityCost: decimal.Zero,
			GateRejections:  copyGateMap(runtime.gateRejections),
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
