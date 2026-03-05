package autonomous

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// DefaultRollbackConfig returns default rollback configuration.
func DefaultRollbackConfig() RollbackConfig {
	return RollbackConfig{
		PnLThreshold:           decimal.NewFromFloat(-500), // -$500
		SlippageThreshold:      decimal.NewFromFloat(1.0),  // 1%
		RejectionRateThreshold: 0.3,                        // 30%
		MaxDrawdownThreshold:   decimal.NewFromFloat(10.0), // 10%
		ConsecutiveLossLimit:   5,
		CooldownPeriod:         1 * time.Hour,
		GracefulRollback:       true,
	}
}

// AutoRollbackEngine monitors strategies and triggers automatic rollbacks.
type AutoRollbackEngine struct {
	config         RollbackConfig
	repo           StrategyRepository
	rollout        *StagedRolloutManager
	events         EventPublisher
	exchange       ExchangeConnector
	cooldowns      map[string]time.Time
	cooldownsMutex sync.RWMutex
}

// NewAutoRollbackEngine creates a new auto rollback engine.
func NewAutoRollbackEngine(
	config RollbackConfig,
	repo StrategyRepository,
	rollout *StagedRolloutManager,
	events EventPublisher,
	exchange ExchangeConnector,
) *AutoRollbackEngine {
	return &AutoRollbackEngine{
		config:    config,
		repo:      repo,
		rollout:   rollout,
		events:    events,
		exchange:  exchange,
		cooldowns: make(map[string]time.Time),
	}
}

// Evaluate checks if a rollback should be triggered for a strategy.
func (e *AutoRollbackEngine) Evaluate(ctx context.Context, strategyID string, metrics RolloutMetrics) (*RollbackEvent, error) {
	strategyID = strings.TrimSpace(strategyID)
	if strategyID == "" {
		return nil, fmt.Errorf("strategyID is required")
	}

	// Check cooldown
	if e.isOnCooldown(strategyID) {
		return nil, nil
	}
	if e.rollout == nil {
		return nil, fmt.Errorf("rollout manager is required for rollback evaluation")
	}

	// Get current rollout state
	state, err := e.rollout.GetRolloutState(ctx, strategyID)
	if err != nil {
		return nil, fmt.Errorf("failed to get rollout state: %w", err)
	}
	if state == nil || state.Status != StatusActive {
		return nil, nil
	}

	// Check each trigger condition
	trigger, reason := e.checkTriggers(metrics)
	if trigger == "" {
		return nil, nil
	}

	// Execute rollback
	return e.executeRollback(ctx, strategyID, state, trigger, reason)
}

// CheckTriggersOnly checks triggers without executing rollback.
func (e *AutoRollbackEngine) CheckTriggersOnly(metrics RolloutMetrics) (RollbackTrigger, string) {
	return e.checkTriggers(metrics)
}

// checkTriggers checks all trigger conditions.
func (e *AutoRollbackEngine) checkTriggers(metrics RolloutMetrics) (RollbackTrigger, string) {
	// Check PnL breach
	if metrics.TotalPnL.LessThan(e.config.PnLThreshold) {
		return TriggerPnLBreach, fmt.Sprintf("PnL %s below threshold %s",
			metrics.TotalPnL.String(), e.config.PnLThreshold.String())
	}

	// Check slippage spike
	if metrics.AvgSlippage.GreaterThan(e.config.SlippageThreshold) {
		return TriggerSlippageSpike, fmt.Sprintf("slippage %s%% exceeds threshold %s%%",
			metrics.AvgSlippage.String(), e.config.SlippageThreshold.String())
	}

	// Check rejection rate
	if metrics.TotalTrades > 0 {
		rejectionRate := float64(metrics.RejectionCount) / float64(metrics.TotalTrades)
		if rejectionRate > e.config.RejectionRateThreshold {
			return TriggerRejectionRate, fmt.Sprintf("rejection rate %.1f%% exceeds threshold %.1f%%",
				rejectionRate*100, e.config.RejectionRateThreshold*100)
		}
	}

	// Check max drawdown
	if metrics.MaxDrawdown.GreaterThan(e.config.MaxDrawdownThreshold) {
		return TriggerMaxDrawdown, fmt.Sprintf("drawdown %s%% exceeds threshold %s%%",
			metrics.MaxDrawdown.String(), e.config.MaxDrawdownThreshold.String())
	}

	// Check net losses (losing trades minus winning trades in the current rollout window).
	netLosses := netLossCount(metrics)
	if netLosses >= e.config.ConsecutiveLossLimit {
		return TriggerConsecutiveLoss, fmt.Sprintf(
			"net loss count %d exceeds limit %d",
			netLosses,
			e.config.ConsecutiveLossLimit,
		)
	}

	return "", ""
}

func netLossCount(metrics RolloutMetrics) int {
	net := metrics.LosingTrades - metrics.WinningTrades
	if net > 0 {
		return net
	}
	return 0
}

