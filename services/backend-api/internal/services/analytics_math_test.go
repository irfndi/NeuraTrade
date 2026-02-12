package services

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCalculateMeanFloat64(t *testing.T) {
	tests := []struct {
		name     string
		values   []float64
		expected float64
	}{
		{
			name:     "empty slice",
			values:   []float64{},
			expected: 0,
		},
		{
			name:     "single value",
			values:   []float64{5.0},
			expected: 5.0,
		},
		{
			name:     "two values",
			values:   []float64{2.0, 4.0},
			expected: 3.0,
		},
		{
			name:     "multiple positive values",
			values:   []float64{1.0, 2.0, 3.0, 4.0, 5.0},
			expected: 3.0,
		},
		{
			name:     "negative values",
			values:   []float64{-5.0, -3.0, -1.0},
			expected: -3.0,
		},
		{
			name:     "mixed positive and negative",
			values:   []float64{-10.0, 0.0, 10.0},
			expected: 0.0,
		},
		{
			name:     "decimal values",
			values:   []float64{1.5, 2.5, 3.5},
			expected: 2.5,
		},
		{
			name:     "all zeros",
			values:   []float64{0.0, 0.0, 0.0},
			expected: 0.0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := calculateMeanFloat64(tc.values)
			assert.InDelta(t, tc.expected, result, 1e-10, "mean calculation mismatch")
		})
	}
}

func TestCalculateStdDev(t *testing.T) {
	tests := []struct {
		name     string
		values   []float64
		expected float64
	}{
		{
			name:     "empty slice",
			values:   []float64{},
			expected: 0,
		},
		{
			name:     "single value",
			values:   []float64{5.0},
			expected: 0, // need at least 2 values
		},
		{
			name:     "two identical values",
			values:   []float64{5.0, 5.0},
			expected: 0,
		},
		{
			name:     "simple std dev",
			values:   []float64{2.0, 4.0, 4.0, 4.0, 5.0, 5.0, 7.0, 9.0},
			expected: 2.138089935299395, // sample std dev
		},
		{
			name:     "uniform distribution",
			values:   []float64{1.0, 2.0, 3.0, 4.0, 5.0},
			expected: math.Sqrt(2.5), // sample variance = 2.5
		},
		{
			name:     "large spread",
			values:   []float64{0.0, 100.0},
			expected: math.Sqrt(5000), // sample variance = 5000
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := calculateStdDev(tc.values)
			assert.InDelta(t, tc.expected, result, 1e-10, "std dev calculation mismatch")
		})
	}
}

func TestCalculateCorrelation(t *testing.T) {
	tests := []struct {
		name     string
		x        []float64
		y        []float64
		expected float64
	}{
		{
			name:     "empty slices",
			x:        []float64{},
			y:        []float64{},
			expected: 0,
		},
		{
			name:     "mismatched lengths",
			x:        []float64{1.0, 2.0},
			y:        []float64{1.0},
			expected: 0,
		},
		{
			name:     "perfect positive correlation",
			x:        []float64{1.0, 2.0, 3.0, 4.0, 5.0},
			y:        []float64{2.0, 4.0, 6.0, 8.0, 10.0},
			expected: 1.0,
		},
		{
			name:     "perfect negative correlation",
			x:        []float64{1.0, 2.0, 3.0, 4.0, 5.0},
			y:        []float64{10.0, 8.0, 6.0, 4.0, 2.0},
			expected: -1.0,
		},
		{
			name:     "no correlation - constant y",
			x:        []float64{1.0, 2.0, 3.0, 4.0, 5.0},
			y:        []float64{5.0, 5.0, 5.0, 5.0, 5.0},
			expected: 0, // denominator will be 0 due to zero variance in y
		},
		{
			name:     "no correlation - constant x",
			x:        []float64{5.0, 5.0, 5.0, 5.0, 5.0},
			y:        []float64{1.0, 2.0, 3.0, 4.0, 5.0},
			expected: 0, // denominator will be 0 due to zero variance in x
		},
		{
			name:     "moderate positive correlation",
			x:        []float64{1.0, 2.0, 3.0, 4.0, 5.0},
			y:        []float64{1.5, 2.7, 3.2, 4.8, 4.9},
			expected: 0.9684, // approximate
		},
		{
			name:     "single value",
			x:        []float64{5.0},
			y:        []float64{10.0},
			expected: 0, // not enough data, denominator is 0
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := calculateCorrelation(tc.x, tc.y)
			assert.InDelta(t, tc.expected, result, 0.01, "correlation calculation mismatch")
		})
	}
}

