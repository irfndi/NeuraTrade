package metrics

import (
	"math"

	"github.com/irfndi/goflux/pkg/decimal"
)

type Trade struct {
	Profit    decimal.Decimal
	ProfitPct decimal.Decimal
	Duration  int
	IsWin     bool
}

type EquityPoint struct {
	Equity      decimal.Decimal
	Drawdown    decimal.Decimal
	DrawdownPct decimal.Decimal
}

type PerformanceMetrics struct {
	TotalTrades          int
	WinningTrades        int
	LosingTrades         int
	WinRate              decimal.Decimal
	TotalProfit          decimal.Decimal
	GrossProfit          decimal.Decimal
	GrossLoss            decimal.Decimal
	ProfitFactor         decimal.Decimal
	AverageWin           decimal.Decimal
	AverageLoss          decimal.Decimal
	AverageTrade         decimal.Decimal
	AverageWinPct        decimal.Decimal
	AverageLossPct       decimal.Decimal
	MaxConsecutiveWins   int
	MaxConsecutiveLosses int
	MaxDrawdown          decimal.Decimal
	MaxDrawdownPct       decimal.Decimal
	AvgDrawdown          decimal.Decimal
	AvgDrawdownPct       decimal.Decimal
	RecoveryFactor       decimal.Decimal
	RiskRewardRatio      decimal.Decimal
	CAGR                 decimal.Decimal
	SharpeRatio          decimal.Decimal
	SortinoRatio         decimal.Decimal
	CalmarRatio          decimal.Decimal
	SterlingRatio        decimal.Decimal
	BurkeRatio           decimal.Decimal
	Skewness             decimal.Decimal
	Kurtosis             decimal.Decimal
	FinalEquity          decimal.Decimal
	InitialEquity        decimal.Decimal
	TotalReturn          decimal.Decimal
	TotalReturnPct       decimal.Decimal
	RiskFreeRate         decimal.Decimal
	TradingDays          int
}

func NewPerformanceMetrics() *PerformanceMetrics {
	return &PerformanceMetrics{
		RiskFreeRate: decimal.New(0.02),
	}
}

func (pm *PerformanceMetrics) Calculate(trades []Trade, equityCurve []EquityPoint, initialEquity, finalEquity decimal.Decimal, tradingDays int) {
	pm.InitialEquity = initialEquity
	pm.FinalEquity = finalEquity
	pm.TradingDays = tradingDays
	pm.GrossProfit = decimal.ZERO
	pm.GrossLoss = decimal.ZERO
	pm.TotalProfit = decimal.ZERO
	pm.AverageWinPct = decimal.ZERO
	pm.AverageLossPct = decimal.ZERO

	if len(trades) == 0 {
		return
	}

	pm.TotalTrades = len(trades)

	var consecutiveWins, maxConsecutiveWins int
	var consecutiveLosses, maxConsecutiveLosses int
	totalWinPct := decimal.ZERO
	totalLossPct := decimal.ZERO

	for _, trade := range trades {
		if trade.IsWin {
			pm.WinningTrades++
			pm.GrossProfit = pm.GrossProfit.Add(trade.Profit)
			totalWinPct = totalWinPct.Add(trade.ProfitPct)
			consecutiveWins++
			consecutiveLosses = 0
			if consecutiveWins > maxConsecutiveWins {
				maxConsecutiveWins = consecutiveWins
			}
		} else {
			pm.LosingTrades++
			pm.GrossLoss = pm.GrossLoss.Sub(trade.Profit)
			totalLossPct = totalLossPct.Add(trade.ProfitPct)
			consecutiveLosses++
			consecutiveWins = 0
			if consecutiveLosses > maxConsecutiveLosses {
				maxConsecutiveLosses = consecutiveLosses
			}
		}

		pm.TotalProfit = pm.TotalProfit.Add(trade.Profit)
	}

	pm.MaxConsecutiveWins = maxConsecutiveWins
	pm.MaxConsecutiveLosses = maxConsecutiveLosses

	if pm.WinningTrades > 0 {
		pm.AverageWin = pm.GrossProfit.Div(decimal.New(float64(pm.WinningTrades)))
		pm.AverageWinPct = totalWinPct.Div(decimal.New(float64(pm.WinningTrades)))
	}

	if pm.LosingTrades > 0 {
		pm.AverageLoss = pm.GrossLoss.Div(decimal.New(float64(pm.LosingTrades)))
		pm.AverageLossPct = totalLossPct.Div(decimal.New(float64(pm.LosingTrades)))
	}

	pm.AverageTrade = pm.TotalProfit.Div(decimal.New(float64(pm.TotalTrades)))

	if pm.WinningTrades+pm.LosingTrades > 0 {
		pm.WinRate = decimal.New(float64(pm.WinningTrades)).Div(decimal.New(float64(pm.TotalTrades)))
	}

	if !pm.GrossLoss.IsZero() {
		pm.ProfitFactor = pm.GrossProfit.Div(pm.GrossLoss)
	}

	pm.TotalReturn = finalEquity.Sub(initialEquity)
	if !initialEquity.IsZero() {
		pm.TotalReturnPct = pm.TotalReturn.Div(initialEquity)
	}

	pm.calculateDrawdownMetrics(equityCurve)
	pm.calculateRiskAdjustedMetrics(trades)
}

