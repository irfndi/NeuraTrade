package indicators

import (
	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/series"
)

type williamsRIndicator struct {
	Indicator
	series *series.TimeSeries
	high   Indicator
	low    Indicator
	close  Indicator
	window int
}

func NewWilliamsRIndicator(s *series.TimeSeries, window int) Indicator {
	return &williamsRIndicator{
		series: s,
		high:   NewHighPriceIndicator(s),
		low:    NewLowPriceIndicator(s),
		close:  NewClosePriceIndicator(s),
		window: window,
	}
}

func (wi *williamsRIndicator) Calculate(index int) decimal.Decimal {
	if index < wi.window-1 {
		return decimal.ZERO
	}

	highestHigh := wi.high.Calculate(index - wi.window + 1)
	lowestLow := wi.low.Calculate(index - wi.window + 1)

	for i := index - wi.window + 2; i <= index; i++ {
		highVal := wi.high.Calculate(i)
		lowVal := wi.low.Calculate(i)

		if highestHigh.LT(highVal) {
			highestHigh = highVal
		}
		if lowestLow.GT(lowVal) {
			lowestLow = lowVal
		}
	}

	closePrice := wi.close.Calculate(index)
	rangeVal := highestHigh.Sub(lowestLow)

	if rangeVal.Zero() {
		return decimal.ZERO
	}

	numerator := highestHigh.Sub(closePrice)
	result := numerator.Div(rangeVal).Mul(decimal.New(-100))

	return result
}
