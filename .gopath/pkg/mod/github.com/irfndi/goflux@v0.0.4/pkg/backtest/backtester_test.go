package backtest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/metrics"
	"github.com/irfndi/goflux/pkg/series"
	"github.com/irfndi/goflux/pkg/testutils"
	"github.com/irfndi/goflux/pkg/trading"
)

func createTestSeries() *series.TimeSeries {
	s := series.NewTimeSeries()

	baseTime := time.Now().Add(-100 * time.Hour)

	for i := 0; i < 100; i++ {
		trend := decimal.New(float64(i) * 0.5)
		noise := decimal.New(float64(i%10) * 0.1)
		close := trend.Add(noise).Add(decimal.New(100))

		s.AddCandle(&series.Candle{
			OpenPrice:  close.Sub(decimal.New(1)),
			MaxPrice:   close.Add(decimal.New(2)),
			MinPrice:   close.Sub(decimal.New(2)),
			ClosePrice: close,
			Volume:     decimal.New(1000),
			Period:     series.NewTimePeriod(baseTime.Add(time.Duration(i)*time.Hour), time.Hour),
		})
	}

	return s
}

type simpleStrategy struct {
	trading.Strategy
}

func (s *simpleStrategy) ShouldEnter(index int, record *trading.TradingRecord) bool {
	return (index == 0 && record.CurrentPosition().IsNew()) || (index > 20 && index%10 == 0 && record.CurrentPosition().IsNew())
}

func (s *simpleStrategy) ShouldExit(index int, record *trading.TradingRecord) bool {
	return index > 30 && index%15 == 0 && record.CurrentPosition().IsOpen()
}

func TestBacktesterBasic(t *testing.T) {
	s := createTestSeries()
	strategy := &simpleStrategy{}

	backtester := NewBacktester(s, strategy)

	config := BacktestConfig{
		InitialCapital: decimal.New(10000),
		PositionSize:   decimal.New(100),
		AllowLong:      true,
		AllowShort:     false,
	}

	result := backtester.Run(config)

	assert.NotEqual(t, 0, result.TotalTrades)
	assert.True(t, result.FinalEquity.GT(decimal.ZERO))
}

func TestBacktesterNoTrades(t *testing.T) {
	s := series.NewTimeSeries()

	for i := 0; i < 10; i++ {
		s.AddCandle(&series.Candle{
			OpenPrice:  decimal.New(100),
			MaxPrice:   decimal.New(105),
			MinPrice:   decimal.New(95),
			ClosePrice: decimal.New(100),
			Volume:     decimal.New(1000),
			Period:     series.NewTimePeriod(time.Now().Add(time.Duration(i)*time.Hour), time.Hour),
		})
	}

	noSignalStrategy := &noSignalStrategy{}

	backtester := NewBacktester(s, noSignalStrategy)

	config := BacktestConfig{
		InitialCapital: decimal.New(10000),
		PositionSize:   decimal.New(100),
		AllowLong:      true,
	}

	result := backtester.Run(config)

	assert.Equal(t, 0, result.TotalTrades)
	assert.Equal(t, config.InitialCapital.Float(), result.FinalEquity.Float())
}

type noSignalStrategy struct {
	trading.Strategy
}

func (s *noSignalStrategy) ShouldEnter(index int, record *trading.TradingRecord) bool {
	return false
}

func (s *noSignalStrategy) ShouldExit(index int, record *trading.TradingRecord) bool {
	return false
}

func TestBacktestResultCalculations(t *testing.T) {
	bt := &Backtester{}

	trades := []Trade{
		{Profit: decimal.New(100)},
		{Profit: decimal.New(-50)},
		{Profit: decimal.New(200)},
	}

	equityCurve := []decimal.Decimal{decimal.New(1000), decimal.New(1100), decimal.New(1050), decimal.New(1250)}

	result := bt.calculateResults(trades, equityCurve, decimal.New(1000), decimal.New(1250))

	assert.Equal(t, 3, result.TotalTrades)
	assert.Equal(t, 2, result.WinningTrades)
	assert.Equal(t, 1, result.LosingTrades)
	assert.Equal(t, 250.0, result.NetProfit.Float())
}

type stopLossStrategy struct {
	trading.Strategy
}

func (s *stopLossStrategy) ShouldEnter(index int, record *trading.TradingRecord) bool {
	return index == 0 && record.CurrentPosition().IsNew()
}

