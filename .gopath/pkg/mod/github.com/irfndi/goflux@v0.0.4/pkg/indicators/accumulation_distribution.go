package indicators

import (
	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/series"
)

type adLineIndicator struct {
	Indicator
	series *series.TimeSeries
	cache  []decimal.Decimal
}

// NewADLineIndicator returns an indicator that calculates the Accumulation/Distribution Line.
// https://www.investopedia.com/terms/a/accumulationdistributionplus.asp
func NewADLineIndicator(s *series.TimeSeries) Indicator {
	return &adLineIndicator{
		series: s,
		cache:  make([]decimal.Decimal, 0),
	}
}

func (adl *adLineIndicator) Calculate(index int) decimal.Decimal {
	if index < 0 || index >= len(adl.series.Candles) {
		return decimal.ZERO
	}

	if index < len(adl.cache) {
		return adl.cache[index]
	}

	start := len(adl.cache)
	var prevADL decimal.Decimal
	if start > 0 {
		prevADL = adl.cache[start-1]
	}

	for i := start; i <= index; i++ {
		candle := adl.series.Candles[i]
		high := candle.MaxPrice
		low := candle.MinPrice
		close := candle.ClosePrice
		volume := candle.Volume

		highLow := high.Sub(low)
		var mfm decimal.Decimal // Money Flow Multiplier
		if highLow.IsZero() {
			mfm = decimal.ZERO
		} else {
			mfm = close.Sub(low).Sub(high.Sub(close)).Div(highLow)
		}

		mfv := mfm.Mul(volume) // Money Flow Volume
		currentADL := prevADL.Add(mfv)

		adl.cache = append(adl.cache, currentADL)
		prevADL = currentADL
	}

	return adl.cache[index]
}
