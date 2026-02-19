package backtest

import (
	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/metrics"
)

type ExpectancyAnalyzer struct{}

func (ea *ExpectancyAnalyzer) Name() string {
	return "expectancy"
}

func (ea *ExpectancyAnalyzer) Analyze(trades []metrics.Trade, equityCurve []metrics.EquityPoint) interface{} {
	if len(trades) == 0 {
		return decimal.ZERO
	}

	totalTrades := decimal.New(float64(len(trades)))
	winningTrades := 0
	totalWinPct := decimal.ZERO
	totalLossPct := decimal.ZERO

	for _, trade := range trades {
		if trade.IsWin {
			winningTrades++
			totalWinPct = totalWinPct.Add(trade.ProfitPct)
		} else {
			totalLossPct = totalLossPct.Add(trade.ProfitPct.Abs())
		}
	}

	if winningTrades > 0 {
		winRate := decimal.New(float64(winningTrades)).Div(totalTrades)
		avgWinPct := totalWinPct.Div(decimal.New(float64(winningTrades)))
		avgLossPct := decimal.ZERO
		if len(trades)-winningTrades > 0 {
			avgLossPct = totalLossPct.Div(decimal.New(float64(len(trades) - winningTrades)))
		}

		lossRate := decimal.ONE.Sub(winRate)
		expectancy := winRate.Mul(avgWinPct).Sub(lossRate.Mul(avgLossPct))
		return expectancy
	}

	return decimal.ZERO
}

type ProfitFactorAnalyzer struct{}

func (pfa *ProfitFactorAnalyzer) Name() string {
	return "profit_factor"
}

func (pfa *ProfitFactorAnalyzer) Analyze(trades []metrics.Trade, equityCurve []metrics.EquityPoint) interface{} {
	grossProfit := decimal.ZERO
	grossLoss := decimal.ZERO

	for _, trade := range trades {
		if trade.Profit.IsPositive() {
			grossProfit = grossProfit.Add(trade.Profit)
		} else {
			grossLoss = grossLoss.Add(trade.Profit.Abs())
		}
	}

	if grossLoss.IsZero() {
		if grossProfit.IsZero() {
			return decimal.ZERO
		}
		return decimal.New(999)
	}

	return grossProfit.Div(grossLoss)
}

type AverageTradeDurationAnalyzer struct{}

func (atda *AverageTradeDurationAnalyzer) Name() string {
	return "avg_trade_duration"
}

func (atda *AverageTradeDurationAnalyzer) Analyze(trades []metrics.Trade, equityCurve []metrics.EquityPoint) interface{} {
	if len(trades) == 0 {
		return 0
	}

	totalDuration := 0
	for _, trade := range trades {
		totalDuration += trade.Duration
	}

	return totalDuration / len(trades)
}

type MaxConsecutiveAnalyzer struct{}

func (mca *MaxConsecutiveAnalyzer) Name() string {
	return "max_consecutive"
}

func (mca *MaxConsecutiveAnalyzer) Analyze(trades []metrics.Trade, equityCurve []metrics.EquityPoint) interface{} {
	result := map[string]int{
		"wins":   0,
		"losses": 0,
	}

	maxWins := 0
	maxLosses := 0
	currentWins := 0
	currentLosses := 0

	for _, trade := range trades {
		if trade.IsWin {
			currentWins++
			currentLosses = 0
			if currentWins > maxWins {
				maxWins = currentWins
			}
		} else {
			currentLosses++
			currentWins = 0
			if currentLosses > maxLosses {
				maxLosses = currentLosses
			}
		}
	}

	result["wins"] = maxWins
	result["losses"] = maxLosses
	return result
}

type ExpectancyPerTradeAnalyzer struct{}

func (ept *ExpectancyPerTradeAnalyzer) Name() string {
	return "expectancy_per_trade"
}

func (ept *ExpectancyPerTradeAnalyzer) Analyze(trades []metrics.Trade, equityCurve []metrics.EquityPoint) interface{} {
	if len(trades) == 0 {
		return decimal.ZERO
	}

	totalProfit := decimal.ZERO
	for _, trade := range trades {
		totalProfit = totalProfit.Add(trade.Profit)
	}

	return totalProfit.Div(decimal.New(float64(len(trades))))
}

type SystemQualityNumberAnalyzer struct{}

func (sqna *SystemQualityNumberAnalyzer) Name() string {
	return "sqn"
}

func (sqna *SystemQualityNumberAnalyzer) Analyze(trades []metrics.Trade, equityCurve []metrics.EquityPoint) interface{} {
	if len(trades) < 10 {
		return decimal.ZERO
	}

	expectancyAnalyzer := &ExpectancyAnalyzer{}
	expectancy := expectancyAnalyzer.Analyze(trades, equityCurve).(decimal.Decimal)

	avgTrade := decimal.ZERO
	for _, trade := range trades {
		avgTrade = avgTrade.Add(trade.Profit)
	}
	avgTrade = avgTrade.Div(decimal.New(float64(len(trades))))

	stdDev := decimal.ZERO
	for _, trade := range trades {
		diff := trade.Profit.Sub(avgTrade)
		stdDev = stdDev.Add(diff.Mul(diff))
	}
	if len(trades) > 1 {
		stdDev = stdDev.Div(decimal.New(float64(len(trades) - 1)))
		stdDev = stdDev.Sqrt()
	}

	if stdDev.IsZero() {
		return decimal.ZERO
	}

	sqn := expectancy.Div(stdDev).Mul(decimal.New(float64(len(trades))).Sqrt())
	return sqn
}

type WinLossRatioAnalyzer struct{}

func (wlra *WinLossRatioAnalyzer) Name() string {
	return "win_loss_ratio"
}

func (wlra *WinLossRatioAnalyzer) Analyze(trades []metrics.Trade, equityCurve []metrics.EquityPoint) interface{} {
	totalWins := 0
	totalLosses := 0
	avgWin := decimal.ZERO
	avgLoss := decimal.ZERO

	for _, trade := range trades {
		if trade.IsWin {
			totalWins++
			avgWin = avgWin.Add(trade.Profit)
		} else {
			totalLosses++
			avgLoss = avgLoss.Add(trade.Profit.Abs())
		}
	}

	if totalWins == 0 || avgWin.IsZero() {
		return decimal.ZERO
	}
	if totalLosses == 0 {
		avgLoss = decimal.New(0.0001)
	}

	avgWin = avgWin.Div(decimal.New(float64(totalWins)))
	avgLoss = avgLoss.Div(decimal.New(float64(totalLosses)))

	if avgLoss.IsZero() {
		return decimal.New(999)
	}

	return avgWin.Div(avgLoss)
}

type RExpectancyAnalyzer struct{}

func (rea *RExpectancyAnalyzer) Name() string {
	return "r_expectancy"
}

func (rea *RExpectancyAnalyzer) Analyze(trades []metrics.Trade, equityCurve []metrics.EquityPoint) interface{} {
	if len(trades) == 0 {
		return decimal.ZERO
	}

	totalTrades := decimal.New(float64(len(trades)))
	avgR := decimal.ZERO

	for i, trade := range trades {
		r := decimal.New(float64(i))
		if r.IsZero() {
			r = decimal.New(1)
		}
		profitR := trade.ProfitPct.Div(r)
		avgR = avgR.Add(profitR)
	}

	avgR = avgR.Div(totalTrades)
	return avgR
}
