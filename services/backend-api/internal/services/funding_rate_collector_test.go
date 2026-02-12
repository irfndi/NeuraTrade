package services

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

// TestNewFundingRateCollector tests the constructor
func TestNewFundingRateCollector(t *testing.T) {
	tests := []struct {
		name         string
		collectorCfg *FundingRateCollectorConfig
		expectedDays int
		expectedExch []string
	}{
		{
			name:         "nil config uses defaults",
			collectorCfg: nil,
			expectedDays: 90,
			expectedExch: []string{"binance", "bybit"},
		},
		{
			name: "custom config",
			collectorCfg: &FundingRateCollectorConfig{
				RetentionDays:      30,
				CollectionInterval: 5 * time.Minute,
				TargetExchanges:    []string{"binance", "okx"},
			},
			expectedDays: 30,
			expectedExch: []string{"binance", "okx"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector := NewFundingRateCollector(nil, nil, nil, nil, tt.collectorCfg, nil)

			assert.NotNil(t, collector)
			assert.Equal(t, tt.expectedDays, collector.retentionDays)
			assert.Equal(t, tt.expectedExch, collector.targetExchanges)
		})
	}
}

// TestFundingRateCollector_StartStop tests the lifecycle methods
func TestFundingRateCollector_StartStop(t *testing.T) {
	collector := NewFundingRateCollector(nil, nil, nil, nil, nil, nil)

	// Initially not running
	assert.False(t, collector.IsRunning())

	// Start should fail without proper dependencies, but we test the logic
	// Note: In a real test, we'd mock the dependencies properly
}

// TestFundingRateCollector_IsRunning tests the running state
func TestFundingRateCollector_IsRunning(t *testing.T) {
	collector := NewFundingRateCollector(nil, nil, nil, nil, nil, nil)

	// Not running initially
	assert.False(t, collector.IsRunning())

	// Manually set running for testing
	collector.mu.Lock()
	collector.running = true
	collector.mu.Unlock()

	assert.True(t, collector.IsRunning())

	// Reset
	collector.mu.Lock()
	collector.running = false
	collector.mu.Unlock()

	assert.False(t, collector.IsRunning())
}

// TestFundingRateCollector_CalculateMean tests the mean calculation
func TestFundingRateCollector_CalculateMean(t *testing.T) {
	collector := NewFundingRateCollector(nil, nil, nil, nil, nil, nil)

	tests := []struct {
		name     string
		rates    []decimal.Decimal
		expected decimal.Decimal
	}{
		{
			name:     "empty slice",
			rates:    []decimal.Decimal{},
			expected: decimal.Zero,
		},
		{
			name: "single value",
			rates: []decimal.Decimal{
				decimal.NewFromFloat(0.0001),
			},
			expected: decimal.NewFromFloat(0.0001),
		},
		{
			name: "multiple values",
			rates: []decimal.Decimal{
				decimal.NewFromFloat(0.0001),
				decimal.NewFromFloat(0.0002),
				decimal.NewFromFloat(0.0003),
			},
			expected: decimal.NewFromFloat(0.0002),
		},
		{
			name: "negative values",
			rates: []decimal.Decimal{
				decimal.NewFromFloat(-0.0001),
				decimal.NewFromFloat(0.0001),
			},
			expected: decimal.Zero,
		},
		{
			name: "mixed values",
			rates: []decimal.Decimal{
				decimal.NewFromFloat(0.0001),
				decimal.NewFromFloat(-0.0002),
				decimal.NewFromFloat(0.0004),
			},
			expected: decimal.NewFromFloat(0.0001),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.calculateMean(tt.rates)
			assert.True(t, result.Equal(tt.expected), "expected %s, got %s", tt.expected, result)
		})
	}
}

