package indicators

import (
	"sync"
	"testing"

	"github.com/irfndi/goflux/pkg/decimal"
)

type mockCachedIndicator struct {
	store         *cache
	calculateFunc func(int) decimal.Decimal
	window        int
}

func (m *mockCachedIndicator) Calculate(index int) decimal.Decimal {
	return m.calculateFunc(index)
}

func (m *mockCachedIndicator) cache() resultCache {
	return m.store.items
}

func (m *mockCachedIndicator) setCache(c resultCache) {
	m.store.items = c
}

func (m *mockCachedIndicator) windowSize() int {
	return m.window
}

func (m *mockCachedIndicator) cacheMutex() *sync.RWMutex {
	return &m.store.mu
}

func (m *mockCachedIndicator) maxCacheSize() int {
	return m.store.maxSize
}

func TestNewCache(t *testing.T) {
	c := NewCache(5)
	if len(c.items) != 5 {
		t.Errorf("Expected items length 5, got %d", len(c.items))
	}
	if c.maxSize != defaultMaxCacheSize {
		t.Errorf("Expected maxSize %d, got %d", defaultMaxCacheSize, c.maxSize)
	}
}

func TestCacheSetGet(t *testing.T) {
	c := NewCache(5)

	val := decimal.New(42.5)
	c.Set(0, val)

	if result := c.Get(0); result == nil || result.String() != val.String() {
		t.Errorf("Expected %s, got %v", val.String(), result)
	}
}

func TestCacheSetGetMultiple(t *testing.T) {
	c := NewCache(5)

	vals := []decimal.Decimal{
		decimal.New(1),
		decimal.New(2),
		decimal.New(3),
	}

	for i, v := range vals {
		c.Set(i, v)
	}

	for i, expected := range vals {
		if result := c.Get(i); result == nil || result.String() != expected.String() {
			t.Errorf("Index %d: expected %s, got %v", i, expected.String(), result)
		}
	}
}

func TestCacheExpand(t *testing.T) {
	c := NewCache(5)

	c.Set(10, decimal.New(99))
	if len(c.items) <= 5 {
		t.Errorf("Expected cache to expand, got length %d", len(c.items))
	}
	if result := c.Get(10); result == nil || result.String() != "99" {
		t.Errorf("Expected 99 at index 10, got %v", result)
	}
}

func TestCacheClear(t *testing.T) {
	c := NewCache(5)
	c.Set(0, decimal.New(1))
	c.Set(1, decimal.New(2))

	c.Clear()
	if c.Len() != defaultCacheSize {
		t.Errorf("Expected size %d after clear, got %d", defaultCacheSize, c.Len())
	}
	if c.Get(0) != nil {
		t.Error("Expected nil after clear")
	}
}

func TestCacheEviction(t *testing.T) {
	c := NewCache(10)
	c.maxSize = 50
	for i := 0; i < 100; i++ {
		c.Set(i, decimal.New(float64(i)))
	}
	if c.Len() > c.maxSize {
		t.Errorf("Cache size %d exceeds max size %d", c.Len(), c.maxSize)
	}
}

func TestCacheGetOutOfBounds(t *testing.T) {
	c := NewCache(5)
	if result := c.Get(100); result != nil {
		t.Error("Expected nil for out of bounds index")
	}
}

func TestCacheSetBeyondMaxSize(t *testing.T) {
	c := NewCache(5)
	c.maxSize = 10
	for i := 0; i < 20; i++ {
		c.Set(i, decimal.New(float64(i)))
	}
	if c.Len() > c.maxSize {
		t.Errorf("Cache size %d exceeds max size %d", c.Len(), c.maxSize)
	}
}

func TestCacheThreadSafety(t *testing.T) {
	c := NewCache(100)

	var wg sync.WaitGroup
	numGoroutines := 100
	iterationsPerGoroutine := 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterationsPerGoroutine; j++ {
				idx := id*iterationsPerGoroutine + j
				c.Set(idx, decimal.New(float64(idx)))
				c.Get(idx)
			}
		}(i)
	}

	wg.Wait()
}

