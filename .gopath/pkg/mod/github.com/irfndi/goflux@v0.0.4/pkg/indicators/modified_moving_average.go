package indicators

import (
	"sync"

	"github.com/irfndi/goflux/pkg/decimal"
)

type modifiedMovingAverageIndicator struct {
	indicator   Indicator
	window      int
	resultCache resultCache
	cacheMu     sync.RWMutex
}

// NewMMAIndicator returns a derivative indciator which returns the modified moving average of the underlying
// indictator. An in-depth explanation can be found here:
// https://en.wikipedia.org/wiki/Moving_average#Modified_moving_average
func NewMMAIndicator(indicator Indicator, window int) Indicator {
	return &modifiedMovingAverageIndicator{
		indicator:   indicator,
		window:      window,
		resultCache: make([]*decimal.Decimal, 10000),
	}
}

func (mma *modifiedMovingAverageIndicator) Calculate(index int) decimal.Decimal {
	if cachedValue := returnIfCached(mma, index, func(i int) decimal.Decimal {
		return NewSimpleMovingAverage(mma.indicator, mma.window).Calculate(i)
	}); cachedValue != nil {
		return *cachedValue
	}

	todayVal := mma.indicator.Calculate(index)
	lastVal := mma.Calculate(index - 1)

	result := lastVal.Add(decimal.New(1.0 / float64(mma.window)).Mul(todayVal.Sub(lastVal)))
	cacheResult(mma, index, result)

	return result
}

func (mma *modifiedMovingAverageIndicator) cache() resultCache {
	return mma.resultCache
}

func (mma *modifiedMovingAverageIndicator) setCache(cache resultCache) {
	mma.resultCache = cache
}

func (mma *modifiedMovingAverageIndicator) windowSize() int {
	return mma.window
}

func (mma *modifiedMovingAverageIndicator) cacheMutex() *sync.RWMutex {
	return &mma.cacheMu
}

func (mma *modifiedMovingAverageIndicator) maxCacheSize() int {
	return defaultMaxCacheSize
}
