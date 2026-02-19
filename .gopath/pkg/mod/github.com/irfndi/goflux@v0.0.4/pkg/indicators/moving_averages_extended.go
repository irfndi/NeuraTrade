package indicators

import (
	"math"
	"sync"

	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/series"
)

type vwmaIndicator struct {
	indicator Indicator
	volume    Indicator
	window    int
}

// NewVWMAIndicator returns a Volume Weighted Moving Average
func NewVWMAIndicator(indicator, volume Indicator, window int) Indicator {
	return vwmaIndicator{indicator, volume, window}
}

// NewVWMAIndicatorFromSeries is a helper to create VWMA from series
func NewVWMAIndicatorFromSeries(s *series.TimeSeries, window int) Indicator {
	return NewVWMAIndicator(
		NewClosePriceIndicator(s),
		NewVolumeIndicator(s),
		window,
	)
}

func (vwma vwmaIndicator) Calculate(index int) decimal.Decimal {
	if index < vwma.window-1 {
		return decimal.ZERO
	}

	priceVolumeSum := decimal.ZERO
	volumeSum := decimal.ZERO

	for i := index - vwma.window + 1; i <= index; i++ {
		price := vwma.indicator.Calculate(i)
		vol := vwma.volume.Calculate(i)
		priceVolumeSum = priceVolumeSum.Add(price.Mul(vol))
		volumeSum = volumeSum.Add(vol)
	}

	if volumeSum.IsZero() {
		return decimal.ZERO
	}

	return priceVolumeSum.Div(volumeSum)
}

// TimeSeriesIndicator is a helper to access TimeSeries from within indicators if needed
type TimeSeriesIndicator struct {
	series *series.TimeSeries
}

func NewTimeSeriesIndicator(s *series.TimeSeries) *TimeSeriesIndicator {
	return &TimeSeriesIndicator{s}
}

func (tsi *TimeSeriesIndicator) Calculate(index int) decimal.Decimal {
	return tsi.series.GetCandle(index).ClosePrice
}

// RMAIndicator is Running Moving Average (used in RSI, also known as SMMA)
type rmaIndicator struct {
	indicator   Indicator
	window      int
	alpha       decimal.Decimal
	resultCache resultCache
	cacheMu     sync.RWMutex
}

func NewRMAIndicator(indicator Indicator, window int) Indicator {
	return &rmaIndicator{
		indicator:   indicator,
		window:      window,
		alpha:       decimal.ONE.Div(decimal.New(float64(window))),
		resultCache: make([]*decimal.Decimal, 1000),
	}
}

func (rma *rmaIndicator) Calculate(index int) decimal.Decimal {
	if cachedValue := returnIfCached(rma, index, func(i int) decimal.Decimal {
		return NewSimpleMovingAverage(rma.indicator, rma.window).Calculate(i)
	}); cachedValue != nil {
		return *cachedValue
	}

	todayVal := rma.indicator.Calculate(index)
	lastVal := rma.Calculate(index - 1)

	// RMA = alpha * today + (1 - alpha) * last
	result := todayVal.Mul(rma.alpha).Add(lastVal.Mul(decimal.ONE.Sub(rma.alpha)))
	cacheResult(rma, index, result)

	return result
}

func (rma *rmaIndicator) cache() resultCache        { return rma.resultCache }
func (rma *rmaIndicator) setCache(c resultCache)    { rma.resultCache = c }
func (rma *rmaIndicator) windowSize() int           { return rma.window }
func (rma *rmaIndicator) cacheMutex() *sync.RWMutex { return &rma.cacheMu }
func (rma *rmaIndicator) maxCacheSize() int         { return defaultMaxCacheSize }

type trimaIndicator struct {
	indicator Indicator
	window    int
}

func NewTRIMAIndicator(indicator Indicator, window int) Indicator {
	return &trimaIndicator{
		indicator: indicator,
		window:    window,
	}
}

func (t *trimaIndicator) Calculate(index int) decimal.Decimal {
	if index < t.window-1 {
		return decimal.ZERO
	}

	half := (t.window + 1) / 2
	var denomInt int
	if t.window%2 == 0 {
		h := t.window / 2
		denomInt = h * (h + 1)
	} else {
		denomInt = half * half
	}
	denom := decimal.New(float64(denomInt))

	numerator := decimal.ZERO
	start := index - t.window + 1
	for i := 0; i < t.window; i++ {
		pos := i + 1
		weightInt := pos
		if pos > half {
			weightInt = t.window - pos + 1
		}
		weight := decimal.New(float64(weightInt))
		numerator = numerator.Add(t.indicator.Calculate(start + i).Mul(weight))
	}

	return numerator.Div(denom)
}

// wmaIndicator is the Weighted Moving Average
type wmaIndicator struct {
	indicator Indicator
	window    int
}

// NewWMAIndicator returns a new Weighted Moving Average
func NewWMAIndicator(indicator Indicator, window int) Indicator {
	return &wmaIndicator{indicator, window}
}

