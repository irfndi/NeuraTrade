package phase_management

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestNewPhaseManager(t *testing.T) {
	config := DefaultPhaseManagerConfig()
	manager := NewPhaseManager(config, nil)

	assert.NotNil(t, manager)
	assert.NotNil(t, manager.detector)
	assert.NotNil(t, manager.adapter)
	assert.NotNil(t, manager.scaler)
	assert.Equal(t, PhaseBootstrap, manager.GetCurrentPhase())
}

func TestPhaseManager_StartStop(t *testing.T) {
	config := DefaultPhaseManagerConfig()
	config.CheckInterval = 100 * time.Millisecond
	manager := NewPhaseManager(config, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := manager.Start(ctx)
	assert.NoError(t, err)

	time.Sleep(150 * time.Millisecond)

	manager.Stop()
}

func TestPhaseManager_OnPortfolioValueUpdate(t *testing.T) {
	config := DefaultPhaseManagerConfig()
	config.DetectorConfig.MinPhaseDuration = 0
	manager := NewPhaseManager(config, nil)

	event := manager.OnPortfolioValueUpdate(decimal.NewFromFloat(5000), "initial")
	assert.Empty(t, event.Reason)

	event = manager.OnPortfolioValueUpdate(decimal.NewFromFloat(10500), "growth")
	assert.Equal(t, PhaseGrowth, event.ToPhase)
	assert.Equal(t, "growth", event.Reason)

	strategy := manager.GetCurrentStrategy()
	assert.Equal(t, "moderate_growth", strategy.Name)
}

func TestPhaseManager_GetCurrentStrategy(t *testing.T) {
	config := DefaultPhaseManagerConfig()
	manager := NewPhaseManager(config, nil)

	strategy := manager.GetCurrentStrategy()
	assert.Equal(t, StrategyConservative, strategy.Type)
	assert.Equal(t, "conservative_bootstrap", strategy.Name)
}

func TestPhaseManager_GetCurrentRiskParams(t *testing.T) {
	config := DefaultPhaseManagerConfig()
	manager := NewPhaseManager(config, nil)

	risk := manager.GetCurrentRiskParams()
	assert.Equal(t, decimal.NewFromFloat(2.0), risk.MaxDailyLossPercent)
}

func TestPhaseManager_CalculatePositionSize(t *testing.T) {
	config := DefaultPhaseManagerConfig()
	manager := NewPhaseManager(config, nil)

	baseSize := decimal.NewFromFloat(1000)
	confidence := 0.8

	size := manager.CalculatePositionSize(baseSize, confidence)
	assert.True(t, size.GreaterThan(decimal.Zero))
}

func TestPhaseManager_GetCapitalAllocation(t *testing.T) {
	config := DefaultPhaseManagerConfig()
	manager := NewPhaseManager(config, nil)

	totalCapital := decimal.NewFromFloat(50000)
	alloc := manager.GetCapitalAllocation(totalCapital)

	assert.Equal(t, decimal.NewFromFloat(80.0), alloc.PrimaryStrategyPercent)
}

func TestPhaseManager_RegisterPhaseTransitionHandler(t *testing.T) {
	config := DefaultPhaseManagerConfig()
	config.DetectorConfig.MinPhaseDuration = 0
	manager := NewPhaseManager(config, nil)

	handlerCalled := false
	manager.RegisterPhaseTransitionHandler(func(event PhaseTransitionEvent) {
		handlerCalled = true
	})

	manager.OnPortfolioValueUpdate(decimal.NewFromFloat(10500), "test")

	time.Sleep(100 * time.Millisecond)
	assert.True(t, handlerCalled)
}

func TestPhaseManager_GetPhaseForValue(t *testing.T) {
	config := DefaultPhaseManagerConfig()
	manager := NewPhaseManager(config, nil)

	phase := manager.GetPhaseForValue(decimal.NewFromFloat(75000))
	assert.Equal(t, PhaseScale, phase)
}

func TestPhaseManager_GetPhaseDuration(t *testing.T) {
	config := DefaultPhaseManagerConfig()
	manager := NewPhaseManager(config, nil)

	duration := manager.GetPhaseDuration()
	assert.Greater(t, duration, time.Duration(0))
}

func TestPhaseManager_GetTransitionHistory(t *testing.T) {
	config := DefaultPhaseManagerConfig()
	config.DetectorConfig.MinPhaseDuration = 0
	manager := NewPhaseManager(config, nil)

	history := manager.GetTransitionHistory()
	assert.Empty(t, history)

	manager.OnPortfolioValueUpdate(decimal.NewFromFloat(10500), "test")

	history = manager.GetTransitionHistory()
	assert.Len(t, history, 1)
}

func TestPhaseManager_ForcePhase(t *testing.T) {
	config := DefaultPhaseManagerConfig()
	manager := NewPhaseManager(config, nil)

	err := manager.ForcePhase(PhaseScale, "manual override")
	assert.NoError(t, err)
	assert.Equal(t, PhaseScale, manager.GetCurrentPhase())

	strategy := manager.GetCurrentStrategy()
	assert.Equal(t, "moderate_scale", strategy.Name)

	err = manager.ForcePhase(Phase(-1), "invalid")
	assert.Error(t, err)
}

func TestPhaseManager_GetComponents(t *testing.T) {
	config := DefaultPhaseManagerConfig()
	manager := NewPhaseManager(config, nil)

	assert.NotNil(t, manager.GetDetector())
	assert.NotNil(t, manager.GetAdapter())
	assert.NotNil(t, manager.GetScaler())
}

func TestPhaseManager_PhaseTransitionUpdatesStrategy(t *testing.T) {
	config := DefaultPhaseManagerConfig()
	config.DetectorConfig.MinPhaseDuration = 0
	manager := NewPhaseManager(config, nil)

	initialStrategy := manager.GetCurrentStrategy()
	assert.Equal(t, "conservative_bootstrap", initialStrategy.Name)

	manager.OnPortfolioValueUpdate(decimal.NewFromFloat(25000), "growth")

	newStrategy := manager.GetCurrentStrategy()
	assert.Equal(t, "moderate_growth", newStrategy.Name)

	newRisk := manager.GetCurrentRiskParams()
	assert.Equal(t, decimal.NewFromFloat(3.0), newRisk.MaxDailyLossPercent)
}
