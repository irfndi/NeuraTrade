package trading

import (
	"github.com/irfndi/goflux/pkg/decimal"
)

type PositionSizer interface {
	CalculateSize(config PositionSizingConfig) decimal.Decimal
}

type PositionSizingConfig struct {
	Capital      decimal.Decimal
	CurrentPrice decimal.Decimal
	StopLoss     decimal.Decimal
	RiskPerTrade decimal.Decimal
	ATR          decimal.Decimal
	Volatility   decimal.Decimal
	WinRate      decimal.Decimal
	AvgWin       decimal.Decimal
	AvgLoss      decimal.Decimal
}

func NewFixedFractionalSizer(fraction float64) PositionSizer {
	return &fixedFractionalSizer{fraction: decimal.New(fraction)}
}

type fixedFractionalSizer struct {
	fraction decimal.Decimal
}

func (ffs *fixedFractionalSizer) CalculateSize(config PositionSizingConfig) decimal.Decimal {
	if config.Capital.IsZero() {
		return decimal.ZERO
	}
	return config.Capital.Mul(ffs.fraction).Div(config.CurrentPrice)
}

func NewFixedAmountSizer(amount float64) PositionSizer {
	return &fixedAmountSizer{amount: decimal.New(amount)}
}

type fixedAmountSizer struct {
	amount decimal.Decimal
}

func (fas *fixedAmountSizer) CalculateSize(config PositionSizingConfig) decimal.Decimal {
	return fas.amount
}

func NewKellyCriterionSizer() PositionSizer {
	return &kellyCriterionSizer{}
}

type kellyCriterionSizer struct{}

func (kcs *kellyCriterionSizer) CalculateSize(config PositionSizingConfig) decimal.Decimal {
	if config.WinRate.IsZero() {
		return decimal.ZERO
	}

	winRate := config.WinRate
	lossRate := decimal.ONE.Sub(winRate)

	avgWin := config.AvgWin
	avgLoss := config.AvgLoss
	if avgLoss.IsZero() {
		return decimal.ZERO
	}

	winLossRatio := avgWin.Div(avgLoss)

	numerator := winRate.Mul(winLossRatio).Sub(lossRate)
	kellyFraction := numerator.Div(winLossRatio)

	if kellyFraction.IsNegative() {
		return decimal.ZERO
	}

	if kellyFraction.GT(decimal.New(0.5)) {
		kellyFraction = decimal.New(0.5)
	}

	return kellyFraction.Mul(config.Capital).Div(config.CurrentPrice)
}

func NewVolatilityBasedSizer(multiplier float64) PositionSizer {
	return &volatilityBasedSizer{multiplier: decimal.New(multiplier)}
}

type volatilityBasedSizer struct {
	multiplier decimal.Decimal
}

func (vbs *volatilityBasedSizer) CalculateSize(config PositionSizingConfig) decimal.Decimal {
	if config.Volatility.IsZero() || config.StopLoss.IsZero() || config.CurrentPrice.IsZero() {
		return decimal.ZERO
	}

	atrPercent := config.ATR.Div(config.CurrentPrice)

	atrStopLoss := config.CurrentPrice.Sub(config.CurrentPrice.Mul(atrPercent.Mul(vbs.multiplier)))

	if config.StopLoss.IsZero() {
		config.StopLoss = atrStopLoss
	}

	riskAmount := config.Capital.Mul(decimal.New(0.01))
	riskPerShare := config.CurrentPrice.Sub(config.StopLoss)

	if riskPerShare.IsZero() || riskPerShare.IsNegative() {
		return decimal.ZERO
	}

	size := riskAmount.Div(riskPerShare)

	maxSize := config.Capital.Mul(decimal.New(0.2)).Div(config.CurrentPrice)
	if size.GT(maxSize) {
		size = maxSize
	}

	return size
}

func NewRiskBasedSizer() PositionSizer {
	return &riskBasedSizer{}
}

type riskBasedSizer struct{}

func (rbs *riskBasedSizer) CalculateSize(config PositionSizingConfig) decimal.Decimal {
	if config.RiskPerTrade.IsZero() || config.StopLoss.IsZero() || config.CurrentPrice.IsZero() {
		return decimal.ZERO
	}

	riskAmount := config.Capital.Mul(config.RiskPerTrade)
	riskPerShare := config.CurrentPrice.Sub(config.StopLoss)

	if riskPerShare.IsZero() || riskPerShare.IsNegative() {
		return decimal.ZERO
	}

	size := riskAmount.Div(riskPerShare)

	maxSize := config.Capital.Mul(decimal.New(0.25)).Div(config.CurrentPrice)
	if size.GT(maxSize) {
		size = maxSize
	}

	return size
}

func NewCanonicalSizer() PositionSizer {
	return &canonicalSizer{}
}

type canonicalSizer struct{}

func (cs *canonicalSizer) CalculateSize(config PositionSizingConfig) decimal.Decimal {
	if !config.ATR.IsZero() && !config.Volatility.IsZero() {
		sizer := NewVolatilityBasedSizer(2.0)
		size := sizer.CalculateSize(config)
		if !size.IsZero() {
			return size
		}
	}

	if !config.RiskPerTrade.IsZero() {
		sizer := NewRiskBasedSizer()
		size := sizer.CalculateSize(config)
		if !size.IsZero() {
			return size
		}
	}

	sizer := NewFixedFractionalSizer(0.02)
	return sizer.CalculateSize(config)
}
