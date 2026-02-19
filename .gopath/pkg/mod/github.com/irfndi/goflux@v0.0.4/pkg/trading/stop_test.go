package trading_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/testutils"
	"github.com/irfndi/goflux/pkg/trading"
)

func TestStopLossRule(t *testing.T) {
	t.Run("Returns false when position is new or closed", func(t *testing.T) {
		record := trading.NewTradingRecord()

		ts := testutils.MockTimeSeriesFl(1, 2, 3, 4)

		slr := trading.NewStopLossRule(ts, -0.1)

		assert.False(t, slr.IsSatisfied(3, record))
	})

	t.Run("Returns true when losses exceed tolerance", func(t *testing.T) {
		record := trading.NewTradingRecord()
		record.Operate(trading.Order{
			Side:   trading.BUY,
			Amount: decimal.NewFromString("10"),
			Price:  decimal.ONE,
		})

		ts := testutils.MockTimeSeriesFl(10, 9) // Lose 10%

		slr := trading.NewStopLossRule(ts, -0.05)

		assert.True(t, slr.IsSatisfied(1, record))
	})

	t.Run("Returns false when losses do not exceed tolerance", func(t *testing.T) {
		record := trading.NewTradingRecord()

		record.Operate(trading.Order{
			Side:   trading.BUY,
			Amount: decimal.NewFromString("10"),
			Price:  decimal.ONE,
		})

		ts := testutils.MockTimeSeriesFl(10, 10.1) // Gain 1%

		slr := trading.NewStopLossRule(ts, -0.05)

		assert.False(t, slr.IsSatisfied(1, record))
	})
}