func (pm *PerformanceMetrics) calculateDrawdownMetrics(equityCurve []EquityPoint) {
	maxDrawdown := decimal.ZERO
	maxDrawdownPct := decimal.ZERO
	totalDrawdown := decimal.ZERO
	totalDrawdownPct := decimal.ZERO
	peak := decimal.ZERO

	for _, point := range equityCurve {
		if point.Equity.GT(peak) {
			peak = point.Equity
		}

		drawdown := peak.Sub(point.Equity)
		if drawdown.GT(maxDrawdown) {
			maxDrawdown = drawdown
			if !peak.IsZero() {
				maxDrawdownPct = drawdown.Div(peak)
			}
		}

		totalDrawdown = totalDrawdown.Add(point.Drawdown)
		totalDrawdownPct = totalDrawdownPct.Add(point.DrawdownPct)
	}

	pm.MaxDrawdown = maxDrawdown
	pm.MaxDrawdownPct = maxDrawdownPct

	if len(equityCurve) > 0 {
		pm.AvgDrawdown = totalDrawdown.Div(decimal.New(float64(len(equityCurve))))
		pm.AvgDrawdownPct = totalDrawdownPct.Div(decimal.New(float64(len(equityCurve))))
	}

	if !maxDrawdown.IsZero() {
		pm.RecoveryFactor = pm.TotalProfit.Div(maxDrawdown)
	}

	if !pm.AverageLoss.IsZero() && pm.AverageWin.GT(pm.AverageLoss) {
		pm.RiskRewardRatio = pm.AverageWin.Div(pm.AverageLoss)
	}
}

func (pm *PerformanceMetrics) calculateRiskAdjustedMetrics(trades []Trade) {
	if len(trades) == 0 || pm.TradingDays == 0 {
		return
	}

	annualizationFactor := decimal.New(252.0).Div(decimal.New(float64(pm.TradingDays)))

	cagr := pm.calculateCAGR()
	pm.CAGR = cagr

	sharpe := pm.calculateSharpeRatio(trades, annualizationFactor)
	pm.SharpeRatio = sharpe

	sortino := pm.calculateSortinoRatio(trades, annualizationFactor)
	pm.SortinoRatio = sortino

	calmar := pm.calculateCalmarRatio(cagr)
	pm.CalmarRatio = calmar

	sterling := pm.calculateSterlingRatio(cagr)
	pm.SterlingRatio = sterling

	burke := pm.calculateBurkeRatio()
	pm.BurkeRatio = burke

	pm.calculateHigherMoments(trades)
}

func (pm *PerformanceMetrics) calculateCAGR() decimal.Decimal {
	if pm.InitialEquity.IsZero() || pm.FinalEquity.LTE(pm.InitialEquity) {
		return decimal.ZERO
	}

	years := decimal.New(float64(pm.TradingDays)).Div(decimal.New(365.0))
	if years.IsZero() {
		return decimal.ZERO
	}

	equityRatio := pm.FinalEquity.Div(pm.InitialEquity)
	exponent := 1.0 / years.Float()
	cagr := decimal.New(math.Pow(equityRatio.Float(), exponent))

	return cagr.Sub(decimal.New(1))
}

func (pm *PerformanceMetrics) calculateSharpeRatio(trades []Trade, annualizationFactor decimal.Decimal) decimal.Decimal {
	if len(trades) < 2 {
		return decimal.ZERO
	}

	meanReturn := pm.calculateMeanReturn(trades)
	stdDev := pm.calculateStandardDeviation(trades, meanReturn)

	if stdDev.IsZero() {
		return decimal.ZERO
	}

	excessReturn := meanReturn.Sub(pm.RiskFreeRate.Div(decimal.New(365.0)))
	sharpe := excessReturn.Div(stdDev).Mul(annualizationFactor.Sqrt())

	return sharpe
}

func (pm *PerformanceMetrics) calculateSortinoRatio(trades []Trade, annualizationFactor decimal.Decimal) decimal.Decimal {
	if len(trades) < 2 {
		return decimal.ZERO
	}

	meanReturn := pm.calculateMeanReturn(trades)
	downsideDev := pm.calculateDownsideDeviation(trades, meanReturn)

	if downsideDev.IsZero() {
		return decimal.ZERO
	}

	excessReturn := meanReturn.Sub(pm.RiskFreeRate.Div(decimal.New(365.0)))
	sortino := excessReturn.Div(downsideDev).Mul(annualizationFactor.Sqrt())

	return sortino
}

func (pm *PerformanceMetrics) calculateCalmarRatio(cagr decimal.Decimal) decimal.Decimal {
	if pm.MaxDrawdown.IsZero() || pm.MaxDrawdownPct.IsZero() {
		return decimal.ZERO
	}

	return cagr.Div(pm.MaxDrawdownPct)
}

