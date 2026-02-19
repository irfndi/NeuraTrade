package indicators

import (
	"sync"

	"github.com/irfndi/goflux/pkg/decimal"
)

const (
	defaultCacheSize    = 1000
	defaultMaxCacheSize = 10000
)

type resultCache []*decimal.Decimal

type cachedIndicator interface {
	Indicator
	cache() resultCache
	setCache(cache resultCache)
	windowSize() int
	cacheMutex() *sync.RWMutex
	maxCacheSize() int
}

func cacheResult(indicator cachedIndicator, index int, val decimal.Decimal) {
	cacheMutex := indicator.cacheMutex()
	cacheMutex.Lock()
	defer cacheMutex.Unlock()

	c := indicator.cache()
	if index < len(c) {
		c[index] = &val
	} else if index == len(c) {
		if len(c) >= indicator.maxCacheSize() {
			return
		}
		indicator.setCache(append(c, &val))
	} else {
		expandResultCache(indicator, index+1)
		indicator.cache()[index] = &val
	}
}

func expandResultCache(indicator cachedIndicator, newSize int) {
	c := indicator.cache()
	sizeDiff := newSize - len(c)
	if sizeDiff <= 0 {
		return
	}

	if newSize > indicator.maxCacheSize() {
		newSize = indicator.maxCacheSize()
		sizeDiff = newSize - len(c)
		if sizeDiff <= 0 {
			return
		}
	}

	expansion := make([]*decimal.Decimal, sizeDiff)
	indicator.setCache(append(c, expansion...))
}

func returnIfCached(indicator cachedIndicator, index int, firstValueFallback func(int) decimal.Decimal) *decimal.Decimal {
	cacheMutex := indicator.cacheMutex()
	cacheMutex.RLock()
	c := indicator.cache()
	if index < len(c) && index >= indicator.windowSize()-1 {
		if val := c[index]; val != nil {
			cacheMutex.RUnlock()
			return val
		}
	}
	cacheMutex.RUnlock()

	if index < indicator.windowSize()-1 {
		return &decimal.ZERO
	}

	if index == indicator.windowSize()-1 {
		value := firstValueFallback(index)
		cacheResult(indicator, index, value)
		return &value
	}

	return nil
}

type cache struct {
	mu      sync.RWMutex
	items   resultCache
	maxSize int
}

func NewCache(initialSize int) *cache {
	size := initialSize
	if size <= 0 {
		size = defaultCacheSize
	}
	return &cache{
		items:   make(resultCache, size),
		maxSize: defaultMaxCacheSize,
	}
}

func (c *cache) Get(index int) *decimal.Decimal {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if index < 0 || index >= len(c.items) {
		return nil
	}
	return c.items[index]
}

func (c *cache) Set(index int, val decimal.Decimal) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if index < 0 {
		return
	}

	if index >= len(c.items) {
		if len(c.items) >= c.maxSize {
			return
		}
		newSize := index + 1
		if newSize > c.maxSize {
			newSize = c.maxSize
		}
		expansion := make(resultCache, newSize-len(c.items))
		c.items = append(c.items, expansion...)
	}
	c.items[index] = &val
}

func (c *cache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}

func (c *cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(resultCache, defaultCacheSize)
}

func ClearCache(indicator cachedIndicator) {
	indicator.setCache(make([]*decimal.Decimal, 0, indicator.windowSize()))
}

func GetCacheSize(indicator cachedIndicator) int {
	return len(indicator.cache())
}

func GetCacheCapacity(indicator cachedIndicator) int {
	return cap(indicator.cache())
}
