package trading

import "github.com/irfndi/goflux/pkg/indicators"

// NewCrossUpIndicatorRule returns a new rule that is satisfied when the lower indicator has crossed above the upper
// indicator.
func NewCrossUpIndicatorRule(upper, lower indicators.Indicator) Rule {
	return crossRule{
		upper: upper,
		lower: lower,
		cmp:   1,
	}
}

// NewCrossDownIndicatorRule returns a new rule that is satisfied when the upper indicator has crossed below the lower
// indicator.
func NewCrossDownIndicatorRule(upper, lower indicators.Indicator) Rule {
	return crossRule{
		upper: lower,
		lower: upper,
		cmp:   -1,
	}
}

type crossRule struct {
	upper indicators.Indicator
	lower indicators.Indicator
	cmp   int
}

func (cr crossRule) IsSatisfied(index int, record *TradingRecord) bool {
	i := index

	if i == 0 {
		return false
	}

	if cmp := cr.lower.Calculate(i).Cmp(cr.upper.Calculate(i)); cmp == 0 || cmp == cr.cmp {
		for ; i >= 0; i-- {
			if cmp = cr.lower.Calculate(i).Cmp(cr.upper.Calculate(i)); cmp == 0 || cmp == -cr.cmp {
				return true
			}
		}
	}

	return false
}