// TestFundingRateCollector_CalculateStdDev tests the standard deviation calculation
func TestFundingRateCollector_CalculateStdDev(t *testing.T) {
	collector := NewFundingRateCollector(nil, nil, nil, nil, nil, nil)

	tests := []struct {
		name          string
		rates         []decimal.Decimal
		mean          decimal.Decimal
		expectedZero  bool
		expectedRange [2]float64 // min and max expected values
	}{
		{
			name:         "empty slice",
			rates:        []decimal.Decimal{},
			mean:         decimal.Zero,
			expectedZero: true,
		},
		{
			name: "single value",
			rates: []decimal.Decimal{
				decimal.NewFromFloat(0.0001),
			},
			mean:         decimal.NewFromFloat(0.0001),
			expectedZero: true,
		},
		{
			name: "identical values",
			rates: []decimal.Decimal{
				decimal.NewFromFloat(0.0001),
				decimal.NewFromFloat(0.0001),
				decimal.NewFromFloat(0.0001),
			},
			mean:         decimal.NewFromFloat(0.0001),
			expectedZero: true,
		},
		{
			name: "varying values",
			rates: []decimal.Decimal{
				decimal.NewFromFloat(0.0001),
				decimal.NewFromFloat(0.0002),
				decimal.NewFromFloat(0.0003),
			},
			mean:          decimal.NewFromFloat(0.0002),
			expectedZero:  false,
			expectedRange: [2]float64{0.00009, 0.00011}, // approximately 0.0001
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.calculateStdDev(tt.rates, tt.mean)

			if tt.expectedZero {
				assert.True(t, result.IsZero(), "expected zero, got %s", result)
			} else {
				resultFloat, _ := result.Float64()
				assert.True(t, resultFloat >= tt.expectedRange[0] && resultFloat <= tt.expectedRange[1],
					"expected %f to be between %f and %f", resultFloat, tt.expectedRange[0], tt.expectedRange[1])
			}
		})
	}
}

// TestFundingRateCollector_CalculateMinMax tests the min/max calculation
func TestFundingRateCollector_CalculateMinMax(t *testing.T) {
	collector := NewFundingRateCollector(nil, nil, nil, nil, nil, nil)

	tests := []struct {
		name        string
		rates       []decimal.Decimal
		expectedMin decimal.Decimal
		expectedMax decimal.Decimal
	}{
		{
			name:        "empty slice",
			rates:       []decimal.Decimal{},
			expectedMin: decimal.Zero,
			expectedMax: decimal.Zero,
		},
		{
			name: "single value",
			rates: []decimal.Decimal{
				decimal.NewFromFloat(0.0001),
			},
			expectedMin: decimal.NewFromFloat(0.0001),
			expectedMax: decimal.NewFromFloat(0.0001),
		},
		{
			name: "multiple values",
			rates: []decimal.Decimal{
				decimal.NewFromFloat(0.0003),
				decimal.NewFromFloat(0.0001),
				decimal.NewFromFloat(0.0002),
			},
			expectedMin: decimal.NewFromFloat(0.0001),
			expectedMax: decimal.NewFromFloat(0.0003),
		},
		{
			name: "negative and positive",
			rates: []decimal.Decimal{
				decimal.NewFromFloat(-0.0002),
				decimal.NewFromFloat(0.0001),
				decimal.NewFromFloat(0.0003),
			},
			expectedMin: decimal.NewFromFloat(-0.0002),
			expectedMax: decimal.NewFromFloat(0.0003),
		},
		{
			name: "all negative",
			rates: []decimal.Decimal{
				decimal.NewFromFloat(-0.0001),
				decimal.NewFromFloat(-0.0003),
				decimal.NewFromFloat(-0.0002),
			},
			expectedMin: decimal.NewFromFloat(-0.0003),
			expectedMax: decimal.NewFromFloat(-0.0001),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			min, max := collector.calculateMinMax(tt.rates)
			assert.True(t, min.Equal(tt.expectedMin), "min: expected %s, got %s", tt.expectedMin, min)
			assert.True(t, max.Equal(tt.expectedMax), "max: expected %s, got %s", tt.expectedMax, max)
		})
	}
}

