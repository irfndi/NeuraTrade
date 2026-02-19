package backtest

import (
	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/metrics"
	"github.com/irfndi/goflux/pkg/series"
	"github.com/irfndi/goflux/pkg/trading"
)

type Trade struct {
	EntryTime     int
	EntryPrice    decimal.Decimal
	ExitTime      int
	ExitPrice     decimal.Decimal
	Direction     string
	Quantity      decimal.Decimal
	Profit        decimal.Decimal
	ProfitPercent decimal.Decimal
	Duration      int
}

type Position struct {
	EntryTime  int
	EntryPrice decimal.Decimal
	Direction  string
	Quantity   decimal.Decimal
	StopLoss   decimal.Decimal
	TakeProfit decimal.Decimal
}

type BacktestResult struct {
	TotalTrades          int
	WinningTrades        int
	LosingTrades         int
	WinRate              decimal.Decimal
	TotalProfit          decimal.Decimal
	TotalLoss            decimal.Decimal
	NetProfit            decimal.Decimal
	GrossProfit          decimal.Decimal
	GrossLoss            decimal.Decimal
	ProfitFactor         decimal.Decimal
	AverageWin           decimal.Decimal
	AverageLoss          decimal.Decimal
	AverageTrade         decimal.Decimal
	MaxConsecutiveWins   int
	MaxConsecutiveLosses int
	MaxDrawdown          decimal.Decimal
	MaxDrawdownPercent   decimal.Decimal
	RecoveryFactor       decimal.Decimal
	RiskRewardRatio      decimal.Decimal
	CalmarRatio          decimal.Decimal
	SortinoRatio         decimal.Decimal
	SharpeRatio          decimal.Decimal
	CAGR                 decimal.Decimal
	FinalEquity          decimal.Decimal
	InitialCapital       decimal.Decimal
	Trades               []Trade
	Analysis             AnalysisResult
}

type BacktestConfig struct {
	InitialCapital decimal.Decimal
	PositionSize   decimal.Decimal
	RiskPerTrade   decimal.Decimal
	Commission     decimal.Decimal
	Slippage       decimal.Decimal
	AllowShort     bool
	AllowLong      bool
}

type Backtester struct {
	series    *series.TimeSeries
	strategy  trading.Strategy
	analyzers *AnalyzerRegistry
}

func NewBacktester(s *series.TimeSeries, strategy trading.Strategy) *Backtester {
	return &Backtester{
		series:    s,
		strategy:  strategy,
		analyzers: NewAnalyzerRegistry(),
	}
}

// AddAnalyzer adds an analyzer to the backtester.
func (b *Backtester) AddAnalyzer(a Analyzer) {
	b.analyzers.Add(a)
}

func (b *Backtester) Run(config BacktestConfig) BacktestResult {
	positions := make([]Position, 0)
	trades := make([]Trade, 0)
	equityCurve := make([]decimal.Decimal, len(b.series.Candles))
	equity := config.InitialCapital

	record := trading.NewTradingRecord()

	for i := 0; i < len(b.series.Candles); i++ {
		b.step(i, &positions, &trades, equityCurve, &equity, record, config)
	}

	b.finalizeOpenPositions(&positions, &trades, &equity)

	result := b.calculateResults(trades, equityCurve, config.InitialCapital, equity)

	// Run analyzers
	metricsTrades := make([]metrics.Trade, len(trades))
	for i, t := range trades {
		metricsTrades[i] = metrics.Trade{
			Profit:    t.Profit,
			ProfitPct: t.ProfitPercent,
			Duration:  t.Duration,
			IsWin:     t.Profit.IsPositive(),
		}
	}

	metricsEquityCurve := make([]metrics.EquityPoint, len(equityCurve))
	peak := config.InitialCapital
	for i, eq := range equityCurve {
		if eq.GT(peak) {
			peak = eq
		}
		drawdown := peak.Sub(eq)
		var drawdownPct decimal.Decimal
		if !peak.IsZero() {
			drawdownPct = drawdown.Div(peak)
		}
		metricsEquityCurve[i] = metrics.EquityPoint{
			Equity:      eq,
			Drawdown:    drawdown,
			DrawdownPct: drawdownPct,
		}
	}

	result.Analysis = b.analyzers.Run(metricsTrades, metricsEquityCurve)

	return result
}

