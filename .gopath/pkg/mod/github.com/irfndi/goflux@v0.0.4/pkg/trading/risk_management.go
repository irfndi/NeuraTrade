package trading

import (
	"github.com/irfndi/goflux/pkg/decimal"
)

type DrawdownProtectionRule struct {
	MaxDrawdownPct decimal.Decimal
}

func NewDrawdownProtectionRule(maxDrawdownPct float64) Rule {
	return DrawdownProtectionRule{
		MaxDrawdownPct: decimal.New(maxDrawdownPct),
	}
}

func (dpr DrawdownProtectionRule) IsSatisfied(index int, record *TradingRecord) bool {
	if len(record.Trades) == 0 && record.CurrentPosition().IsNew() {
		return false
	}

	var peakBalance, currentBalance decimal.Decimal
	peakBalance = decimal.New(10000)

	for _, trade := range record.Trades {
		if trade.IsClosed() {
			pnl := trade.ExitValue().Sub(trade.CostBasis())
			currentBalance = currentBalance.Add(pnl)
		}
	}

	if record.CurrentPosition().IsOpen() {
		currentBalance = record.CurrentPosition().CostBasis()
	}

	if currentBalance.GT(peakBalance) {
		peakBalance = currentBalance
	}

	drawdown := peakBalance.Sub(currentBalance)
	if peakBalance.IsZero() {
		return false
	}

	drawdownPct := drawdown.Div(peakBalance)
	return drawdownPct.GTE(dpr.MaxDrawdownPct)
}

type MaxLossRule struct {
	MaxLossPct     decimal.Decimal
	InitialCapital decimal.Decimal
}

func NewMaxLossRule(maxLossPct float64) Rule {
	return MaxLossRule{
		MaxLossPct:     decimal.New(maxLossPct),
		InitialCapital: decimal.New(10000),
	}
}

func NewMaxLossRuleWithCapital(maxLossPct float64, initialCapital float64) Rule {
	return MaxLossRule{
		MaxLossPct:     decimal.New(maxLossPct),
		InitialCapital: decimal.New(initialCapital),
	}
}

func (mlr MaxLossRule) IsSatisfied(index int, record *TradingRecord) bool {
	if record.CurrentPosition().IsNew() && len(record.Trades) == 0 {
		return false
	}

	var totalPL decimal.Decimal
	for _, trade := range record.Trades {
		if trade.IsClosed() {
			pnl := trade.ExitValue().Sub(trade.CostBasis())
			totalPL = totalPL.Add(pnl)
		}
	}

	maxAllowedLoss := mlr.InitialCapital.Mul(mlr.MaxLossPct)
	currentLoss := mlr.InitialCapital.Sub(mlr.InitialCapital.Add(totalPL))

	return currentLoss.GTE(maxAllowedLoss)
}

type DailyLossLimitRule struct {
	MaxDailyLoss decimal.Decimal
	DailyPnL     decimal.Decimal
	SessionStart decimal.Decimal
}

func NewDailyLossLimitRule(maxDailyLoss float64) Rule {
	return DailyLossLimitRule{
		MaxDailyLoss: decimal.New(maxDailyLoss),
		DailyPnL:     decimal.ZERO,
		SessionStart: decimal.New(10000),
	}
}

func (dllr DailyLossLimitRule) IsSatisfied(index int, record *TradingRecord) bool {
	if len(record.Trades) == 0 {
		return false
	}

	var sessionPL decimal.Decimal
	for _, trade := range record.Trades {
		if trade.IsClosed() {
			pnl := trade.ExitValue().Sub(trade.CostBasis())
			sessionPL = sessionPL.Add(pnl)
		}
	}

	return sessionPL.LTE(dllr.MaxDailyLoss.Neg())
}

type ConsecutiveLossRule struct {
	MaxConsecutiveLosses int
	currentConsecutive   int
}

func NewConsecutiveLossRule(maxConsecutiveLosses int) Rule {
	return ConsecutiveLossRule{
		MaxConsecutiveLosses: maxConsecutiveLosses,
		currentConsecutive:   0,
	}
}

func (clr ConsecutiveLossRule) IsSatisfied(index int, record *TradingRecord) bool {
	if len(record.Trades) == 0 {
		return false
	}

	lastTrade := record.LastTrade()
	if lastTrade == nil || !lastTrade.IsClosed() {
		return false
	}

	lastPnL := lastTrade.ExitValue().Sub(lastTrade.CostBasis())
	if lastPnL.LT(decimal.ZERO) {
		clr.currentConsecutive++
	} else {
		clr.currentConsecutive = 0
	}

	return clr.currentConsecutive >= clr.MaxConsecutiveLosses
}

type PositionSizeRiskRule struct {
	MaxPositionSize decimal.Decimal
	CurrentSize     decimal.Decimal
}

func NewPositionSizeRiskRule(maxPositionSize float64) Rule {
	return PositionSizeRiskRule{
		MaxPositionSize: decimal.New(maxPositionSize),
		CurrentSize:     decimal.ZERO,
	}
}

func (psrr PositionSizeRiskRule) IsSatisfied(index int, record *TradingRecord) bool {
	if record.CurrentPosition().IsNew() {
		return false
	}

	psrr.CurrentSize = record.CurrentPosition().CostBasis()
	return psrr.CurrentSize.GT(psrr.MaxPositionSize)
}

type PortfolioExposureRule struct {
	MaxExposure decimal.Decimal
	TotalValue  decimal.Decimal
}

func NewPortfolioExposureRule(maxExposure float64) Rule {
	return PortfolioExposureRule{
		MaxExposure: decimal.New(maxExposure),
		TotalValue:  decimal.New(10000),
	}
}

func (per PortfolioExposureRule) IsSatisfied(index int, record *TradingRecord) bool {
	var totalExposure decimal.Decimal
	for _, trade := range record.Trades {
		if trade.IsOpen() {
			totalExposure = totalExposure.Add(trade.CostBasis())
		}
	}

	exposurePct := totalExposure.Div(per.TotalValue)
	return exposurePct.GTE(per.MaxExposure)
}

func (mlr *MaxLossRule) Reset() {
	mlr.InitialCapital = decimal.New(10000)
}