func (s *stopLossStrategy) ShouldExit(index int, record *trading.TradingRecord) bool {
	return index > 5 && index%5 == 0 && record.CurrentPosition().IsOpen()
}

func TestBacktesterWithStopLoss(t *testing.T) {
	s := series.NewTimeSeries()

	for i := 0; i < 50; i++ {
		price := decimal.New(float64(100 + i))
		s.AddCandle(&series.Candle{
			OpenPrice:  price.Sub(decimal.New(1)),
			MaxPrice:   price.Add(decimal.New(2)),
			MinPrice:   price.Sub(decimal.New(3)),
			ClosePrice: price,
			Volume:     decimal.New(1000),
			Period:     series.NewTimePeriod(time.Now().Add(time.Duration(i)*time.Hour), time.Hour),
		})
	}

	strategy := &stopLossStrategy{}

	backtester := NewBacktester(s, strategy)

	config := BacktestConfig{
		InitialCapital: decimal.New(10000),
		PositionSize:   decimal.New(50),
		AllowLong:      true,
	}

	result := backtester.Run(config)

	assert.True(t, result.TotalTrades > 0)
}

func TestBacktesterWithAnalyzers(t *testing.T) {
	s := createTestSeries()
	strategy := &simpleStrategy{}

	backtester := NewBacktester(s, strategy)
	backtester.AddAnalyzer(&TradeStatsAnalyzer{})
	backtester.AddAnalyzer(&DrawdownAnalyzer{})
	backtester.AddAnalyzer(&SharpeRatioAnalyzer{RiskFreeRate: decimal.New(0.02)})

	config := BacktestConfig{
		InitialCapital: decimal.New(10000),
		PositionSize:   decimal.New(100),
		AllowLong:      true,
	}

	result := backtester.Run(config)

	assert.NotNil(t, result.Analysis)

	stats := result.Analysis["TradeStats"].(TradeStats)
	if result.TotalTrades == 0 {
		t.Log("No trades were made")
	} else {
		t.Logf("Total Trades: %d, Win Rate: %s", stats.TotalTrades, stats.WinRate.String())
	}
	assert.Equal(t, result.TotalTrades, stats.TotalTrades)
	assert.True(t, stats.WinRate.GT(decimal.ZERO))

	dd := result.Analysis["Drawdown"].(DrawdownStats)
	t.Logf("Max Drawdown: %s, Max Drawdown Pct: %s", dd.MaxDrawdown.String(), dd.MaxDrawdownPct.String())
	assert.True(t, dd.MaxDrawdown.Float() >= 0)

	sharpe := result.Analysis["SharpeRatio"].(decimal.Decimal)
	assert.NotNil(t, sharpe)
}

func TestBacktestExtendedAnalyzers(t *testing.T) {
	trades := []metrics.Trade{
		{Profit: decimal.New(100), ProfitPct: decimal.New(0.02), Duration: 5, IsWin: true},
		{Profit: decimal.New(-50), ProfitPct: decimal.New(-0.01), Duration: 10, IsWin: false},
		{Profit: decimal.New(75), ProfitPct: decimal.New(0.015), Duration: 3, IsWin: true},
	}
	equity := []metrics.EquityPoint{
		{Equity: decimal.New(10000)},
		{Equity: decimal.New(10050)},
		{Equity: decimal.New(10025)},
	}

	ea := &ExpectancyAnalyzer{}
	assert.Equal(t, "expectancy", ea.Name())
	expectancy := ea.Analyze(trades, equity).(decimal.Decimal)
	assert.True(t, expectancy.GT(decimal.ZERO))

	pfa := &ProfitFactorAnalyzer{}
	assert.Equal(t, "profit_factor", pfa.Name())
	profitFactor := pfa.Analyze(trades, equity).(decimal.Decimal)
	assert.True(t, profitFactor.GT(decimal.ZERO))

	atda := &AverageTradeDurationAnalyzer{}
	assert.Equal(t, "avg_trade_duration", atda.Name())
	avgDuration := atda.Analyze(trades, equity).(int)
	assert.Equal(t, 6, avgDuration)

	mca := &MaxConsecutiveAnalyzer{}
	assert.Equal(t, "max_consecutive", mca.Name())
	maxConsecutive := mca.Analyze(trades, equity).(map[string]int)
	assert.Equal(t, 1, maxConsecutive["wins"])
	assert.Equal(t, 1, maxConsecutive["losses"])

	ept := &ExpectancyPerTradeAnalyzer{}
	assert.Equal(t, "expectancy_per_trade", ept.Name())
	expectancyPerTrade := ept.Analyze(trades, equity).(decimal.Decimal)
	assert.True(t, expectancyPerTrade.GT(decimal.ZERO))

	wlra := &WinLossRatioAnalyzer{}
	assert.Equal(t, "win_loss_ratio", wlra.Name())
	winLossRatio := wlra.Analyze(trades, equity).(decimal.Decimal)
	assert.True(t, winLossRatio.GT(decimal.ZERO))

	rea := &RExpectancyAnalyzer{}
	assert.Equal(t, "r_expectancy", rea.Name())
	rExpectancy := rea.Analyze(trades, equity).(decimal.Decimal)
	assert.False(t, rExpectancy.IsZero())
}

