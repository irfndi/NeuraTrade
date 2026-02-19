package indicators

import "github.com/irfndi/goflux/pkg/decimal"

// DerivativeIndicator returns an indicator that calculates the derivative of the underlying Indicator.
// The derivative is defined as the difference between the value at the previous index and the value at the current index.
// Eg series [1, 1, 2, 3, 5, 8] -> [0, 0, 1, 1, 2, 3]
type DerivativeIndicator struct {
	Indicator Indicator
}

// NewDerivativeIndicator returns an indicator that calculates the derivative of the underlying Indicator.
func NewDerivativeIndicator(indicator Indicator) Indicator {
	return DerivativeIndicator{Indicator: indicator}
}

// Calculate returns the derivative of the underlying indicator. At index 0, it will always return 0.
func (di DerivativeIndicator) Calculate(index int) decimal.Decimal {
	if index == 0 {
		return decimal.ZERO
	}

	return di.Indicator.Calculate(index).Sub(di.Indicator.Calculate(index - 1))
}
