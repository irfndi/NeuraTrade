package backtest

import (
	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/indicators"
	"github.com/irfndi/goflux/pkg/series"
)

// PortfolioSimulator simulates a portfolio based on signals
type PortfolioSimulator struct {
	InitialCapital decimal.Decimal
	Fees           decimal.Decimal
	Slippage       decimal.Decimal
}

// NewPortfolioSimulator returns a new PortfolioSimulator
func NewPortfolioSimulator(initialCapital, fees, slippage float64) *PortfolioSimulator {
	return &PortfolioSimulator{
		InitialCapital: decimal.New(initialCapital),
		Fees:           decimal.New(fees),
		Slippage:       decimal.New(slippage),
	}
}

// SimulateLongOnly simulates a long-only portfolio based on buy/sell signals
func (ps *PortfolioSimulator) SimulateLongOnly(s *series.TimeSeries, signals []int) BacktestResult {
	// Implementation of vectorized-like simulation in Go
	// signals: 1 = buy, -1 = sell, 0 = neutral

	equity := ps.InitialCapital
	position := decimal.ZERO
	trades := make([]Trade, 0)
	equityCurve := make([]decimal.Decimal, s.Length())

	entryIndex := -1
	entryPrice := decimal.ZERO

	for i := 0; i < s.Length(); i++ {
		price := s.GetCandle(i).ClosePrice
		signal := 0
		if i < len(signals) {
			signal = signals[i]
		}

		if position.IsZero() && signal == indicators.SignalBuy {
			// Buy
			entryPrice = price.Mul(decimal.ONE.Add(ps.Slippage))
			position = equity.Div(entryPrice)
			// Apply fees
			fee := equity.Mul(ps.Fees)
			equity = equity.Sub(fee)
			entryIndex = i
		} else if position.IsPositive() && signal == indicators.SignalSell {
			// Sell
			exitPrice := price.Mul(decimal.ONE.Sub(ps.Slippage))
			profit := exitPrice.Sub(entryPrice).Mul(position)
			fee := exitPrice.Mul(position).Mul(ps.Fees)
			profit = profit.Sub(fee)

			trades = append(trades, Trade{
				EntryTime:     entryIndex,
				EntryPrice:    entryPrice,
				ExitTime:      i,
				ExitPrice:     exitPrice,
				Direction:     "long",
				Quantity:      position,
				Profit:        profit,
				ProfitPercent: profit.Div(entryPrice.Mul(position)),
				Duration:      i - entryIndex,
			})

			equity = equity.Add(profit)
			position = decimal.ZERO
		}

		if position.IsPositive() {
			equityCurve[i] = equity.Add(price.Sub(entryPrice).Mul(position))
		} else {
			equityCurve[i] = equity
		}
	}

	// Finalize results
	bt := &Backtester{series: s}
	result := bt.calculateResults(trades, equityCurve, ps.InitialCapital, equity)
	return result
}
