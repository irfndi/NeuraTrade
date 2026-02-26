package services

import "math"

// RiskAdjustedMetrics captures return-distribution quality metrics for autonomous controls.
type RiskAdjustedMetrics struct {
	Sharpe      float64 `json:"sharpe"`
	Sortino     float64 `json:"sortino"`
	MaxDrawdown float64 `json:"max_drawdown"`
	Expectancy  float64 `json:"expectancy"`
	SampleSize  int     `json:"sample_size"`
}

// ComputeRiskAdjustedMetrics calculates risk-adjusted metrics from per-trade return samples.
// Returns are expected as decimal returns (for example, 0.01 for +1%).
func ComputeRiskAdjustedMetrics(returns []float64) RiskAdjustedMetrics {
	metrics := RiskAdjustedMetrics{SampleSize: len(returns)}
	if len(returns) == 0 {
		return metrics
	}

	metrics.Expectancy = meanFloat64(returns)
	std := stdDevFloat64(returns, metrics.Expectancy)
	downsideStd := downsideStdDevFloat64(returns)
	metrics.MaxDrawdown = maxDrawdownFromReturns(returns)

	// Trade-sample scaling keeps ratios comparable across windows without pretending calendar frequency.
	scale := math.Sqrt(float64(len(returns)))
	if std > 0 {
		metrics.Sharpe = (metrics.Expectancy / std) * scale
	}
	if downsideStd > 0 {
		metrics.Sortino = (metrics.Expectancy / downsideStd) * scale
	}

	if math.IsNaN(metrics.Sharpe) || math.IsInf(metrics.Sharpe, 0) {
		metrics.Sharpe = 0
	}
	if math.IsNaN(metrics.Sortino) || math.IsInf(metrics.Sortino, 0) {
		metrics.Sortino = 0
	}
	if math.IsNaN(metrics.MaxDrawdown) || math.IsInf(metrics.MaxDrawdown, 0) || metrics.MaxDrawdown < 0 {
		metrics.MaxDrawdown = 0
	}
	return metrics
}

func meanFloat64(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func stdDevFloat64(values []float64, mean float64) float64 {
	if len(values) < 2 {
		return 0
	}
	variance := 0.0
	for _, v := range values {
		diff := v - mean
		variance += diff * diff
	}
	variance /= float64(len(values) - 1)
	if variance <= 0 {
		return 0
	}
	return math.Sqrt(variance)
}

func downsideStdDevFloat64(values []float64) float64 {
	negatives := make([]float64, 0, len(values))
	for _, v := range values {
		if v < 0 {
			negatives = append(negatives, v)
		}
	}
	if len(negatives) < 2 {
		return 0
	}
	return stdDevFloat64(negatives, meanFloat64(negatives))
}

func maxDrawdownFromReturns(returns []float64) float64 {
	equity := 1.0
	peak := 1.0
	maxDD := 0.0
	for _, r := range returns {
		if r <= -0.99 {
			r = -0.99
		}
		equity *= 1 + r
		if equity > peak {
			peak = equity
		}
		if peak <= 0 {
			continue
		}
		dd := (peak - equity) / peak
		if dd > maxDD {
			maxDD = dd
		}
	}
	return maxDD
}
