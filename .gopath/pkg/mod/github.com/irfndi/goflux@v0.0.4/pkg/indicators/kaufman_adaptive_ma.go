package indicators

import (
	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/series"
)

type kamaIndicator struct {
	Indicator
	series      *series.TimeSeries
	indicator   Indicator
	window      int
	erWindow    int
	smoothing1  decimal.Decimal
	smoothing2  decimal.Decimal
	prevKAMA    decimal.Decimal
	initialized bool
}

func NewKAMAIndicator(s *series.TimeSeries, window int) Indicator {
	kama := &kamaIndicator{
		series:      s,
		indicator:   NewClosePriceIndicator(s),
		window:      window,
		erWindow:    window,
		smoothing1:  decimal.New(2).Div(decimal.New(31)),
		smoothing2:  decimal.New(2).Div(decimal.New(31)).Sub(decimal.ONE).Pow(2),
		prevKAMA:    decimal.ZERO,
		initialized: false,
	}
	return kama
}

func (k *kamaIndicator) Calculate(index int) decimal.Decimal {
	if index < k.erWindow-1 {
		return k.indicator.Calculate(index)
	}

	currentPrice := k.indicator.Calculate(index)

	if !k.initialized {
		k.prevKAMA = k.calculateSMA(index, k.window)
		k.initialized = true
		return k.prevKAMA
	}

	change := currentPrice.Sub(k.indicator.Calculate(index - k.erWindow))
	volatility := k.calculateVolatility(index, k.erWindow)

	er := decimal.ZERO
	if !volatility.Zero() {
		er = change.Abs().Div(volatility)
	}

	alpha := er.Mul(k.smoothing1.Add(k.smoothing2)).Add(k.smoothing2)
	if alpha.GT(decimal.ONE) {
		alpha = decimal.ONE
	}
	if alpha.LT(decimal.New(0.0001)) {
		alpha = decimal.New(0.0001)
	}

	k.prevKAMA = k.prevKAMA.Add(alpha.Mul(currentPrice.Sub(k.prevKAMA)))

	return k.prevKAMA
}

func (k *kamaIndicator) calculateSMA(index int, window int) decimal.Decimal {
	if index < window-1 {
		return k.indicator.Calculate(index)
	}

	sum := decimal.ZERO
	for i := 0; i < window; i++ {
		sum = sum.Add(k.indicator.Calculate(index - i))
	}

	return sum.Div(decimal.New(float64(window)))
}

func (k *kamaIndicator) calculateVolatility(index int, window int) decimal.Decimal {
	if index < window {
		return decimal.ONE
	}

	sum := decimal.ZERO
	prevPrice := k.indicator.Calculate(index - window)

	for i := 1; i <= window; i++ {
		change := k.indicator.Calculate(index - window + i).Sub(prevPrice)
		sum = sum.Add(change.Mul(change))
		prevPrice = k.indicator.Calculate(index - window + i)
	}

	return sum.Sqrt()
}

type demaIndicator struct {
	Indicator
	window int
	ema1   *emaAllIndicator
	ema2   *emaAllIndicator
}

func NewDEMAIndicator(s *series.TimeSeries, window int) Indicator {
	base := NewClosePriceIndicator(s)
	ema1 := newEmaAllIndicator(base, window)
	ema2 := newEmaAllIndicator(ema1, window)
	return &demaIndicator{
		window: window,
		ema1:   ema1,
		ema2:   ema2,
	}
}

func (d *demaIndicator) Calculate(index int) decimal.Decimal {
	if index < d.window-1 {
		return decimal.ZERO
	}
	return d.ema1.Calculate(index).Mul(decimal.New(2)).Sub(d.ema2.Calculate(index))
}

type emaAllIndicator struct {
	indicator   Indicator
	window      int
	alpha       decimal.Decimal
	resultCache resultCache
}

func newEmaAllIndicator(indicator Indicator, window int) *emaAllIndicator {
	return &emaAllIndicator{
		indicator:   indicator,
		window:      window,
		alpha:       decimal.New(2).Div(decimal.NewFromInt(int64(window + 1))),
		resultCache: make([]*decimal.Decimal, 1000),
	}
}

func (ema *emaAllIndicator) Calculate(index int) decimal.Decimal {
	if index < 0 {
		return decimal.ZERO
	}

	if index >= len(ema.resultCache) {
		expansion := make([]*decimal.Decimal, index+1-len(ema.resultCache))
		ema.resultCache = append(ema.resultCache, expansion...)
	}

	if cached := ema.resultCache[index]; cached != nil {
		return *cached
	}

	if index == 0 {
		val := ema.indicator.Calculate(0)
		ema.resultCache[index] = &val
		return val
	}

	if index < ema.window-1 {
		val := ema.indicator.Calculate(index)
		ema.resultCache[index] = &val
		return val
	}

	if index == ema.window-1 {
		sum := decimal.ZERO
		for i := 0; i < ema.window; i++ {
			sum = sum.Add(ema.indicator.Calculate(index - i))
		}
		val := sum.Div(decimal.NewFromInt(int64(ema.window)))
		ema.resultCache[index] = &val
		return val
	}

	todayVal := ema.indicator.Calculate(index).Mul(ema.alpha)
	val := todayVal.Add(ema.Calculate(index - 1).Mul(decimal.ONE.Sub(ema.alpha)))
	ema.resultCache[index] = &val
	return val
}

type temaIndicator struct {
	Indicator
	window int
	ema1   *emaAllIndicator
	ema2   *emaAllIndicator
	ema3   *emaAllIndicator
}

func NewTEMAIndicator(s *series.TimeSeries, window int) Indicator {
	base := NewClosePriceIndicator(s)
	ema1 := newEmaAllIndicator(base, window)
	ema2 := newEmaAllIndicator(ema1, window)
	ema3 := newEmaAllIndicator(ema2, window)
	return &temaIndicator{
		window: window,
		ema1:   ema1,
		ema2:   ema2,
		ema3:   ema3,
	}
}

func (t *temaIndicator) Calculate(index int) decimal.Decimal {
	if index < t.window-1 {
		return decimal.ZERO
	}
	return t.ema1.Calculate(index).
		Mul(decimal.New(3)).
		Sub(t.ema2.Calculate(index).Mul(decimal.New(3))).
		Add(t.ema3.Calculate(index))
}
