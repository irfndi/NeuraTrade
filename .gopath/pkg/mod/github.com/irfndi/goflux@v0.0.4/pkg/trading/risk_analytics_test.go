package trading

import (
	"testing"

	"github.com/irfndi/goflux/pkg/decimal"
)

func TestHistoricalVaR(t *testing.T) {
	returns := []decimal.Decimal{
		decimal.New(0.01),
		decimal.New(-0.02),
		decimal.New(0.03),
		decimal.New(-0.05),
		decimal.New(0.02),
		decimal.New(-0.01),
		decimal.New(0.04),
		decimal.New(-0.03),
	}

	calc := NewVaRCalculator(HistoricalVaR, 0.95, 1)
	result := calc.Calculate(returns)

	if result.VaR.IsZero() {
		t.Error("VaR should not be zero")
	}
	if result.CVaR.IsZero() {
		t.Error("CVaR should not be zero")
	}
	if !result.Confidence.EQ(decimal.New(0.95)) {
		t.Errorf("Expected confidence 0.95, got %v", result.Confidence)
	}
}

func TestParametricVaR(t *testing.T) {
	returns := []decimal.Decimal{
		decimal.New(0.01),
		decimal.New(-0.02),
		decimal.New(0.03),
		decimal.New(-0.05),
		decimal.New(0.02),
		decimal.New(-0.01),
		decimal.New(0.04),
		decimal.New(-0.03),
	}

	calc := NewVaRCalculator(ParametricVaR, 0.95, 1)
	result := calc.Calculate(returns)

	if result.VaR.IsZero() {
		t.Error("VaR should not be zero")
	}
}

func TestMonteCarloVaR(t *testing.T) {
	returns := []decimal.Decimal{
		decimal.New(0.01),
		decimal.New(-0.02),
		decimal.New(0.03),
		decimal.New(-0.05),
		decimal.New(0.02),
	}

	calc := NewVaRCalculator(MonteCarloVaR, 0.95, 1)
	result := calc.Calculate(returns)

	if result.VaR.IsZero() {
		t.Error("VaR should not be zero")
	}
}

func TestVaREmptyReturns(t *testing.T) {
	returns := []decimal.Decimal{}

	calc := NewVaRCalculator(HistoricalVaR, 0.95, 1)
	result := calc.Calculate(returns)

	if !result.VaR.IsZero() {
		t.Errorf("Expected zero VaR for empty returns, got %v", result.VaR)
	}
	if !result.CVaR.IsZero() {
		t.Errorf("Expected zero CVaR for empty returns, got %v", result.CVaR)
	}
}

func TestCalculateReturns(t *testing.T) {
	prices := []decimal.Decimal{
		decimal.New(100),
		decimal.New(102),
		decimal.New(101),
		decimal.New(105),
		decimal.New(103),
	}

	returns := CalculateReturns(prices)

	if len(returns) != 4 {
		t.Errorf("Expected 4 returns, got %d", len(returns))
	}

	expectedReturns := []decimal.Decimal{
		decimal.New(0.02),
		decimal.New(-0.009804),
		decimal.New(0.039604),
		decimal.New(-0.019048),
	}

	for i, expected := range expectedReturns {
		diff := returns[i].Sub(expected).Abs()
		if diff.GT(decimal.New(0.0001)) {
			t.Errorf("Return %d: expected ~%v, got %v", i, expected, returns[i])
		}
	}
}

func TestCalculateLogReturns(t *testing.T) {
	prices := []decimal.Decimal{
		decimal.New(100),
		decimal.New(105),
		decimal.New(110.25),
	}

	logReturns := CalculateLogReturns(prices)

	if len(logReturns) != 2 {
		t.Errorf("Expected 2 log returns, got %d", len(logReturns))
	}
}

func TestCalculateMaximumDrawdown(t *testing.T) {
	equityCurve := []decimal.Decimal{
		decimal.New(10000),
		decimal.New(10500),
		decimal.New(10200),
		decimal.New(11000),
		decimal.New(10800),
		decimal.New(11500),
	}

	maxDD := CalculateMaximumDrawdown(equityCurve)

	expectedMaxDD := decimal.New(300)
	if !maxDD.EQ(expectedMaxDD) {
		t.Errorf("Expected max drawdown %v, got %v", expectedMaxDD, maxDD)
	}
}

func TestCalculateMaximumDrawdownEmpty(t *testing.T) {
	equityCurve := []decimal.Decimal{}

	maxDD := CalculateMaximumDrawdown(equityCurve)

	if !maxDD.IsZero() {
		t.Errorf("Expected zero max drawdown for empty equity curve, got %v", maxDD)
	}
}

func TestCalculateCalmarRatio(t *testing.T) {
	annualizedReturn := decimal.New(0.25)
	maxDrawdown := decimal.New(0.10)

	calmar := CalculateCalmarRatio(annualizedReturn, maxDrawdown)

	expectedCalmar := decimal.New(2.5)
	if !calmar.EQ(expectedCalmar) {
		t.Errorf("Expected Calmar ratio %v, got %v", expectedCalmar, calmar)
	}
}

func TestCalculateCalmarRatioZeroDrawdown(t *testing.T) {
	annualizedReturn := decimal.New(0.25)
	maxDrawdown := decimal.ZERO

	calmar := CalculateCalmarRatio(annualizedReturn, maxDrawdown)

	if !calmar.IsZero() {
		t.Errorf("Expected zero Calmar ratio when drawdown is zero, got %v", calmar)
	}
}

