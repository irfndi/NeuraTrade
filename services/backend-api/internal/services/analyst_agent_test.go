package services

import (
	"context"
	"testing"
)

func TestAnalystAgentConfig_Defaults(t *testing.T) {
	config := DefaultAnalystAgentConfig()

	if config.MinConfidence != 0.6 {
		t.Errorf("expected MinConfidence to be 0.6, got %f", config.MinConfidence)
	}

	if config.SignalThreshold != 0.5 {
		t.Errorf("expected SignalThreshold to be 0.5, got %f", config.SignalThreshold)
	}
}

func TestAnalystAgent_NewAgent(t *testing.T) {
	agent := NewAnalystAgent(DefaultAnalystAgentConfig())

	if agent == nil {
		t.Fatal("expected agent to not be nil")
	}
}

func TestAnalystAgent_Analyze_Bullish(t *testing.T) {
	agent := NewAnalystAgent(DefaultAnalystAgentConfig())

	signals := []AnalystSignal{
		{Name: "rsi", Value: 0.7, Weight: 1.0, Direction: DirectionBullish},
		{Name: "macd", Value: 0.8, Weight: 1.0, Direction: DirectionBullish},
		{Name: "trend", Value: 0.6, Weight: 0.5, Direction: DirectionBullish},
	}

	analysis, err := agent.Analyze(context.Background(), "BTC/USDT", AnalystRoleTechnical, signals)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if analysis.Symbol != "BTC/USDT" {
		t.Errorf("expected symbol to be BTC/USDT, got %s", analysis.Symbol)
	}

	if analysis.Role != AnalystRoleTechnical {
		t.Errorf("expected role to be technical, got %s", analysis.Role)
	}

	if analysis.Recommendation != RecommendationBuy {
		t.Errorf("expected buy recommendation, got %s", analysis.Recommendation)
	}
}

func TestAnalystAgent_Analyze_Bearish(t *testing.T) {
	agent := NewAnalystAgent(DefaultAnalystAgentConfig())

	signals := []AnalystSignal{
		{Name: "rsi", Value: -0.7, Weight: 1.0, Direction: DirectionBearish},
		{Name: "macd", Value: -0.8, Weight: 1.0, Direction: DirectionBearish},
		{Name: "trend", Value: -0.6, Weight: 0.5, Direction: DirectionBearish},
	}

	analysis, err := agent.Analyze(context.Background(), "ETH/USDT", AnalystRoleTechnical, signals)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if analysis.Recommendation != RecommendationSell {
		t.Errorf("expected sell recommendation, got %s", analysis.Recommendation)
	}
}

func TestAnalystAgent_Analyze_Neutral(t *testing.T) {
	agent := NewAnalystAgent(DefaultAnalystAgentConfig())

	signals := []AnalystSignal{
		{Name: "rsi", Value: 0.3, Weight: 1.0, Direction: DirectionNeutral},
		{Name: "macd", Value: 0.2, Weight: 1.0, Direction: DirectionNeutral},
		{Name: "trend", Value: 0.1, Weight: 0.5, Direction: DirectionNeutral},
	}

	analysis, err := agent.Analyze(context.Background(), "BTC/USDT", AnalystRoleTechnical, signals)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if analysis.Recommendation != RecommendationHold && analysis.Recommendation != RecommendationWatch {
		t.Errorf("expected hold or watch recommendation, got %s", analysis.Recommendation)
	}
}

func TestAnalystAgent_QuickAnalyze(t *testing.T) {
	agent := NewAnalystAgent(DefaultAnalystAgentConfig())

	signals := []AnalystSignal{
		{Name: "rsi", Value: 0.8, Weight: 1.0, Direction: DirectionBullish},
	}

	recommendation := agent.QuickAnalyze("BTC/USDT", signals)

	if recommendation == "" {
		t.Error("expected non-empty recommendation")
	}
}

func TestAnalystAgent_ShouldTrade(t *testing.T) {
	agent := NewAnalystAgent(DefaultAnalystAgentConfig())

	buyAnalysis := &AnalystAnalysis{
		Recommendation: RecommendationBuy,
		Confidence:     0.8,
		RiskLevel:      "low",
	}

	if !agent.ShouldTrade(buyAnalysis) {
		t.Error("expected ShouldTrade to return true for buy with high confidence and low risk")
	}

	lowConfidenceAnalysis := &AnalystAnalysis{
		Recommendation: RecommendationBuy,
		Confidence:     0.3,
		RiskLevel:      "low",
	}

	if agent.ShouldTrade(lowConfidenceAnalysis) {
		t.Error("expected ShouldTrade to return false for low confidence")
	}

	highRiskAnalysis := &AnalystAnalysis{
		Recommendation: RecommendationBuy,
		Confidence:     0.8,
		RiskLevel:      "high",
	}

	if agent.ShouldTrade(highRiskAnalysis) {
		t.Error("expected ShouldTrade to return false for high risk")
	}
}

