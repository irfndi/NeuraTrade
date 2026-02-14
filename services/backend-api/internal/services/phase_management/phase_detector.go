package phase_management

import (
	"context"
	"fmt"
	"sync"
	"time"

	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"
	"github.com/shopspring/decimal"
)

type PhaseDetector struct {
	config             PhaseDetectorConfig
	currentPhase       Phase
	phaseStartTime     time.Time
	transitionHandlers []PhaseTransitionHandler
	transitionHistory  []PhaseTransitionEvent
	mu                 sync.RWMutex
	logger             *zaplogrus.Logger
}

type PhaseDetectorConfig struct {
	Thresholds         PhaseThresholds
	HysteresisPercent  decimal.Decimal
	MinPhaseDuration   time.Duration
	PersistenceEnabled bool
}

func DefaultPhaseDetectorConfig() PhaseDetectorConfig {
	return PhaseDetectorConfig{
		Thresholds:         DefaultPhaseThresholds(),
		HysteresisPercent:  decimal.NewFromFloat(5.0),
		MinPhaseDuration:   24 * time.Hour,
		PersistenceEnabled: true,
	}
}

func NewPhaseDetector(config PhaseDetectorConfig, logger *zaplogrus.Logger) *PhaseDetector {
	if logger == nil {
		logger = zaplogrus.New()
	}

	return &PhaseDetector{
		config:             config,
		currentPhase:       PhaseBootstrap,
		phaseStartTime:     time.Now(),
		transitionHandlers: make([]PhaseTransitionHandler, 0),
		transitionHistory:  make([]PhaseTransitionEvent, 0),
		logger:             logger,
	}
}

// DetectPhase determines the appropriate phase for a given portfolio value
func (pd *PhaseDetector) DetectPhase(portfolioValue decimal.Decimal) Phase {
	thresholds := pd.config.Thresholds

	switch {
	case portfolioValue.LessThanOrEqual(thresholds.BootstrapMax):
		return PhaseBootstrap
	case portfolioValue.LessThanOrEqual(thresholds.GrowthMax):
		return PhaseGrowth
	case portfolioValue.LessThanOrEqual(thresholds.ScaleMax):
		return PhaseScale
	default:
		return PhaseMature
	}
}

func (pd *PhaseDetector) ShouldTransition(currentPhase, newPhase Phase, portfolioValue decimal.Decimal) bool {
	pd.mu.RLock()
	defer pd.mu.RUnlock()
	return pd.shouldTransitionLocked(currentPhase, newPhase, portfolioValue)
}

func (pd *PhaseDetector) shouldTransitionLocked(currentPhase, newPhase Phase, portfolioValue decimal.Decimal) bool {
	if currentPhase == newPhase {
		return false
	}

	if time.Since(pd.phaseStartTime) < pd.config.MinPhaseDuration {
		pd.logger.WithField("phase", currentPhase.String()).
			WithField("elapsed", time.Since(pd.phaseStartTime)).
			Debug("Phase transition delayed: minimum duration not met")
		return false
	}

	thresholds := pd.config.Thresholds
	hysteresis := pd.config.HysteresisPercent.Div(decimal.NewFromInt(100))

	isUpward := newPhase > currentPhase

	var threshold decimal.Decimal
	switch {
	case isUpward:
		switch currentPhase {
		case PhaseBootstrap:
			threshold = thresholds.BootstrapMax
		case PhaseGrowth:
			threshold = thresholds.GrowthMax
		case PhaseScale:
			threshold = thresholds.ScaleMax
		}
	default:
		switch currentPhase {
		case PhaseGrowth:
			threshold = thresholds.BootstrapMax
		case PhaseScale:
			threshold = thresholds.GrowthMax
		case PhaseMature:
			threshold = thresholds.ScaleMax
		}
	}

	if isUpward {
		requiredValue := threshold.Add(threshold.Mul(hysteresis))
		if portfolioValue.LessThan(requiredValue) {
			return false
		}
	} else {
		requiredValue := threshold.Sub(threshold.Mul(hysteresis))
		if portfolioValue.GreaterThan(requiredValue) {
			return false
		}
	}

	return true
}

