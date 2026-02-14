package phase_management

import (
	"context"
	"fmt"
	"sync"
	"time"

	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"
	"github.com/shopspring/decimal"
)

type PhaseManager struct {
	config          PhaseManagerConfig
	detector        *PhaseDetector
	adapter         *StrategyAdapter
	scaler          *CapitalScaler
	currentStrategy StrategyConfig
	currentRisk     RiskParameters
	portfolioGetter func() (decimal.Decimal, error)
	mu              sync.RWMutex
	logger          *zaplogrus.Logger
	stopCh          chan struct{}
	stopOnce        sync.Once
	wg              sync.WaitGroup
}

type PhaseManagerConfig struct {
	DetectorConfig PhaseDetectorConfig
	AdapterConfig  StrategyAdapterConfig
	CheckInterval  time.Duration
}

func DefaultPhaseManagerConfig() PhaseManagerConfig {
	return PhaseManagerConfig{
		DetectorConfig: DefaultPhaseDetectorConfig(),
		AdapterConfig:  DefaultStrategyAdapterConfig(),
		CheckInterval:  5 * time.Minute,
	}
}

func NewPhaseManager(config PhaseManagerConfig, logger *zaplogrus.Logger) *PhaseManager {
	if logger == nil {
		logger = zaplogrus.New()
	}

	adapter := NewStrategyAdapter(config.AdapterConfig)
	detector := NewPhaseDetector(config.DetectorConfig, logger)
	scaler := NewCapitalScaler(adapter, logger)

	return &PhaseManager{
		config:          config,
		detector:        detector,
		adapter:         adapter,
		scaler:          scaler,
		currentStrategy: adapter.SelectStrategy(PhaseBootstrap),
		currentRisk:     adapter.GetRiskParams(PhaseBootstrap),
		logger:          logger,
		stopCh:          make(chan struct{}),
	}
}

func (pm *PhaseManager) Start(ctx context.Context) error {
	pm.logger.Info("Starting phase manager")

	pm.wg.Add(1)
	go pm.run(ctx)

	return nil
}

func (pm *PhaseManager) Stop() {
	pm.logger.Info("Stopping phase manager")
	pm.stopOnce.Do(func() { close(pm.stopCh) })
	pm.wg.Wait()
}

func (pm *PhaseManager) run(ctx context.Context) {
	defer pm.wg.Done()

	ticker := time.NewTicker(pm.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-pm.stopCh:
			return
		case <-ticker.C:
			pm.checkPhaseTransition(ctx)
		}
	}
}

func (pm *PhaseManager) checkPhaseTransition(ctx context.Context) {
	pm.mu.RLock()
	getter := pm.portfolioGetter
	pm.mu.RUnlock()

	if getter == nil {
		return
	}

	portfolioValue, err := getter()
	if err != nil {
		pm.logger.WithError(err).Error("Failed to get portfolio value for phase check")
		return
	}

	event, transitioned := pm.detector.AttemptTransition(portfolioValue, "periodic check")
	if transitioned {
		pm.mu.Lock()
		pm.currentStrategy = pm.adapter.SelectStrategy(event.ToPhase)
		pm.currentRisk = pm.adapter.GetRiskParams(event.ToPhase)
		strategyName := pm.currentStrategy.Name
		pm.mu.Unlock()

		pm.logger.WithField("phase", event.ToPhase.String()).
			WithField("strategy", strategyName).
			WithField("portfolio_value", portfolioValue.String()).
			Info("Phase auto-transitioned during periodic check")
	}
}

func (pm *PhaseManager) SetPortfolioGetter(getter func() (decimal.Decimal, error)) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.portfolioGetter = getter
}

func (pm *PhaseManager) OnPortfolioValueUpdate(portfolioValue decimal.Decimal, reason string) PhaseTransitionEvent {
	event, transitioned := pm.detector.AttemptTransition(portfolioValue, reason)

	if transitioned {
		pm.mu.Lock()
		pm.currentStrategy = pm.adapter.SelectStrategy(event.ToPhase)
		pm.currentRisk = pm.adapter.GetRiskParams(event.ToPhase)
		strategyName := pm.currentStrategy.Name
		pm.mu.Unlock()

		pm.logger.WithField("phase", event.ToPhase.String()).
			WithField("strategy", strategyName).
			Info("Phase transitioned - strategy updated")
	}

	return event
}

func (pm *PhaseManager) GetCurrentPhase() Phase {
	return pm.detector.CurrentPhase()
}

func (pm *PhaseManager) GetCurrentStrategy() StrategyConfig {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.currentStrategy
}

func (pm *PhaseManager) GetCurrentRiskParams() RiskParameters {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.currentRisk
}

func (pm *PhaseManager) CalculatePositionSize(baseSize decimal.Decimal, confidence float64) decimal.Decimal {
	currentPhase := pm.detector.CurrentPhase()
	return pm.scaler.CalculatePositionSize(currentPhase, baseSize, confidence)
}

func (pm *PhaseManager) GetCapitalAllocation(totalCapital decimal.Decimal) AllocationConfig {
	currentPhase := pm.detector.CurrentPhase()
	return pm.scaler.GetCapitalAllocation(currentPhase, totalCapital)
}

func (pm *PhaseManager) RegisterPhaseTransitionHandler(handler PhaseTransitionHandler) {
	pm.detector.RegisterTransitionHandler(handler)
}

func (pm *PhaseManager) GetPhaseForValue(portfolioValue decimal.Decimal) Phase {
	return pm.detector.GetPhaseForValue(portfolioValue)
}

func (pm *PhaseManager) GetPhaseDuration() time.Duration {
	return pm.detector.PhaseDuration()
}

func (pm *PhaseManager) GetTransitionHistory() []PhaseTransitionEvent {
	return pm.detector.TransitionHistory()
}

func (pm *PhaseManager) ForcePhase(phase Phase, reason string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if err := pm.detector.SetPhase(phase, reason); err != nil {
		return fmt.Errorf("failed to set phase %v: %w", phase, err)
	}

	pm.currentStrategy = pm.adapter.SelectStrategy(phase)
	pm.currentRisk = pm.adapter.GetRiskParams(phase)

	return nil
}

func (pm *PhaseManager) GetDetector() *PhaseDetector {
	return pm.detector
}

func (pm *PhaseManager) GetAdapter() *StrategyAdapter {
	return pm.adapter
}

func (pm *PhaseManager) GetScaler() *CapitalScaler {
	return pm.scaler
}