func (wma wmaIndicator) Calculate(index int) decimal.Decimal {
	if index < wma.window-1 {
		return decimal.ZERO
	}

	numerator := decimal.ZERO
	denominator := decimal.New(float64(wma.window * (wma.window + 1) / 2))

	for i := 0; i < wma.window; i++ {
		weight := decimal.New(float64(wma.window - i))
		numerator = numerator.Add(wma.indicator.Calculate(index - i).Mul(weight))
	}

	return numerator.Div(denominator)
}

// t3Indicator is the Tillson T3 Moving Average
type t3Indicator struct {
	indicator Indicator
	window    int
	vFactor   decimal.Decimal
	e3        Indicator
	e4        Indicator
	e5        Indicator
	e6        Indicator
}

// NewT3Indicator returns a new Tillson T3 Moving Average
func NewT3Indicator(indicator Indicator, window int, vFactor float64) Indicator {
	e1 := NewEMAIndicator(indicator, window)
	e2 := NewEMAIndicator(e1, window)
	e3 := NewEMAIndicator(e2, window)
	e4 := NewEMAIndicator(e3, window)
	e5 := NewEMAIndicator(e4, window)
	e6 := NewEMAIndicator(e5, window)
	return &t3Indicator{
		indicator: indicator,
		window:    window,
		vFactor:   decimal.New(vFactor),
		e3:        e3,
		e4:        e4,
		e5:        e5,
		e6:        e6,
	}
}

func (t3 *t3Indicator) Calculate(index int) decimal.Decimal {
	v := t3.vFactor
	v2 := v.Mul(v)
	v3 := v2.Mul(v)

	c1 := v3.Neg()
	c2 := v2.Mul(decimal.New(3)).Add(v3.Mul(decimal.New(3)))
	c3 := v2.Mul(decimal.New(-6)).Sub(v.Mul(decimal.New(3))).Sub(v3.Mul(decimal.New(3)))
	c4 := decimal.ONE.Add(v.Mul(decimal.New(3))).Add(v2.Mul(decimal.New(3))).Add(v3)

	res := c1.Mul(t3.e6.Calculate(index)).
		Add(c2.Mul(t3.e5.Calculate(index))).
		Add(c3.Mul(t3.e4.Calculate(index))).
		Add(c4.Mul(t3.e3.Calculate(index)))

	return res
}

// almaIndicator is the Arnaud Legoux Moving Average
type almaIndicator struct {
	indicator Indicator
	window    int
	offset    float64
	sigma     float64
}

// NewALMAIndicator returns a new Arnaud Legoux Moving Average
func NewALMAIndicator(indicator Indicator, window int, offset, sigma float64) Indicator {
	return &almaIndicator{indicator, window, offset, sigma}
}

func (alma *almaIndicator) Calculate(index int) decimal.Decimal {
	if index < alma.window-1 {
		return alma.indicator.Calculate(index)
	}

	m := alma.offset * float64(alma.window-1)
	s := float64(alma.window) / alma.sigma

	sum := decimal.ZERO
	norm := 0.0

	for i := 0; i < alma.window; i++ {
		weight := math.Exp(-math.Pow(float64(i)-m, 2) / (2 * math.Pow(s, 2)))
		sum = sum.Add(alma.indicator.Calculate(index - (alma.window - 1 - i)).Mul(decimal.New(weight)))
		norm += weight
	}

	return sum.Div(decimal.New(norm))
}

// vidyaIndicator is Variable Index Dynamic Average
type vidyaIndicator struct {
	indicator Indicator
	window    int
	alpha     decimal.Decimal
	cmo       Indicator
	cache     []decimal.Decimal
}

// NewVIDYAIndicator returns a new Variable Index Dynamic Average
func NewVIDYAIndicator(indicator Indicator, window int) Indicator {
	return &vidyaIndicator{
		indicator: indicator,
		window:    window,
		alpha:     decimal.New(2).Div(decimal.New(float64(window + 1))),
		cmo:       NewChandeMomentumOscillatorIndicator(indicator, window),
		cache:     make([]decimal.Decimal, 0),
	}
}

func (vidya *vidyaIndicator) Calculate(index int) decimal.Decimal {
	if index < 0 {
		return decimal.ZERO
	}

	if index < len(vidya.cache) {
		return vidya.cache[index]
	}

	start := len(vidya.cache)
	if start == 0 {
		vidya.cache = append(vidya.cache, vidya.indicator.Calculate(0))
		start = 1
	}

	for i := start; i <= index; i++ {
		if i < vidya.window {
			vidya.cache = append(vidya.cache, vidya.indicator.Calculate(i))
			continue
		}

		k := vidya.cmo.Calculate(i).Abs().Div(decimal.New(100))
		ak := vidya.alpha.Mul(k)
		val := vidya.indicator.Calculate(i)
		prev := vidya.cache[i-1]
		vidya.cache = append(vidya.cache, ak.Mul(val).Add(decimal.ONE.Sub(ak).Mul(prev)))
	}

	return vidya.cache[index]
}