// executeRollback executes the rollback process.
func (e *AutoRollbackEngine) executeRollback(
	ctx context.Context,
	strategyID string,
	state *RolloutState,
	trigger RollbackTrigger,
	reason string,
) (*RollbackEvent, error) {
	var actionsTaken []string

	// If graceful rollback, cancel orders and flatten positions
	if e.config.GracefulRollback && e.exchange != nil && state.CurrentStage == StageLive {
		// Cancel all orders
		if err := e.exchange.CancelAllOrders(ctx, strategyID, ""); err != nil {
			actionsTaken = append(actionsTaken, fmt.Sprintf("cancel_orders_failed: %v", err))
		} else {
			actionsTaken = append(actionsTaken, "cancelled_all_orders")
		}

		// Flatten positions
		if err := e.exchange.FlattenPositions(ctx, strategyID, ""); err != nil {
			actionsTaken = append(actionsTaken, fmt.Sprintf("flatten_positions_failed: %v", err))
		} else {
			actionsTaken = append(actionsTaken, "flattened_positions")
		}
	}

	// Roll back to previous stage
	newState, err := e.rollout.Rollback(ctx, strategyID, trigger, reason)
	if err != nil {
		return nil, fmt.Errorf("failed to execute rollback: %w", err)
	}
	actionsTaken = append(actionsTaken, fmt.Sprintf("rolled_back_to_%s", newState.CurrentStage))

	// Create rollback event
	event := &RollbackEvent{
		ID:                "rb_" + uuid.New().String()[:8],
		StrategyID:        strategyID,
		Trigger:           trigger,
		FromStage:         state.CurrentStage,
		ToStage:           newState.CurrentStage,
		Reason:            reason,
		MetricsAtRollback: state.Metrics,
		ActionsTaken:      actionsTaken,
		Timestamp:         time.Now(),
	}

	// Save event
	if e.repo != nil {
		if err := e.repo.SaveRollbackEvent(ctx, event); err != nil {
			log.Printf(
				"[AUTONOMY] SaveRollbackEvent failed (event_id=%s strategy_id=%s trigger=%s): %v",
				event.ID,
				event.StrategyID,
				event.Trigger,
				err,
			)
		}
	}

	// Publish event
	if e.events != nil {
		if err := e.events.PublishRollbackEvent(ctx, event); err != nil {
			log.Printf(
				"[AUTONOMY] PublishRollbackEvent failed (event_id=%s strategy_id=%s trigger=%s): %v",
				event.ID,
				event.StrategyID,
				event.Trigger,
				err,
			)
		}
	}

	// Set cooldown
	e.setCooldown(strategyID)

	return event, nil
}

// ForceRollback forces a rollback regardless of trigger conditions.
func (e *AutoRollbackEngine) ForceRollback(
	ctx context.Context,
	strategyID string,
	trigger RollbackTrigger,
	reason string,
) (*RollbackEvent, error) {
	strategyID = strings.TrimSpace(strategyID)
	if strategyID == "" {
		return nil, fmt.Errorf("strategyID is required")
	}

	if e.rollout == nil {
		return nil, fmt.Errorf("rollout manager is required for forced rollback")
	}
	state, err := e.rollout.GetRolloutState(ctx, strategyID)
	if err != nil {
		return nil, fmt.Errorf("failed to get rollout state: %w", err)
	}
	if state == nil {
		return nil, ErrStrategyNotFound
	}

	return e.executeRollback(ctx, strategyID, state, trigger, reason)
}

// isOnCooldown checks if a strategy is on cooldown.
func (e *AutoRollbackEngine) isOnCooldown(strategyID string) bool {
	e.cooldownsMutex.RLock()
	defer e.cooldownsMutex.RUnlock()

	cooldownEnd, exists := e.cooldowns[strategyID]
	if !exists {
		return false
	}
	return time.Now().Before(cooldownEnd)
}

// setCooldown sets the cooldown for a strategy.
func (e *AutoRollbackEngine) setCooldown(strategyID string) {
	e.cooldownsMutex.Lock()
	defer e.cooldownsMutex.Unlock()

	e.cooldowns[strategyID] = time.Now().Add(e.config.CooldownPeriod)
}

// ClearCooldown clears the cooldown for a strategy.
func (e *AutoRollbackEngine) ClearCooldown(strategyID string) {
	e.cooldownsMutex.Lock()
	defer e.cooldownsMutex.Unlock()

	delete(e.cooldowns, strategyID)
}

// GetRollbackHistory retrieves the rollback history for a strategy.
func (e *AutoRollbackEngine) GetRollbackHistory(ctx context.Context, strategyID string, limit int) ([]RollbackEvent, error) {
	if e.repo == nil {
		return nil, nil
	}
	return e.repo.GetRollbackHistory(ctx, strategyID, limit)
}
