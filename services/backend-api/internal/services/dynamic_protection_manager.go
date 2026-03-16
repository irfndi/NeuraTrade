package services

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/shopspring/decimal"
)

type protectionTickerService interface {
	FetchSingleTicker(ctx context.Context, exchange, symbol string) (ccxt.MarketPriceInterface, error)
}

type positionProtectionSync interface {
	SyncPositionProtection(
		ctx context.Context,
		exchange string,
		position ManagedOpenPosition,
		stopLoss decimal.Decimal,
		takeProfit decimal.Decimal,
	) error
}

type DynamicProtectionConfig struct {
	Enabled               bool
	MaxPositions          int
	UpdateCooldown        time.Duration
	ProfitActivationPct   float64
	BreakevenBufferPct    float64
	TrailingStopPct       float64
	TakeProfitDistancePct float64
	MinAdjustmentPct      float64
}

var ErrProtectionSyncUnsupported = errors.New("exchange-side protection sync unsupported")

func DefaultDynamicProtectionConfig() DynamicProtectionConfig {
	return DynamicProtectionConfig{
		Enabled:               true,
		MaxPositions:          25,
		UpdateCooldown:        45 * time.Second,
		ProfitActivationPct:   0.35,
		BreakevenBufferPct:    0.05,
		TrailingStopPct:       0.45,
		TakeProfitDistancePct: 0.60,
		MinAdjustmentPct:      0.05,
	}
}

type DynamicProtectionSummary struct {
	Exchange            string
	PositionsEvaluated  int
	ProtectionsUpdated  int
	MissingProtection   int
	RecoveredProtection int
	Errors              int
}

type DynamicProtectionManager struct {
	config       DynamicProtectionConfig
	lifecycle    *TradingLifecycleStore
	tickerSource protectionTickerService
	protection   positionProtectionSync
	logger       *log.Logger
	missingMu    sync.Mutex
	missingRetry map[string]time.Time
}

func NewDynamicProtectionManager(
	config DynamicProtectionConfig,
	lifecycle *TradingLifecycleStore,
	tickerSource protectionTickerService,
	logger *log.Logger,
) *DynamicProtectionManager {
	if logger == nil {
		logger = log.Default()
	}
	return &DynamicProtectionManager{
		config:       config,
		lifecycle:    lifecycle,
		tickerSource: tickerSource,
		logger:       logger,
	}
}

func (m *DynamicProtectionManager) SetPositionProtectionSync(sync positionProtectionSync) {
	m.protection = sync
}

func (m *DynamicProtectionManager) ReconcileOpenPositions(ctx context.Context, chatID, exchange string) (DynamicProtectionSummary, error) {
	summary := DynamicProtectionSummary{
		Exchange: strings.TrimSpace(exchange),
	}
	if !m.config.Enabled {
		return summary, nil
	}
	if m.lifecycle == nil || m.tickerSource == nil {
		return summary, fmt.Errorf("dynamic protection manager dependencies are not configured")
	}

	limit := m.config.MaxPositions
	if limit <= 0 {
		limit = 25
	}

	positions, err := m.lifecycle.ListManagedOpenPositions(ctx, chatID, exchange, limit)
	if err != nil {
		return summary, err
	}
	now := time.Now().UTC()
	for _, pos := range positions {
		if !isAutonomousManagedPosition(pos) {
			continue
		}
		summary.PositionsEvaluated++
		missingProtection := pos.StopLoss.LessThanOrEqual(decimal.Zero) || pos.TakeProfit.LessThanOrEqual(decimal.Zero)
		if missingProtection {
			summary.MissingProtection++
		}
		ticker, fetchErr := m.tickerSource.FetchSingleTicker(ctx, pos.Exchange, pos.Symbol)
		if fetchErr != nil {
			summary.Errors++
			m.logger.Printf("[PROTECTION] Ticker unavailable for %s %s: %v", pos.Exchange, pos.Symbol, fetchErr)
			continue
		}
		currentPrice := decimal.NewFromFloat(ticker.GetPrice())
		if currentPrice.LessThanOrEqual(decimal.Zero) {
			summary.Errors++
			continue
		}

		newStop, newTake, changed := m.computeTargets(pos, currentPrice, now)
		unrealized := calculateUnrealizedPnL(pos.Side, pos.EntryPrice, currentPrice, pos.Size)
		if changed {
			m.logger.Printf(
				"[PROTECTION] Updating position_id=%q %s %s stop=%s->%s take=%s->%s last=%s",
				pos.PositionID,
				pos.Exchange,
				pos.Symbol,
				pos.StopLoss.String(),
				newStop.String(),
				pos.TakeProfit.String(),
				newTake.String(),
				currentPrice.String(),
			)
		}
		if changed && m.protection != nil {
			if m.shouldSkipMissingRetry(pos, now) {
				m.logger.Printf(
					"[PROTECTION] Skipping stale protection retry due to cooldown for %s %s %s",
					pos.Exchange,
					pos.Symbol,
					pos.Side,
				)
				continue
			}
			if err := m.protection.SyncPositionProtection(ctx, pos.Exchange, pos, newStop, newTake); err != nil {
				if errors.Is(err, ErrProtectionSyncUnsupported) {
					m.logger.Printf("[PROTECTION] Exchange-side TP/SL sync unsupported for %s, persisting lifecycle-only update", pos.PositionID)
				} else if isPositionMissingProtectionError(err) {
					m.markMissingRetry(pos, now)
					if closeErr := m.reconcileMissingPosition(ctx, pos, err); closeErr != nil {
						summary.Errors++
						m.logger.Printf("[PROTECTION] Failed to reconcile stale lifecycle position %s: %v", pos.PositionID, closeErr)
						continue
					}
					continue
				} else {
					summary.Errors++
					m.logger.Printf("[PROTECTION] Failed to sync exchange-side TP/SL for %s: %v", pos.PositionID, err)
					continue
				}
			}
		}
		if err := m.lifecycle.UpdatePositionProtection(ctx, pos.PositionID, newStop, newTake, currentPrice, unrealized, now); err != nil {
			summary.Errors++
			m.logger.Printf("[PROTECTION] Failed to update protection for %s: %v", pos.PositionID, err)
			continue
		}
		if changed {
			summary.ProtectionsUpdated++
			if missingProtection {
				summary.RecoveredProtection++
			}
		}
	}

	return summary, nil
}

