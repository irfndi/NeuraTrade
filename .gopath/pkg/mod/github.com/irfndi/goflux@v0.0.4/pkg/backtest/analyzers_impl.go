package backtest

import (
	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/metrics"
)

// TradeStats represents basic trade statistics.
type TradeStats struct {
	TotalTrades    int
	WinningTrades  int
	LosingTrades   int
	WinRate        decimal.Decimal
	ProfitFactor   decimal.Decimal
	Expectancy     decimal.Decimal
	AverageWin     decimal.Decimal
	AverageLoss    decimal.Decimal
	TotalNetProfit decimal.Decimal
}

// TradeStatsAnalyzer analyzes trade-level performance.
type TradeStatsAnalyzer struct{}

func (a *TradeStatsAnalyzer) Name() string { return "TradeStats" }

func (a *TradeStatsAnalyzer) Analyze(trades []metrics.Trade, equityCurve []metrics.EquityPoint) interface{} {
	stats := TradeStats{}
	if len(trades) == 0 {
		return stats
	}

	stats.TotalTrades = len(trades)
	grossProfit := decimal.ZERO
	grossLoss := decimal.ZERO

	for _, trade := range trades {
		if trade.IsWin {
			stats.WinningTrades++
			grossProfit = grossProfit.Add(trade.Profit)
		} else {
			stats.LosingTrades++
			grossLoss = grossLoss.Sub(trade.Profit) // Profit is negative for losses
		}
		stats.TotalNetProfit = stats.TotalNetProfit.Add(trade.Profit)
	}

	stats.WinRate = decimal.New(float64(stats.WinningTrades)).Div(decimal.New(float64(stats.TotalTrades)))

	if !grossLoss.IsZero() {
		stats.ProfitFactor = grossProfit.Div(grossLoss)
	}

	if stats.WinningTrades > 0 {
		stats.AverageWin = grossProfit.Div(decimal.New(float64(stats.WinningTrades)))
	}
	if stats.LosingTrades > 0 {
		stats.AverageLoss = grossLoss.Div(decimal.New(float64(stats.LosingTrades)))
	}

	// Expectancy = (WinRate * AvgWin) - (LossRate * AvgLoss)
	lossRate := decimal.ONE.Sub(stats.WinRate)
	stats.Expectancy = stats.WinRate.Mul(stats.AverageWin).Sub(lossRate.Mul(stats.AverageLoss))

	return stats
}

// DrawdownStats represents drawdown statistics.
type DrawdownStats struct {
	MaxDrawdown    decimal.Decimal
	MaxDrawdownPct decimal.Decimal
}

// DrawdownAnalyzer analyzes drawdown performance.
type DrawdownAnalyzer struct{}

func (a *DrawdownAnalyzer) Name() string { return "Drawdown" }

func (a *DrawdownAnalyzer) Analyze(trades []metrics.Trade, equityCurve []metrics.EquityPoint) interface{} {
	stats := DrawdownStats{
		MaxDrawdown:    decimal.ZERO,
		MaxDrawdownPct: decimal.ZERO,
	}
	if len(equityCurve) == 0 {
		return stats
	}

	peak := decimal.ZERO
	for _, point := range equityCurve {
		if point.Equity.GT(peak) {
			peak = point.Equity
		}

		drawdown := peak.Sub(point.Equity)
		if drawdown.GT(stats.MaxDrawdown) {
			stats.MaxDrawdown = drawdown
			if !peak.IsZero() {
				stats.MaxDrawdownPct = drawdown.Div(peak)
			}
		}
	}

	return stats
}

// EquityCurveAnalyzer simply returns the equity curve data points.
type EquityCurveAnalyzer struct{}

func (a *EquityCurveAnalyzer) Name() string { return "EquityCurve" }

func (a *EquityCurveAnalyzer) Analyze(trades []metrics.Trade, equityCurve []metrics.EquityPoint) interface{} {
	return equityCurve
}

// SharpeRatioAnalyzer calculates the Sharpe Ratio.
type SharpeRatioAnalyzer struct {
	RiskFreeRate decimal.Decimal
}

func (a *SharpeRatioAnalyzer) Name() string { return "SharpeRatio" }

func (a *SharpeRatioAnalyzer) Analyze(trades []metrics.Trade, equityCurve []metrics.EquityPoint) interface{} {
	if len(trades) < 2 {
		return decimal.ZERO
	}

	// Calculate mean return
	sum := decimal.ZERO
	for _, trade := range trades {
		sum = sum.Add(trade.ProfitPct)
	}
	mean := sum.Div(decimal.New(float64(len(trades))))

	// Calculate standard deviation
	sumSquares := decimal.ZERO
	for _, trade := range trades {
		diff := trade.ProfitPct.Sub(mean)
		sumSquares = sumSquares.Add(diff.Mul(diff))
	}
	variance := sumSquares.Div(decimal.New(float64(len(trades) - 1)))
	stdDev := variance.Sqrt()

	if stdDev.IsZero() {
		return decimal.ZERO
	}

	excessReturn := mean.Sub(a.RiskFreeRate.Div(decimal.New(365.0)))
	// Simple annualization assuming daily trades for now
	// In a real system, we'd need timestamps to be more accurate
	annualizationFactor := decimal.New(252.0).Sqrt()
	sharpe := excessReturn.Div(stdDev).Mul(annualizationFactor)

	return sharpe
}
