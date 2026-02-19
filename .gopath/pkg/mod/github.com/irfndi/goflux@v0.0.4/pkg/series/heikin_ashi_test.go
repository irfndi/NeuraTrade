package series_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/series"
)

func TestNewHeikinAshiseries(t *testing.T) {
	t.Run("empty series returns empty series", func(t *testing.T) {
		ts := series.NewTimeSeries()
		haSeries := series.NewHeikinAshiseries(ts)

		assert.Equal(t, 0, len(haSeries.Candles))
	})

	t.Run("single candle", func(t *testing.T) {
		ts := series.NewTimeSeries()
		candle := &series.Candle{
			Period:     series.TimePeriod{Start: time.Now(), End: time.Now()},
			OpenPrice:  decimal.New(100),
			ClosePrice: decimal.New(110),
			MaxPrice:   decimal.New(115),
			MinPrice:   decimal.New(95),
			Volume:     decimal.New(1000),
		}
		ts.AddCandle(candle)

		haSeries := series.NewHeikinAshiseries(ts)

		assert.Equal(t, 1, len(haSeries.Candles))

		// HA-Close = (100 + 115 + 95 + 110) / 4 = 105
		expectedClose := decimal.New(105)
		assert.Equal(t, expectedClose.String(), haSeries.Candles[0].ClosePrice.String())

		// HA-Open = (100 + 110) / 2 = 105 (first candle)
		expectedOpen := decimal.New(105)
		assert.Equal(t, expectedOpen.String(), haSeries.Candles[0].OpenPrice.String())
	})

	t.Run("multiple candles", func(t *testing.T) {
		ts := series.NewTimeSeries()

		// First candle
		ts.AddCandle(&series.Candle{
			Period:     series.TimePeriod{Start: time.Now(), End: time.Now()},
			OpenPrice:  decimal.New(100),
			ClosePrice: decimal.New(110),
			MaxPrice:   decimal.New(115),
			MinPrice:   decimal.New(95),
			Volume:     decimal.New(1000),
		})

		// Second candle
		ts.AddCandle(&series.Candle{
			Period:     series.TimePeriod{Start: time.Now(), End: time.Now()},
			OpenPrice:  decimal.New(110),
			ClosePrice: decimal.New(120),
			MaxPrice:   decimal.New(125),
			MinPrice:   decimal.New(95),
			Volume:     decimal.New(1500),
		})

		haSeries := series.NewHeikinAshiseries(ts)

		assert.Equal(t, 2, len(haSeries.Candles))

		// Verify first HA candle
		expectedClose1 := decimal.New(105)
		assert.Equal(t, expectedClose1.String(), haSeries.Candles[0].ClosePrice.String())

		// Verify second HA candle
		// HA-Close = (110 + 125 + 95 + 120) / 4 = 112.5
		expectedClose2 := decimal.New(112.5)
		assert.Equal(t, expectedClose2.String(), haSeries.Candles[1].ClosePrice.String())

		// HA-Open = (105 + 105) / 2 = 105 (using previous HA-Open and HA-Close)
		expectedOpen2 := decimal.New(105)
		assert.Equal(t, expectedOpen2.String(), haSeries.Candles[1].OpenPrice.String())
	})
}
