package indicators

import (
	"sync"

	"github.com/irfndi/goflux/pkg/decimal"
)

type emaIndicator struct {
	indicator   Indicator
	window      int
	alpha       decimal.Decimal
	resultCache resultCache
	cacheMu     sync.RWMutex
}

// NewEMAIndicator returns a derivative indicator which returns the average of the current and preceding values in
// the given windowSize, with values closer to current index given more weight. A more in-depth explanation can be found here:
// http://www.investopedia.com/terms/e/ema.asp
func NewEMAIndicator(indicator Indicator, window int) Indicator {
	return &emaIndicator{
		indicator:   indicator,
		window:      window,
		alpha:       decimal.New(2).Div(decimal.NewFromInt(int64(window + 1))),
		resultCache: make([]*decimal.Decimal, 1000),
	}
}

func (ema *emaIndicator) Calculate(index int) decimal.Decimal {
	if cachedValue := returnIfCached(ema, index, func(i int) decimal.Decimal {
		return NewSimpleMovingAverage(ema.indicator, ema.window).Calculate(i)
	}); cachedValue != nil {
		return *cachedValue
	}

	todayVal := ema.indicator.Calculate(index).Mul(ema.alpha)
	result := todayVal.Add(ema.Calculate(index - 1).Mul(decimal.ONE.Sub(ema.alpha)))

	cacheResult(ema, index, result)

	return result
}

func (ema *emaIndicator) cache() resultCache { return ema.resultCache }

func (ema *emaIndicator) setCache(newCache resultCache) {
	ema.resultCache = newCache
}

func (ema *emaIndicator) windowSize() int { return ema.window }

func (ema *emaIndicator) cacheMutex() *sync.RWMutex {
	return &ema.cacheMu
}

func (ema *emaIndicator) maxCacheSize() int {
	return defaultMaxCacheSize
}
