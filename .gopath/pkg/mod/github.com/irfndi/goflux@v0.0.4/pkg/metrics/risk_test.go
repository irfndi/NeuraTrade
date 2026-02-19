package metrics_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/irfndi/goflux/pkg/metrics"
)

func TestValueAtRisk(t *testing.T) {
	t.Run("returns 0 for empty returns", func(t *testing.T) {
		varr := metrics.ValueAtRisk([]float64{}, 0.95)
		assert.Equal(t, 0.0, varr)
	})

	t.Run("calculates VaR for 95% confidence", func(t *testing.T) {
		returns := []float64{-0.05, -0.03, -0.02, -0.01, 0.01, 0.02, 0.03, 0.04}
		varr := metrics.ValueAtRisk(returns, 0.95)
		assert.Greater(t, varr, 0.0)
		assert.LessOrEqual(t, varr, 0.05)
	})

	t.Run("calculates VaR for 99% confidence", func(t *testing.T) {
		returns := []float64{-0.05, -0.03, -0.02, -0.01, 0.01, 0.02, 0.03, 0.04}
		varr := metrics.ValueAtRisk(returns, 0.99)
		assert.Greater(t, varr, 0.0)
	})

	t.Run("handles index bounds correctly", func(t *testing.T) {
		returns := []float64{-0.05}
		varr := metrics.ValueAtRisk(returns, 0.99)
		assert.Equal(t, 0.05, varr)
	})
}

func TestConditionalValueAtRisk(t *testing.T) {
	t.Run("returns 0 for empty returns", func(t *testing.T) {
		cvarr := metrics.ConditionalValueAtRisk([]float64{}, 0.95)
		assert.Equal(t, 0.0, cvarr)
	})

	t.Run("calculates CVaR for 95% confidence", func(t *testing.T) {
		returns := []float64{-0.05, -0.03, -0.02, -0.01, 0.01, 0.02, 0.03, 0.04}
		cvarr := metrics.ConditionalValueAtRisk(returns, 0.95)
		assert.Greater(t, cvarr, 0.0)
	})

	t.Run("returns average of worst returns for low confidence", func(t *testing.T) {
		returns := []float64{-0.05, -0.03, -0.02, -0.01, 0.01, 0.02, 0.03, 0.04}
		cvarr := metrics.ConditionalValueAtRisk(returns, 0.1)
		// For 8 returns and 0.1 confidence, limit = floor((1-0.1)*8) = floor(7.2) = 7
		// This means averaging returns[0:7] which excludes the last return
		assert.Greater(t, cvarr, 0.0)
	})
}

func TestKellyCriterion(t *testing.T) {
	t.Run("calculates Kelly fraction for positive expectancy", func(t *testing.T) {
		kelly := metrics.KellyCriterion(0.6, 2.0)
		expected := 0.6 - (1-0.6)/2.0
		assert.InDelta(t, expected, kelly, 0.0001)
	})

	t.Run("returns 0 for zero win-loss ratio", func(t *testing.T) {
		kelly := metrics.KellyCriterion(0.6, 0.0)
		assert.Equal(t, 0.0, kelly)
	})

	t.Run("returns 0 for negative win-loss ratio", func(t *testing.T) {
		kelly := metrics.KellyCriterion(0.6, -1.0)
		assert.Equal(t, 0.0, kelly)
	})

	t.Run("calculates negative Kelly for poor win rate", func(t *testing.T) {
		kelly := metrics.KellyCriterion(0.4, 1.0)
		expected := 0.4 - (1-0.4)/1.0
		assert.InDelta(t, expected, kelly, 0.0001)
	})
}

func TestParametricValueAtRisk(t *testing.T) {
	t.Run("returns 0 for insufficient data", func(t *testing.T) {
		varr := metrics.ParametricValueAtRisk([]float64{}, 0.95)
		assert.Equal(t, 0.0, varr)

		varr = metrics.ParametricValueAtRisk([]float64{0.01}, 0.95)
		assert.Equal(t, 0.0, varr)
	})

	t.Run("calculates parametric VaR for 95% confidence", func(t *testing.T) {
		returns := []float64{-0.05, -0.03, -0.02, -0.01, 0.01, 0.02, 0.03, 0.04, 0.05}
		varr := metrics.ParametricValueAtRisk(returns, 0.95)
		assert.Greater(t, varr, 0.0)
	})

	t.Run("calculates parametric VaR for 99% confidence", func(t *testing.T) {
		returns := []float64{-0.05, -0.03, -0.02, -0.01, 0.01, 0.02, 0.03, 0.04, 0.05}
		varr := metrics.ParametricValueAtRisk(returns, 0.99)
		assert.Greater(t, varr, 0.0)
	})
}

func TestMonteCarloValueAtRisk(t *testing.T) {
	t.Run("returns 0 for insufficient data", func(t *testing.T) {
		varr := metrics.MonteCarloValueAtRisk([]float64{}, 0.95, 1000)
		assert.Equal(t, 0.0, varr)

		varr = metrics.MonteCarloValueAtRisk([]float64{0.01}, 0.95, 1000)
		assert.Equal(t, 0.0, varr)
	})

	t.Run("calculates Monte Carlo VaR for 95% confidence", func(t *testing.T) {
		returns := []float64{-0.05, -0.03, -0.02, -0.01, 0.01, 0.02, 0.03, 0.04, 0.05}
		varr := metrics.MonteCarloValueAtRisk(returns, 0.95, 1000)
		assert.Greater(t, varr, 0.0)
	})

	t.Run("calculates Monte Carlo VaR for 99% confidence", func(t *testing.T) {
		returns := []float64{-0.05, -0.03, -0.02, -0.01, 0.01, 0.02, 0.03, 0.04, 0.05}
		varr := metrics.MonteCarloValueAtRisk(returns, 0.99, 1000)
		assert.Greater(t, varr, 0.0)
	})
}