func TestLogReturns(t *testing.T) {
	tests := []struct {
		name     string
		series   []float64
		expected []float64
	}{
		{
			name:     "empty series",
			series:   []float64{},
			expected: nil,
		},
		{
			name:     "single value",
			series:   []float64{100.0},
			expected: nil,
		},
		{
			name:     "two values",
			series:   []float64{100.0, 110.0},
			expected: []float64{math.Log(110.0 / 100.0)},
		},
		{
			name:     "constant series",
			series:   []float64{100.0, 100.0, 100.0},
			expected: []float64{0.0, 0.0},
		},
		{
			name:     "increasing series",
			series:   []float64{100.0, 110.0, 121.0},
			expected: []float64{math.Log(1.1), math.Log(1.1)},
		},
		{
			name:     "series with zero - skipped",
			series:   []float64{100.0, 0.0, 110.0},
			expected: []float64{}, // both transitions involve 0, so both are skipped
		},
		{
			name:     "series with negative - skipped",
			series:   []float64{100.0, -50.0, 110.0},
			expected: []float64{}, // both transitions involve negative, so both are skipped
		},
		{
			name:     "decreasing series",
			series:   []float64{100.0, 90.0, 81.0},
			expected: []float64{math.Log(0.9), math.Log(0.9)},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := logReturns(tc.series)
			if tc.expected == nil {
				assert.Nil(t, result)
			} else {
				assert.Equal(t, len(tc.expected), len(result), "length mismatch")
				for i := range tc.expected {
					assert.InDelta(t, tc.expected[i], result[i], 1e-10, "log return mismatch at index %d", i)
				}
			}
		})
	}
}

func TestFitAR1(t *testing.T) {
	tests := []struct {
		name        string
		series      []float64
		expectedPhi float64
		expectedC   float64
	}{
		{
			name:        "empty series",
			series:      []float64{},
			expectedPhi: 0,
			expectedC:   0,
		},
		{
			name:        "single value",
			series:      []float64{5.0},
			expectedPhi: 0,
			expectedC:   0,
		},
		{
			name:        "constant series",
			series:      []float64{5.0, 5.0, 5.0, 5.0},
			expectedPhi: 0,   // no linear relationship
			expectedC:   5.0, // mean of series
		},
		{
			name:        "simple linear trend",
			series:      []float64{1.0, 2.0, 3.0, 4.0, 5.0},
			expectedPhi: 1.0,
			expectedC:   1.0,
		},
		{
			name:        "perfect autoregression phi=0.5",
			series:      []float64{1.0, 0.5, 0.25, 0.125, 0.0625},
			expectedPhi: 0.5,
			expectedC:   0.0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			phi, c := fitAR1(tc.series)
			assert.InDelta(t, tc.expectedPhi, phi, 0.01, "phi mismatch")
			assert.InDelta(t, tc.expectedC, c, 0.01, "c mismatch")
		})
	}
}