func (b *Backtester) step(
	index int,
	positions *[]Position,
	trades *[]Trade,
	equityCurve []decimal.Decimal,
	equity *decimal.Decimal,
	record *trading.TradingRecord,
	config BacktestConfig,
) {
	equityCurve[index] = *equity

	candle := b.series.Candles[index]
	if candle == nil {
		return
	}

	currentPrice := candle.ClosePrice

	b.closePositionsByStops(index, currentPrice, positions, trades, equity, record)
	b.applyStrategy(index, currentPrice, positions, trades, equity, record, config)
}

func (b *Backtester) closePositionsByStops(
	index int,
	currentPrice decimal.Decimal,
	positions *[]Position,
	trades *[]Trade,
	equity *decimal.Decimal,
	record *trading.TradingRecord,
) {
	for j := len(*positions) - 1; j >= 0; j-- {
		pos := (*positions)[j]
		if !b.exitTriggered(pos, currentPrice) {
			continue
		}

		profit := b.positionProfit(pos, currentPrice)
		*trades = append(*trades, b.makeTrade(pos, index, currentPrice, profit))
		*equity = equity.Add(profit)

		record.Operate(trading.Order{
			Side:   trading.SELL,
			Price:  currentPrice,
			Amount: pos.Quantity,
		})

		*positions = append((*positions)[:j], (*positions)[j+1:]...)
	}
}

func (b *Backtester) applyStrategy(
	index int,
	currentPrice decimal.Decimal,
	positions *[]Position,
	trades *[]Trade,
	equity *decimal.Decimal,
	record *trading.TradingRecord,
	config BacktestConfig,
) {
	if b.strategy.ShouldEnter(index, record) {
		if config.AllowLong {
			b.openLong(index, currentPrice, positions, equity, record, config)
		}
		return
	}

	if !b.strategy.ShouldExit(index, record) || len(*positions) == 0 {
		return
	}

	b.closeAllPositions(index, currentPrice, positions, trades, equity, record)
}

func (b *Backtester) openLong(
	index int,
	currentPrice decimal.Decimal,
	positions *[]Position,
	equity *decimal.Decimal,
	record *trading.TradingRecord,
	config BacktestConfig,
) {
	quantity := config.PositionSize
	if quantity.IsZero() {
		quantity = equity.Div(currentPrice)
	}

	*positions = append(*positions, Position{
		EntryTime:  index,
		EntryPrice: currentPrice,
		Direction:  "long",
		Quantity:   quantity,
	})

	record.Operate(trading.Order{
		Side:   trading.BUY,
		Price:  currentPrice,
		Amount: quantity,
	})
}

func (b *Backtester) closeAllPositions(
	exitTime int,
	exitPrice decimal.Decimal,
	positions *[]Position,
	trades *[]Trade,
	equity *decimal.Decimal,
	record *trading.TradingRecord,
) {
	for j := len(*positions) - 1; j >= 0; j-- {
		pos := (*positions)[j]
		profit := b.positionProfit(pos, exitPrice)
		*trades = append(*trades, b.makeTrade(pos, exitTime, exitPrice, profit))
		*equity = equity.Add(profit)

		record.Operate(trading.Order{
			Side:   trading.SELL,
			Price:  exitPrice,
			Amount: pos.Quantity,
		})

		*positions = append((*positions)[:j], (*positions)[j+1:]...)
	}
}

func (b *Backtester) finalizeOpenPositions(positions *[]Position, trades *[]Trade, equity *decimal.Decimal) {
	if len(*positions) == 0 || len(b.series.Candles) == 0 {
		return
	}

	lastIndex := len(b.series.Candles) - 1
	lastCandle := b.series.Candles[lastIndex]
	if lastCandle == nil {
		return
	}
	exitPrice := lastCandle.ClosePrice

	for _, pos := range *positions {
		profit := b.positionProfit(pos, exitPrice)
		*trades = append(*trades, b.makeTrade(pos, lastIndex, exitPrice, profit))
		*equity = equity.Add(profit)
	}
}

