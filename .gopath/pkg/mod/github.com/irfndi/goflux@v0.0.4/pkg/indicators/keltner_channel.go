package indicators

import (
	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/series"
)

type keltnerChannelIndicator struct {
	ema    Indicator
	atr    Indicator
	mul    decimal.Decimal
	window int
}

func NewKeltnerChannelUpperIndicator(series *series.TimeSeries, window int) Indicator {
	return keltnerChannelIndicator{
		atr:    NewAverageTrueRangeIndicator(series, window),
		ema:    NewEMAIndicator(NewClosePriceIndicator(series), window),
		mul:    decimal.ONE,
		window: window,
	}
}

func NewKeltnerChannelLowerIndicator(series *series.TimeSeries, window int) Indicator {
	return keltnerChannelIndicator{
		atr:    NewAverageTrueRangeIndicator(series, window),
		ema:    NewEMAIndicator(NewClosePriceIndicator(series), window),
		mul:    decimal.ONE.Neg(),
		window: window,
	}
}

func (kci keltnerChannelIndicator) Calculate(index int) decimal.Decimal {
	if index <= kci.window-1 {
		return decimal.ZERO
	}

	coefficient := decimal.New(2).Mul(kci.mul)

	return kci.ema.Calculate(index).Add(kci.atr.Calculate(index).Mul(coefficient))
}
