package indicators

import (
	"github.com/irfndi/goflux/pkg/decimal"
)

type smaIndicator struct {
	indicator Indicator
	window    int
}

// NewSimpleMovingAverage returns a derivative Indicator which returns the average of the current value and preceding
// values in the given windowSize.
func NewSimpleMovingAverage(indicator Indicator, window int) Indicator {
	return smaIndicator{indicator, window}
}

func (sma smaIndicator) Calculate(index int) decimal.Decimal {
	if index < sma.window-1 {
		return decimal.ZERO
	}

	sum := decimal.ZERO
	for i := index; i > index-sma.window; i-- {
		sum = sum.Add(sma.indicator.Calculate(i))
	}

	result := sum.Div(decimal.NewFromInt(int64(sma.window)))

	return result
}

func (sma smaIndicator) Lookback() int {
	return sma.window - 1
}

func (sma smaIndicator) Metadata() IndicatorMetadata {
	m, _ := GetMetadata("sma")
	return m
}