func TestBacktestSQNAnalyzer(t *testing.T) {
	trades := make([]metrics.Trade, 0, 10)
	for i := 0; i < 6; i++ {
		trades = append(trades, metrics.Trade{
			Profit:    decimal.New(float64(100 + i)),
			ProfitPct: decimal.New(0.02),
			Duration:  5,
			IsWin:     true,
		})
	}
	for i := 0; i < 4; i++ {
		trades = append(trades, metrics.Trade{
			Profit:    decimal.New(float64(-50 - i)),
			ProfitPct: decimal.New(-0.01),
			Duration:  5,
			IsWin:     false,
		})
	}

	equity := []metrics.EquityPoint{{Equity: decimal.New(10000)}}
	sqna := &SystemQualityNumberAnalyzer{}
	assert.Equal(t, "sqn", sqna.Name())
	sqn := sqna.Analyze(trades, equity).(decimal.Decimal)
	assert.True(t, sqn.GT(decimal.ZERO))
}

func TestEquityCurveAnalyzer(t *testing.T) {
	equity := []metrics.EquityPoint{
		{Equity: decimal.New(10000)},
		{Equity: decimal.New(10100)},
	}
	eca := &EquityCurveAnalyzer{}
	assert.Equal(t, "EquityCurve", eca.Name())
	out := eca.Analyze(nil, equity).([]metrics.EquityPoint)
	assert.Equal(t, equity, out)
}

type enterOnceNoExitStrategy struct {
	trading.Strategy
}

func (s *enterOnceNoExitStrategy) ShouldEnter(index int, record *trading.TradingRecord) bool {
	return index == 0 && record.CurrentPosition().IsNew()
}

func (s *enterOnceNoExitStrategy) ShouldExit(index int, record *trading.TradingRecord) bool {
	return false
}

func TestMultiAssetBacktester(t *testing.T) {
	ts1 := testutils.MockTimeSeriesFl(100, 101, 102, 103)
	ts2 := testutils.MockTimeSeriesFl(200, 199, 198, 197)

	m := NewMultiAssetBacktester(&enterOnceNoExitStrategy{})
	m.AddAsset("AAA", ts1)
	m.AddAsset("BBB", ts2)
	m.analyzers.Add(&EquityCurveAnalyzer{})

	results := m.Run(BacktestConfig{
		InitialCapital: decimal.New(10000),
		PositionSize:   decimal.New(10),
		AllowLong:      true,
	})

	assert.Contains(t, results, "AAA")
	assert.Contains(t, results, "BBB")
	assert.NotNil(t, results["AAA"].Analysis)
	assert.NotNil(t, results["BBB"].Analysis)

	aaaEquity := results["AAA"].Analysis["EquityCurve"].([]metrics.EquityPoint)
	assert.Equal(t, len(ts1.Candles), len(aaaEquity))
}

func TestExitTriggeredShort(t *testing.T) {
	bt := &Backtester{}

	pos := Position{
		Direction: "short",
		StopLoss:  decimal.New(105),
	}
	assert.True(t, bt.exitTriggered(pos, decimal.New(105)))
	assert.False(t, bt.exitTriggered(pos, decimal.New(104)))

	pos2 := Position{
		Direction:  "short",
		TakeProfit: decimal.New(95),
	}
	assert.True(t, bt.exitTriggered(pos2, decimal.New(95)))
	assert.False(t, bt.exitTriggered(pos2, decimal.New(96)))

	pos3 := Position{
		Direction: "short",
	}
	assert.False(t, bt.exitTriggered(pos3, decimal.New(100)))
}
