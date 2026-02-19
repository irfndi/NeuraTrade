package indicators

import (
	"sync"

	"github.com/irfndi/goflux/pkg/decimal"
)

// MultiCalculate calculates multiple indicators for a given index in parallel
func MultiCalculate(index int, indicators ...Indicator) []decimal.Decimal {
	results := make([]decimal.Decimal, len(indicators))
	var wg sync.WaitGroup
	wg.Add(len(indicators))

	for i, ind := range indicators {
		go func(idx int, indicator Indicator) {
			defer wg.Done()
			results[idx] = indicator.Calculate(index)
		}(i, ind)
	}

	wg.Wait()
	return results
}

// BatchCalculate calculates an indicator for a range of indices in parallel
// NOTE: This only works for non-recursive indicators (like SMA, RSI, but NOT EMA)
// unless the cache is already populated or the indicator handles concurrency internally.
func BatchCalculate(ind Indicator, indices []int) []decimal.Decimal {
	results := make([]decimal.Decimal, len(indices))
	var wg sync.WaitGroup
	wg.Add(len(indices))

	for i, idx := range indices {
		go func(resultIdx int, dataIdx int) {
			defer wg.Done()
			results[resultIdx] = ind.Calculate(dataIdx)
		}(i, idx)
	}

	wg.Wait()
	return results
}