func TestAnalystAgent_Metrics(t *testing.T) {
	agent := NewAnalystAgent(DefaultAnalystAgentConfig())

	bullishSignals := []AnalystSignal{
		{Name: "rsi", Value: 0.7, Weight: 1.0, Direction: DirectionBullish},
	}
	bearishSignals := []AnalystSignal{
		{Name: "rsi", Value: -0.7, Weight: 1.0, Direction: DirectionBearish},
	}
	neutralSignals := []AnalystSignal{
		{Name: "rsi", Value: 0.1, Weight: 1.0, Direction: DirectionNeutral},
	}

	if _, err := agent.Analyze(context.Background(), "BTC/USDT", AnalystRoleTechnical, bullishSignals); err != nil {
		t.Errorf("Analyze BTC failed: %v", err)
	}
	if _, err := agent.Analyze(context.Background(), "ETH/USDT", AnalystRoleSentiment, bearishSignals); err != nil {
		t.Errorf("Analyze ETH failed: %v", err)
	}
	if _, err := agent.Analyze(context.Background(), "SOL/USDT", AnalystRoleTechnical, neutralSignals); err != nil {
		t.Errorf("Analyze SOL failed: %v", err)
	}

	metrics := agent.GetMetrics()

	if metrics.TotalAnalyses != 3 {
		t.Errorf("expected 3 total analyses, got %d", metrics.TotalAnalyses)
	}

	if metrics.BuySignals != 1 {
		t.Errorf("expected 1 buy signal, got %d", metrics.BuySignals)
	}

	if metrics.SellSignals != 1 {
		t.Errorf("expected 1 sell signal, got %d", metrics.SellSignals)
	}

	if metrics.HoldSignals != 1 {
		t.Errorf("expected 1 hold signal, got %d", metrics.HoldSignals)
	}
}

func TestAnalystAgent_SetGetConfig(t *testing.T) {
	agent := NewAnalystAgent(DefaultAnalystAgentConfig())

	newConfig := AnalystAgentConfig{
		MinConfidence:    0.8,
		SignalThreshold:  0.6,
		MaxRiskScore:     0.5,
		AnalysisCooldown: 10,
	}

	agent.SetConfig(newConfig)
	gotConfig := agent.GetConfig()

	if gotConfig.MinConfidence != 0.8 {
		t.Errorf("expected MinConfidence to be 0.8, got %f", gotConfig.MinConfidence)
	}
}

func TestCalculateConfidence(t *testing.T) {
	signals := []AnalystSignal{
		{Name: "rsi", Value: 0.7, Weight: 1.0, Direction: DirectionBullish},
		{Name: "macd", Value: 0.8, Weight: 1.0, Direction: DirectionBullish},
	}

	confidence := calculateConfidence(signals, 0.75)

	if confidence <= 0 {
		t.Error("expected positive confidence")
	}

	if confidence > 1.0 {
		t.Error("expected confidence <= 1.0")
	}
}

func TestDetermineCondition(t *testing.T) {
	tests := []struct {
		score    float64
		expected MarketCondition
	}{
		{0.7, ConditionBullish},
		{-0.7, ConditionBearish},
		{0.1, ConditionNeutral},
	}

	for _, tt := range tests {
		condition := determineCondition(tt.score, []AnalystSignal{})
		if condition != tt.expected {
			t.Errorf("determineCondition(%f) = %s, expected %s", tt.score, condition, tt.expected)
		}
	}
}

func TestCalculateRiskLevel(t *testing.T) {
	// volatility value 0.8 * 0.4 = 0.32, which is > 0.3 but < 0.6, so it's medium
	highRiskSignals := []AnalystSignal{
		{Name: "volatility", Value: 0.8, Weight: 1.0, Direction: DirectionNeutral},
	}

	risk := calculateRiskLevel(highRiskSignals)
	if risk != "medium" {
		t.Errorf("expected medium risk for volatility 0.8, got %s", risk)
	}

	// Use higher volatility value to get high risk (> 0.6)
	// volatility value 2.0 * 0.4 = 0.8 > 0.6
	veryHighRiskSignals := []AnalystSignal{
		{Name: "volatility", Value: 2.0, Weight: 1.0, Direction: DirectionNeutral},
	}

	risk = calculateRiskLevel(veryHighRiskSignals)
	if risk != "high" {
		t.Errorf("expected high risk for volatility 1.0, got %s", risk)
	}

	lowRiskSignals := []AnalystSignal{
		{Name: "volatility", Value: 0.1, Weight: 1.0, Direction: DirectionNeutral},
		{Name: "volume", Value: 0.8, Weight: 1.0, Direction: DirectionNeutral},
	}

	risk = calculateRiskLevel(lowRiskSignals)
	if risk != "low" {
		t.Errorf("expected low risk, got %s", risk)
	}
}

func TestGenerateSummary(t *testing.T) {
	analysis := &AnalystAnalysis{
		Symbol:         "BTC/USDT",
		Role:           AnalystRoleTechnical,
		Condition:      ConditionBullish,
		Recommendation: RecommendationBuy,
		Confidence:     0.85,
	}

	summary := generateSummary(analysis)

	if summary == "" {
		t.Error("expected non-empty summary")
	}
}
