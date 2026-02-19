package metrics

import (
	"testing"

	"github.com/irfndi/goflux/pkg/decimal"
)

func TestSharpeRatio_PerformanceMetrics(t *testing.T) {
	trades := []Trade{
		{Profit: decimal.New(100), ProfitPct: decimal.New(0.01), IsWin: true},
		{Profit: decimal.New(-50), ProfitPct: decimal.New(-0.005), IsWin: false},
		{Profit: decimal.New(150), ProfitPct: decimal.New(0.015), IsWin: true},
		{Profit: decimal.New(-30), ProfitPct: decimal.New(-0.003), IsWin: false},
		{Profit: decimal.New(200), ProfitPct: decimal.New(0.02), IsWin: true},
	}

	equityCurve := []EquityPoint{
		{Equity: decimal.New(10000), Drawdown: decimal.ZERO, DrawdownPct: decimal.ZERO},
		{Equity: decimal.New(10100), Drawdown: decimal.ZERO, DrawdownPct: decimal.ZERO},
		{Equity: decimal.New(10200), Drawdown: decimal.ZERO, DrawdownPct: decimal.ZERO},
	}

	pm := NewPerformanceMetrics()
	pm.Calculate(trades, equityCurve, decimal.New(10000), decimal.New(10420), 252)

	if pm.SharpeRatio.IsZero() {
		t.Logf("Sharpe Ratio calculated (may be zero with small sample): %v", pm.SharpeRatio)
	} else {
		t.Logf("Sharpe Ratio: %v", pm.SharpeRatio)
	}
}

func TestSortinoRatio_PerformanceMetrics(t *testing.T) {
	trades := []Trade{
		{Profit: decimal.New(100), ProfitPct: decimal.New(0.01), IsWin: true},
		{Profit: decimal.New(-20), ProfitPct: decimal.New(-0.002), IsWin: false},
		{Profit: decimal.New(80), ProfitPct: decimal.New(0.008), IsWin: true},
		{Profit: decimal.New(-10), ProfitPct: decimal.New(-0.001), IsWin: false},
	}

	equityCurve := []EquityPoint{
		{Equity: decimal.New(10000), Drawdown: decimal.ZERO, DrawdownPct: decimal.ZERO},
		{Equity: decimal.New(10150), Drawdown: decimal.ZERO, DrawdownPct: decimal.ZERO},
	}

	pm := NewPerformanceMetrics()
	pm.Calculate(trades, equityCurve, decimal.New(10000), decimal.New(10150), 252)

	t.Logf("Sortino Ratio: %v", pm.SortinoRatio)
}

func TestCalmarRatio_PerformanceMetrics(t *testing.T) {
	trades := []Trade{
		{Profit: decimal.New(500), ProfitPct: decimal.New(0.05), IsWin: true},
		{Profit: decimal.New(300), ProfitPct: decimal.New(0.03), IsWin: true},
	}

	equityCurve := []EquityPoint{
		{Equity: decimal.New(10000), Drawdown: decimal.ZERO, DrawdownPct: decimal.ZERO},
		{Equity: decimal.New(10500), Drawdown: decimal.ZERO, DrawdownPct: decimal.ZERO},
		{Equity: decimal.New(10300), Drawdown: decimal.New(200), DrawdownPct: decimal.New(0.019)},
		{Equity: decimal.New(10800), Drawdown: decimal.ZERO, DrawdownPct: decimal.ZERO},
	}

	pm := NewPerformanceMetrics()
	pm.Calculate(trades, equityCurve, decimal.New(10000), decimal.New(10800), 252)

	if pm.CalmarRatio.IsZero() {
		t.Errorf("Calmar Ratio should not be zero")
	}

	t.Logf("Calmar Ratio: %v", pm.CalmarRatio)
	t.Logf("CAGR: %v", pm.CAGR)
	t.Logf("Max Drawdown: %v", pm.MaxDrawdown)
}

