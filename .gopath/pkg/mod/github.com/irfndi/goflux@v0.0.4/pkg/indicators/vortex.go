package indicators

import (
	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/series"
)

type vortexIndicator struct {
	Indicator
	series *series.TimeSeries
	high   Indicator
	low    Indicator
	close  Indicator
	period int
}

func NewVortexIndicator(s *series.TimeSeries, period int) Indicator {
	return &vortexIndicator{
		series: s,
		high:   NewHighPriceIndicator(s),
		low:    NewLowPriceIndicator(s),
		close:  NewClosePriceIndicator(s),
		period: period,
	}
}

func (v *vortexIndicator) Calculate(index int) decimal.Decimal {
	if index < v.period {
		return decimal.ZERO
	}

	trueRange := v.calculateTrueRangeSum(index)
	positiveVM := v.calculatePositiveVMSum(index)
	negativeVM := v.calculateNegativeVMSum(index)

	if trueRange.Zero() {
		return decimal.ZERO
	}

	positiveVI := positiveVM.Div(trueRange)
	negativeVI := negativeVM.Div(trueRange)

	return positiveVI.Sub(negativeVI)
}

func (v *vortexIndicator) calculateTrueRangeSum(index int) decimal.Decimal {
	trSum := decimal.ZERO
	for i := 0; i < v.period; i++ {
		idx := index - i
		if idx <= 0 {
			continue
		}

		high := v.high.Calculate(idx)
		low := v.low.Calculate(idx)
		prevClose := v.close.Calculate(idx - 1)

		tr := high.Sub(low)
		trHighClose := high.Sub(prevClose).Abs()
		trLowClose := low.Sub(prevClose).Abs()

		if trHighClose.GT(tr) {
			tr = trHighClose
		}
		if trLowClose.GT(tr) {
			tr = trLowClose
		}

		trSum = trSum.Add(tr)
	}

	return trSum
}

func (v *vortexIndicator) calculatePositiveVMSum(index int) decimal.Decimal {
	vmSum := decimal.ZERO
	for i := 0; i < v.period; i++ {
		idx := index - i
		if idx <= 0 {
			continue
		}

		currentHigh := v.high.Calculate(idx)
		prevLow := v.low.Calculate(idx - 1)

		vm := currentHigh.Sub(prevLow).Abs()
		vmSum = vmSum.Add(vm)
	}

	return vmSum
}

func (v *vortexIndicator) calculateNegativeVMSum(index int) decimal.Decimal {
	vmSum := decimal.ZERO
	for i := 0; i < v.period; i++ {
		idx := index - i
		if idx <= 0 {
			continue
		}

		currentLow := v.low.Calculate(idx)
		prevHigh := v.high.Calculate(idx - 1)

		vm := prevHigh.Sub(currentLow).Abs()
		vmSum = vmSum.Add(vm)
	}

	return vmSum
}
