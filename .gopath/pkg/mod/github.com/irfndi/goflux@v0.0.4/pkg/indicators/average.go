package indicators

import (
	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/math"
)

type averageIndicator struct {
	Indicator
	window int
}

// NewAverageGainsIndicator Returns a new average gains indicator, which returns the average gains
// in the given window based on the given indicator.
func NewAverageGainsIndicator(indicator Indicator, window int) Indicator {
	return averageIndicator{
		NewCumulativeGainsIndicator(indicator, window),
		window,
	}
}

// NewAverageLossesIndicator Returns a new average losses indicator, which returns the average losses
// in the given window based on the given indicator.
func NewAverageLossesIndicator(indicator Indicator, window int) Indicator {
	return averageIndicator{
		NewCumulativeLossesIndicator(indicator, window),
		window,
	}
}

func (ai averageIndicator) Calculate(index int) decimal.Decimal {
	return ai.Indicator.Calculate(index).Div(decimal.New(float64(math.Min(index+1, ai.window))))
}
