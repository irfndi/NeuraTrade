package phase_management

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestCapitalScaler_CalculatePositionSize(t *testing.T) {
	config := DefaultStrategyAdapterConfig()
	adapter := NewStrategyAdapter(config)
	scaler := NewCapitalScaler(adapter, nil)

	tests := []struct {
		name       string
		phase      Phase
		baseSize   decimal.Decimal
		confidence float64
	}{
		{"bootstrap - low confidence", PhaseBootstrap, decimal.NewFromFloat(1000), 0.5},
		{"bootstrap - high confidence", PhaseBootstrap, decimal.NewFromFloat(1000), 0.9},
		{"growth - medium confidence", PhaseGrowth, decimal.NewFromFloat(5000), 0.7},
		{"scale - high confidence", PhaseScale, decimal.NewFromFloat(10000), 0.85},
		{"mature - low confidence", PhaseMature, decimal.NewFromFloat(20000), 0.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			size := scaler.CalculatePositionSize(tt.phase, tt.baseSize, tt.confidence)
			assert.True(t, size.GreaterThan(decimal.Zero))
		})
	}
}

func TestCapitalScaler_CalculatePositionSize_RespectsMinMax(t *testing.T) {
	config := DefaultStrategyAdapterConfig()
	adapter := NewStrategyAdapter(config)
	scaler := NewCapitalScaler(adapter, nil)

	rules := adapter.GetPositionSizingRules(PhaseBootstrap)

	tests := []struct {
		name       string
		baseSize   decimal.Decimal
		confidence float64
		expected   string
	}{
		{"below min", decimal.NewFromFloat(10), 1.0, "min"},
		{"above max", decimal.NewFromFloat(100000), 1.0, "max"},
		{"normal", decimal.NewFromFloat(1000), 0.7, "calculated"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			size := scaler.CalculatePositionSize(PhaseBootstrap, tt.baseSize, tt.confidence)

			switch tt.expected {
			case "min":
				assert.True(t, size.GreaterThanOrEqual(rules.MinPositionUSD))
			case "max":
				assert.True(t, size.LessThanOrEqual(rules.MaxPositionUSD))
			case "calculated":
				assert.True(t, size.GreaterThanOrEqual(rules.MinPositionUSD))
				assert.True(t, size.LessThanOrEqual(rules.MaxPositionUSD))
			}
		})
	}
}

func TestCapitalScaler_GetCapitalAllocation(t *testing.T) {
	config := DefaultStrategyAdapterConfig()
	adapter := NewStrategyAdapter(config)
	scaler := NewCapitalScaler(adapter, nil)

	totalCapital := decimal.NewFromFloat(100000)
	alloc := scaler.GetCapitalAllocation(PhaseGrowth, totalCapital)

	assert.Equal(t, decimal.NewFromFloat(70.0), alloc.PrimaryStrategyPercent)
	assert.Equal(t, 5, alloc.MaxConcurrentPositions)
}

func TestCapitalScaler_CalculateScaledPositionSize(t *testing.T) {
	config := DefaultStrategyAdapterConfig()
	adapter := NewStrategyAdapter(config)
	scaler := NewCapitalScaler(adapter, nil)

	totalCapital := decimal.NewFromFloat(100000)
	confidence := 0.8
	volatility := 0.5

	size := scaler.CalculateScaledPositionSize(PhaseScale, totalCapital, confidence, volatility)
	assert.True(t, size.GreaterThan(decimal.Zero))
}

func TestCapitalScaler_CalculateScaledPositionSize_WithoutVolatility(t *testing.T) {
	config := DefaultStrategyAdapterConfig()
	adapter := NewStrategyAdapter(config)
	scaler := NewCapitalScaler(adapter, nil)

	totalCapital := decimal.NewFromFloat(100000)
	confidence := 0.8
	volatility := 0.0

	size := scaler.CalculateScaledPositionSize(PhaseGrowth, totalCapital, confidence, volatility)
	assert.True(t, size.GreaterThan(decimal.Zero))
}

func TestCapitalScaler_GetMaxPositionSizeForPhase(t *testing.T) {
	config := DefaultStrategyAdapterConfig()
	adapter := NewStrategyAdapter(config)
	scaler := NewCapitalScaler(adapter, nil)

	maxBootstrap := scaler.GetMaxPositionSizeForPhase(PhaseBootstrap)
	assert.Equal(t, decimal.NewFromFloat(2000.0), maxBootstrap)

	maxScale := scaler.GetMaxPositionSizeForPhase(PhaseScale)
	assert.Equal(t, decimal.NewFromFloat(50000.0), maxScale)
}

func TestCapitalScaler_GetMinPositionSizeForPhase(t *testing.T) {
	config := DefaultStrategyAdapterConfig()
	adapter := NewStrategyAdapter(config)
	scaler := NewCapitalScaler(adapter, nil)

	minBootstrap := scaler.GetMinPositionSizeForPhase(PhaseBootstrap)
	assert.Equal(t, decimal.NewFromFloat(100.0), minBootstrap)

	minMature := scaler.GetMinPositionSizeForPhase(PhaseMature)
	assert.Equal(t, decimal.NewFromFloat(2000.0), minMature)
}

func TestCapitalScaler_ValidatePositionSize(t *testing.T) {
	config := DefaultStrategyAdapterConfig()
	adapter := NewStrategyAdapter(config)
	scaler := NewCapitalScaler(adapter, nil)

	tests := []struct {
		name        string
		phase       Phase
		size        decimal.Decimal
		expectError bool
	}{
		{"valid bootstrap size", PhaseBootstrap, decimal.NewFromFloat(500), false},
		{"too small for bootstrap", PhaseBootstrap, decimal.NewFromFloat(50), true},
		{"too large for bootstrap", PhaseBootstrap, decimal.NewFromFloat(5000), true},
		{"valid scale size", PhaseScale, decimal.NewFromFloat(10000), false},
		{"boundary min", PhaseBootstrap, decimal.NewFromFloat(100), false},
		{"boundary max", PhaseBootstrap, decimal.NewFromFloat(2000), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := scaler.ValidatePositionSize(tt.phase, tt.size)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