func (m *DynamicProtectionManager) computeTargets(pos ManagedOpenPosition, currentPrice decimal.Decimal, now time.Time) (decimal.Decimal, decimal.Decimal, bool) {
	stop := pos.StopLoss
	take := pos.TakeProfit
	entry := pos.EntryPrice
	changed := false

	if entry.LessThanOrEqual(decimal.Zero) {
		return stop, take, false
	}

	isLong := isLongSide(pos.Side)
	if stop.LessThanOrEqual(decimal.Zero) || take.LessThanOrEqual(decimal.Zero) {
		defaultStop, defaultTake := defaultExitLevels(entry.InexactFloat64(), normalizedSideForDefault(pos.Side))
		if stop.LessThanOrEqual(decimal.Zero) {
			stop = defaultStop
			changed = true
		}
		if take.LessThanOrEqual(decimal.Zero) {
			take = defaultTake
			changed = true
		}
	}

	if m.config.UpdateCooldown > 0 && !pos.ProtectionUpdatedAt.IsZero() && now.Sub(pos.ProtectionUpdatedAt) < m.config.UpdateCooldown {
		return stop, take, changed
	}

	profitPct := calculatePnLPercent(pos.Side, entry, currentPrice)
	if profitPct < m.config.ProfitActivationPct {
		return stop, take, changed
	}

	beBuffer := decimal.NewFromFloat(m.config.BreakevenBufferPct / 100)
	trail := decimal.NewFromFloat(m.config.TrailingStopPct / 100)
	tpDist := decimal.NewFromFloat(m.config.TakeProfitDistancePct / 100)

	if isLong {
		breakEvenStop := entry.Mul(decimal.NewFromInt(1).Add(beBuffer))
		trailingStop := currentPrice.Mul(decimal.NewFromInt(1).Sub(trail))
		candidateStop := maxDecimal(stop, breakEvenStop, trailingStop)
		candidateTake := maxDecimal(take, currentPrice.Mul(decimal.NewFromInt(1).Add(tpDist)))
		if candidateStop.GreaterThan(stop) && shouldApplyProtectionAdjustment(stop, candidateStop, m.config.MinAdjustmentPct) {
			stop = candidateStop
			changed = true
		}
		if candidateTake.GreaterThan(take) && shouldApplyProtectionAdjustment(take, candidateTake, m.config.MinAdjustmentPct) {
			take = candidateTake
			changed = true
		}
		if stop.GreaterThanOrEqual(currentPrice) {
			stop = currentPrice.Mul(decimal.NewFromFloat(0.999))
		}
		if take.LessThanOrEqual(currentPrice) {
			take = currentPrice.Mul(decimal.NewFromFloat(1.001))
		}
		return stop, take, changed
	}

	breakEvenStop := entry.Mul(decimal.NewFromInt(1).Sub(beBuffer))
	trailingStop := currentPrice.Mul(decimal.NewFromInt(1).Add(trail))
	candidateStop := minDecimal(stop, breakEvenStop, trailingStop)
	candidateTake := minDecimal(take, currentPrice.Mul(decimal.NewFromInt(1).Sub(tpDist)))
	if candidateStop.LessThan(stop) && shouldApplyProtectionAdjustment(stop, candidateStop, m.config.MinAdjustmentPct) {
		stop = candidateStop
		changed = true
	}
	if candidateTake.LessThan(take) && shouldApplyProtectionAdjustment(take, candidateTake, m.config.MinAdjustmentPct) {
		take = candidateTake
		changed = true
	}
	if stop.LessThanOrEqual(currentPrice) {
		stop = currentPrice.Mul(decimal.NewFromFloat(1.001))
	}
	if take.GreaterThanOrEqual(currentPrice) {
		take = currentPrice.Mul(decimal.NewFromFloat(0.999))
	}
	return stop, take, changed
}

