package indicators

import (
	"github.com/irfndi/goflux/pkg/decimal"
)

type unstableIndicator struct {
	indicator      Indicator
	unstablePeriod int
}

// NewUnstableIndicator wraps an indicator and returns ZERO for any index within the unstable period.
func NewUnstableIndicator(indicator Indicator, unstablePeriod int) Indicator {
	return unstableIndicator{
		indicator:      indicator,
		unstablePeriod: unstablePeriod,
	}
}

func (ui unstableIndicator) Calculate(index int) decimal.Decimal {
	if index < ui.unstablePeriod {
		return decimal.ZERO
	}
	return ui.indicator.Calculate(index)
}
