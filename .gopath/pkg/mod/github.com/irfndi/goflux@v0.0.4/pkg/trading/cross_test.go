package trading_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/irfndi/goflux/pkg/indicators"
	"github.com/irfndi/goflux/pkg/trading"
)

func TestCrossUpIndicatorRule(t *testing.T) {
	upInd := indicators.NewFixedIndicator(3, 4, 5, 6)
	dnInd := indicators.NewFixedIndicator(6, 5, 4, 3)

	rule := trading.NewCrossUpIndicatorRule(dnInd, upInd)

	t.Run("always returns false when index == 0", func(t *testing.T) {
		assert.False(t, rule.IsSatisfied(0, nil))
	})

	t.Run("Returns true when lower indicator crosses upper indicator", func(t *testing.T) {
		assert.False(t, rule.IsSatisfied(1, nil))
		assert.True(t, rule.IsSatisfied(2, nil))
		assert.True(t, rule.IsSatisfied(3, nil))
	})
}

func TestCrossDownIndicatorRule(t *testing.T) {
	upInd := indicators.NewFixedIndicator(3, 4, 5, 6)
	dnInd := indicators.NewFixedIndicator(6, 5, 4, 3)

	rule := trading.NewCrossDownIndicatorRule(dnInd, upInd)

	t.Run("returns false when index == 0", func(t *testing.T) {
		assert.False(t, rule.IsSatisfied(0, nil))
	})

	t.Run("returns true when upper indicator crosses below lower indicator", func(t *testing.T) {
		assert.False(t, rule.IsSatisfied(1, nil))
		assert.True(t, rule.IsSatisfied(2, nil))
		assert.True(t, rule.IsSatisfied(3, nil))
	})
}
