package metrics

import (
	cryptorand "crypto/rand"
	"encoding/binary"
	"math"
	"sort"
)

// ValueAtRisk calculates the VaR for a given confidence level (e.g., 0.95) using historical method
func ValueAtRisk(returns []float64, confidence float64) float64 {
	if len(returns) == 0 {
		return 0
	}

	sort.Float64s(returns)
	index := int(math.Floor((1 - confidence) * float64(len(returns))))
	if index < 0 {
		index = 0
	}
	return -returns[index]
}

// ConditionalValueAtRisk (Expected Shortfall) calculates the average loss beyond VaR
func ConditionalValueAtRisk(returns []float64, confidence float64) float64 {
	if len(returns) == 0 {
		return 0
	}

	sort.Float64s(returns)
	limit := int(math.Floor((1 - confidence) * float64(len(returns))))
	if limit <= 0 {
		return -returns[0]
	}

	sum := 0.0
	for i := 0; i < limit; i++ {
		sum += returns[i]
	}
	return -sum / float64(limit)
}

// KellyCriterion calculates the optimal fraction of capital to risk
func KellyCriterion(winRate, winLossRatio float64) float64 {
	// K = W - (1-W)/R
	if winLossRatio <= 0 {
		return 0
	}
	return winRate - (1-winRate)/winLossRatio
}

// ParametricValueAtRisk calculates VaR assuming normal distribution
func ParametricValueAtRisk(returns []float64, confidence float64) float64 {
	if len(returns) < 2 {
		return 0
	}

	sum := 0.0
	for _, r := range returns {
		sum += r
	}
	mean := sum / float64(len(returns))

	variance := 0.0
	for _, r := range returns {
		variance += math.Pow(r-mean, 2)
	}
	stdDev := math.Sqrt(variance / float64(len(returns)-1))

	// Get Z-score for confidence
	zScore := getZScore(confidence)
	return -(mean - zScore*stdDev)
}

// MonteCarloValueAtRisk calculates VaR using Monte Carlo simulation
func MonteCarloValueAtRisk(returns []float64, confidence float64, simulations int) float64 {
	if len(returns) < 2 {
		return 0
	}

	sum := 0.0
	for _, r := range returns {
		sum += r
	}
	mean := sum / float64(len(returns))

	variance := 0.0
	for _, r := range returns {
		variance += math.Pow(r-mean, 2)
	}
	stdDev := math.Sqrt(variance / float64(len(returns)-1))

	simReturns := make([]float64, simulations)
	for i := 0; i < simulations; i++ {
		u1, ok1 := cryptoRandFloat64()
		u2, ok2 := cryptoRandFloat64()
		if !ok1 || !ok2 {
			simReturns[i] = mean
			continue
		}

		z := math.Sqrt(-2.0*math.Log(u1)) * math.Cos(2.0*math.Pi*u2)
		simReturns[i] = mean + stdDev*z
	}

	sort.Float64s(simReturns)
	index := int(math.Floor((1 - confidence) * float64(simulations)))
	if index < 0 {
		index = 0
	}
	if index >= simulations {
		index = simulations - 1
	}
	return -simReturns[index]
}

func cryptoRandFloat64() (float64, bool) {
	var b [8]byte
	if _, err := cryptorand.Read(b[:]); err != nil {
		return 0, false
	}

	u := binary.LittleEndian.Uint64(b[:])
	max := float64(^uint64(0))
	return (float64(u) + 0.5) / (max + 1.0), true
}

func getZScore(confidence float64) float64 {
	// Approximation of inverse CDF of normal distribution
	switch {
	case confidence >= 0.99:
		return 2.326
	case confidence >= 0.95:
		return 1.645
	case confidence >= 0.90:
		return 1.282
	default:
		return 1.0
	}
}