func (pd *PhaseDetector) AttemptTransition(portfolioValue decimal.Decimal, reason string) (PhaseTransitionEvent, bool) {
	pd.mu.Lock()

	newPhase := pd.DetectPhase(portfolioValue)

	if !pd.shouldTransitionLocked(pd.currentPhase, newPhase, portfolioValue) {
		pd.mu.Unlock()
		return PhaseTransitionEvent{}, false
	}

	event := PhaseTransitionEvent{
		FromPhase:      pd.currentPhase,
		ToPhase:        newPhase,
		PortfolioValue: portfolioValue,
		TransitionedAt: time.Now(),
		Reason:         reason,
	}

	oldPhase := pd.currentPhase
	pd.currentPhase = newPhase
	pd.phaseStartTime = event.TransitionedAt
	pd.transitionHistory = append(pd.transitionHistory, event)

	pd.logger.WithField("from_phase", oldPhase.String()).
		WithField("to_phase", newPhase.String()).
		WithField("portfolio_value", portfolioValue.String()).
		WithField("reason", reason).
		Info("Phase transition occurred")

	handlers := pd.getHandlersCopy()
	pd.mu.Unlock()
	pd.notifyHandlers(event, handlers)

	return event, true
}

func (pd *PhaseDetector) getHandlersCopy() []PhaseTransitionHandler {
	handlers := make([]PhaseTransitionHandler, len(pd.transitionHandlers))
	copy(handlers, pd.transitionHandlers)
	return handlers
}

func (pd *PhaseDetector) notifyHandlers(event PhaseTransitionEvent, handlers []PhaseTransitionHandler) {
	for _, handler := range handlers {
		go func(h PhaseTransitionHandler) {
			defer func() {
				if r := recover(); r != nil {
					pd.logger.WithField("panic", r).
						Error("Panic in phase transition handler")
				}
			}()
			h(event)
		}(handler)
	}
}

func (pd *PhaseDetector) RegisterTransitionHandler(handler PhaseTransitionHandler) {
	pd.mu.Lock()
	defer pd.mu.Unlock()
	pd.transitionHandlers = append(pd.transitionHandlers, handler)
}

func (pd *PhaseDetector) CurrentPhase() Phase {
	pd.mu.RLock()
	defer pd.mu.RUnlock()
	return pd.currentPhase
}

func (pd *PhaseDetector) PhaseDuration() time.Duration {
	pd.mu.RLock()
	defer pd.mu.RUnlock()
	return time.Since(pd.phaseStartTime)
}

// TransitionHistory returns the history of phase transitions
func (pd *PhaseDetector) TransitionHistory() []PhaseTransitionEvent {
	pd.mu.RLock()
	defer pd.mu.RUnlock()

	history := make([]PhaseTransitionEvent, len(pd.transitionHistory))
	copy(history, pd.transitionHistory)
	return history
}

func (pd *PhaseDetector) GetPhaseForValue(portfolioValue decimal.Decimal) Phase {
	return pd.DetectPhase(portfolioValue)
}

func (pd *PhaseDetector) SetPhase(phase Phase, reason string) error {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	if phase < PhaseBootstrap || phase > PhaseMature {
		return fmt.Errorf("invalid phase: %v", phase)
	}

	oldPhase := pd.currentPhase
	pd.currentPhase = phase
	pd.phaseStartTime = time.Now()

	pd.logger.WithField("from_phase", oldPhase.String()).
		WithField("to_phase", phase.String()).
		WithField("reason", reason).
		Warn("Phase manually set")

	return nil
}

func (pd *PhaseDetector) Save(ctx context.Context) error {
	pd.mu.RLock()
	defer pd.mu.RUnlock()

	if !pd.config.PersistenceEnabled {
		return nil
	}

	pd.logger.Debug("Phase state saved (persistence not implemented)")
	return nil
}

func (pd *PhaseDetector) Load(ctx context.Context, initialPortfolioValue decimal.Decimal) error {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	if !pd.config.PersistenceEnabled {
		pd.currentPhase = pd.DetectPhase(initialPortfolioValue)
		pd.phaseStartTime = time.Now()
		return nil
	}

	pd.logger.Debug("Phase state loaded (persistence not implemented)")
	return nil
}
