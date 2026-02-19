package indicators

import (
	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/series"
)

type averageTrueRangeIndicator struct {
	series *series.TimeSeries
	window int
}

// NewAverageTrueRangeIndicator returns a base indicator that calculates the average true range of the
// underlying over a window
// https://www.investopedia.com/terms/a/atr.asp
func NewAverageTrueRangeIndicator(series *series.TimeSeries, window int) Indicator {
	return averageTrueRangeIndicator{
		series: series,
		window: window,
	}
}

func (atr averageTrueRangeIndicator) Calculate(index int) decimal.Decimal {
	if index < atr.window {
		return decimal.ZERO
	}

	sum := decimal.ZERO

	for i := index; i > index-atr.window; i-- {
		sum = sum.Add(NewTrueRangeIndicator(atr.series).Calculate(i))
	}

	return sum.Div(decimal.New(float64(atr.window)))
}
