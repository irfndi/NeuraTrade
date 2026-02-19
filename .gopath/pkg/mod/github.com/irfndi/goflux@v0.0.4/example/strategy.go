package example

import (
	"github.com/irfndi/goflux/pkg/indicators"
	"github.com/irfndi/goflux/pkg/trading"
)

// StrategyExample shows how to create a simple trading strategy. In this example, a position should
// be opened if the price moves above 70, and the position should be closed if a position moves below 30.
func StrategyExample() {
	indicator := BasicEma() // from basic.go

	// record trades on this object
	record := trading.NewTradingRecord()

	entryConstant := indicators.NewConstantIndicator(30)
	exitConstant := indicators.NewConstantIndicator(10)

	entryRule := trading.And(
		trading.NewCrossUpIndicatorRule(entryConstant, indicator),
		trading.PositionNewRule{}) // Is satisfied when the price ema moves above 30 and the current position is new

	exitRule := trading.And(
		trading.NewCrossDownIndicatorRule(indicator, exitConstant),
		trading.PositionOpenRule{}) // Is satisfied when the price ema moves below 10 and the current position is open

	strategy := trading.RuleStrategy{
		UnstablePeriod: 10,
		EntryRule:      entryRule,
		ExitRule:       exitRule,
	}

	strategy.ShouldEnter(0, record) // returns false
}