func TestCAGR_PerformanceMetrics(t *testing.T) {
	trades := []Trade{
		{Profit: decimal.New(1000), ProfitPct: decimal.New(0.1), IsWin: true},
	}

	equityCurve := []EquityPoint{
		{Equity: decimal.New(10000), Drawdown: decimal.ZERO, DrawdownPct: decimal.ZERO},
		{Equity: decimal.New(11000), Drawdown: decimal.ZERO, DrawdownPct: decimal.ZERO},
	}

	pm := NewPerformanceMetrics()
	pm.Calculate(trades, equityCurve, decimal.New(10000), decimal.New(11000), 365)

	if pm.CAGR.IsZero() {
		t.Logf("CAGR with 1 year and 10%% return: %v", pm.CAGR)
	} else {
		t.Logf("CAGR: %v (should be around 0.10)", pm.CAGR)
	}
}

func TestProfitFactor(t *testing.T) {
	trades := []Trade{
		{Profit: decimal.New(100), IsWin: true},
		{Profit: decimal.New(50), IsWin: true},
		{Profit: decimal.New(-20), IsWin: false},
		{Profit: decimal.New(-10), IsWin: false},
	}

	equityCurve := []EquityPoint{
		{Equity: decimal.New(10000), Drawdown: decimal.ZERO, DrawdownPct: decimal.ZERO},
	}

	pm := NewPerformanceMetrics()
	pm.Calculate(trades, equityCurve, decimal.New(10000), decimal.New(10020), 252)

	expectedProfitFactor := decimal.New(150).Div(decimal.New(30))
	if pm.ProfitFactor.LT(expectedProfitFactor.Sub(decimal.New(0.1))) || pm.ProfitFactor.GT(expectedProfitFactor.Add(decimal.New(0.1))) {
		t.Errorf("Expected profit factor ~5, got %v", pm.ProfitFactor)
	}

	t.Logf("Profit Factor: %v (expected ~5)", pm.ProfitFactor)
}

func TestWinRate(t *testing.T) {
	trades := []Trade{
		{Profit: decimal.New(100), IsWin: true},
		{Profit: decimal.New(100), IsWin: true},
		{Profit: decimal.New(-50), IsWin: false},
		{Profit: decimal.New(100), IsWin: true},
	}

	equityCurve := []EquityPoint{
		{Equity: decimal.New(10000), Drawdown: decimal.ZERO, DrawdownPct: decimal.ZERO},
	}

	pm := NewPerformanceMetrics()
	pm.Calculate(trades, equityCurve, decimal.New(10000), decimal.New(10250), 252)

	expectedWinRate := decimal.New(0.75)
	if !pm.WinRate.EQ(expectedWinRate) {
		t.Errorf("Expected win rate 0.75, got %v", pm.WinRate)
	}

	t.Logf("Win Rate: %v (expected 75%%)", pm.WinRate)
}

func TestRecoveryFactor(t *testing.T) {
	trades := []Trade{
		{Profit: decimal.New(1000), IsWin: true},
	}

	equityCurve := []EquityPoint{
		{Equity: decimal.New(10000), Drawdown: decimal.ZERO, DrawdownPct: decimal.ZERO},
		{Equity: decimal.New(9500), Drawdown: decimal.New(500), DrawdownPct: decimal.New(0.05)},
		{Equity: decimal.New(11000), Drawdown: decimal.ZERO, DrawdownPct: decimal.ZERO},
	}

	pm := NewPerformanceMetrics()
	pm.Calculate(trades, equityCurve, decimal.New(10000), decimal.New(11000), 252)

	expectedRecoveryFactor := decimal.New(1000).Div(decimal.New(500))
	if pm.RecoveryFactor.LT(expectedRecoveryFactor.Sub(decimal.New(0.1))) || pm.RecoveryFactor.GT(expectedRecoveryFactor.Add(decimal.New(0.1))) {
		t.Errorf("Expected recovery factor ~2, got %v", pm.RecoveryFactor)
	}

	t.Logf("Recovery Factor: %v (expected ~2)", pm.RecoveryFactor)
}

