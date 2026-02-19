package indicators

import (
	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/series"
)

type kvoIndicator struct {
	Indicator
	series *series.TimeSeries
	ema34  Indicator
	ema55  Indicator
}

// NewKVOIndicator returns an indicator that calculates the Klinger Volume Oscillator.
// https://www.investopedia.com/terms/k/klingeroscillator.asp
func NewKVOIndicator(s *series.TimeSeries) Indicator {
	vf := &vfIndicator{
		series: s,
		cache:  make([]decimal.Decimal, 0),
	}
	return &kvoIndicator{
		series: s,
		ema34:  NewEMAIndicator(vf, 34),
		ema55:  NewEMAIndicator(vf, 55),
	}
}

func (k *kvoIndicator) Calculate(index int) decimal.Decimal {
	return k.ema34.Calculate(index).Sub(k.ema55.Calculate(index))
}

type vfIndicator struct {
	Indicator
	series    *series.TimeSeries
	cache     []decimal.Decimal
	prevCM    decimal.Decimal
	prevDM    decimal.Decimal
	prevTrend int
}

func (v *vfIndicator) Calculate(index int) decimal.Decimal {
	if index <= 0 {
		return decimal.ZERO
	}

	if index < len(v.cache) {
		return v.cache[index]
	}

	// Fill cache
	start := len(v.cache)
	if start == 0 {
		v.cache = append(v.cache, decimal.ZERO)
		v.prevDM = v.series.Candles[0].MaxPrice.Sub(v.series.Candles[0].MinPrice)
		v.prevCM = decimal.ZERO
		v.prevTrend = 0
		start = 1
	}

	for i := start; i <= index; i++ {
		candle := v.series.Candles[i]
		prevCandle := v.series.Candles[i-1]

		tp := candle.MaxPrice.Add(candle.MinPrice).Add(candle.ClosePrice)
		prevTP := prevCandle.MaxPrice.Add(prevCandle.MinPrice).Add(prevCandle.ClosePrice)

		trend := 1
		if tp.LT(prevTP) {
			trend = -1
		}

		dm := candle.MaxPrice.Sub(candle.MinPrice)

		if trend == v.prevTrend {
			v.prevCM = v.prevCM.Add(dm)
		} else {
			v.prevCM = v.prevDM.Add(dm)
		}

		v.prevTrend = trend
		v.prevDM = dm

		vf := decimal.ZERO
		if !v.prevCM.IsZero() {
			// abs(2 * (dm/cm) - 1)
			term := dm.Div(v.prevCM).Mul(decimal.New(2)).Sub(decimal.ONE).Abs()
			vf = candle.Volume.Mul(decimal.New(float64(trend))).Mul(term).Mul(decimal.New(100))
		}
		v.cache = append(v.cache, vf)
	}

	return v.cache[index]
}
