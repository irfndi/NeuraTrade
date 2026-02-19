package series

import (
	"time"
)

// Resample converts a TimeSeries from its current duration to a higher duration.
// For example, from 1 minute candles to 5 minute candles.
func Resample(s *TimeSeries, newDuration time.Duration) *TimeSeries {
	resampled := NewTimeSeries()
	if s.Length() == 0 {
		return resampled
	}

	// Group candles by their period
	var currentHA *Candle
	var currentPeriod TimePeriod

	for _, candle := range s.Candles {
		// Calculate the start time of the new period
		periodStart := candle.Period.Start.Truncate(newDuration)

		if currentHA == nil || !periodStart.Equal(currentPeriod.Start) {
			// Save previous
			if currentHA != nil {
				resampled.AddCandle(currentHA)
			}

			// New candle
			currentPeriod = NewTimePeriod(periodStart, newDuration)
			currentHA = NewCandle(currentPeriod)
			currentHA.OpenPrice = candle.OpenPrice
			currentHA.MaxPrice = candle.MaxPrice
			currentHA.MinPrice = candle.MinPrice
			currentHA.ClosePrice = candle.ClosePrice
			currentHA.Volume = candle.Volume
			currentHA.TradeCount = candle.TradeCount
		} else {
			// Update current
			if candle.MaxPrice.GT(currentHA.MaxPrice) {
				currentHA.MaxPrice = candle.MaxPrice
			}
			if candle.MinPrice.LT(currentHA.MinPrice) {
				currentHA.MinPrice = candle.MinPrice
			}
			currentHA.ClosePrice = candle.ClosePrice
			currentHA.Volume = currentHA.Volume.Add(candle.Volume)
			currentHA.TradeCount += candle.TradeCount
		}
	}

	if currentHA != nil {
		resampled.AddCandle(currentHA)
	}

	return resampled
}
