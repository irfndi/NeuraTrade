package trading_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/irfndi/goflux/pkg/indicators"
	"github.com/irfndi/goflux/pkg/testutils"
	"github.com/irfndi/goflux/pkg/trading"
)

func TestIncreaseRule(t *testing.T) {
	t.Run("returns false when index == 0", func(t *testing.T) {
		rule := trading.IncreaseRule{}

		assert.False(t, rule.IsSatisfied(0, nil))
	})

	t.Run("returns true when increase", func(t *testing.T) {
		ts := testutils.MockTimeSeries("1", "2")
		rule := trading.IncreaseRule{Indicator: indicators.NewClosePriceIndicator(ts)}

		assert.True(t, rule.IsSatisfied(1, nil))
	})

	t.Run("returns false when same", func(t *testing.T) {
		ts := testutils.MockTimeSeries("1", "1")
		rule := trading.IncreaseRule{Indicator: indicators.NewClosePriceIndicator(ts)}

		assert.False(t, rule.IsSatisfied(1, nil))
	})

	t.Run("returns false when decrease", func(t *testing.T) {
		ts := testutils.MockTimeSeries("1", "0")
		rule := trading.IncreaseRule{Indicator: indicators.NewClosePriceIndicator(ts)}

		assert.False(t, rule.IsSatisfied(1, nil))
	})
}

func TestDecreaseRule(t *testing.T) {
	t.Run("returns false when index == 0", func(t *testing.T) {
		rule := trading.DecreaseRule{}

		assert.False(t, rule.IsSatisfied(0, nil))
	})

	t.Run("returns true when decrease", func(t *testing.T) {
		ts := testutils.MockTimeSeries("1", "0")
		rule := trading.DecreaseRule{Indicator: indicators.NewClosePriceIndicator(ts)}

		assert.True(t, rule.IsSatisfied(1, nil))
	})

	t.Run("returns false when  decrease", func(t *testing.T) {
		ts := testutils.MockTimeSeries("1", "2")
		rule := trading.DecreaseRule{Indicator: indicators.NewClosePriceIndicator(ts)}

		assert.False(t, rule.IsSatisfied(1, nil))
	})

	t.Run("returns false when same", func(t *testing.T) {
		ts := testutils.MockTimeSeries("1", "1")
		rule := trading.IncreaseRule{Indicator: indicators.NewClosePriceIndicator(ts)}

		assert.False(t, rule.IsSatisfied(1, nil))
	})
}