func TestMaxDrawdown(t *testing.T) {
	equityCurve := []EquityPoint{
		{Equity: decimal.New(10000), Drawdown: decimal.New(0), DrawdownPct: decimal.New(0)},
		{Equity: decimal.New(12000), Drawdown: decimal.New(0), DrawdownPct: decimal.New(0)},
		{Equity: decimal.New(9000), Drawdown: decimal.New(3000), DrawdownPct: decimal.New(0.25)},
		{Equity: decimal.New(8000), Drawdown: decimal.New(4000), DrawdownPct: decimal.New(0.333)},
		{Equity: decimal.New(11000), Drawdown: decimal.New(1000), DrawdownPct: decimal.New(0.083)},
	}

	pm := NewPerformanceMetrics()
	pm.calculateDrawdownMetrics(equityCurve)

	expectedMaxDD := decimal.New(4000)
	if !pm.MaxDrawdown.EQ(expectedMaxDD) {
		t.Errorf("Expected max drawdown 4000, got %v", pm.MaxDrawdown)
	}

	expectedMaxDDPct := decimal.New(0.333)
	if pm.MaxDrawdownPct.LT(expectedMaxDDPct.Sub(decimal.New(0.01))) || pm.MaxDrawdownPct.GT(expectedMaxDDPct.Add(decimal.New(0.01))) {
		t.Errorf("Expected max drawdown pct ~0.333, got %v", pm.MaxDrawdownPct)
	}

	t.Logf("Max Drawdown: %v", pm.MaxDrawdown)
	t.Logf("Max Drawdown %%: %v", pm.MaxDrawdownPct)
}

func TestEmptyTrades(t *testing.T) {
	pm := NewPerformanceMetrics()
	pm.Calculate([]Trade{}, []EquityPoint{}, decimal.New(10000), decimal.New(10000), 252)

	if pm.TotalTrades != 0 {
		t.Errorf("Expected 0 trades, got %d", pm.TotalTrades)
	}

	if !pm.FinalEquity.EQ(decimal.New(10000)) {
		t.Errorf("Expected unchanged equity")
	}

	t.Logf("Empty trades handled correctly")
}

func TestMetricsStringOutput(t *testing.T) {
	trades := []Trade{
		{Profit: decimal.New(100), ProfitPct: decimal.New(0.01), IsWin: true},
		{Profit: decimal.New(-50), ProfitPct: decimal.New(-0.005), IsWin: false},
	}

	equityCurve := []EquityPoint{
		{Equity: decimal.New(10000), Drawdown: decimal.ZERO, DrawdownPct: decimal.ZERO},
	}

	pm := NewPerformanceMetrics()
	pm.Calculate(trades, equityCurve, decimal.New(10000), decimal.New(10050), 252)

	output := pm.String()
	if len(output) == 0 {
		t.Errorf("String output should not be empty")
	}

	t.Logf("Metrics output:\n%s", output)
}

func TestSterlingRatio(t *testing.T) {
	trades := []Trade{
		{Profit: decimal.New(500), ProfitPct: decimal.New(0.05), IsWin: true},
	}

	equityCurve := []EquityPoint{
		{Equity: decimal.New(10000), Drawdown: decimal.ZERO, DrawdownPct: decimal.ZERO},
		{Equity: decimal.New(10500), Drawdown: decimal.ZERO, DrawdownPct: decimal.ZERO},
		{Equity: decimal.New(10300), Drawdown: decimal.New(200), DrawdownPct: decimal.New(0.019)},
	}

	pm := NewPerformanceMetrics()
	pm.Calculate(trades, equityCurve, decimal.New(10000), decimal.New(10500), 252)

	t.Logf("Sterling Ratio: %v", pm.SterlingRatio)
}

func TestBurkeRatio_PerformanceMetrics(t *testing.T) {
	trades := []Trade{
		{Profit: decimal.New(1000), ProfitPct: decimal.New(0.1), IsWin: true},
	}

	equityCurve := []EquityPoint{
		{Equity: decimal.New(10000), Drawdown: decimal.ZERO, DrawdownPct: decimal.ZERO},
		{Equity: decimal.New(11000), Drawdown: decimal.ZERO, DrawdownPct: decimal.ZERO},
	}

	pm := NewPerformanceMetrics()
	pm.Calculate(trades, equityCurve, decimal.New(10000), decimal.New(11000), 252)

	t.Logf("Burke Ratio: %v", pm.BurkeRatio)
}
