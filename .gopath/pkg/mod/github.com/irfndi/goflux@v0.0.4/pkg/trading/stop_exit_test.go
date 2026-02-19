package trading_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/series"
	"github.com/irfndi/goflux/pkg/testutils"
	"github.com/irfndi/goflux/pkg/trading"
)

func testTime(seconds int) time.Time {
	return time.Unix(int64(seconds), 0)
}

var testDuration = time.Second

func TestTrailingStopLossRule(t *testing.T) {
	t.Run("Returns false when position is new or closed", func(t *testing.T) {
		record := trading.NewTradingRecord()
		ts := testutils.MockTimeSeriesFl(1, 2, 3, 4)
		tsl := trading.NewTrailingStopLossRule(ts, -0.1)
		assert.False(t, tsl.IsSatisfied(3, record))
	})

	t.Run("Returns false when price hasn't dropped from peak", func(t *testing.T) {
		record := trading.NewTradingRecord()
		record.Operate(trading.Order{
			Side:          trading.BUY,
			Amount:        decimal.NewFromString("10"),
			Price:         decimal.NewFromString("100"),
			ExecutionTime: testTime(1),
		})

		ts := testutils.MockTimeSeriesFl(100, 105, 110, 115)

		tsl := trading.NewTrailingStopLossRule(ts, -0.1)

		assert.False(t, tsl.IsSatisfied(3, record))
	})

	t.Run("Returns true when price drops 10% from peak", func(t *testing.T) {
		record := trading.NewTradingRecord()
		record.Operate(trading.Order{
			Side:          trading.BUY,
			Amount:        decimal.NewFromString("10"),
			Price:         decimal.NewFromString("100"),
			ExecutionTime: testTime(1),
		})

		ts := testutils.MockTimeSeriesFl(100, 110, 100, 99)

		tsl := trading.NewTrailingStopLossRule(ts, -0.1)

		assert.True(t, tsl.IsSatisfied(3, record))
	})

	t.Run("Returns false when drop is less than tolerance", func(t *testing.T) {
		record := trading.NewTradingRecord()
		record.Operate(trading.Order{
			Side:          trading.BUY,
			Amount:        decimal.NewFromString("10"),
			Price:         decimal.NewFromString("100"),
			ExecutionTime: testTime(1),
		})

		ts := testutils.MockTimeSeriesFl(100, 110, 105, 104)

		tsl := trading.NewTrailingStopLossRule(ts, -0.1)

		assert.False(t, tsl.IsSatisfied(3, record))
	})
}

func TestTrailingTakeProfitRule(t *testing.T) {
	t.Run("Returns false when position is new or closed", func(t *testing.T) {
		record := trading.NewTradingRecord()
		ts := testutils.MockTimeSeriesFl(1, 2, 3, 4)
		ttp := trading.NewTrailingTakeProfitRule(ts, 0.1, 0.05)
		assert.False(t, ttp.IsSatisfied(3, record))
	})

	t.Run("Returns false when threshold profit not reached", func(t *testing.T) {
		record := trading.NewTradingRecord()
		record.Operate(trading.Order{
			Side:          trading.BUY,
			Amount:        decimal.NewFromString("10"),
			Price:         decimal.NewFromString("100"),
			ExecutionTime: testTime(1),
		})

		ts := testutils.MockTimeSeriesFl(100, 105, 108, 109)

		ttp := trading.NewTrailingTakeProfitRule(ts, 0.15, 0.05)

		assert.False(t, ttp.IsSatisfied(3, record))
	})

	t.Run("Returns false when threshold reached but no trailing drop", func(t *testing.T) {
		record := trading.NewTradingRecord()
		record.Operate(trading.Order{
			Side:          trading.BUY,
			Amount:        decimal.NewFromString("10"),
			Price:         decimal.NewFromString("100"),
			ExecutionTime: testTime(1),
		})

		ts := testutils.MockTimeSeriesFl(100, 115, 120, 125)

		ttp := trading.NewTrailingTakeProfitRule(ts, 0.1, 0.05)

		assert.False(t, ttp.IsSatisfied(3, record))
	})

	t.Run("Returns true when threshold reached then price drops by trailing amount", func(t *testing.T) {
		record := trading.NewTradingRecord()
		record.Operate(trading.Order{
			Side:          trading.BUY,
			Amount:        decimal.NewFromString("10"),
			Price:         decimal.NewFromString("100"),
			ExecutionTime: testTime(1),
		})

		ts := testutils.MockTimeSeriesFl(100, 120, 115, 114)

		ttp := trading.NewTrailingTakeProfitRule(ts, 0.1, 0.05)

		assert.True(t, ttp.IsSatisfied(3, record))
	})

	t.Run("Returns false when drop is less than trailing amount", func(t *testing.T) {
		record := trading.NewTradingRecord()
		record.Operate(trading.Order{
			Side:          trading.BUY,
			Amount:        decimal.NewFromString("10"),
			Price:         decimal.NewFromString("100"),
			ExecutionTime: testTime(1),
		})

		ts := testutils.MockTimeSeriesFl(100, 120, 118, 117)

		ttp := trading.NewTrailingTakeProfitRule(ts, 0.1, 0.05)

		assert.False(t, ttp.IsSatisfied(3, record))
	})
}

