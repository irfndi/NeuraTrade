package indicators

import (
	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/series"
)

type superTrendIndicator struct {
	Indicator
	series     *series.TimeSeries
	atr        Indicator
	multiplier decimal.Decimal
	cache      []decimal.Decimal
	cacheTrend []int // 1 for UP, -1 for DOWN

	finalUpper decimal.Decimal
	finalLower decimal.Decimal
}

// NewSuperTrendIndicator returns an indicator that calculates the SuperTrend.
// https://www.tradingview.com/support/solutions/43000634738-supertrend/
func NewSuperTrendIndicator(s *series.TimeSeries, window int, multiplier float64) Indicator {
	return &superTrendIndicator{
		series:     s,
		atr:        NewAverageTrueRangeIndicator(s, window),
		multiplier: decimal.New(multiplier),
		cache:      make([]decimal.Decimal, 0),
		cacheTrend: make([]int, 0),
	}
}

func (st *superTrendIndicator) Calculate(index int) decimal.Decimal {
	if index < 0 || index >= len(st.series.Candles) {
		return decimal.ZERO
	}

	if index < len(st.cache) {
		return st.cache[index]
	}

	// Fill cache
	start := len(st.cache)
	if start == 0 {
		st.cache = append(st.cache, decimal.ZERO)
		st.cacheTrend = append(st.cacheTrend, 1)
		st.finalUpper = decimal.ZERO
		st.finalLower = decimal.ZERO
		start = 1
	}

	for i := start; i <= index; i++ {
		candle := st.series.Candles[i]
		prevCandle := st.series.Candles[i-1]
		atr := st.atr.Calculate(i)

		median := candle.MaxPrice.Add(candle.MinPrice).Div(decimal.New(2))
		basicUpper := median.Add(st.multiplier.Mul(atr))
		basicLower := median.Sub(st.multiplier.Mul(atr))

		// Final Upperband
		if basicUpper.LT(st.finalUpper) || prevCandle.ClosePrice.GT(st.finalUpper) {
			st.finalUpper = basicUpper
		}

		// Final Lowerband
		if basicLower.GT(st.finalLower) || prevCandle.ClosePrice.LT(st.finalLower) {
			st.finalLower = basicLower
		}

		prevTrend := st.cacheTrend[i-1]
		trend := prevTrend
		var value decimal.Decimal

		if prevTrend == 1 {
			if candle.ClosePrice.LT(st.finalLower) {
				trend = -1
				value = st.finalUpper
			} else {
				value = st.finalLower
			}
		} else {
			if candle.ClosePrice.GT(st.finalUpper) {
				trend = 1
				value = st.finalLower
			} else {
				value = st.finalUpper
			}
		}

		st.cache = append(st.cache, value)
		st.cacheTrend = append(st.cacheTrend, trend)
	}

	return st.cache[index]
}

func (st *superTrendIndicator) Trend(index int) int {
	st.Calculate(index)
	if index < 0 || index >= len(st.cacheTrend) {
		return 0
	}
	return st.cacheTrend[index]
}
