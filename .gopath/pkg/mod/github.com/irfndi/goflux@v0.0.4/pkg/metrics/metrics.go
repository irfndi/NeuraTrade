package metrics

import (
	"math"

	"github.com/irfndi/goflux/pkg/decimal"
)

// SharpeRatio calculates the Sharpe ratio for a series of returns.
// The risk-free rate should be a decimal number (e.g., 0.02 for 2%).
func SharpeRatio(returns []decimal.Decimal, riskFreeRate decimal.Decimal) decimal.Decimal {
	if len(returns) < 2 {
		return decimal.ZERO
	}

	mean := meanReturn(returns)
	stdDev := standardDeviation(returns, mean)

	if stdDev.IsZero() {
		return decimal.ZERO
	}

	excessReturn := mean.Sub(riskFreeRate)
	return excessReturn.Div(stdDev).Mul(decimal.New(252).Sqrt())
}

// SortinoRatio calculates the Sortino ratio for a series of returns.
// The risk-free rate should be a decimal number (e.g., 0.02 for 2%).
func SortinoRatio(returns []decimal.Decimal, riskFreeRate decimal.Decimal) decimal.Decimal {
	if len(returns) < 2 {
		return decimal.ZERO
	}

	mean := meanReturn(returns)
	downsideDev := downsideDeviation(returns, mean)

	if downsideDev.IsZero() {
		return decimal.ZERO
	}

	excessReturn := mean.Sub(riskFreeRate)
	return excessReturn.Div(downsideDev).Mul(decimal.New(252).Sqrt())
}

// CalmarRatio calculates the Calmar ratio given CAGR and maximum drawdown.
func CalmarRatio(cagr decimal.Decimal, maxDrawdown decimal.Decimal) decimal.Decimal {
	if maxDrawdown.IsZero() {
		return decimal.ZERO
	}
	return cagr.Div(maxDrawdown)
}

// CAGR calculates the Compound Annual Growth Rate.
func CAGR(initialEquity, finalEquity decimal.Decimal, years int) decimal.Decimal {
	if initialEquity.IsZero() || years <= 0 {
		return decimal.ZERO
	}

	equityRatio := finalEquity.Div(initialEquity)
	exponent := 1.0 / float64(years)
	return decimal.New(math.Pow(equityRatio.Float(), exponent)).Sub(decimal.New(1))
}

// BurkeRatio calculates the Burke ratio given average return and drawdowns.
func BurkeRatio(averageReturn decimal.Decimal, drawdowns []float64) decimal.Decimal {
	if averageReturn.IsZero() || len(drawdowns) == 0 {
		return decimal.ZERO
	}

	sumSquaredDrawdowns := 0.0
	for _, dd := range drawdowns {
		sumSquaredDrawdowns += dd * dd
	}

	if sumSquaredDrawdowns == 0 {
		return decimal.ZERO
	}

	return averageReturn.Div(decimal.New(sumSquaredDrawdowns))
}

// meanReturn calculates the arithmetic mean of a series of returns.
func meanReturn(returns []decimal.Decimal) decimal.Decimal {
	if len(returns) == 0 {
		return decimal.ZERO
	}

	sum := decimal.ZERO
	for _, r := range returns {
		sum = sum.Add(r)
	}

	return sum.Div(decimal.New(float64(len(returns))))
}

// standardDeviation calculates the standard deviation of a series of returns.
func standardDeviation(returns []decimal.Decimal, mean decimal.Decimal) decimal.Decimal {
	if len(returns) < 2 {
		return decimal.ZERO
	}

	sumSquares := decimal.ZERO
	for _, r := range returns {
		diff := r.Sub(mean)
		sumSquares = sumSquares.Add(diff.Mul(diff))
	}

	variance := sumSquares.Div(decimal.New(float64(len(returns) - 1)))
	return variance.Sqrt()
}

// downsideDeviation calculates the downside deviation of a series of returns.
func downsideDeviation(returns []decimal.Decimal, mean decimal.Decimal) decimal.Decimal {
	if len(returns) < 2 {
		return decimal.ZERO
	}

	sumSquares := decimal.ZERO
	count := 0
	for _, r := range returns {
		if r.LT(mean) {
			diff := mean.Sub(r)
			sumSquares = sumSquares.Add(diff.Mul(diff))
			count++
		}
	}

	if count < 2 {
		return decimal.New(0.0001)
	}

	variance := sumSquares.Div(decimal.New(float64(count - 1)))
	return variance.Sqrt()
}
