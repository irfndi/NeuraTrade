package indicators

import (
	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/series"
)

type obvIndicator struct {
	Indicator
	series *series.TimeSeries
	close  Indicator
	volume Indicator
	cache  []decimal.Decimal
}

func NewOBVIndicator(s *series.TimeSeries) Indicator {
	return &obvIndicator{
		series: s,
		close:  NewClosePriceIndicator(s),
		volume: NewVolumeIndicator(s),
		cache:  make([]decimal.Decimal, 0),
	}
}

func (obv *obvIndicator) Calculate(index int) decimal.Decimal {
	if index < 0 {
		return decimal.ZERO
	}

	if index < len(obv.cache) {
		return obv.cache[index]
	}

	// Calculate missing values in cache
	start := len(obv.cache)
	var prevOBV decimal.Decimal
	if start > 0 {
		prevOBV = obv.cache[start-1]
	}

	for i := start; i <= index; i++ {
		var currentOBV decimal.Decimal
		if i == 0 {
			currentOBV = obv.volume.Calculate(0)
		} else {
			currentClose := obv.close.Calculate(i)
			previousClose := obv.close.Calculate(i - 1)
			currentVolume := obv.volume.Calculate(i)

			if currentClose.GT(previousClose) {
				currentOBV = prevOBV.Add(currentVolume)
			} else if currentClose.LT(previousClose) {
				currentOBV = prevOBV.Sub(currentVolume)
			} else {
				currentOBV = prevOBV
			}
		}
		obv.cache = append(obv.cache, currentOBV)
		prevOBV = currentOBV
	}

	return obv.cache[index]
}