func calculateUnrealizedPnL(side string, entryPrice, currentPrice, size decimal.Decimal) decimal.Decimal {
	if size.LessThanOrEqual(decimal.Zero) || entryPrice.LessThanOrEqual(decimal.Zero) || currentPrice.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero
	}
	diff := currentPrice.Sub(entryPrice)
	if isLongSide(side) {
		return diff.Mul(size)
	}
	return diff.Mul(size).Neg()
}

func calculatePnLPercent(side string, entryPrice, currentPrice decimal.Decimal) float64 {
	if entryPrice.LessThanOrEqual(decimal.Zero) || currentPrice.LessThanOrEqual(decimal.Zero) {
		return 0
	}
	move := currentPrice.Sub(entryPrice).Div(entryPrice).Mul(decimal.NewFromInt(100))
	if isLongSide(side) {
		return move.InexactFloat64()
	}
	return move.Neg().InexactFloat64()
}

func isLongSide(side string) bool {
	switch strings.ToLower(strings.TrimSpace(side)) {
	case "buy", "long", "open_long":
		return true
	default:
		return false
	}
}

func normalizedSideForDefault(side string) string {
	if isLongSide(side) {
		return "buy"
	}
	return "sell"
}

func maxDecimal(values ...decimal.Decimal) decimal.Decimal {
	best := values[0]
	for i := 1; i < len(values); i++ {
		if values[i].GreaterThan(best) {
			best = values[i]
		}
	}
	return best
}

func minDecimal(values ...decimal.Decimal) decimal.Decimal {
	best := values[0]
	for i := 1; i < len(values); i++ {
		if values[i].LessThan(best) {
			best = values[i]
		}
	}
	return best
}

func shouldApplyProtectionAdjustment(current, next decimal.Decimal, minAdjustmentPct float64) bool {
	if minAdjustmentPct <= 0 {
		return true
	}
	if current.LessThanOrEqual(decimal.Zero) {
		return true
	}
	deltaPct := next.Sub(current).Abs().Div(current).Mul(decimal.NewFromInt(100)).InexactFloat64()
	return deltaPct >= minAdjustmentPct
}

func isPositionMissingProtectionError(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(lower, "no position to close") ||
		strings.Contains(lower, "insufficient position") ||
		strings.Contains(lower, "code: 22002") ||
		strings.Contains(lower, "code: 43023")
}

func (m *DynamicProtectionManager) reconcileMissingPosition(
	ctx context.Context,
	pos ManagedOpenPosition,
	cause error,
) error {
	if m.lifecycle == nil {
		return nil
	}

	orderID := strings.TrimSpace(pos.OrderID)
	if orderID == "" {
		orderID = strings.TrimSpace(pos.PositionID)
	}
	if orderID == "" {
		return fmt.Errorf("cannot reconcile missing position without order identifier")
	}

	exitPrice := pos.LastPrice
	if exitPrice.LessThanOrEqual(decimal.Zero) {
		exitPrice = pos.EntryPrice
	}

	if err := m.lifecycle.RecordClosedOrder(ctx, LifecycleCloseRecord{
		OrderID:     orderID,
		ChatID:      pos.ChatID,
		Exchange:    pos.Exchange,
		Symbol:      pos.Symbol,
		Side:        pos.Side,
		MarketType:  pos.MarketType,
		Filled:      pos.Size.Abs(),
		EntryPrice:  pos.EntryPrice,
		ExitPrice:   exitPrice,
		RealizedPnL: decimal.Zero,
		Source:      "protection_exchange_missing",
		ClosedAt:    time.Now().UTC(),
	}); err != nil {
		return err
	}

	m.logger.Printf(
		"[PROTECTION] Reconciled stale lifecycle position %s (%s %s) as closed after exchange reported missing position: %v",
		pos.PositionID,
		pos.Exchange,
		pos.Symbol,
		cause,
	)
	return nil
}

func (m *DynamicProtectionManager) shouldSkipMissingRetry(pos ManagedOpenPosition, now time.Time) bool {
	cooldown := stateDriftRepairCooldown()
	if cooldown <= 0 {
		return false
	}
	key := stalePositionRetryKey(pos)
	if key == "|||" {
		return false
	}

	m.missingMu.Lock()
	defer m.missingMu.Unlock()
	if m.missingRetry == nil {
		m.missingRetry = make(map[string]time.Time)
		return false
	}

	for k, ts := range m.missingRetry {
		if now.Sub(ts) >= cooldown {
			delete(m.missingRetry, k)
		}
	}

	last, ok := m.missingRetry[key]
	if !ok {
		return false
	}
	return now.Sub(last) < cooldown
}

func (m *DynamicProtectionManager) markMissingRetry(pos ManagedOpenPosition, now time.Time) {
	key := stalePositionRetryKey(pos)
	if key == "|||" {
		return
	}
	m.missingMu.Lock()
	defer m.missingMu.Unlock()
	if m.missingRetry == nil {
		m.missingRetry = make(map[string]time.Time)
	}
	m.missingRetry[key] = now
}
