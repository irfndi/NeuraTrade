package indicators

import (
	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/series"
)

type awesomeOscillatorIndicator struct {
	Indicator
	series     *series.TimeSeries
	windowFast int
	windowSlow int
}

func NewAwesomeOscillatorIndicator(s *series.TimeSeries, windowFast, windowSlow int) Indicator {
	return &awesomeOscillatorIndicator{
		series:     s,
		windowFast: windowFast,
		windowSlow: windowSlow,
	}
}

func NewDefaultAwesomeOscillatorIndicator(s *series.TimeSeries) Indicator {
	return NewAwesomeOscillatorIndicator(s, 5, 34)
}

func (ao *awesomeOscillatorIndicator) Calculate(index int) decimal.Decimal {
	if index < ao.windowSlow-1 {
		return decimal.ZERO
	}

	smaFast := ao.calculateSMA(index, ao.windowFast)
	smaSlow := ao.calculateSMA(index, ao.windowSlow)

	return smaFast.Sub(smaSlow)
}

func (ao *awesomeOscillatorIndicator) calculateSMA(index int, window int) decimal.Decimal {
	if index < window-1 {
		return decimal.ZERO
	}

	sum := decimal.ZERO
	for i := 0; i < window; i++ {
		idx := index - i
		if idx >= 0 && idx < len(ao.series.Candles) {
			candle := ao.series.Candles[idx]
			high := candle.MaxPrice
			low := candle.MinPrice
			medianPrice := high.Add(low).Div(decimal.New(2))
			sum = sum.Add(medianPrice)
		}
	}

	return sum.Div(decimal.New(float64(window)))
}
