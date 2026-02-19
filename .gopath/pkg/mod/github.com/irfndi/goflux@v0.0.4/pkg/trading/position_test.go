package trading_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/trading"
)

func TestPositionNewRule(t *testing.T) {
	t.Run("returns true when position new", func(t *testing.T) {
		record := trading.NewTradingRecord()
		rule := trading.PositionNewRule{}

		assert.True(t, rule.IsSatisfied(0, record))
	})

	t.Run("returns false when position open", func(t *testing.T) {
		record := trading.NewTradingRecord()

		record.Operate(trading.Order{
			Side:   trading.BUY,
			Amount: decimal.ONE,
			Price:  decimal.ONE,
		})

		rule := trading.PositionNewRule{}

		assert.False(t, rule.IsSatisfied(0, record))
	})
}

func TestPositionOpenRule(t *testing.T) {
	t.Run("returns false when position new", func(t *testing.T) {
		record := trading.NewTradingRecord()

		rule := trading.PositionOpenRule{}

		assert.False(t, rule.IsSatisfied(0, record))
	})

	t.Run("returns true when position open", func(t *testing.T) {
		record := trading.NewTradingRecord()

		record.Operate(trading.Order{
			Side:   trading.BUY,
			Amount: decimal.ONE,
			Price:  decimal.ONE,
		})

		rule := trading.PositionOpenRule{}

		assert.True(t, rule.IsSatisfied(0, record))
	})
}

func TestNewPosition(t *testing.T) {
	entryOrder := trading.Order{
		Side:   trading.BUY,
		Amount: decimal.New(10),
		Price:  decimal.New(100),
	}

	position := trading.NewPosition(entryOrder)

	assert.NotNil(t, position)
	assert.NotNil(t, position.EntranceOrder())
	assert.Nil(t, position.ExitOrder())
	assert.Equal(t, decimal.New(1000), position.CostBasis())
}

func TestPositionIsNew(t *testing.T) {
	position := &trading.Position{}

	assert.True(t, position.IsNew())

	entryOrder := trading.Order{
		Side:   trading.BUY,
		Amount: decimal.New(10),
		Price:  decimal.New(100),
	}
	position.Enter(entryOrder)

	assert.False(t, position.IsNew())
}

func TestPositionIsOpen(t *testing.T) {
	position := &trading.Position{}

	assert.False(t, position.IsOpen())

	entryOrder := trading.Order{
		Side:   trading.BUY,
		Amount: decimal.New(10),
		Price:  decimal.New(100),
	}
	position.Enter(entryOrder)

	assert.True(t, position.IsOpen())

	exitOrder := trading.Order{
		Side:   trading.SELL,
		Amount: decimal.New(10),
		Price:  decimal.New(110),
	}
	position.Exit(exitOrder)

	assert.False(t, position.IsOpen())
}

func TestPositionIsClosed(t *testing.T) {
	position := &trading.Position{}

	assert.False(t, position.IsClosed())

	entryOrder := trading.Order{
		Side:   trading.BUY,
		Amount: decimal.New(10),
		Price:  decimal.New(100),
	}
	position.Enter(entryOrder)

	assert.False(t, position.IsClosed())

	exitOrder := trading.Order{
		Side:   trading.SELL,
		Amount: decimal.New(10),
		Price:  decimal.New(110),
	}
	position.Exit(exitOrder)

	assert.True(t, position.IsClosed())
}

func TestPositionCostBasis(t *testing.T) {
	position := &trading.Position{}

	assert.Equal(t, decimal.ZERO, position.CostBasis())

	entryOrder := trading.Order{
		Side:   trading.BUY,
		Amount: decimal.New(10),
		Price:  decimal.New(100),
	}
	position.Enter(entryOrder)

	assert.Equal(t, decimal.New(1000), position.CostBasis())
}

func TestPositionExitValue(t *testing.T) {
	position := &trading.Position{}

	assert.Equal(t, decimal.ZERO, position.ExitValue())

	entryOrder := trading.Order{
		Side:   trading.BUY,
		Amount: decimal.New(10),
		Price:  decimal.New(100),
	}
	position.Enter(entryOrder)

	assert.Equal(t, decimal.ZERO, position.ExitValue())

	exitOrder := trading.Order{
		Side:   trading.SELL,
		Amount: decimal.New(10),
		Price:  decimal.New(110),
	}
	position.Exit(exitOrder)

	assert.Equal(t, decimal.New(1100), position.ExitValue())
}

func TestPositionIsLong(t *testing.T) {
	position := &trading.Position{}

	assert.False(t, position.IsLong())

	entryOrder := trading.Order{
		Side:   trading.BUY,
		Amount: decimal.New(10),
		Price:  decimal.New(100),
	}
	position.Enter(entryOrder)

	assert.True(t, position.IsLong())

	entryOrder2 := trading.Order{
		Side:   trading.SELL,
		Amount: decimal.New(10),
		Price:  decimal.New(100),
	}
	position2 := trading.NewPosition(entryOrder2)

	assert.False(t, position2.IsLong())
}

func TestPositionIsShort(t *testing.T) {
	position := &trading.Position{}

	assert.False(t, position.IsShort())

	entryOrder := trading.Order{
		Side:   trading.SELL,
		Amount: decimal.New(10),
		Price:  decimal.New(100),
	}
	position.Enter(entryOrder)

	assert.True(t, position.IsShort())

	entryOrder2 := trading.Order{
		Side:   trading.BUY,
		Amount: decimal.New(10),
		Price:  decimal.New(100),
	}
	position2 := trading.NewPosition(entryOrder2)

	assert.False(t, position2.IsShort())
}
