package indicators

import (
	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/series"
)

type mfiIndicator struct {
	Indicator
	series *series.TimeSeries
	high   Indicator
	low    Indicator
	close  Indicator
	volume Indicator
	window int
}

func NewMFIIndicator(s *series.TimeSeries, window int) Indicator {
	return &mfiIndicator{
		series: s,
		high:   NewHighPriceIndicator(s),
		low:    NewLowPriceIndicator(s),
		close:  NewClosePriceIndicator(s),
		volume: NewVolumeIndicator(s),
		window: window,
	}
}

func (mfi *mfiIndicator) Calculate(index int) decimal.Decimal {
	if index < mfi.window {
		return decimal.ZERO
	}

	positiveFlow := decimal.ZERO
	negativeFlow := decimal.ZERO

	for i := 0; i < mfi.window; i++ {
		idx := index - i
		if idx <= 0 {
			break
		}
		typicalPrice := mfi.calculateTypicalPrice(idx)
		prevTypicalPrice := mfi.calculateTypicalPrice(idx - 1)
		volume := mfi.volume.Calculate(idx)

		rawMoneyFlow := typicalPrice.Mul(volume)

		if typicalPrice.GT(prevTypicalPrice) {
			positiveFlow = positiveFlow.Add(rawMoneyFlow)
		} else if typicalPrice.LT(prevTypicalPrice) {
			negativeFlow = negativeFlow.Add(rawMoneyFlow)
		}
	}

	if negativeFlow.Zero() {
		if positiveFlow.Zero() {
			return decimal.New(50)
		}
		return decimal.New(100)
	}

	moneyFlowRatio := positiveFlow.Div(negativeFlow)
	mfiResult := decimal.New(100).Sub(decimal.New(100).Div(moneyFlowRatio.Add(decimal.ONE)))

	return mfiResult
}

func (mfi *mfiIndicator) calculateTypicalPrice(index int) decimal.Decimal {
	high := mfi.high.Calculate(index)
	low := mfi.low.Calculate(index)
	close := mfi.close.Calculate(index)

	return high.Add(low).Add(close).Div(decimal.New(3))
}
