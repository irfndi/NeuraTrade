package indicators

import (
	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/series"
)

type vwapIndicator struct {
	Indicator
	series       *series.TimeSeries
	typicalPrice Indicator
	volume       Indicator
	cachePV      []decimal.Decimal
	cacheV       []decimal.Decimal
}

// NewVWAPIndicator returns an indicator that calculates the Volume Weighted Average Price.
// This implementation is cumulative since the start of the time series.
// https://www.investopedia.com/terms/v/vwap.asp
func NewVWAPIndicator(s *series.TimeSeries) Indicator {
	return &vwapIndicator{
		series:       s,
		typicalPrice: NewTypicalPriceIndicator(s),
		volume:       NewVolumeIndicator(s),
		cachePV:      make([]decimal.Decimal, 0),
		cacheV:       make([]decimal.Decimal, 0),
	}
}

func (v *vwapIndicator) Calculate(index int) decimal.Decimal {
	if index < 0 || index >= len(v.series.Candles) {
		return decimal.ZERO
	}

	if index < len(v.cachePV) {
		sumPV := decimal.ZERO
		sumV := decimal.ZERO
		for i := 0; i <= index; i++ {
			sumPV = sumPV.Add(v.cachePV[i])
			sumV = sumV.Add(v.cacheV[i])
		}
		if sumV.IsZero() {
			return decimal.ZERO
		}
		return sumPV.Div(sumV)
	}

	// Efficiency: we should store the running sums instead of just individual PV and V
	// But let's just implement the logic first.

	start := len(v.cachePV)
	for i := start; i <= index; i++ {
		tp := v.typicalPrice.Calculate(i)
		vol := v.volume.Calculate(i)
		v.cachePV = append(v.cachePV, tp.Mul(vol))
		v.cacheV = append(v.cacheV, vol)
	}

	sumPV := decimal.ZERO
	sumV := decimal.ZERO
	for i := 0; i <= index; i++ {
		sumPV = sumPV.Add(v.cachePV[i])
		sumV = sumV.Add(v.cacheV[i])
	}

	if sumV.IsZero() {
		return decimal.ZERO
	}

	return sumPV.Div(sumV)
}

type windowedVWAPIndicator struct {
	Indicator
	series       *series.TimeSeries
	typicalPrice Indicator
	volume       Indicator
	window       int
}

// NewWindowedVWAPIndicator returns an indicator that calculates the VWAP over a fixed window.
func NewWindowedVWAPIndicator(s *series.TimeSeries, window int) Indicator {
	return &windowedVWAPIndicator{
		series:       s,
		typicalPrice: NewTypicalPriceIndicator(s),
		volume:       NewVolumeIndicator(s),
		window:       window,
	}
}

func (v *windowedVWAPIndicator) Calculate(index int) decimal.Decimal {
	if index < v.window-1 {
		return decimal.ZERO
	}

	sumPV := decimal.ZERO
	sumV := decimal.ZERO

	for i := 0; i < v.window; i++ {
		idx := index - i
		tp := v.typicalPrice.Calculate(idx)
		vol := v.volume.Calculate(idx)
		sumPV = sumPV.Add(tp.Mul(vol))
		sumV = sumV.Add(vol)
	}

	if sumV.IsZero() {
		return decimal.ZERO
	}

	return sumPV.Div(sumV)
}
