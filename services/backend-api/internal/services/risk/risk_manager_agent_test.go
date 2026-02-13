package risk

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"
)

func TestRiskManagerConfig_Defaults(t *testing.T) {
	config := DefaultRiskManagerConfig()

	if config.MaxPortfolioRisk != 0.1 {
		t.Errorf("expected MaxPortfolioRisk to be 0.1, got %f", config.MaxPortfolioRisk)
	}

	if config.MaxPositionRisk != 0.02 {
		t.Errorf("expected MaxPositionRisk to be 0.02, got %f", config.MaxPositionRisk)
	}

	if config.ConsecutiveLossLimit != 3 {
		t.Errorf("expected ConsecutiveLossLimit to be 3, got %d", config.ConsecutiveLossLimit)
	}
}

func TestRiskManagerAgent_NewAgent(t *testing.T) {
	agent := NewRiskManagerAgent(DefaultRiskManagerConfig())

	if agent == nil {
		t.Fatal("expected agent to not be nil")
	}
}

func TestRiskManagerAgent_AssessPortfolioRisk_LowRisk(t *testing.T) {
	agent := NewRiskManagerAgent(DefaultRiskManagerConfig())

	signals := []RiskSignal{
		{Name: "volatility", Value: 0.1, Weight: 1.0, Threshold: 0.5},
		{Name: "drawdown", Value: 0.05, Weight: 1.0, Threshold: 0.5},
		{Name: "exposure", Value: 0.1, Weight: 0.5, Threshold: 0.5},
	}

	assessment, err := agent.AssessPortfolioRisk(context.Background(), signals)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if assessment.Role != RiskManagerRolePortfolio {
		t.Errorf("expected role to be portfolio, got %s", assessment.Role)
	}

	if assessment.Action != RiskActionApprove {
		t.Errorf("expected action to be approve for low risk, got %s", assessment.Action)
	}

	if assessment.RiskLevel != RiskLevelLow {
		t.Errorf("expected risk level to be low, got %s", assessment.RiskLevel)
	}
}

func TestRiskManagerAgent_AssessPortfolioRisk_HighRisk(t *testing.T) {
	agent := NewRiskManagerAgent(DefaultRiskManagerConfig())

	signals := []RiskSignal{
		{Name: "volatility", Value: 0.8, Weight: 1.0, Threshold: 0.5, Description: "High volatility detected"},
		{Name: "drawdown", Value: 0.7, Weight: 1.0, Threshold: 0.5, Description: "High drawdown detected"},
		{Name: "exposure", Value: 0.9, Weight: 0.5, Threshold: 0.5, Description: "High exposure"},
	}

	assessment, err := agent.AssessPortfolioRisk(context.Background(), signals)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if assessment.RiskLevel != RiskLevelHigh && assessment.RiskLevel != RiskLevelExtreme {
		t.Errorf("expected risk level to be high or extreme, got %s", assessment.RiskLevel)
	}
}

func TestRiskManagerAgent_AssessPositionRisk(t *testing.T) {
	agent := NewRiskManagerAgent(DefaultRiskManagerConfig())

	signals := []RiskSignal{
		{Name: "liquidity", Value: 0.3, Weight: 1.0, Threshold: 0.5},
		{Name: "spread", Value: 0.2, Weight: 1.0, Threshold: 0.5},
	}

	positionSize := decimal.NewFromFloat(500)

	assessment, err := agent.AssessPositionRisk(context.Background(), "BTC/USDT", signals, positionSize)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if assessment.Symbol != "BTC/USDT" {
		t.Errorf("expected symbol to be BTC/USDT, got %s", assessment.Symbol)
	}

	if assessment.Role != RiskManagerRolePosition {
		t.Errorf("expected role to be position, got %s", assessment.Role)
	}

	if assessment.StopLossPct == 0 {
		t.Error("expected stop loss percentage to be set")
	}
}

func TestRiskManagerAgent_AssessPositionRisk_ExceedsLimit(t *testing.T) {
	agent := NewRiskManagerAgent(DefaultRiskManagerConfig())

	signals := []RiskSignal{
		{Name: "liquidity", Value: 0.3, Weight: 1.0, Threshold: 0.5},
	}

	positionSize := decimal.NewFromFloat(2000)

	assessment, err := agent.AssessPositionRisk(context.Background(), "BTC/USDT", signals, positionSize)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if assessment.Action != RiskActionReduce {
		t.Errorf("expected action to be reduce when position exceeds limit, got %s", assessment.Action)
	}
}

func TestRiskManagerAgent_AssessTradingRisk_Approved(t *testing.T) {
	agent := NewRiskManagerAgent(DefaultRiskManagerConfig())

	signals := []RiskSignal{
		{Name: "signal_strength", Value: 0.1, Weight: 1.0, Threshold: 0.5},
		{Name: "market_condition", Value: 0.1, Weight: 1.0, Threshold: 0.5},
		{Name: "volatility", Value: 0.1, Weight: 1.0, Threshold: 0.5},
		{Name: "liquidity", Value: 0.1, Weight: 1.0, Threshold: 0.5},
	}

	assessment, err := agent.AssessTradingRisk(context.Background(), "BTC/USDT", "long", signals)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if assessment.Action != RiskActionApprove {
		t.Errorf("expected action to be approve for low risk signals, got %s", assessment.Action)
	}
}