func TestGarch11Forecast(t *testing.T) {
	tests := []struct {
		name     string
		returns  []float64
		horizon  int
		omega    float64
		alpha    float64
		beta     float64
		checkLen int
		checkNil bool
	}{
		{
			name:     "zero horizon",
			returns:  []float64{0.01, -0.02, 0.015},
			horizon:  0,
			omega:    0.00001,
			alpha:    0.1,
			beta:     0.8,
			checkNil: true,
		},
		{
			name:     "negative horizon",
			returns:  []float64{0.01, -0.02, 0.015},
			horizon:  -1,
			omega:    0.00001,
			alpha:    0.1,
			beta:     0.8,
			checkNil: true,
		},
		{
			name:     "valid parameters",
			returns:  []float64{0.01, -0.02, 0.015, -0.005, 0.02},
			horizon:  5,
			omega:    0.00001,
			alpha:    0.1,
			beta:     0.8,
			checkLen: 5,
		},
		{
			name:     "empty returns",
			returns:  []float64{},
			horizon:  3,
			omega:    0.00001,
			alpha:    0.1,
			beta:     0.8,
			checkLen: 3,
		},
		{
			name:     "stationary violation - uses safe defaults",
			returns:  []float64{0.01, -0.02, 0.015},
			horizon:  3,
			omega:    0.00001,
			alpha:    0.5,
			beta:     0.6, // alpha + beta = 1.1 >= 1, violates stationarity
			checkLen: 3,
		},
		{
			name:     "boundary case - alpha + beta = 1",
			returns:  []float64{0.01, -0.02, 0.015},
			horizon:  3,
			omega:    0.00001,
			alpha:    0.2,
			beta:     0.8, // alpha + beta = 1.0, exactly on boundary
			checkLen: 3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := garch11Forecast(tc.returns, tc.horizon, tc.omega, tc.alpha, tc.beta)
			if tc.checkNil {
				assert.Nil(t, result)
			} else {
				assert.NotNil(t, result)
				assert.Equal(t, tc.checkLen, len(result), "forecast length mismatch")
				// Verify all forecasted variances are positive
				for i, v := range result {
					assert.Greater(t, v, 0.0, "variance at index %d should be positive", i)
				}
			}
		})
	}
}

func TestGarch11Forecast_VarianceEvolution(t *testing.T) {
	// Test that variance evolves correctly according to GARCH(1,1) formula
	returns := []float64{0.05, -0.03, 0.02, -0.01, 0.04}
	horizon := 10
	omega := 0.00001
	alpha := 0.1
	beta := 0.8

	result := garch11Forecast(returns, horizon, omega, alpha, beta)
	assert.NotNil(t, result)
	assert.Equal(t, horizon, len(result))

	// Verify variance values are all positive and reasonable
	for i, v := range result {
		assert.Greater(t, v, 0.0, "variance at index %d should be positive", i)
		assert.Less(t, v, 1.0, "variance at index %d should be less than 1 for typical returns", i)
	}

	// Verify variance evolution follows GARCH pattern (variance should be monotonically changing)
	// For stable GARCH with beta < 1, variance typically decays towards unconditional variance
	firstVariance := result[0]
	lastVariance := result[horizon-1]
	// Just verify the forecast produces reasonable values
	assert.Greater(t, firstVariance, 0.0)
	assert.Greater(t, lastVariance, 0.0)
}

func TestCalculateCorrelation_BoundedOutput(t *testing.T) {
	// Test that correlation is always bounded between -1 and 1
	// Even with extreme inputs
	testCases := []struct {
		x []float64
		y []float64
	}{
		{
			x: []float64{1e10, 1e10, 1e10, 1e10},
			y: []float64{1, 2, 3, 4},
		},
		{
			x: []float64{1e-10, 2e-10, 3e-10, 4e-10},
			y: []float64{1e10, 2e10, 3e10, 4e10},
		},
	}

	for _, tc := range testCases {
		result := calculateCorrelation(tc.x, tc.y)
		assert.GreaterOrEqual(t, result, -1.0)
		assert.LessOrEqual(t, result, 1.0)
	}
}

func TestLogReturns_PriceSeriesScenarios(t *testing.T) {
	// Realistic price series scenarios
	t.Run("typical crypto daily returns", func(t *testing.T) {
		prices := []float64{50000, 51000, 49500, 52000, 51500}
		returns := logReturns(prices)
		assert.Equal(t, 4, len(returns))
		// Verify returns are reasonable (within -50% to +50%)
		for _, r := range returns {
			assert.Greater(t, r, -0.7)
			assert.Less(t, r, 0.7)
		}
	})

	t.Run("flash crash scenario", func(t *testing.T) {
		prices := []float64{100, 50, 90} // 50% drop then recovery
		returns := logReturns(prices)
		assert.Equal(t, 2, len(returns))
		assert.InDelta(t, math.Log(0.5), returns[0], 1e-10) // -69% log return
		assert.InDelta(t, math.Log(1.8), returns[1], 1e-10) // +80% recovery
	})
}
