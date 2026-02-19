package trading_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/trading"
)

func TestNewTradingRecord(t *testing.T) {
	record := trading.NewTradingRecord()

	assert.Len(t, record.Trades, 0)
	assert.True(t, record.CurrentPosition().IsNew())
}

func TestTradingRecord_CurrentTrade(t *testing.T) {
	record := trading.NewTradingRecord()

	yesterday := time.Now().Add(-time.Hour * 24)
	record.Operate(trading.Order{
		Side:          trading.BUY,
		Amount:        decimal.ONE,
		Price:         decimal.NewFromString("2"),
		ExecutionTime: yesterday,
	})

	assert.EqualValues(t, "1", record.CurrentPosition().EntranceOrder().Amount.String())
	assert.EqualValues(t, "2", record.CurrentPosition().EntranceOrder().Price.String())
	assert.EqualValues(t, yesterday.UnixNano(),
		record.CurrentPosition().EntranceOrder().ExecutionTime.UnixNano())

	now := time.Now()
	record.Operate(trading.Order{
		Side:          trading.SELL,
		Amount:        decimal.NewFromString("3"),
		Price:         decimal.NewFromString("4"),
		ExecutionTime: now,
	})
	assert.True(t, record.CurrentPosition().IsNew())

	lastTrade := record.LastTrade()

	assert.EqualValues(t, "3", lastTrade.ExitOrder().Amount.String())
	assert.EqualValues(t, "4", lastTrade.ExitOrder().Price.String())
	assert.EqualValues(t, now.UnixNano(),
		lastTrade.ExitOrder().ExecutionTime.UnixNano())
}

func TestTradingRecord_Enter(t *testing.T) {
	t.Run("Does not add trades older than last trade", func(t *testing.T) {
		record := trading.NewTradingRecord()

		now := time.Now()

		record.Operate(trading.Order{
			Side:          trading.BUY,
			Amount:        decimal.ONE,
			Price:         decimal.NewFromString("2"),
			ExecutionTime: now,
		})

		record.Operate(trading.Order{
			Side:          trading.SELL,
			Amount:        decimal.NewFromString("2"),
			Price:         decimal.NewFromString("2"),
			ExecutionTime: now.Add(time.Minute),
		})

		record.Operate(trading.Order{
			Side:          trading.BUY,
			Amount:        decimal.NewFromString("2"),
			Price:         decimal.NewFromString("2"),
			ExecutionTime: now.Add(-time.Minute),
		})

		assert.True(t, record.CurrentPosition().IsNew())
		assert.Len(t, record.Trades, 1)
	})
}

func TestTradingRecord_Exit(t *testing.T) {
	t.Run("Does not add trades older than last trade", func(t *testing.T) {
		record := trading.NewTradingRecord()

		now := time.Now()
		record.Operate(trading.Order{

			Side:          trading.BUY,
			Amount:        decimal.ONE,
			Price:         decimal.NewFromString("2"),
			ExecutionTime: now,
		})

		record.Operate(trading.Order{
			Side:          trading.SELL,
			Amount:        decimal.NewFromString("2"),
			Price:         decimal.NewFromString("2"),
			ExecutionTime: now.Add(-time.Minute),
		})

		assert.True(t, record.CurrentPosition().IsOpen())
	})
}
