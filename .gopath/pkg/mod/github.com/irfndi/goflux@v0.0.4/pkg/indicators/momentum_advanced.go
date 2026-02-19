package indicators

import (
	"github.com/irfndi/goflux/pkg/decimal"
)

type cmoIndicator struct {
	gains  Indicator
	losses Indicator
}

// NewChandeMomentumOscillatorIndicator returns a new Chande Momentum Oscillator
func NewChandeMomentumOscillatorIndicator(indicator Indicator, window int) Indicator {
	return cmoIndicator{
		gains:  NewCumulativeGainsIndicator(indicator, window),
		losses: NewCumulativeLossesIndicator(indicator, window),
	}
}

func (cmo cmoIndicator) Calculate(index int) decimal.Decimal {
	gains := cmo.gains.Calculate(index)
	losses := cmo.losses.Calculate(index)

	sum := gains.Add(losses)
	if sum.IsZero() {
		return decimal.ZERO
	}

	diff := gains.Sub(losses)
	return diff.Div(sum).Mul(decimal.New(100))
}