func (pm *PerformanceMetrics) calculateSterlingRatio(cagr decimal.Decimal) decimal.Decimal {
	if pm.AvgDrawdownPct.IsZero() {
		return decimal.ZERO
	}

	adjustedDrawdown := pm.AvgDrawdownPct.Mul(decimal.New(1.5))
	if adjustedDrawdown.IsZero() {
		return decimal.ZERO
	}

	return cagr.Div(adjustedDrawdown)
}

func (pm *PerformanceMetrics) calculateBurkeRatio() decimal.Decimal {
	if pm.MaxDrawdown.IsZero() {
		return decimal.ZERO
	}

	drawdownVariance := pm.MaxDrawdown.Pow(2)
	if drawdownVariance.IsZero() {
		return decimal.ZERO
	}

	return pm.TotalProfit.Div(drawdownVariance)
}

func (pm *PerformanceMetrics) calculateMeanReturn(trades []Trade) decimal.Decimal {
	if len(trades) == 0 {
		return decimal.ZERO
	}

	sum := decimal.ZERO
	for _, trade := range trades {
		sum = sum.Add(trade.ProfitPct)
	}

	return sum.Div(decimal.New(float64(len(trades))))
}

func (pm *PerformanceMetrics) calculateStandardDeviation(trades []Trade, mean decimal.Decimal) decimal.Decimal {
	if len(trades) < 2 {
		return decimal.ZERO
	}

	sumSquares := decimal.ZERO
	for _, trade := range trades {
		diff := trade.ProfitPct.Sub(mean)
		sumSquares = sumSquares.Add(diff.Mul(diff))
	}

	variance := sumSquares.Div(decimal.New(float64(len(trades) - 1)))

	return variance.Sqrt()
}

func (pm *PerformanceMetrics) calculateDownsideDeviation(trades []Trade, mean decimal.Decimal) decimal.Decimal {
	if len(trades) < 2 {
		return decimal.ZERO
	}

	sumSquares := decimal.ZERO
	for _, trade := range trades {
		if trade.ProfitPct.LT(mean) {
			diff := mean.Sub(trade.ProfitPct)
			sumSquares = sumSquares.Add(diff.Mul(diff))
		}
	}

	targetReturn := decimal.New(0)
	nDownside := 0
	for _, trade := range trades {
		if trade.ProfitPct.LT(targetReturn) {
			nDownside++
		}
	}

	if nDownside < 2 {
		return decimal.New(0.0001)
	}

	variance := sumSquares.Div(decimal.New(float64(nDownside - 1)))

	return variance.Sqrt()
}

func (pm *PerformanceMetrics) calculateHigherMoments(trades []Trade) {
	if len(trades) < 3 {
		return
	}

	mean := pm.calculateMeanReturn(trades)
	stdDev := pm.calculateStandardDeviation(trades, mean)

	if stdDev.IsZero() {
		return
	}

	sumSkewness := decimal.ZERO
	sumKurtosis := decimal.ZERO
	for _, trade := range trades {
		z := trade.ProfitPct.Sub(mean).Div(stdDev)
		sumSkewness = sumSkewness.Add(z.Pow(3))
		sumKurtosis = sumKurtosis.Add(z.Pow(4))
	}

	n := decimal.New(float64(len(trades)))
	pm.Skewness = sumSkewness.Div(n)
	pm.Kurtosis = sumKurtosis.Div(n).Sub(decimal.New(3))
}

func (pm *PerformanceMetrics) String() string {
	return pm.formatMetrics()
}

func (pm *PerformanceMetrics) formatMetrics() string {
	result := "Performance Metrics:\n"
	result += "═══════════════════════════════════════\n"
	result += pm.formatMetric("Total Trades", pm.TotalTradeString())
	result += pm.formatMetric("Win Rate", pm.WinRateFormatted())
	result += pm.formatMetric("Profit Factor", pm.ProfitFactor.String())
	result += pm.formatMetric("Net Profit", pm.TotalProfit.String())
	result += pm.formatMetric("CAGR", pm.CAGR.String())
	result += pm.formatMetric("Sharpe Ratio", pm.SharpeRatio.String())
	result += pm.formatMetric("Sortino Ratio", pm.SortinoRatio.String())
	result += pm.formatMetric("Calmar Ratio", pm.CalmarRatio.String())
	result += pm.formatMetric("Max Drawdown", pm.MaxDrawdown.String())
	result += pm.formatMetric("Recovery Factor", pm.RecoveryFactor.String())
	result += pm.formatMetric("Average Win", pm.AverageWin.String())
	result += pm.formatMetric("Average Loss", pm.AverageLoss.String())
	result += pm.formatMetric("Total Return", pm.TotalReturnPct.String())
	return result
}

func (pm *PerformanceMetrics) formatMetric(name, value string) string {
	return name + ": " + value + "\n"
}

func (pm *PerformanceMetrics) TotalTradeString() string {
	return decimal.New(float64(pm.TotalTrades)).String()
}

func (pm *PerformanceMetrics) WinRateFormatted() string {
	return pm.WinRate.Mul(decimal.New(100)).FormattedString(2) + "%"
}
