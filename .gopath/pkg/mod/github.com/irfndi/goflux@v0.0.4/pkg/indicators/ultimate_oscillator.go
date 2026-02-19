package indicators

import (
	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/series"
)

type ultimateOscillatorIndicator struct {
	Indicator
	series  *series.TimeSeries
	close   Indicator
	high    Indicator
	low     Indicator
	period1 int
	period2 int
	period3 int
	weight1 decimal.Decimal
	weight2 decimal.Decimal
	weight3 decimal.Decimal
}

func NewUltimateOscillatorIndicator(s *series.TimeSeries, period1, period2, period3 int) Indicator {
	uo := &ultimateOscillatorIndicator{
		series:  s,
		close:   NewClosePriceIndicator(s),
		high:    NewHighPriceIndicator(s),
		low:     NewLowPriceIndicator(s),
		period1: period1,
		period2: period2,
		period3: period3,
	}

	totalWeight := decimal.New(float64(period1 + period2 + period3))
	uo.weight1 = decimal.New(float64(period1)).Div(totalWeight)
	uo.weight2 = decimal.New(float64(period2)).Div(totalWeight)
	uo.weight3 = decimal.New(float64(period3)).Div(totalWeight)

	return uo
}

func (uo *ultimateOscillatorIndicator) Calculate(index int) decimal.Decimal {
	maxPeriod := uo.period3
	if index < maxPeriod {
		return decimal.ZERO
	}

	avg1 := uo.calculateAverage(index, uo.period1)
	avg2 := uo.calculateAverage(index, uo.period2)
	avg3 := uo.calculateAverage(index, uo.period3)

	result := uo.weight1.Mul(avg1).Add(uo.weight2.Mul(avg2)).Add(uo.weight3.Mul(avg3))
	return result.Mul(decimal.New(100))
}

func (uo *ultimateOscillatorIndicator) calculateAverage(index int, period int) decimal.Decimal {
	if index < period {
		return decimal.ZERO
	}

	rawBuyingPressureSum := decimal.ZERO
	trueRangeSum := decimal.ZERO

	for i := 0; i < period; i++ {
		idx := index - i
		if idx <= 0 {
			break
		}

		currentClose := uo.close.Calculate(idx)
		currentLow := uo.low.Calculate(idx)
		currentHigh := uo.high.Calculate(idx)
		prevClose := uo.close.Calculate(idx - 1)

		// True Low = Min(current low, previous close)
		trueLow := currentLow
		if prevClose.LT(trueLow) {
			trueLow = prevClose
		}

		// Buying Pressure = current close - true low
		buyPressure := currentClose.Sub(trueLow)
		rawBuyingPressureSum = rawBuyingPressureSum.Add(buyPressure)

		// True Range = Max(current high, previous close) - true low
		trueHigh := currentHigh
		if prevClose.GT(trueHigh) {
			trueHigh = prevClose
		}
		tr := trueHigh.Sub(trueLow)
		trueRangeSum = trueRangeSum.Add(tr)
	}

	if trueRangeSum.Zero() {
		return decimal.ZERO
	}

	return rawBuyingPressureSum.Div(trueRangeSum)
}
