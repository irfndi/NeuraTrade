package indicators

import "github.com/irfndi/goflux/pkg/decimal"

type fixedIndicator []float64

// NewFixedIndicator returns an indicator with a fixed set of values that are returned when an index is passed in
func NewFixedIndicator(vals ...float64) Indicator {
	return fixedIndicator(vals)
}

func (fi fixedIndicator) Calculate(index int) decimal.Decimal {
	if index < 0 || index >= len(fi) {
		return decimal.ZERO
	}
	return decimal.New(fi[index])
}

type fixedDecimalIndicator []decimal.Decimal

func NewFixedDecimalIndicator(vals ...decimal.Decimal) Indicator {
	return fixedDecimalIndicator(vals)
}

func (fi fixedDecimalIndicator) Calculate(index int) decimal.Decimal {
	if index < 0 || index >= len(fi) {
		return decimal.ZERO
	}
	return fi[index]
}
