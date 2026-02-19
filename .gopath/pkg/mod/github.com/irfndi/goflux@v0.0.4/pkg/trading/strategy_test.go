package trading_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/trading"
)

type alwaysSatisfiedRule struct{}

func (a alwaysSatisfiedRule) IsSatisfied(index int, record *trading.TradingRecord) bool {
	return true
}

func TestNewRuleStrategy(t *testing.T) {
	t.Run("Returns error when EntryRule is nil", func(t *testing.T) {
		_, err := trading.NewRuleStrategy(nil, alwaysSatisfiedRule{}, 5)
		assert.Error(t, err)
		assert.Equal(t, trading.ErrNilRule, err)
	})

	t.Run("Returns error when ExitRule is nil", func(t *testing.T) {
		_, err := trading.NewRuleStrategy(alwaysSatisfiedRule{}, nil, 5)
		assert.Error(t, err)
		assert.Equal(t, trading.ErrNilRule, err)
	})

	t.Run("Returns valid RuleStrategy when both rules are provided", func(t *testing.T) {
		s, err := trading.NewRuleStrategy(alwaysSatisfiedRule{}, alwaysSatisfiedRule{}, 5)
		assert.NoError(t, err)
		assert.NotNil(t, s.EntryRule)
		assert.NotNil(t, s.ExitRule)
		assert.Equal(t, 5, s.UnstablePeriod)
	})
}

func TestRuleStrategy_ShouldEnter(t *testing.T) {
	t.Run("Returns false if index < unstable period", func(t *testing.T) {
		record := trading.NewTradingRecord()

		s := trading.RuleStrategy{
			EntryRule:      alwaysSatisfiedRule{},
			ExitRule:       alwaysSatisfiedRule{},
			UnstablePeriod: 5,
		}

		assert.False(t, s.ShouldEnter(0, record))
	})

	t.Run("Returns false if a position is open", func(t *testing.T) {
		record := trading.NewTradingRecord()

		record.Operate(trading.Order{
			Side:   trading.BUY,
			Amount: decimal.ONE,
			Price:  decimal.ONE,
		})

		s := trading.RuleStrategy{
			EntryRule:      alwaysSatisfiedRule{},
			ExitRule:       alwaysSatisfiedRule{},
			UnstablePeriod: 5,
		}

		assert.False(t, s.ShouldEnter(6, record))
	})

	t.Run("Returns true when position is closed", func(t *testing.T) {
		record := trading.NewTradingRecord()

		s := trading.RuleStrategy{
			EntryRule:      alwaysSatisfiedRule{},
			ExitRule:       alwaysSatisfiedRule{},
			UnstablePeriod: 5,
		}

		assert.True(t, s.ShouldEnter(6, record))
	})

	t.Run("Returns false when entry rule is nil", func(t *testing.T) {
		s := trading.RuleStrategy{
			ExitRule:       alwaysSatisfiedRule{},
			UnstablePeriod: 10,
		}

		assert.False(t, s.ShouldEnter(0, nil))
	})
}

func TestRuleStrategy_ShouldExit(t *testing.T) {
	t.Run("Returns false if index < unstablePeriod", func(t *testing.T) {
		record := trading.NewTradingRecord()

		record.Operate(trading.Order{
			Side:   trading.BUY,
			Amount: decimal.ONE,
			Price:  decimal.ONE,
		})

		s := trading.RuleStrategy{
			EntryRule:      alwaysSatisfiedRule{},
			ExitRule:       alwaysSatisfiedRule{},
			UnstablePeriod: 5,
		}

		assert.False(t, s.ShouldExit(0, record))
	})

	t.Run("Returns false when position is closed", func(t *testing.T) {
		record := trading.NewTradingRecord()

		s := trading.RuleStrategy{
			EntryRule:      alwaysSatisfiedRule{},
			ExitRule:       alwaysSatisfiedRule{},
			UnstablePeriod: 5,
		}

		assert.False(t, s.ShouldExit(6, record))
	})

	t.Run("Returns true when position is open", func(t *testing.T) {
		record := trading.NewTradingRecord()

		record.Operate(trading.Order{
			Side:   trading.BUY,
			Amount: decimal.ONE,
			Price:  decimal.ONE,
		})

		s := trading.RuleStrategy{
			EntryRule:      alwaysSatisfiedRule{},
			ExitRule:       alwaysSatisfiedRule{},
			UnstablePeriod: 5,
		}

		assert.True(t, s.ShouldExit(6, record))
	})

	t.Run("Returns false when exit rule is nil", func(t *testing.T) {
		s := trading.RuleStrategy{
			EntryRule:      alwaysSatisfiedRule{},
			UnstablePeriod: 10,
		}

		assert.False(t, s.ShouldExit(0, nil))
	})
}
