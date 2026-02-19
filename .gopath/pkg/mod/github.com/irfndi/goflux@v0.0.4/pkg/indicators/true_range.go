package indicators

import (
	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/series"
)

type trueRangeIndicator struct {
	series *series.TimeSeries
}

// NewTrueRangeIndicator returns a base indicator
// which calculates the true range at the current point in time for a series
// https://www.investopedia.com/terms/a/atr.asp
func NewTrueRangeIndicator(series *series.TimeSeries) Indicator {
	return trueRangeIndicator{
		series: series,
	}
}

func (tri trueRangeIndicator) Calculate(index int) decimal.Decimal {
	if index-1 < 0 {
		return decimal.ZERO
	}

	candle := tri.series.Candles[index]
	previousClose := tri.series.Candles[index-1].ClosePrice

	trueHigh := candle.MaxPrice.Max(previousClose)
	trueLow := candle.MinPrice.Min(previousClose)

	return trueHigh.Sub(trueLow)
}
