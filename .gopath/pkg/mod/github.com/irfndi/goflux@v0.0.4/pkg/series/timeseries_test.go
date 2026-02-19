package series_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/series"
)

func TestTimeSeries_AddCandle(t *testing.T) {
	t.Run("Returns false if nil candle passed", func(t *testing.T) {
		ts := series.NewTimeSeries()
		assert.False(t, ts.AddCandle(nil))
	})

	t.Run("Adds candle if last is nil", func(t *testing.T) {
		ts := series.NewTimeSeries()

		candle := series.NewCandle(series.NewTimePeriod(time.Now(), time.Minute))
		candle.ClosePrice = decimal.New(1)

		ts.AddCandle(candle)

		assert.Len(t, ts.Candles, 1)
	})

	t.Run("Does not add candle if before last candle", func(t *testing.T) {
		ts := series.NewTimeSeries()

		now := time.Now()
		candle := series.NewCandle(series.NewTimePeriod(now, time.Minute))
		candle.ClosePrice = decimal.New(1)

		ts.AddCandle(candle)
		then := now.Add(-time.Minute * 10)

		nextCandle := series.NewCandle(series.NewTimePeriod(then, time.Minute))
		candle.ClosePrice = decimal.New(2)

		ts.AddCandle(nextCandle)

		assert.Len(t, ts.Candles, 1)
		assert.EqualValues(t, now.UnixNano(), ts.Candles[0].Period.Start.UnixNano())
	})
}

func TestTimeSeries_AddCandleErr(t *testing.T) {
	t.Run("Returns error if nil candle passed", func(t *testing.T) {
		ts := series.NewTimeSeries()
		err := ts.AddCandleErr(nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot be nil")
	})

	t.Run("Adds candle if last is nil", func(t *testing.T) {
		ts := series.NewTimeSeries()

		candle := series.NewCandle(series.NewTimePeriod(time.Now(), time.Minute))
		candle.ClosePrice = decimal.New(1)

		err := ts.AddCandleErr(candle)

		assert.NoError(t, err)
		assert.Len(t, ts.Candles, 1)
	})

	t.Run("Returns error if candle is before last candle", func(t *testing.T) {
		ts := series.NewTimeSeries()

		now := time.Now()
		candle := series.NewCandle(series.NewTimePeriod(now, time.Minute))
		candle.ClosePrice = decimal.New(1)

		err := ts.AddCandleErr(candle)

		assert.NoError(t, err)

		then := now.Add(-time.Minute * 10)
		nextCandle := series.NewCandle(series.NewTimePeriod(then, time.Minute))
		nextCandle.ClosePrice = decimal.New(2)

		err = ts.AddCandleErr(nextCandle)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not after last candle")
		assert.Len(t, ts.Candles, 1)
	})
}

func TestTimeSeries_LastCandle(t *testing.T) {
	ts := series.NewTimeSeries()

	now := time.Now()
	candle := series.NewCandle(series.NewTimePeriod(now, time.Minute))
	candle.ClosePrice = decimal.New(1)

	ts.AddCandle(candle)

	assert.EqualValues(t, now.UnixNano(), ts.LastCandle().Period.Start.UnixNano())
	assert.EqualValues(t, 1, ts.LastCandle().ClosePrice.Float())

	next := time.Now().Add(time.Minute)
	newCandle := series.NewCandle(series.NewTimePeriod(next, time.Minute))
	newCandle.ClosePrice = decimal.New(2)

	ts.AddCandle(newCandle)

	assert.Len(t, ts.Candles, 2)

	assert.EqualValues(t, next.UnixNano(), ts.LastCandle().Period.Start.UnixNano())
	assert.EqualValues(t, 2, ts.LastCandle().ClosePrice.Float())
}

func TestTimeSeries_LastIndex(t *testing.T) {
	ts := series.NewTimeSeries()

	candle := series.NewCandle(series.NewTimePeriod(time.Now(), time.Minute))
	ts.AddCandle(candle)

	assert.EqualValues(t, 0, ts.LastIndex())

	candle = series.NewCandle(series.NewTimePeriod(time.Now().Add(time.Minute), time.Minute))
	ts.AddCandle(candle)

	assert.EqualValues(t, 1, ts.LastIndex())
}
