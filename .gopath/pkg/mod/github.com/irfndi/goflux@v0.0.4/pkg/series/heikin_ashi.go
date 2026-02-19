package series

import (
	"github.com/irfndi/goflux/pkg/decimal"
)

// NewHeikinAshiseries converts a regular TimeSeries into a Heikin Ashi TimeSeries.
// https://www.investopedia.com/trading/heikin-ashi-better-way-to-trade/
func NewHeikinAshiseries(s *TimeSeries) *TimeSeries {
	haSeries := NewTimeSeries()
	if len(s.Candles) == 0 {
		return haSeries
	}

	var prevHAOpen decimal.Decimal
	var prevHAClose decimal.Decimal

	for i, candle := range s.Candles {
		haCandle := NewCandle(candle.Period)

		// HA-Close = (Open + High + Low + Close) / 4
		haClose := candle.OpenPrice.Add(candle.MaxPrice).Add(candle.MinPrice).Add(candle.ClosePrice).Div(decimal.New(4))
		haCandle.ClosePrice = haClose

		// HA-Open = (prev-HA-Open + prev-HA-Close) / 2
		// For the first candle, use (Open + Close) / 2
		var haOpen decimal.Decimal
		if i == 0 {
			haOpen = candle.OpenPrice.Add(candle.ClosePrice).Div(decimal.New(2))
		} else {
			haOpen = prevHAOpen.Add(prevHAClose).Div(decimal.New(2))
		}
		haCandle.OpenPrice = haOpen

		// HA-High = Max(High, HA-Open, HA-Close)
		haHigh := candle.MaxPrice.Max(haOpen).Max(haClose)
		haCandle.MaxPrice = haHigh

		// HA-Low = Min(Low, HA-Open, HA-Close)
		haLow := candle.MinPrice.Min(haOpen).Min(haClose)
		haCandle.MinPrice = haLow

		haCandle.Volume = candle.Volume
		haCandle.TradeCount = candle.TradeCount

		haSeries.AddCandle(haCandle)

		prevHAOpen = haOpen
		prevHAClose = haClose
	}

	return haSeries
}
