package services

import (
	"log"
	"math"
)

func calculateMeanFloat64(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func calculateStdDev(values []float64) float64 {
	if len(values) < 2 {
		return 0
	}
	mean := calculateMeanFloat64(values)
	var sumSquares float64
	for _, v := range values {
		diff := v - mean
		sumSquares += diff * diff
	}
	variance := sumSquares / float64(len(values)-1)
	return math.Sqrt(variance)
}

func calculateCorrelation(x []float64, y []float64) float64 {
	n := len(x)
	if n == 0 || len(y) != n {
		return 0
	}
	meanX := calculateMeanFloat64(x)
	meanY := calculateMeanFloat64(y)

	var numerator float64
	var denomX float64
	var denomY float64

	for i := 0; i < n; i++ {
		dx := x[i] - meanX
		dy := y[i] - meanY
		numerator += dx * dy
		denomX += dx * dx
		denomY += dy * dy
	}

	denom := math.Sqrt(denomX * denomY)
	if denom == 0 {
		return 0
	}

	corr := numerator / denom
	if corr > 1 {
		return 1
	}
	if corr < -1 {
		return -1
	}
	return corr
}

func logReturns(series []float64) []float64 {
	if len(series) < 2 {
		return nil
	}
	returns := make([]float64, 0, len(series)-1)
	for i := 1; i < len(series); i++ {
		if series[i-1] <= 0 || series[i] <= 0 {
			continue
		}
		returns = append(returns, math.Log(series[i]/series[i-1]))
	}
	return returns
}

// fitAR1 estimates an AR(1) model: y_t = c + phi*y_{t-1} + e_t.
func fitAR1(series []float64) (phi float64, c float64) {
	if len(series) < 2 {
		return 0, 0
	}

	var sumX float64
	var sumY float64
	var sumXX float64
	var sumXY float64
	for i := 1; i < len(series); i++ {
		x := series[i-1]
		y := series[i]
		sumX += x
		sumY += y
		sumXX += x * x
		sumXY += x * y
	}

	n := float64(len(series) - 1)
	denom := n*sumXX - sumX*sumX
	if denom == 0 {
		return 0, calculateMeanFloat64(series)
	}
	phi = (n*sumXY - sumX*sumY) / denom
	c = (sumY - phi*sumX) / n
	return phi, c
}

// garch11Forecast computes forecasted variance using simple GARCH(1,1) parameters.
// For GARCH(1,1) stationarity, alpha + beta must be < 1.
func garch11Forecast(returns []float64, horizon int, omega float64, alpha float64, beta float64) []float64 {
	if horizon <= 0 {
		return nil
	}

	// GARCH(1,1) stationarity constraint: alpha + beta < 1
	// If violated, use safe defaults and log warning
	if alpha+beta >= 1 {
		log.Printf("WARNING: GARCH parameters violate stationarity (alpha=%.4f + beta=%.4f = %.4f >= 1). Using safe defaults (alpha=0.1, beta=0.8)", alpha, beta, alpha+beta)
		alpha = 0.1
		beta = 0.8
	}

	variance := math.Pow(calculateStdDev(returns), 2)
	if variance <= 0 {
		variance = 1e-6
	}

	out := make([]float64, horizon)
	for i := 0; i < horizon; i++ {
		var lastReturn float64
		if len(returns) > 0 {
			lastReturn = returns[len(returns)-1]
		}
		variance = omega + alpha*lastReturn*lastReturn + beta*variance
		out[i] = variance
	}
	return out
}