func TestFixedBarExitRule(t *testing.T) {
	t.Run("Returns false when position is new or closed", func(t *testing.T) {
		record := trading.NewTradingRecord()
		ts := series.NewTimeSeries()
		fbe := trading.NewFixedBarExitRule(ts, 5)
		assert.False(t, fbe.IsSatisfied(3, record))
	})

	t.Run("Returns false before bar count reached", func(t *testing.T) {
		ts := series.NewTimeSeries()
		for i := 0; i < 10; i++ {
			candle := series.NewCandle(series.NewTimePeriod(testTime(i), testDuration))
			candle.OpenPrice = decimal.New(100)
			candle.ClosePrice = decimal.New(100)
			candle.MaxPrice = decimal.New(101)
			candle.MinPrice = decimal.New(99)
			ts.AddCandle(candle)
		}

		record := trading.NewTradingRecord()
		record.Operate(trading.Order{
			Side:          trading.BUY,
			Amount:        decimal.NewFromString("10"),
			Price:         decimal.NewFromString("100"),
			ExecutionTime: testTime(1),
		})

		fbe := trading.NewFixedBarExitRule(ts, 5)

		assert.False(t, fbe.IsSatisfied(4, record))
	})

	t.Run("Returns true when bar count is reached", func(t *testing.T) {
		ts := series.NewTimeSeries()
		for i := 0; i < 10; i++ {
			candle := series.NewCandle(series.NewTimePeriod(testTime(i), testDuration))
			candle.OpenPrice = decimal.New(100)
			candle.ClosePrice = decimal.New(100)
			candle.MaxPrice = decimal.New(101)
			candle.MinPrice = decimal.New(99)
			ts.AddCandle(candle)
		}

		record := trading.NewTradingRecord()
		record.Operate(trading.Order{
			Side:          trading.BUY,
			Amount:        decimal.NewFromString("10"),
			Price:         decimal.NewFromString("100"),
			ExecutionTime: testTime(1),
		})

		fbe := trading.NewFixedBarExitRule(ts, 5)

		assert.True(t, fbe.IsSatisfied(5, record))
	})

	t.Run("Returns true when bar count exceeded", func(t *testing.T) {
		ts := series.NewTimeSeries()
		for i := 0; i < 10; i++ {
			candle := series.NewCandle(series.NewTimePeriod(testTime(i), testDuration))
			candle.OpenPrice = decimal.New(100)
			candle.ClosePrice = decimal.New(100)
			candle.MaxPrice = decimal.New(101)
			candle.MinPrice = decimal.New(99)
			ts.AddCandle(candle)
		}

		record := trading.NewTradingRecord()
		record.Operate(trading.Order{
			Side:          trading.BUY,
			Amount:        decimal.NewFromString("10"),
			Price:         decimal.NewFromString("100"),
			ExecutionTime: testTime(1),
		})

		fbe := trading.NewFixedBarExitRule(ts, 5)

		assert.True(t, fbe.IsSatisfied(9, record))
	})
}

