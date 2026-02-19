package indicators

import (
	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/series"
)

type bbandWidthIndicator struct {
	upper  Indicator
	lower  Indicator
	middle Indicator
}

// NewBollingerBandwidthIndicator returns an indicator which calculates the width of bollinger bands
func NewBollingerBandwidthIndicator(indicator Indicator, window int, sigma float64) Indicator {
	return bbandWidthIndicator{
		upper:  NewBollingerUpperBandIndicator(indicator, window, sigma),
		lower:  NewBollingerLowerBandIndicator(indicator, window, sigma),
		middle: NewSimpleMovingAverage(indicator, window),
	}
}

func (bbw bbandWidthIndicator) Calculate(index int) decimal.Decimal {
	middle := bbw.middle.Calculate(index)
	if middle.IsZero() {
		return decimal.ZERO
	}

	return bbw.upper.Calculate(index).Sub(bbw.lower.Calculate(index))
}

// ATRRatioIndicator is ATR / Price
type atrRatioIndicator struct {
	atr   Indicator
	price Indicator
}

func NewATRRatioIndicator(atr, price Indicator) Indicator {
	return atrRatioIndicator{atr, price}
}

func NewATRRatioIndicatorFromSeries(s *series.TimeSeries, atrWindow int) Indicator {
	return NewATRRatioIndicator(
		NewAverageTrueRangeIndicator(s, atrWindow),
		NewClosePriceIndicator(s),
	)
}

func (ari atrRatioIndicator) Calculate(index int) decimal.Decimal {
	price := ari.price.Calculate(index)
	if price.IsZero() {
		return decimal.ZERO
	}
	return ari.atr.Calculate(index).Div(price)
}
