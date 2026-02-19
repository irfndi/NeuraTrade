package indicators

import (
	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/math"
)

type trendLineIndicator struct {
	indicator Indicator
	window    int
}

// NewTrendlineIndicator returns an indicator whose output is the slope of the trend
// line given by the values in the window.
func NewTrendlineIndicator(indicator Indicator, window int) Indicator {
	return trendLineIndicator{
		indicator: indicator,
		window:    window,
	}
}

func (tli trendLineIndicator) Calculate(index int) decimal.Decimal {
	window := math.Min(index+1, tli.window)

	values := make([]decimal.Decimal, window)

	for i := 0; i < window; i++ {
		values[i] = tli.indicator.Calculate(index - (window - 1) + i)
	}

	n := decimal.ONE.Mul(decimal.New(float64(window)))
	ab := sumXy(values).Mul(n).Sub(sumX(values).Mul(sumY(values)))
	cd := sumX2(values).Mul(n).Sub(sumX(values).Pow(2))

	return ab.Div(cd)
}

func sumX(decimals []decimal.Decimal) (s decimal.Decimal) {
	s = decimal.ZERO

	for i := range decimals {
		s = s.Add(decimal.New(float64(i)))
	}

	return s
}

func sumY(decimals []decimal.Decimal) (b decimal.Decimal) {
	b = decimal.ZERO
	for _, d := range decimals {
		b = b.Add(d)
	}

	return
}

func sumXy(decimals []decimal.Decimal) (b decimal.Decimal) {
	b = decimal.ZERO

	for i, d := range decimals {
		b = b.Add(d.Mul(decimal.New(float64(i))))
	}

	return
}

func sumX2(decimals []decimal.Decimal) decimal.Decimal {
	b := decimal.ZERO

	for i := range decimals {
		b = b.Add(decimal.New(float64(i)).Pow(2))
	}

	return b
}