func TestWaitDurationRule(t *testing.T) {
	t.Run("Returns false when position is new or closed", func(t *testing.T) {
		record := trading.NewTradingRecord()
		ts := series.NewTimeSeries()
		wdr := trading.NewWaitDurationRule(ts, time.Hour)
		assert.False(t, wdr.IsSatisfied(3, record))
	})

	t.Run("Returns false before duration reached", func(t *testing.T) {
		ts := series.NewTimeSeries()
		for i := 0; i < 10; i++ {
			// Use contiguous periods
			candle := series.NewCandle(series.NewTimePeriod(testTime(i*60), time.Minute))
			candle.OpenPrice = decimal.New(100)
			candle.ClosePrice = decimal.New(100)
			candle.MaxPrice = decimal.New(101)
			candle.MinPrice = decimal.New(99)
			ts.AddCandle(candle)
		}

		record := trading.NewTradingRecord()
		record.Operate(trading.Order{
			Side:          trading.BUY,
			Amount:        decimal.NewFromString("10"),
			Price:         decimal.NewFromString("100"),
			ExecutionTime: testTime(60), // End of first candle
		})

		// 5 minutes
		wdr := trading.NewWaitDurationRule(ts, 5*time.Minute)

		assert.False(t, wdr.IsSatisfied(4, record))
	})

	t.Run("Returns true when duration is reached", func(t *testing.T) {
		ts := series.NewTimeSeries()
		for i := 0; i < 600; i++ {
			// Use contiguous periods
			candle := series.NewCandle(series.NewTimePeriod(testTime(i), time.Second))
			candle.OpenPrice = decimal.New(100)
			candle.ClosePrice = decimal.New(100)
			candle.MaxPrice = decimal.New(101)
			candle.MinPrice = decimal.New(99)
			ts.AddCandle(candle)
		}

		record := trading.NewTradingRecord()
		record.Operate(trading.Order{
			Side:          trading.BUY,
			Amount:        decimal.NewFromString("10"),
			Price:         decimal.NewFromString("100"),
			ExecutionTime: testTime(1), // End of first candle
		})

		// 5 minutes = 300 seconds
		wdr := trading.NewWaitDurationRule(ts, 300*time.Second)

		// At index 299, end time is 300. 300-1 = 299. < 300.
		assert.False(t, wdr.IsSatisfied(299, record))
		// At index 300, end time is 301. 301-1 = 300. >= 300.
		assert.True(t, wdr.IsSatisfied(300, record))
	})
}

func TestTimeOfDayExitRule(t *testing.T) {
	t.Run("Returns false when position is new or closed", func(t *testing.T) {
		record := trading.NewTradingRecord()
		ts := series.NewTimeSeries()
		tdr := trading.NewTimeOfDayExitRule(ts, 15, 30)
		assert.False(t, tdr.IsSatisfied(3, record))
	})

	t.Run("Returns false before time reached", func(t *testing.T) {
		ts := series.NewTimeSeries()
		// 14:00
		baseTime := time.Date(2023, 1, 1, 14, 0, 0, 0, time.UTC)
		for i := 0; i < 60; i++ {
			candle := series.NewCandle(series.NewTimePeriod(baseTime.Add(time.Duration(i)*time.Minute), time.Minute))
			candle.OpenPrice = decimal.New(100)
			candle.ClosePrice = decimal.New(100)
			ts.AddCandle(candle)
		}

		record := trading.NewTradingRecord()
		record.Operate(trading.Order{
			Side:          trading.BUY,
			Amount:        decimal.NewFromString("10"),
			Price:         decimal.NewFromString("100"),
			ExecutionTime: baseTime.Add(time.Minute),
		})

		// Exit at 15:30
		tdr := trading.NewTimeOfDayExitRule(ts, 15, 30)

		// At index 59, time is 14:00 + 60 min = 15:00.
		assert.False(t, tdr.IsSatisfied(59, record))
	})

	t.Run("Returns true when time reached", func(t *testing.T) {
		ts := series.NewTimeSeries()
		// 15:00
		baseTime := time.Date(2023, 1, 1, 15, 0, 0, 0, time.UTC)
		for i := 0; i < 60; i++ {
			candle := series.NewCandle(series.NewTimePeriod(baseTime.Add(time.Duration(i)*time.Minute), time.Minute))
			candle.OpenPrice = decimal.New(100)
			candle.ClosePrice = decimal.New(100)
			ts.AddCandle(candle)
		}

		record := trading.NewTradingRecord()
		record.Operate(trading.Order{
			Side:          trading.BUY,
			Amount:        decimal.NewFromString("10"),
			Price:         decimal.NewFromString("100"),
			ExecutionTime: baseTime.Add(time.Minute),
		})

		// Exit at 15:30
		tdr := trading.NewTimeOfDayExitRule(ts, 15, 30)

		// At index 29, end time is 15:00 + 30 min = 15:30.
		assert.True(t, tdr.IsSatisfied(29, record))
	})
}