func TestCalculateSortinoRatio(t *testing.T) {
	returns := []decimal.Decimal{
		decimal.New(0.02),
		decimal.New(-0.01),
		decimal.New(0.03),
		decimal.New(-0.02),
		decimal.New(0.01),
	}

	sortino := CalculateSortinoRatio(returns, decimal.ZERO)

	if sortino.IsZero() {
		t.Error("Sortino ratio should not be zero")
	}
}

func TestCalculateBeta(t *testing.T) {
	stockReturns := []decimal.Decimal{
		decimal.New(0.02),
		decimal.New(-0.01),
		decimal.New(0.03),
		decimal.New(-0.02),
	}

	marketReturns := []decimal.Decimal{
		decimal.New(0.01),
		decimal.New(-0.005),
		decimal.New(0.02),
		decimal.New(-0.01),
	}

	beta := CalculateBeta(stockReturns, marketReturns)

	if beta.IsZero() {
		t.Error("Beta should not be zero")
	}
	if beta.LT(decimal.ZERO) {
		t.Error("Beta should be positive for positively correlated assets")
	}
}

func TestCalculateBetaEmpty(t *testing.T) {
	stockReturns := []decimal.Decimal{}
	marketReturns := []decimal.Decimal{}

	beta := CalculateBeta(stockReturns, marketReturns)

	if !beta.EQ(decimal.ONE) {
		t.Errorf("Expected beta 1.0 for empty returns, got %v", beta)
	}
}

func TestCalculateCorrelation(t *testing.T) {
	series1 := []decimal.Decimal{
		decimal.New(1),
		decimal.New(2),
		decimal.New(3),
		decimal.New(4),
		decimal.New(5),
	}

	series2 := []decimal.Decimal{
		decimal.New(2),
		decimal.New(4),
		decimal.New(6),
		decimal.New(8),
		decimal.New(10),
	}

	corr := CalculateCorrelation(series1, series2)

	if corr.LT(decimal.New(0.99)) {
		t.Errorf("Expected correlation close to 1.0 for perfectly correlated series, got %v", corr)
	}
}

func TestCalculateCorrelationOpposite(t *testing.T) {
	series1 := []decimal.Decimal{
		decimal.New(1),
		decimal.New(2),
		decimal.New(3),
		decimal.New(4),
		decimal.New(5),
	}

	series2 := []decimal.Decimal{
		decimal.New(10),
		decimal.New(8),
		decimal.New(6),
		decimal.New(4),
		decimal.New(2),
	}

	corr := CalculateCorrelation(series1, series2)

	if corr.GT(decimal.New(-0.99)) {
		t.Errorf("Expected correlation close to -1.0 for perfectly opposite series, got %v", corr)
	}
}

func TestCalculateCorrelationEmpty(t *testing.T) {
	series1 := []decimal.Decimal{}
	series2 := []decimal.Decimal{}

	corr := CalculateCorrelation(series1, series2)

	if !corr.IsZero() {
		t.Errorf("Expected zero correlation for empty series, got %v", corr)
	}
}

func TestCalculateRollingVaR(t *testing.T) {
	prices := []decimal.Decimal{
		decimal.New(100),
		decimal.New(102),
		decimal.New(101),
		decimal.New(105),
		decimal.New(103),
		decimal.New(108),
		decimal.New(106),
		decimal.New(110),
	}

	results := CalculateRollingVaR(prices, 5, 0.95, HistoricalVaR)

	if len(results) != 3 {
		t.Errorf("Expected 3 rolling VaR results, got %d", len(results))
	}
}

func TestCalculateRollingVaRInsufficientData(t *testing.T) {
	prices := []decimal.Decimal{
		decimal.New(100),
		decimal.New(102),
		decimal.New(101),
	}

	results := CalculateRollingVaR(prices, 5, 0.95, HistoricalVaR)

	if len(results) != 0 {
		t.Errorf("Expected 0 rolling VaR results for insufficient data, got %d", len(results))
	}
}

func TestGetZScoreForConfidence(t *testing.T) {
	tests := []struct {
		confidence float64
		expected   float64
	}{
		{0.99, 2.326},
		{0.975, 1.96},
		{0.95, 1.645},
		{0.90, 1.282},
		{0.80, 0.842},
		{0.75, 0.674},
		{0.50, 1.0},
	}

	for _, tt := range tests {
		zScore := getZScoreForConfidence(tt.confidence)
		if zScore.LT(decimal.New(tt.expected-0.001)) || zScore.GT(decimal.New(tt.expected+0.001)) {
			t.Errorf("Expected z-score %v for confidence %v, got %v", tt.expected, tt.confidence, zScore)
		}
	}
}

func TestNewVaRResult(t *testing.T) {
	result := NewVaRResult(
		decimal.New(-0.05),
		decimal.New(-0.07),
		decimal.New(0.95),
		10,
	)

	if !result.VaR.EQ(decimal.New(-0.05)) {
		t.Errorf("Expected VaR -0.05, got %v", result.VaR)
	}
	if !result.CVaR.EQ(decimal.New(-0.07)) {
		t.Errorf("Expected CVaR -0.07, got %v", result.CVaR)
	}
	if !result.Confidence.EQ(decimal.New(0.95)) {
		t.Errorf("Expected Confidence 0.95, got %v", result.Confidence)
	}
	if result.Period != 10 {
		t.Errorf("Expected Period 10, got %d", result.Period)
	}
}
