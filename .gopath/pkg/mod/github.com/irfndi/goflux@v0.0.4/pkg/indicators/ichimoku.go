package indicators

import (
	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/series"
)

type IchimokuIndicator interface {
	Indicator
	TenkanSen(index int) decimal.Decimal
	KijunSen(index int) decimal.Decimal
	SenkouSpanA(index int) decimal.Decimal
	SenkouSpanB(index int) decimal.Decimal
	ChikouSpan(index int) decimal.Decimal
	Cloud(index int) IchimokuCloudResult
}

type ichimokuIndicator struct {
	series   *series.TimeSeries
	high     Indicator
	low      Indicator
	close    Indicator
	period9  int
	period26 int
	period52 int
}

func NewIchimokuIndicator(s *series.TimeSeries) IchimokuIndicator {
	return &ichimokuIndicator{
		series:   s,
		high:     NewHighPriceIndicator(s),
		low:      NewLowPriceIndicator(s),
		close:    NewClosePriceIndicator(s),
		period9:  9,
		period26: 26,
		period52: 52,
	}
}

func (i *ichimokuIndicator) Calculate(index int) decimal.Decimal {
	return i.calculateTenkanSen(index)
}

func (i *ichimokuIndicator) TenkanSen(index int) decimal.Decimal {
	return i.calculateTenkanSen(index)
}

func (i *ichimokuIndicator) KijunSen(index int) decimal.Decimal {
	return i.calculateKijunSen(index)
}

func (i *ichimokuIndicator) SenkouSpanA(index int) decimal.Decimal {
	return i.calculateSenkouSpanA(index)
}

func (i *ichimokuIndicator) SenkouSpanB(index int) decimal.Decimal {
	return i.calculateSenkouSpanB(index)
}

func (i *ichimokuIndicator) ChikouSpan(index int) decimal.Decimal {
	return i.calculateChikouSpan(index)
}

func (i *ichimokuIndicator) calculateTenkanSen(index int) decimal.Decimal {
	return i.calculateHighestHighLowestLow(index, i.period9)
}

func (i *ichimokuIndicator) calculateKijunSen(index int) decimal.Decimal {
	return i.calculateHighestHighLowestLow(index, i.period26)
}

func (i *ichimokuIndicator) calculateHighestHighLowestLow(index int, period int) decimal.Decimal {
	if index < period-1 {
		return decimal.ZERO
	}

	highestHigh := i.high.Calculate(index - period + 1)
	lowestLow := i.low.Calculate(index - period + 1)

	for j := index - period + 2; j <= index; j++ {
		high := i.high.Calculate(j)
		low := i.low.Calculate(j)
		if high.GT(highestHigh) {
			highestHigh = high
		}
		if low.LT(lowestLow) {
			lowestLow = low
		}
	}

	return highestHigh.Add(lowestLow).Div(decimal.New(2))
}

func (i *ichimokuIndicator) calculateSenkouSpanA(index int) decimal.Decimal {
	tenkan := i.calculateTenkanSen(index)
	kijun := i.calculateKijunSen(index)

	return tenkan.Add(kijun).Div(decimal.New(2))
}

func (i *ichimokuIndicator) calculateSenkouSpanB(index int) decimal.Decimal {
	return i.calculateHighestHighLowestLow(index, i.period52)
}

func (i *ichimokuIndicator) calculateChikouSpan(index int) decimal.Decimal {
	if index < 0 {
		return decimal.ZERO
	}
	return i.close.Calculate(index)
}

type IchimokuCloudResult struct {
	TenkanSen   decimal.Decimal
	KijunSen    decimal.Decimal
	SenkouSpanA decimal.Decimal
	SenkouSpanB decimal.Decimal
	ChikouSpan  decimal.Decimal
}

func (i *ichimokuIndicator) Cloud(index int) IchimokuCloudResult {
	return IchimokuCloudResult{
		TenkanSen:   i.calculateTenkanSen(index),
		KijunSen:    i.calculateKijunSen(index),
		SenkouSpanA: i.calculateSenkouSpanA(index),
		SenkouSpanB: i.calculateSenkouSpanB(index),
		ChikouSpan:  i.calculateChikouSpan(index),
	}
}
