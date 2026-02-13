package phase_management

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestStrategyAdapter_SelectStrategy(t *testing.T) {
	config := DefaultStrategyAdapterConfig()
	adapter := NewStrategyAdapter(config)

	tests := []struct {
		name         string
		phase        Phase
		expectedType StrategyType
		expectedName string
	}{
		{"bootstrap", PhaseBootstrap, StrategyConservative, "conservative_bootstrap"},
		{"growth", PhaseGrowth, StrategyModerate, "moderate_growth"},
		{"scale", PhaseScale, StrategyModerate, "moderate_scale"},
		{"mature", PhaseMature, StrategyConservative, "conservative_mature"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			strategy := adapter.SelectStrategy(tt.phase)
			assert.Equal(t, tt.expectedType, strategy.Type)
			assert.Equal(t, tt.expectedName, strategy.Name)
		})
	}
}

func TestStrategyAdapter_GetRiskParams(t *testing.T) {
	config := DefaultStrategyAdapterConfig()
	adapter := NewStrategyAdapter(config)

	bootstrapRisk := adapter.GetRiskParams(PhaseBootstrap)
	assert.Equal(t, decimal.NewFromFloat(2.0), bootstrapRisk.MaxDailyLossPercent)
	assert.Equal(t, decimal.NewFromFloat(3.0), bootstrapRisk.MaxPositionLossPercent)

	matureRisk := adapter.GetRiskParams(PhaseMature)
	assert.Equal(t, decimal.NewFromFloat(1.5), matureRisk.MaxDailyLossPercent)
	assert.Equal(t, decimal.NewFromFloat(2.5), matureRisk.MaxPositionLossPercent)
}

func TestStrategyAdapter_GetPositionSizingRules(t *testing.T) {
	config := DefaultStrategyAdapterConfig()
	adapter := NewStrategyAdapter(config)

	bootstrapSizing := adapter.GetPositionSizingRules(PhaseBootstrap)
	assert.Equal(t, decimal.NewFromFloat(0.5), bootstrapSizing.BaseSizeMultiplier)
	assert.Equal(t, decimal.NewFromFloat(2000.0), bootstrapSizing.MaxPositionUSD)

	scaleSizing := adapter.GetPositionSizingRules(PhaseScale)
	assert.Equal(t, decimal.NewFromFloat(1.0), scaleSizing.BaseSizeMultiplier)
	assert.Equal(t, decimal.NewFromFloat(50000.0), scaleSizing.MaxPositionUSD)
}

func TestStrategyAdapter_GetAllocationConfig(t *testing.T) {
	config := DefaultStrategyAdapterConfig()
	adapter := NewStrategyAdapter(config)

	bootstrapAlloc := adapter.GetAllocationConfig(PhaseBootstrap)
	assert.Equal(t, decimal.NewFromFloat(80.0), bootstrapAlloc.PrimaryStrategyPercent)
	assert.Equal(t, 3, bootstrapAlloc.MaxConcurrentPositions)

	matureAlloc := adapter.GetAllocationConfig(PhaseMature)
	assert.Equal(t, decimal.NewFromFloat(50.0), matureAlloc.PrimaryStrategyPercent)
	assert.Equal(t, 10, matureAlloc.MaxConcurrentPositions)
}

func TestStrategyAdapter_UpdateStrategyConfig(t *testing.T) {
	config := DefaultStrategyAdapterConfig()
	adapter := NewStrategyAdapter(config)

	newConfig := StrategyConfig{
		Type:                StrategyAggressive,
		Name:                "aggressive_bootstrap",
		MaxPositions:        5,
		MaxExposurePercent:  decimal.NewFromFloat(30.0),
		MinSignalConfidence: 0.8,
		HoldTimeMax:         2 * time.Hour,
		RebalanceInterval:   30 * time.Minute,
	}

	adapter.UpdateStrategyConfig(PhaseBootstrap, newConfig)
	updated := adapter.SelectStrategy(PhaseBootstrap)

	assert.Equal(t, StrategyAggressive, updated.Type)
	assert.Equal(t, "aggressive_bootstrap", updated.Name)
}

func TestStrategyAdapter_UpdateRiskParams(t *testing.T) {
	config := DefaultStrategyAdapterConfig()
	adapter := NewStrategyAdapter(config)

	newRisk := RiskParameters{
		MaxDailyLossPercent:    decimal.NewFromFloat(5.0),
		MaxPositionLossPercent: decimal.NewFromFloat(10.0),
		MaxDrawdownPercent:     decimal.NewFromFloat(15.0),
		StopLossPercent:        decimal.NewFromFloat(5.0),
		TakeProfitPercent:      decimal.NewFromFloat(10.0),
		RiskPerTradePercent:    decimal.NewFromFloat(2.0),
	}

	adapter.UpdateRiskParams(PhaseBootstrap, newRisk)
	updated := adapter.GetRiskParams(PhaseBootstrap)

	assert.Equal(t, decimal.NewFromFloat(5.0), updated.MaxDailyLossPercent)
}

func TestStrategyAdapter_GetAllStrategies(t *testing.T) {
	config := DefaultStrategyAdapterConfig()
	adapter := NewStrategyAdapter(config)

	strategies := adapter.GetAllStrategies()
	assert.Len(t, strategies, 4)
	assert.Contains(t, strategies, PhaseBootstrap)
	assert.Contains(t, strategies, PhaseGrowth)
	assert.Contains(t, strategies, PhaseScale)
	assert.Contains(t, strategies, PhaseMature)
}

func TestStrategyType_String(t *testing.T) {
	tests := []struct {
		name     string
		strategy StrategyType
		expected string
	}{
		{"conservative", StrategyConservative, "conservative"},
		{"moderate", StrategyModerate, "moderate"},
		{"aggressive", StrategyAggressive, "aggressive"},
		{"unknown", StrategyType(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.strategy.String())
		})
	}
}