func (b *Backtester) exitTriggered(pos Position, currentPrice decimal.Decimal) bool {
	if pos.Direction == "long" {
		return b.exitTriggeredLong(pos, currentPrice)
	}
	if pos.Direction == "short" {
		return b.exitTriggeredShort(pos, currentPrice)
	}
	return false
}

func (b *Backtester) exitTriggeredLong(pos Position, currentPrice decimal.Decimal) bool {
	if !pos.StopLoss.IsZero() && currentPrice.LTE(pos.StopLoss) {
		return true
	}
	if !pos.TakeProfit.IsZero() && currentPrice.GTE(pos.TakeProfit) {
		return true
	}
	return false
}

func (b *Backtester) exitTriggeredShort(pos Position, currentPrice decimal.Decimal) bool {
	if !pos.StopLoss.IsZero() && currentPrice.GTE(pos.StopLoss) {
		return true
	}
	if !pos.TakeProfit.IsZero() && currentPrice.LTE(pos.TakeProfit) {
		return true
	}
	return false
}

func (b *Backtester) positionProfit(pos Position, exitPrice decimal.Decimal) decimal.Decimal {
	if pos.Direction == "long" {
		return exitPrice.Sub(pos.EntryPrice).Mul(pos.Quantity)
	}
	return pos.EntryPrice.Sub(exitPrice).Mul(pos.Quantity)
}

func (b *Backtester) makeTrade(pos Position, exitTime int, exitPrice, profit decimal.Decimal) Trade {
	trade := Trade{
		EntryTime:  pos.EntryTime,
		EntryPrice: pos.EntryPrice,
		ExitTime:   exitTime,
		ExitPrice:  exitPrice,
		Direction:  pos.Direction,
		Quantity:   pos.Quantity,
		Profit:     profit,
	}
	trade.ProfitPercent = profit.Div(pos.EntryPrice.Mul(pos.Quantity))
	trade.Duration = exitTime - pos.EntryTime
	return trade
}

func (b *Backtester) calculateResults(trades []Trade, equityCurve []decimal.Decimal, initialCapital, finalEquity decimal.Decimal) BacktestResult {
	result := BacktestResult{
		TotalTrades:    len(trades),
		Trades:         trades,
		InitialCapital: initialCapital,
		FinalEquity:    finalEquity,
		GrossProfit:    decimal.ZERO,
		GrossLoss:      decimal.ZERO,
		TotalProfit:    decimal.ZERO,
	}

	if len(trades) == 0 {
		result.NetProfit = decimal.ZERO
		return result
	}

	for _, trade := range trades {
		if trade.Profit.IsPositive() {
			result.WinningTrades++
			result.GrossProfit = result.GrossProfit.Add(trade.Profit)
		} else if trade.Profit.IsNegative() {
			result.LosingTrades++
			result.GrossLoss = result.GrossLoss.Add(trade.Profit.Abs())
		}
		result.TotalProfit = result.TotalProfit.Add(trade.Profit)
	}

	if result.TotalTrades > 0 {
		result.WinRate = decimal.New(float64(result.WinningTrades)).Div(decimal.New(float64(result.TotalTrades)))
		result.AverageTrade = result.TotalProfit.Div(decimal.New(float64(result.TotalTrades)))
	}

	if !result.GrossLoss.IsZero() {
		result.ProfitFactor = result.GrossProfit.Div(result.GrossLoss)
	}

	result.NetProfit = finalEquity.Sub(initialCapital)

	drawdown, drawdownPercent := b.calculateMaxDrawdown(equityCurve, initialCapital)
	result.MaxDrawdown = drawdown
	result.MaxDrawdownPercent = drawdownPercent

	return result
}

func (b *Backtester) calculateMaxDrawdown(equityCurve []decimal.Decimal, initialCapital decimal.Decimal) (decimal.Decimal, decimal.Decimal) {
	maxDrawdown := decimal.ZERO
	maxDrawdownPercent := decimal.ZERO
	peak := initialCapital

	for _, equity := range equityCurve {
		if equity.GT(peak) {
			peak = equity
		}

		drawdown := peak.Sub(equity)
		if drawdown.GT(maxDrawdown) {
			maxDrawdown = drawdown
			if !peak.IsZero() {
				maxDrawdownPercent = drawdown.Div(peak)
			}
		}
	}

	return maxDrawdown, maxDrawdownPercent
}
