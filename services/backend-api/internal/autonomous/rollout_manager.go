package autonomous

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
)

var (
	// ErrInvalidStage indicates an invalid stage transition.
	ErrInvalidStage = errors.New("invalid stage transition")
	// ErrPromotionCriteriaNotMet indicates criteria not met for promotion.
	ErrPromotionCriteriaNotMet = errors.New("promotion criteria not met")
	// ErrStrategyNotFound indicates the strategy was not found.
	ErrStrategyNotFound = errors.New("strategy not found")
	// ErrAlreadyInStage indicates strategy is already in target stage.
	ErrAlreadyInStage = errors.New("strategy already in target stage")
)

// DefaultPromotionCriteria returns default promotion criteria.
func DefaultPromotionCriteria() PromotionCriteria {
	return PromotionCriteria{
		MinTrades:        10,
		MinWinRate:       0.55,
		MaxSlippage:      decimal.NewFromFloat(0.5),
		MaxRejectionRate: 0.1,
		MinUptime:        95.0,
		DurationRequired: 24 * time.Hour,
	}
}

// StagedRolloutManager manages staged deployment of strategies.
type StagedRolloutManager struct {
	repo   StrategyRepository
	events EventPublisher
}

// NewStagedRolloutManager creates a new rollout manager.
func NewStagedRolloutManager(repo StrategyRepository, events EventPublisher) *StagedRolloutManager {
	return &StagedRolloutManager{
		repo:   repo,
		events: events,
	}
}

// InitializeRollout creates a new rollout in shadow stage.
func (m *StagedRolloutManager) InitializeRollout(
	ctx context.Context,
	strategyID string,
	criteria PromotionCriteria,
) (*RolloutState, error) {
	now := time.Now()
	state := &RolloutState{
		StrategyID:        strategyID,
		CurrentStage:      StageShadow,
		Status:            StatusActive,
		EnteredAt:         now,
		PromotionCriteria: criteria,
		Metrics: RolloutMetrics{
			LastUpdated: now,
		},
		History: []StageTransition{},
	}

	if err := m.repo.SaveRolloutState(ctx, state); err != nil {
		return nil, fmt.Errorf("failed to save rollout state: %w", err)
	}

	return state, nil
}

// GetRolloutState retrieves the current rollout state.
func (m *StagedRolloutManager) GetRolloutState(ctx context.Context, strategyID string) (*RolloutState, error) {
	return m.repo.GetRolloutState(ctx, strategyID)
}

// UpdateMetrics updates the performance metrics for a rollout.
func (m *StagedRolloutManager) UpdateMetrics(
	ctx context.Context,
	strategyID string,
	metrics RolloutMetrics,
) error {
	state, err := m.repo.GetRolloutState(ctx, strategyID)
	if err != nil {
		return fmt.Errorf("failed to get rollout state: %w", err)
	}
	if state == nil {
		return ErrStrategyNotFound
	}

	state.Metrics = metrics
	state.Metrics.LastUpdated = time.Now()

	return m.repo.SaveRolloutState(ctx, state)
}

// CheckPromotionEligibility checks if a strategy is eligible for promotion.
func (m *StagedRolloutManager) CheckPromotionEligibility(state *RolloutState) (bool, []string) {
	criteria := state.PromotionCriteria
	metrics := state.Metrics
	var failed []string

	// Check minimum trades
	if metrics.TotalTrades < criteria.MinTrades {
		failed = append(failed, fmt.Sprintf("insufficient trades: %d < %d",
			metrics.TotalTrades, criteria.MinTrades))
	}

	// Check win rate
	if metrics.WinRate < criteria.MinWinRate {
		failed = append(failed, fmt.Sprintf("win rate too low: %.2f%% < %.2f%%",
			metrics.WinRate*100, criteria.MinWinRate*100))
	}

	// Check slippage
	if !criteria.MaxSlippage.IsZero() && metrics.AvgSlippage.GreaterThan(criteria.MaxSlippage) {
		failed = append(failed, fmt.Sprintf("slippage too high: %s > %s",
			metrics.AvgSlippage.String(), criteria.MaxSlippage.String()))
	}

	// Check rejection rate
	if metrics.TotalTrades > 0 {
		rejectionRate := float64(metrics.RejectionCount) / float64(metrics.TotalTrades)
		if rejectionRate > criteria.MaxRejectionRate {
			failed = append(failed, fmt.Sprintf("rejection rate too high: %.2f%% > %.2f%%",
				rejectionRate*100, criteria.MaxRejectionRate*100))
		}
	}

	// Check uptime
	if metrics.UptimePercent < criteria.MinUptime {
		failed = append(failed, fmt.Sprintf("uptime too low: %.2f%% < %.2f%%",
			metrics.UptimePercent, criteria.MinUptime))
	}

	// Check duration in current stage
	timeInStage := time.Since(state.EnteredAt)
	if timeInStage < criteria.DurationRequired {
		failed = append(failed, fmt.Sprintf("insufficient time in stage: %v < %v",
			timeInStage.Round(time.Hour), criteria.DurationRequired))
	}

	return len(failed) == 0, failed
}

