package indicators

import (
	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/series"
)

type rocIndicator struct {
	Indicator
	series    *series.TimeSeries
	indicator Indicator
	period    int
}

func NewROCIndicator(s *series.TimeSeries, period int) Indicator {
	return &rocIndicator{
		series:    s,
		indicator: NewClosePriceIndicator(s),
		period:    period,
	}
}

func (ri *rocIndicator) Calculate(index int) decimal.Decimal {
	periodIndex := index - ri.period
	if periodIndex < 0 {
		return decimal.ZERO
	}

	currentValue := ri.indicator.Calculate(index)
	previousValue := ri.indicator.Calculate(periodIndex)

	if previousValue.Zero() {
		return decimal.ZERO
	}

	roc := currentValue.Sub(previousValue).Div(previousValue).Mul(decimal.New(100))
	return roc
}

type momIndicator struct {
	Indicator
	series    *series.TimeSeries
	indicator Indicator
	period    int
}

func NewMomentumIndicator(s *series.TimeSeries, period int) Indicator {
	return &momIndicator{
		series:    s,
		indicator: NewClosePriceIndicator(s),
		period:    period,
	}
}

func (mi *momIndicator) Calculate(index int) decimal.Decimal {
	periodIndex := index - mi.period
	if periodIndex < 0 {
		return decimal.ZERO
	}

	currentValue := mi.indicator.Calculate(index)
	previousValue := mi.indicator.Calculate(periodIndex)

	return currentValue.Sub(previousValue)
}
