package example

import (
	"strconv"
	"time"

	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/indicators"
	"github.com/irfndi/goflux/pkg/series"
)

// BasicEma is an example of how to create a basic Exponential moving average indicator
// based on close prices of a timeseries from your exchange of choice.
func BasicEma() indicators.Indicator {
	ts := series.NewTimeSeries()

	// fetch this from your preferred exchange
	dataset := [][]string{
		// Timestamp, Open, Close, High, Low, volume
		{"1234567", "1", "2", "3", "5", "6"},
	}

	for _, datum := range dataset {
		start, _ := strconv.ParseInt(datum[0], 10, 64)
		period := series.NewTimePeriod(time.Unix(start, 0), time.Hour*24)

		candle := series.NewCandle(period)
		candle.OpenPrice = decimal.NewFromString(datum[1])
		candle.ClosePrice = decimal.NewFromString(datum[2])
		candle.MaxPrice = decimal.NewFromString(datum[3])
		candle.MinPrice = decimal.NewFromString(datum[4])

		ts.AddCandle(candle)
	}

	closePrices := indicators.NewClosePriceIndicator(ts)
	movingAverage := indicators.NewEMAIndicator(closePrices, 10)

	return movingAverage
}
