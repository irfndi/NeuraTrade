package indicators

import (
	"math"

	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/series"
)

type kIndicator struct {
	closePrice Indicator
	minValue   Indicator
	maxValue   Indicator
	window     int
}

// NewFastStochasticIndicator returns a derivative Indicator which returns the fast stochastic indicator (%K) for the
// given window.
// https://www.investopedia.com/terms/s/stochasticoscillator.asp
func NewFastStochasticIndicator(series *series.TimeSeries, timeframe int) Indicator {
	return kIndicator{
		closePrice: NewClosePriceIndicator(series),
		minValue:   NewMinimumValueIndicator(NewLowPriceIndicator(series), timeframe),
		maxValue:   NewMaximumValueIndicator(NewHighPriceIndicator(series), timeframe),
		window:     timeframe,
	}
}

func (k kIndicator) Calculate(index int) decimal.Decimal {
	closeVal := k.closePrice.Calculate(index)
	minVal := k.minValue.Calculate(index)
	maxVal := k.maxValue.Calculate(index)

	if minVal.EQ(maxVal) {
		return decimal.New(math.Inf(1))
	}

	return closeVal.Sub(minVal).Div(maxVal.Sub(minVal)).Mul(decimal.New(100))
}

type dIndicator struct {
	k      Indicator
	window int
}

// NewSlowStochasticIndicator returns a derivative Indicator which returns the slow stochastic indicator (%D) for the
// given window.
// https://www.investopedia.com/terms/s/stochasticoscillator.asp
func NewSlowStochasticIndicator(k Indicator, window int) Indicator {
	return dIndicator{k, window}
}

func (d dIndicator) Calculate(index int) decimal.Decimal {
	return NewSimpleMovingAverage(d.k, d.window).Calculate(index)
}
