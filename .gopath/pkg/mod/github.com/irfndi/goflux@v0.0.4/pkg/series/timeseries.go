package series

import (
	"fmt"
	"sync"
)

// TimeSeries represents an array of candles with thread-safe operations
type TimeSeries struct {
	mu      sync.RWMutex
	Candles []*Candle
}

// NewTimeSeries returns a new, empty, TimeSeries
func NewTimeSeries() (t *TimeSeries) {
	t = new(TimeSeries)
	t.Candles = make([]*Candle, 0)

	return t
}

// AddCandle adds the given candle to this TimeSeries if it is not nil and after the last candle in this timeseries.
// If the candle is added, AddCandle will return true, otherwise it will return false.
// Thread-safe: uses write lock.
func (ts *TimeSeries) AddCandle(candle *Candle) bool {
	if candle == nil {
		return false
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()

	last := ts.lastCandleUnsafe()
	if last == nil || candle.Period.Since(last.Period) >= 0 {
		ts.Candles = append(ts.Candles, candle)
		return true
	}

	return false
}

// AddCandleErr adds given candle to this TimeSeries with error handling.
// Returns error if candle is nil or if candle cannot be added.
// Thread-safe: uses write lock.
func (ts *TimeSeries) AddCandleErr(candle *Candle) error {
	if candle == nil {
		return fmt.Errorf("candle cannot be nil")
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()

	last := ts.lastCandleUnsafe()
	if last == nil || candle.Period.Since(last.Period) >= 0 {
		ts.Candles = append(ts.Candles, candle)
		return nil
	}

	return fmt.Errorf("candle period (%v) is not after last candle period (%v)", candle.Period, last.Period)
}

// LastCandle will return the lastCandle in this series, or nil if this series is empty
// Thread-safe: uses read lock.
func (ts *TimeSeries) LastCandle() *Candle {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.lastCandleUnsafe()
}

func (ts *TimeSeries) lastCandleUnsafe() *Candle {
	if len(ts.Candles) > 0 {
		return ts.Candles[len(ts.Candles)-1]
	}

	return nil
}

// LastIndex will return the index of the last candle in this series
// Thread-safe: uses read lock.
func (ts *TimeSeries) LastIndex() int {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return len(ts.Candles) - 1
}

// GetCandle returns the candle at the given index, or nil if out of bounds
// Thread-safe: uses read lock.
func (ts *TimeSeries) GetCandle(index int) *Candle {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	if index < 0 || index >= len(ts.Candles) {
		return nil
	}
	return ts.Candles[index]
}

// Length returns the number of candles in the series
// Thread-safe: uses read lock.
func (ts *TimeSeries) Length() int {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return len(ts.Candles)
}
