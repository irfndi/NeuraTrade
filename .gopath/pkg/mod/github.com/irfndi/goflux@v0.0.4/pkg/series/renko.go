package series

import (
	"github.com/irfndi/goflux/pkg/decimal"
)

// Renko converts a TimeSeries into a Renko chart.
// A Renko chart is a series of "bricks" of a fixed size.
// A new brick is created when the price moves by more than the brick size from the previous brick's close.
func Renko(s *TimeSeries, brickSize decimal.Decimal) *TimeSeries {
	renko := NewTimeSeries()
	if s.Length() == 0 {
		return renko
	}

	lastClose := s.Candles[0].ClosePrice
	currentStartTime := s.Candles[0].Period.Start
	brickDuration := s.Candles[0].Period.Length()

	for _, candle := range s.Candles {
		price := candle.ClosePrice
		diff := price.Sub(lastClose)

		for diff.Abs().GTE(brickSize) {
			brick := NewCandle(NewTimePeriod(currentStartTime, brickDuration))
			brick.OpenPrice = lastClose

			if diff.IsPositive() {
				lastClose = lastClose.Add(brickSize)
				brick.ClosePrice = lastClose
				brick.MaxPrice = lastClose
				brick.MinPrice = brick.OpenPrice
			} else {
				lastClose = lastClose.Sub(brickSize)
				brick.ClosePrice = lastClose
				brick.MaxPrice = brick.OpenPrice
				brick.MinPrice = lastClose
			}

			renko.AddCandle(brick)
			currentStartTime = currentStartTime.Add(brickDuration)
			diff = price.Sub(lastClose)
		}
	}

	return renko
}