func TestRiskManagerAgent_AssessTradingRisk_Blocked(t *testing.T) {
	agent := NewRiskManagerAgent(DefaultRiskManagerConfig())

	signals := []RiskSignal{
		{Name: "signal_strength", Value: 0.9, Weight: 1.0, Threshold: 0.5, Description: "Very high signal strength"},
		{Name: "market_condition", Value: 0.8, Weight: 1.0, Threshold: 0.5, Description: "Poor market conditions"},
		{Name: "risk_reward_ratio", Value: 0.5, Weight: 1.0, Threshold: 2.0, Description: "Poor risk/reward"},
	}

	assessment, err := agent.AssessTradingRisk(context.Background(), "BTC/USDT", "long", signals)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if assessment.Action != RiskActionBlock {
		t.Errorf("expected action to be block for high risk, got %s", assessment.Action)
	}
}

func TestRiskManagerAgent_CheckEmergencyConditions_Triggered(t *testing.T) {
	agent := NewRiskManagerAgent(DefaultRiskManagerConfig())

	currentDrawdown := 0.25
	dailyLoss := decimal.NewFromFloat(150)

	assessment, err := agent.CheckEmergencyConditions(context.Background(), currentDrawdown, dailyLoss)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if assessment.Action != RiskActionEmergency {
		t.Errorf("expected emergency action when thresholds exceeded, got %s", assessment.Action)
	}

	if assessment.RiskLevel != RiskLevelExtreme {
		t.Errorf("expected extreme risk level, got %s", assessment.RiskLevel)
	}
}

func TestRiskManagerAgent_CheckEmergencyConditions_Normal(t *testing.T) {
	agent := NewRiskManagerAgent(DefaultRiskManagerConfig())

	currentDrawdown := 0.05
	dailyLoss := decimal.NewFromFloat(50)

	assessment, err := agent.CheckEmergencyConditions(context.Background(), currentDrawdown, dailyLoss)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if assessment.Action != RiskActionApprove {
		t.Errorf("expected approve action when within limits, got %s", assessment.Action)
	}
}

func TestRiskManagerAgent_ShouldTrade(t *testing.T) {
	agent := NewRiskManagerAgent(DefaultRiskManagerConfig())

	tests := []struct {
		action      RiskAction
		shouldTrade bool
	}{
		{RiskActionApprove, true},
		{RiskActionWarning, true},
		{RiskActionBlock, false},
		{RiskActionReduce, true}, // Reduce allows trading at reduced size
		{RiskActionEmergency, false},
	}

	for _, tt := range tests {
		assessment := &RiskAssessment{Action: tt.action}
		result := agent.ShouldTrade(assessment)
		if result != tt.shouldTrade {
			t.Errorf("expected ShouldTrade(%s) to be %v, got %v", tt.action, tt.shouldTrade, result)
		}
	}
}

func TestRiskManagerAgent_Metrics(t *testing.T) {
	agent := NewRiskManagerAgent(DefaultRiskManagerConfig())

	signals := []RiskSignal{
		{Name: "test", Value: 0.3, Weight: 1.0, Threshold: 0.5},
	}

	_, _ = agent.AssessTradingRisk(context.Background(), "BTC/USDT", "long", signals)
	_, _ = agent.AssessTradingRisk(context.Background(), "ETH/USDT", "short", signals)

	metrics := agent.GetMetrics()

	if metrics.TotalAssessments == 0 {
		t.Error("expected metrics to track assessments")
	}
}

func TestRiskManagerAgent_SetGetConfig(t *testing.T) {
	agent := NewRiskManagerAgent(DefaultRiskManagerConfig())

	newConfig := RiskManagerConfig{
		MaxPortfolioRisk:     0.2,
		MaxPositionRisk:      0.05,
		ConsecutiveLossLimit: 5,
	}

	agent.SetConfig(newConfig)

	retrievedConfig := agent.GetConfig()

	if retrievedConfig.MaxPortfolioRisk != 0.2 {
		t.Errorf("expected MaxPortfolioRisk to be 0.2, got %f", retrievedConfig.MaxPortfolioRisk)
	}

	if retrievedConfig.ConsecutiveLossLimit != 5 {
		t.Errorf("expected ConsecutiveLossLimit to be 5, got %d", retrievedConfig.ConsecutiveLossLimit)
	}
}

func TestCalculateRiskScore(t *testing.T) {
	tests := []struct {
		name     string
		signals  []RiskSignal
		expected float64
	}{
		{
			name:     "empty signals",
			signals:  []RiskSignal{},
			expected: 0.0,
		},
		{
			name: "single signal",
			signals: []RiskSignal{
				{Value: 0.5, Weight: 1.0},
			},
			expected: 0.5,
		},
		{
			name: "weighted signals",
			signals: []RiskSignal{
				{Value: 0.8, Weight: 2.0},
				{Value: 0.4, Weight: 1.0},
			},
			expected: (0.8*2.0 + 0.4*1.0) / 3.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculateRiskScore(tt.signals)
			if result != tt.expected {
				t.Errorf("expected %f, got %f", tt.expected, result)
			}
		})
	}
}

func TestDetermineRiskLevel(t *testing.T) {
	tests := []struct {
		score    float64
		expected RiskLevel
	}{
		{0.1, RiskLevelLow},
		{0.3, RiskLevelMedium},
		{0.6, RiskLevelHigh},
		{0.9, RiskLevelExtreme},
	}

	for _, tt := range tests {
		result := determineRiskLevel(tt.score)
		if result != tt.expected {
			t.Errorf("for score %f, expected %s, got %s", tt.score, tt.expected, result)
		}
	}
}
