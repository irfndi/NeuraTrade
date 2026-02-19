package series_test

import (
	"fmt"
	"time"

	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/series"
)

// Example_basicTimeSeries demonstrates creating and working with time series
func Example_basicTimeSeries() {
	ts := series.NewTimeSeries()

	candle := &series.Candle{
		Period:     series.TimePeriod{Start: time.Now(), End: time.Now()},
		OpenPrice:  decimal.New(100),
		ClosePrice: decimal.New(105),
		MaxPrice:   decimal.New(110),
		MinPrice:   decimal.New(95),
		Volume:     decimal.New(1000),
	}

	ts.AddCandle(candle)

	fmt.Printf("Number of candles: %d\n", len(ts.Candles))
	fmt.Printf("Last close price: %s\n", ts.LastCandle().ClosePrice)
}