func TestCacheResult(t *testing.T) {
	store := NewCache(5)
	ind := &mockCachedIndicator{
		store: store,
		calculateFunc: func(idx int) decimal.Decimal {
			return decimal.New(float64(idx))
		},
		window: 5,
	}

	cacheResult(ind, 0, decimal.New(10))
	cacheResult(ind, 1, decimal.New(20))
	cacheResult(ind, 2, decimal.New(30))

	if result := store.Get(1); result == nil || result.String() != "20" {
		t.Errorf("Expected 20, got %v", result)
	}
}

func TestExpandResultCache(t *testing.T) {
	store := NewCache(5)
	ind := &mockCachedIndicator{
		store: store,
		calculateFunc: func(int) decimal.Decimal {
			return decimal.ZERO
		},
		window: 5,
	}

	expandResultCache(ind, 10)
	if len(ind.cache()) != 10 {
		t.Errorf("Expected size 10, got %d", len(ind.cache()))
	}
}

func TestReturnIfCached(t *testing.T) {
	store := NewCache(5)
	ind := &mockCachedIndicator{
		store: store,
		calculateFunc: func(idx int) decimal.Decimal {
			return decimal.New(float64(idx * 10))
		},
		window: 5,
	}

	fallbackCalled := 0
	fallback := func(idx int) decimal.Decimal {
		fallbackCalled++
		return decimal.New(float64(idx * 100))
	}

	result := returnIfCached(ind, 0, fallback)
	if result == nil || result.String() != decimal.ZERO.String() {
		t.Errorf("Expected %s for index below window size, got %v", decimal.ZERO.String(), result)
	}
	if fallbackCalled != 0 {
		t.Error("Fallback should not be called for index below window size")
	}

	store.Set(0, decimal.New(1))
	store.Set(1, decimal.New(2))
	store.Set(2, decimal.New(3))
	store.Set(3, decimal.New(4))
	store.Set(4, decimal.New(5))

	result = returnIfCached(ind, 4, fallback)
	if result == nil || result.String() != "5" {
		t.Errorf("Expected 5, got %v", result)
	}
	if fallbackCalled != 0 {
		t.Error("Fallback should not be called when value is cached")
	}

	result = returnIfCached(ind, 5, fallback)
	if result != nil {
		t.Errorf("Expected nil for index above window size when not cached, got %v", result)
	}
	if fallbackCalled != 0 {
		t.Errorf("Expected fallback not to be called, called %d times", fallbackCalled)
	}
}

func TestClearCache(t *testing.T) {
	store := NewCache(5)
	ind := &mockCachedIndicator{
		store: store,
		calculateFunc: func(int) decimal.Decimal {
			return decimal.ZERO
		},
		window: 5,
	}

	cacheResult(ind, 10, decimal.New(1))
	ClearCache(ind)
	if GetCacheSize(ind) != 0 {
		t.Errorf("Expected size 0 after ClearCache, got %d", GetCacheSize(ind))
	}
	if GetCacheCapacity(ind) != 5 {
		t.Errorf("Expected capacity 5 after ClearCache, got %d", GetCacheCapacity(ind))
	}
}

func BenchmarkCacheSet(b *testing.B) {
	c := NewCache(1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Set(i%1000, decimal.New(float64(i)))
	}
}

func BenchmarkCacheGet(b *testing.B) {
	c := NewCache(1000)
	for i := 0; i < 1000; i++ {
		c.Set(i, decimal.New(float64(i)))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Get(i % 1000)
	}
}

func BenchmarkCacheResult(b *testing.B) {
	store := NewCache(1000)
	ind := &mockCachedIndicator{
		store: store,
		calculateFunc: func(idx int) decimal.Decimal {
			return decimal.New(float64(idx))
		},
		window: 5,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cacheResult(ind, i%1000, decimal.New(float64(i)))
	}
}

func BenchmarkReturnIfCached(b *testing.B) {
	store := NewCache(1000)
	ind := &mockCachedIndicator{
		store: store,
		calculateFunc: func(idx int) decimal.Decimal {
			return decimal.New(float64(idx))
		},
		window: 5,
	}
	for i := 0; i < 1000; i++ {
		store.Set(i, decimal.New(float64(i)))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		returnIfCached(ind, i%1000, func(idx int) decimal.Decimal {
			return decimal.New(float64(idx * 10))
		})
	}
}