// Promote promotes a strategy to the next stage.
func (m *StagedRolloutManager) Promote(ctx context.Context, strategyID string, reason string) (*RolloutState, error) {
	state, err := m.repo.GetRolloutState(ctx, strategyID)
	if err != nil {
		return nil, fmt.Errorf("failed to get rollout state: %w", err)
	}
	if state == nil {
		return nil, ErrStrategyNotFound
	}

	// Determine next stage
	nextStage, err := m.getNextStage(state.CurrentStage)
	if err != nil {
		return nil, err
	}

	// Check if already at max stage
	if nextStage == state.CurrentStage {
		return nil, ErrAlreadyInStage
	}

	// Check promotion eligibility (can be bypassed with force)
	eligible, failedCriteria := m.CheckPromotionEligibility(state)
	if !eligible {
		return nil, fmt.Errorf("%w: %v", ErrPromotionCriteriaNotMet, failedCriteria)
	}

	// Record transition
	transition := StageTransition{
		FromStage:           state.CurrentStage,
		ToStage:             nextStage,
		Reason:              reason,
		Timestamp:           time.Now(),
		MetricsAtTransition: state.Metrics,
	}

	// Update state
	state.CurrentStage = nextStage
	state.EnteredAt = time.Now()
	state.Status = StatusActive
	state.History = append(state.History, transition)

	if err := m.repo.SaveRolloutState(ctx, state); err != nil {
		return nil, fmt.Errorf("failed to save rollout state: %w", err)
	}

	// Publish event
	if m.events != nil {
		_ = m.events.PublishStageTransition(ctx, &transition)
	}

	return state, nil
}

// Rollback rolls back a strategy to the previous stage.
func (m *StagedRolloutManager) Rollback(ctx context.Context, strategyID string, trigger RollbackTrigger, reason string) (*RolloutState, error) {
	state, err := m.repo.GetRolloutState(ctx, strategyID)
	if err != nil {
		return nil, fmt.Errorf("failed to get rollout state: %w", err)
	}
	if state == nil {
		return nil, ErrStrategyNotFound
	}

	// Determine previous stage
	prevStage := m.getPreviousStage(state.CurrentStage)

	// Record transition
	transition := StageTransition{
		FromStage:           state.CurrentStage,
		ToStage:             prevStage,
		Reason:              fmt.Sprintf("rollback: %s - %s", trigger, reason),
		Timestamp:           time.Now(),
		MetricsAtTransition: state.Metrics,
	}

	// Update state
	state.CurrentStage = prevStage
	state.EnteredAt = time.Now()
	state.Status = StatusRolledBack
	state.History = append(state.History, transition)

	if err := m.repo.SaveRolloutState(ctx, state); err != nil {
		return nil, fmt.Errorf("failed to save rollout state: %w", err)
	}

	// Publish event
	if m.events != nil {
		_ = m.events.PublishStageTransition(ctx, &transition)
	}

	return state, nil
}

// Pause pauses a strategy rollout.
func (m *StagedRolloutManager) Pause(ctx context.Context, strategyID string, reason string) error {
	state, err := m.repo.GetRolloutState(ctx, strategyID)
	if err != nil {
		return fmt.Errorf("failed to get rollout state: %w", err)
	}
	if state == nil {
		return ErrStrategyNotFound
	}

	state.Status = StatusPaused
	return m.repo.SaveRolloutState(ctx, state)
}

// Resume resumes a paused strategy rollout.
func (m *StagedRolloutManager) Resume(ctx context.Context, strategyID string) error {
	state, err := m.repo.GetRolloutState(ctx, strategyID)
	if err != nil {
		return fmt.Errorf("failed to get rollout state: %w", err)
	}
	if state == nil {
		return ErrStrategyNotFound
	}

	if state.Status == StatusPaused {
		state.Status = StatusActive
	}
	return m.repo.SaveRolloutState(ctx, state)
}

// getNextStage returns the next stage in the progression.
func (m *StagedRolloutManager) getNextStage(current RolloutStage) (RolloutStage, error) {
	switch current {
	case StageShadow:
		return StagePaper, nil
	case StagePaper:
		return StageLive, nil
	case StageLive:
		return StageLive, nil // Already at max
	default:
		return "", ErrInvalidStage
	}
}

// getPreviousStage returns the previous stage in the progression.
func (m *StagedRolloutManager) getPreviousStage(current RolloutStage) RolloutStage {
	switch current {
	case StageLive:
		return StagePaper
	case StagePaper:
		return StageShadow
	case StageShadow:
		return StageShadow // Already at min
	default:
		return StageShadow
	}
}
