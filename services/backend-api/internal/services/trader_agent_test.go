package services

import (
	"context"
	"testing"
)

func TestTraderAgentConfig_Defaults(t *testing.T) {
	config := DefaultTraderAgentConfig()

	if config.Mode != ModeModerate {
		t.Errorf("expected Mode to be moderate, got %s", config.Mode)
	}

	if config.MaxOpenPositions != 5 {
		t.Errorf("expected MaxOpenPositions to be 5, got %d", config.MaxOpenPositions)
	}
}

func TestTraderAgent_NewAgent(t *testing.T) {
	agent := NewTraderAgent(DefaultTraderAgentConfig())

	if agent == nil {
		t.Fatal("expected agent to not be nil")
	}
}

func TestTraderAgent_MakeDecision_Bullish(t *testing.T) {
	agent := NewTraderAgent(DefaultTraderAgentConfig())

	market := MarketContext{
		Symbol:       "BTC/USDT",
		CurrentPrice: 50000,
		Volatility:   0.2,
		Liquidity:    0.8,
		Signals: []TradingSignal{
			{Name: "rsi", Value: 0.8, Weight: 1.0, Direction: "bullish"},
			{Name: "macd", Value: 0.7, Weight: 1.0, Direction: "bullish"},
		},
	}

	portfolio := PortfolioState{
		TotalValue:    10000,
		AvailableCash: 5000,
		OpenPositions: 0,
	}

	decision, err := agent.MakeDecision(context.Background(), market, portfolio)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if decision.Symbol != "BTC/USDT" {
		t.Errorf("expected symbol to be BTC/USDT, got %s", decision.Symbol)
	}

	if decision.Action != ActionOpenLong {
		t.Errorf("expected open_long action, got %s", decision.Action)
	}
}

func TestTraderAgent_MakeDecision_Bearish(t *testing.T) {
	agent := NewTraderAgent(DefaultTraderAgentConfig())

	market := MarketContext{
		Symbol:       "ETH/USDT",
		CurrentPrice: 3000,
		Volatility:   0.2,
		Liquidity:    0.8,
		Signals: []TradingSignal{
			{Name: "rsi", Value: -0.9, Weight: 1.0, Direction: "bearish"},
			{Name: "macd", Value: -0.9, Weight: 1.0, Direction: "bearish"},
			{Name: "trend", Value: -0.8, Weight: 0.5, Direction: "bearish"},
		},
	}

	portfolio := PortfolioState{
		TotalValue:    10000,
		AvailableCash: 5000,
		OpenPositions: 0,
	}

	decision, err := agent.MakeDecision(context.Background(), market, portfolio)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if decision.Action != ActionOpenShort && decision.Action != ActionWait {
		t.Errorf("expected open_short or wait action, got %s", decision.Action)
	}
}

func TestTraderAgent_MakeDecision_MaxPositions(t *testing.T) {
	config := DefaultTraderAgentConfig()
	config.MaxOpenPositions = 1
	agent := NewTraderAgent(config)

	market := MarketContext{
		Symbol:       "BTC/USDT",
		CurrentPrice: 50000,
		Signals: []TradingSignal{
			{Name: "rsi", Value: 0.8, Weight: 1.0, Direction: "bullish"},
		},
	}

	portfolio := PortfolioState{
		TotalValue:    10000,
		AvailableCash: 5000,
		OpenPositions: 1,
	}

	decision, err := agent.MakeDecision(context.Background(), market, portfolio)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if decision.Action != ActionHold {
		t.Errorf("expected hold action when max positions reached, got %s", decision.Action)
	}
}

func TestTraderAgent_ShouldExecute(t *testing.T) {
	agent := NewTraderAgent(DefaultTraderAgentConfig())

	buyDecision := &TradingDecision{
		Action:      ActionOpenLong,
		Confidence:  0.8,
		SizePercent: 0.1,
	}

	if !agent.ShouldExecute(buyDecision) {
		t.Error("expected ShouldExecute to return true for valid buy decision")
	}

	waitDecision := &TradingDecision{
		Action:     ActionWait,
		Confidence: 0.8,
	}

	if agent.ShouldExecute(waitDecision) {
		t.Error("expected ShouldExecute to return false for wait action")
	}

	lowConfidence := &TradingDecision{
		Action:      ActionOpenLong,
		Confidence:  0.5,
		SizePercent: 0.1,
	}

	if agent.ShouldExecute(lowConfidence) {
		t.Error("expected ShouldExecute to return false for low confidence")
	}
}

func TestTraderAgent_Metrics(t *testing.T) {
	agent := NewTraderAgent(DefaultTraderAgentConfig())

	market := MarketContext{
		Symbol:       "BTC/USDT",
		CurrentPrice: 50000,
		Signals: []TradingSignal{
			{Name: "rsi", Value: 0.8, Weight: 1.0, Direction: "bullish"},
		},
	}

	portfolio := PortfolioState{
		TotalValue:    10000,
		AvailableCash: 5000,
	}

	_, _ = agent.MakeDecision(context.Background(), market, portfolio)
	_, _ = agent.MakeDecision(context.Background(), market, portfolio)

	metrics := agent.GetMetrics()

	if metrics.TotalDecisions != 2 {
		t.Errorf("expected 2 total decisions, got %d", metrics.TotalDecisions)
	}
}

func TestTraderAgent_CalculateConfidence(t *testing.T) {
	agent := NewTraderAgent(DefaultTraderAgentConfig())

	market := MarketContext{
		Signals: []TradingSignal{
			{Name: "rsi", Value: 0.5, Weight: 1.0, Direction: "bullish"},
			{Name: "macd", Value: 0.5, Weight: 1.0, Direction: "bullish"},
		},
	}

	confidence := agent.calculateConfidence(market)

	if confidence <= 0 {
		t.Error("expected positive confidence")
	}

	if confidence > 1.0 {
		t.Error("expected confidence <= 1.0")
	}
}