// TestFundingRateCollector_CalculateTrend tests the trend calculation
func TestFundingRateCollector_CalculateTrend(t *testing.T) {
	collector := NewFundingRateCollector(nil, nil, nil, nil, nil, nil)

	tests := []struct {
		name              string
		rates             []decimal.Decimal
		expectedDirection string
	}{
		{
			name:              "empty slice",
			rates:             []decimal.Decimal{},
			expectedDirection: "stable",
		},
		{
			name: "single value",
			rates: []decimal.Decimal{
				decimal.NewFromFloat(0.0001),
			},
			expectedDirection: "stable",
		},
		{
			name: "two values",
			rates: []decimal.Decimal{
				decimal.NewFromFloat(0.0001),
				decimal.NewFromFloat(0.0002),
			},
			expectedDirection: "stable",
		},
		{
			name: "increasing trend",
			rates: []decimal.Decimal{
				decimal.NewFromFloat(0.0001),
				decimal.NewFromFloat(0.0002),
				decimal.NewFromFloat(0.0003),
				decimal.NewFromFloat(0.0004),
			},
			expectedDirection: "increasing",
		},
		{
			name: "decreasing trend",
			rates: []decimal.Decimal{
				decimal.NewFromFloat(0.0004),
				decimal.NewFromFloat(0.0003),
				decimal.NewFromFloat(0.0002),
				decimal.NewFromFloat(0.0001),
			},
			expectedDirection: "decreasing",
		},
		{
			name: "constant values",
			rates: []decimal.Decimal{
				decimal.NewFromFloat(0.0001),
				decimal.NewFromFloat(0.0001),
				decimal.NewFromFloat(0.0001),
			},
			expectedDirection: "stable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			direction, strength := collector.calculateTrend(tt.rates)
			assert.Equal(t, tt.expectedDirection, direction)
			assert.True(t, strength.GreaterThanOrEqual(decimal.Zero), "strength should be non-negative")
			assert.True(t, strength.LessThanOrEqual(decimal.NewFromFloat(1.0)), "strength should be <= 1.0")
		})
	}
}

// TestFundingRateCollector_CalculateVolatilityScore tests the volatility score calculation
func TestFundingRateCollector_CalculateVolatilityScore(t *testing.T) {
	collector := NewFundingRateCollector(nil, nil, nil, nil, nil, nil)

	tests := []struct {
		name        string
		stdDev      decimal.Decimal
		expectedMax float64
	}{
		{
			name:        "zero std dev",
			stdDev:      decimal.Zero,
			expectedMax: 0,
		},
		{
			name:        "small std dev",
			stdDev:      decimal.NewFromFloat(0.001),
			expectedMax: 10, // 0.001 * 5000 = 5
		},
		{
			name:        "moderate std dev",
			stdDev:      decimal.NewFromFloat(0.01),
			expectedMax: 100, // 0.01 * 5000 = 50
		},
		{
			name:        "large std dev - capped at 100",
			stdDev:      decimal.NewFromFloat(0.05),
			expectedMax: 100, // Would be 250, but capped at 100
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.calculateVolatilityScore(tt.stdDev)
			resultFloat, _ := result.Float64()

			assert.True(t, resultFloat >= 0, "score should be non-negative")
			assert.True(t, resultFloat <= tt.expectedMax+1, "score should not exceed expected max")
			assert.True(t, resultFloat <= 100, "score should be capped at 100")
		})
	}
}

// TestFundingRateCollectorConfig tests the configuration struct
func TestFundingRateCollectorConfig(t *testing.T) {
	config := &FundingRateCollectorConfig{
		RetentionDays:      60,
		CollectionInterval: 10 * time.Minute,
		TargetExchanges:    []string{"binance", "bybit", "okx"},
	}

	assert.Equal(t, 60, config.RetentionDays)
	assert.Equal(t, 10*time.Minute, config.CollectionInterval)
	assert.Equal(t, 3, len(config.TargetExchanges))
	assert.Contains(t, config.TargetExchanges, "binance")
	assert.Contains(t, config.TargetExchanges, "bybit")
	assert.Contains(t, config.TargetExchanges, "okx")
}

// TestFundingRateCollector_ConcurrentAccess tests thread safety
func TestFundingRateCollector_ConcurrentAccess(t *testing.T) {
	collector := NewFundingRateCollector(nil, nil, nil, nil, nil, nil)

	// Test concurrent access to IsRunning
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				_ = collector.IsRunning()
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				collector.mu.Lock()
				collector.running = !collector.running
				collector.mu.Unlock()
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 20; i++ {
		<-done
	}
}
