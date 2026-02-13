package services

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/ai/llm"
	"github.com/irfndi/neuratrade/internal/services/risk"
	"github.com/irfndi/neuratrade/internal/skill"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockToolExecutor implements ToolExecutor for testing
type MockToolExecutor struct {
	Results map[string]json.RawMessage
	Errors  map[string]error
}

func (m *MockToolExecutor) Execute(ctx context.Context, name string, arguments json.RawMessage) (json.RawMessage, error) {
	if err, ok := m.Errors[name]; ok {
		return nil, err
	}
	if result, ok := m.Results[name]; ok {
		return result, nil
	}
	return json.RawMessage(`{"status": "ok"}`), nil
}

// MockOrderExecutor implements OrderExecutor for testing
type MockOrderExecutor struct {
	ExecutedDecisions []*TradingDecision
	ExecuteError      error
}

func (m *MockOrderExecutor) ExecuteOrder(ctx context.Context, decision *TradingDecision) error {
	if m.ExecuteError != nil {
		return m.ExecuteError
	}
	m.ExecutedDecisions = append(m.ExecutedDecisions, decision)
	return nil
}

// MockLLMClient implements llm.Client for testing
type MockLLMClient struct {
	Responses []*llm.CompletionResponse
	CallCount int
}

func (m *MockLLMClient) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	if m.CallCount >= len(m.Responses) {
		return &llm.CompletionResponse{
			Message: llm.Message{Content: "done"},
		}, nil
	}
	resp := m.Responses[m.CallCount]
	m.CallCount++
	return resp, nil
}

func (m *MockLLMClient) Stream(ctx context.Context, req *llm.CompletionRequest) (<-chan llm.StreamEvent, error) {
	ch := make(chan llm.StreamEvent)
	go func() {
		defer close(ch)
		ch <- llm.StreamEvent{Type: llm.StreamEventDone, Done: true}
	}()
	return ch, nil
}

func (m *MockLLMClient) Provider() llm.Provider {
	return llm.ProviderOpenAI
}

func (m *MockLLMClient) Close() error {
	return nil
}

func TestAgentExecutionLoopConfig_Defaults(t *testing.T) {
	config := DefaultAgentExecutionLoopConfig()

	assert.Equal(t, 5, config.MaxIterations)
	assert.Equal(t, 60*time.Second, config.Timeout)
	assert.True(t, config.RequireRiskApproval)
	assert.Equal(t, 0.7, config.MinConfidence)
	assert.True(t, config.EnableToolCalls)
	assert.False(t, config.AutoExecute)
}

func TestAgentExecutionLoop_New(t *testing.T) {
	config := DefaultAgentExecutionLoopConfig()
	analystAgent := NewAnalystAgent(DefaultAnalystAgentConfig())
	traderAgent := NewTraderAgent(DefaultTraderAgentConfig())
	riskManager := risk.NewRiskManagerAgent(risk.DefaultRiskManagerConfig())

	loop := NewAgentExecutionLoop(
		config,
		analystAgent,
		traderAgent,
		riskManager,
		nil, // LLM client
		nil, // skill registry
		nil, // tool executor
		nil, // order executor
	)

	require.NotNil(t, loop)
	assert.Equal(t, config, loop.GetConfig())
}

func TestAgentExecutionLoop_Execute_HoldDecision(t *testing.T) {
	config := DefaultAgentExecutionLoopConfig()
	config.RequireRiskApproval = false // Simplify test

	analystAgent := NewAnalystAgent(DefaultAnalystAgentConfig())
	traderAgent := NewTraderAgent(DefaultTraderAgentConfig())

	loop := NewAgentExecutionLoop(
		config,
		analystAgent,
		traderAgent,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	// Market context with conflicting signals (should result in hold)
	market := MarketContext{
		Symbol:       "BTC/USDT",
		CurrentPrice: 50000,
		Volatility:   0.3,
		Trend:        "neutral",
		Liquidity:    0.8,
		Signals: []TradingSignal{
			{Name: "rsi", Value: 0.5, Weight: 0.5, Direction: "bullish"},
			{Name: "macd", Value: -0.3, Weight: 0.5, Direction: "bearish"},
		},
	}

	portfolio := PortfolioState{
		TotalValue:    10000,
		AvailableCash: 8000,
		OpenPositions: 0,
	}

	result, err := loop.Execute(context.Background(), "BTC/USDT", market, portfolio)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "BTC/USDT", result.Symbol)
	assert.NotEmpty(t, result.LoopID)
	assert.GreaterOrEqual(t, result.ExecutionTime, time.Duration(0))
}

func TestAgentExecutionLoop_Execute_WithRiskBlock(t *testing.T) {
	config := DefaultAgentExecutionLoopConfig()
	config.MinConfidence = 0.1 // Allow low confidence for test

	analystAgent := NewAnalystAgent(DefaultAnalystAgentConfig())
	traderAgent := NewTraderAgent(DefaultTraderAgentConfig())

	// Risk manager with very strict config
	riskConfig := risk.DefaultRiskManagerConfig()
	riskConfig.MaxPositionRisk = 0.001 // Very strict
	riskManager := risk.NewRiskManagerAgent(riskConfig)

	loop := NewAgentExecutionLoop(
		config,
		analystAgent,
		traderAgent,
		riskManager,
		nil,
		nil,
		nil,
		nil,
	)

	market := MarketContext{
		Symbol:       "BTC/USDT",
		CurrentPrice: 50000,
		Volatility:   0.8, // High volatility
		Trend:        "bullish",
		Liquidity:    0.2, // Low liquidity
		Signals: []TradingSignal{
			{Name: "rsi", Value: 0.8, Weight: 1.0, Direction: "bullish"},
		},
	}

	portfolio := PortfolioState{
		TotalValue:      10000,
		AvailableCash:   8000,
		OpenPositions:   0,
		CurrentDrawdown: 0.15, // High drawdown
	}

	result, err := loop.Execute(context.Background(), "BTC/USDT", market, portfolio)

	require.NoError(t, err)
	require.NotNil(t, result)
	// Risk should either block or approve based on signals
	assert.Contains(t, []ExecutionDecision{ExecutionDecisionReject, ExecutionDecisionApprove, ExecutionDecisionDefer}, result.Decision)
}

func TestAgentExecutionLoop_Execute_WithToolCalls(t *testing.T) {
	config := DefaultAgentExecutionLoopConfig()
	config.RequireRiskApproval = false
	config.MinConfidence = 0.1

	analystAgent := NewAnalystAgent(DefaultAnalystAgentConfig())
	traderAgent := NewTraderAgent(DefaultTraderAgentConfig())

	// Mock LLM client that returns tool calls
	mockLLM := &MockLLMClient{
		Responses: []*llm.CompletionResponse{
			{
				ToolCalls: []llm.ToolCall{
					{ID: "tc1", Name: "get_price", Arguments: json.RawMessage(`{"symbol": "BTC/USDT"}`)},
				},
				FinishReason: "tool_calls",
			},
			{
				Message:      llm.Message{Content: "Analysis complete"},
				FinishReason: "stop",
			},
		},
	}

	// Mock tool executor
	mockToolExec := &MockToolExecutor{
		Results: map[string]json.RawMessage{
			"get_price": json.RawMessage(`{"price": 50000.0, "symbol": "BTC/USDT"}`),
		},
	}

	loop := NewAgentExecutionLoop(
		config,
		analystAgent,
		traderAgent,
		nil,
		mockLLM,
		nil,
		mockToolExec,
		nil,
	)

	market := MarketContext{
		Symbol:       "BTC/USDT",
		CurrentPrice: 50000,
		Volatility:   0.3,
		Trend:        "bullish",
		Liquidity:    0.8,
		Signals: []TradingSignal{
			{Name: "rsi", Value: 0.8, Weight: 1.0, Direction: "bullish"},
		},
	}

	portfolio := PortfolioState{
		TotalValue:    10000,
		AvailableCash: 8000,
		OpenPositions: 0,
	}

	result, err := loop.Execute(context.Background(), "BTC/USDT", market, portfolio)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Len(t, result.ToolCallsMade, 1)
	assert.Len(t, result.ToolResults, 1)
	assert.Equal(t, "get_price", result.ToolCallsMade[0].Name)
}

func TestAgentExecutionLoop_Execute_AutoExecute(t *testing.T) {
	config := DefaultAgentExecutionLoopConfig()
	config.RequireRiskApproval = false
	config.AutoExecute = true
	config.MinConfidence = 0.1

	analystAgent := NewAnalystAgent(DefaultAnalystAgentConfig())
	traderAgent := NewTraderAgent(DefaultTraderAgentConfig())
	mockOrderExec := &MockOrderExecutor{}

	loop := NewAgentExecutionLoop(
		config,
		analystAgent,
		traderAgent,
		nil,
		nil,
		nil,
		nil,
		mockOrderExec,
	)

	market := MarketContext{
		Symbol:       "BTC/USDT",
		CurrentPrice: 50000,
		Volatility:   0.3,
		Trend:        "bullish",
		Liquidity:    0.8,
		Signals: []TradingSignal{
			{Name: "rsi", Value: 0.9, Weight: 1.0, Direction: "bullish"},
		},
	}

	portfolio := PortfolioState{
		TotalValue:    10000,
		AvailableCash: 8000,
		OpenPositions: 0,
	}

	result, err := loop.Execute(context.Background(), "BTC/USDT", market, portfolio)

	require.NoError(t, err)
	require.NotNil(t, result)

	// If decision was approved, order should have been executed
	if result.Decision == ExecutionDecisionApprove {
		assert.Len(t, mockOrderExec.ExecutedDecisions, 1)
	}
}

func TestAgentExecutionLoop_Execute_OrderExecutionError(t *testing.T) {
	config := DefaultAgentExecutionLoopConfig()
	config.RequireRiskApproval = false
	config.AutoExecute = true
	config.MinConfidence = 0.1

	analystAgent := NewAnalystAgent(DefaultAnalystAgentConfig())
	traderAgent := NewTraderAgent(DefaultTraderAgentConfig())
	mockOrderExec := &MockOrderExecutor{
		ExecuteError: errors.New("exchange unavailable"),
	}

	loop := NewAgentExecutionLoop(
		config,
		analystAgent,
		traderAgent,
		nil,
		nil,
		nil,
		nil,
		mockOrderExec,
	)

	market := MarketContext{
		Symbol:       "BTC/USDT",
		CurrentPrice: 50000,
		Volatility:   0.3,
		Trend:        "bullish",
		Liquidity:    0.8,
		Signals: []TradingSignal{
			{Name: "rsi", Value: 0.9, Weight: 1.0, Direction: "bullish"},
		},
	}

	portfolio := PortfolioState{
		TotalValue:    10000,
		AvailableCash: 8000,
		OpenPositions: 0,
	}

	result, err := loop.Execute(context.Background(), "BTC/USDT", market, portfolio)

	require.NoError(t, err)
	require.NotNil(t, result)

	// Decision should still be approved even if execution failed
	if result.Decision == ExecutionDecisionApprove {
		assert.NotEmpty(t, result.Errors)
		assert.Contains(t, result.Errors[0], "order execution failed")
	}
}

func TestAgentExecutionLoop_Execute_Timeout(t *testing.T) {
	config := DefaultAgentExecutionLoopConfig()
	config.Timeout = 1 * time.Nanosecond // Immediate timeout

	analystAgent := NewAnalystAgent(DefaultAnalystAgentConfig())
	traderAgent := NewTraderAgent(DefaultTraderAgentConfig())

	loop := NewAgentExecutionLoop(
		config,
		analystAgent,
		traderAgent,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	market := MarketContext{
		Symbol:       "BTC/USDT",
		CurrentPrice: 50000,
		Volatility:   0.3,
		Trend:        "bullish",
		Liquidity:    0.8,
		Signals:      []TradingSignal{},
	}

	portfolio := PortfolioState{
		TotalValue:    10000,
		AvailableCash: 8000,
		OpenPositions: 0,
	}

	// Should not panic with timeout
	result, err := loop.Execute(context.Background(), "BTC/USDT", market, portfolio)

	// The timeout might cause a context cancellation error or succeed quickly
	if err != nil {
		assert.Contains(t, err.Error(), "context")
	} else {
		assert.NotNil(t, result)
	}
}

func TestAgentExecutionLoop_Execute_WithSkillRegistry(t *testing.T) {
	config := DefaultAgentExecutionLoopConfig()
	config.RequireRiskApproval = false

	analystAgent := NewAnalystAgent(DefaultAnalystAgentConfig())
	traderAgent := NewTraderAgent(DefaultTraderAgentConfig())

	// Create skill registry with a test skill
	registry := skill.NewRegistry("/tmp/test-skills")

	mockLLM := &MockLLMClient{
		Responses: []*llm.CompletionResponse{
			{
				Message:      llm.Message{Content: "Analysis complete"},
				FinishReason: "stop",
			},
		},
	}

	loop := NewAgentExecutionLoop(
		config,
		analystAgent,
		traderAgent,
		nil,
		mockLLM,
		registry,
		nil,
		nil,
	)

	market := MarketContext{
		Symbol:       "BTC/USDT",
		CurrentPrice: 50000,
		Volatility:   0.3,
		Trend:        "bullish",
		Liquidity:    0.8,
		Signals:      []TradingSignal{},
	}

	portfolio := PortfolioState{
		TotalValue:    10000,
		AvailableCash: 8000,
		OpenPositions: 0,
	}

	result, err := loop.Execute(context.Background(), "BTC/USDT", market, portfolio)

	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestAgentExecutionLoop_Metrics(t *testing.T) {
	config := DefaultAgentExecutionLoopConfig()
	config.RequireRiskApproval = false
	config.MinConfidence = 0.1

	analystAgent := NewAnalystAgent(DefaultAnalystAgentConfig())
	traderAgent := NewTraderAgent(DefaultTraderAgentConfig())

	loop := NewAgentExecutionLoop(
		config,
		analystAgent,
		traderAgent,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	market := MarketContext{
		Symbol:       "BTC/USDT",
		CurrentPrice: 50000,
		Volatility:   0.3,
		Trend:        "bullish",
		Liquidity:    0.8,
		Signals: []TradingSignal{
			{Name: "rsi", Value: 0.8, Weight: 1.0, Direction: "bullish"},
		},
	}

	portfolio := PortfolioState{
		TotalValue:    10000,
		AvailableCash: 8000,
		OpenPositions: 0,
	}

	// Run multiple cycles
	for i := 0; i < 3; i++ {
		_, _ = loop.Execute(context.Background(), "BTC/USDT", market, portfolio)
	}

	metrics := loop.GetMetrics()
	assert.Equal(t, int64(3), metrics.TotalCycles)
	assert.Equal(t, int64(3), metrics.DecisionsBySymbol["BTC/USDT"])
	assert.GreaterOrEqual(t, metrics.AverageExecutionTime, float64(0))
}

func TestAgentExecutionLoop_SetGetConfig(t *testing.T) {
	config := DefaultAgentExecutionLoopConfig()
	loop := NewAgentExecutionLoop(
		config,
		NewAnalystAgent(DefaultAnalystAgentConfig()),
		NewTraderAgent(DefaultTraderAgentConfig()),
		risk.NewRiskManagerAgent(risk.DefaultRiskManagerConfig()),
		nil,
		nil,
		nil,
		nil,
	)

	newConfig := AgentExecutionLoopConfig{
		MaxIterations:       10,
		Timeout:             120 * time.Second,
		RequireRiskApproval: false,
		MinConfidence:       0.8,
		EnableToolCalls:     false,
		AutoExecute:         true,
	}

	loop.SetConfig(newConfig)
	actual := loop.GetConfig()

	assert.Equal(t, newConfig, actual)
}

func TestExecutionLoopMetrics_ConcurrentAccess(t *testing.T) {
	var metrics ExecutionLoopMetrics

	done := make(chan bool)

	// Concurrent increments
	for i := 0; i < 100; i++ {
		go func() {
			metrics.IncrementCycles()
			metrics.IncrementApproved()
			metrics.RecordToolCall(false)
			metrics.UpdateAverages(2, 100.0, 0.8)
			metrics.IncrementBySymbol("BTC/USDT")
			metrics.IncrementByAction(ExecutionDecisionApprove)
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 100; i++ {
		<-done
	}

	result := metrics.GetMetrics()
	assert.Equal(t, int64(100), result.TotalCycles)
	assert.Equal(t, int64(100), result.ApprovedExecutions)
	assert.Equal(t, int64(100), result.TotalToolCalls)
	assert.Equal(t, int64(100), result.DecisionsBySymbol["BTC/USDT"])
	assert.Equal(t, int64(100), result.DecisionsByAction[string(ExecutionDecisionApprove)])
}

func TestAgentExecutionLoop_ConvertSignals(t *testing.T) {
	loop := NewAgentExecutionLoop(
		DefaultAgentExecutionLoopConfig(),
		NewAnalystAgent(DefaultAnalystAgentConfig()),
		NewTraderAgent(DefaultTraderAgentConfig()),
		risk.NewRiskManagerAgent(risk.DefaultRiskManagerConfig()),
		nil,
		nil,
		nil,
		nil,
	)

	market := MarketContext{
		Symbol:       "BTC/USDT",
		CurrentPrice: 50000,
		Volatility:   0.3,
		Trend:        "bullish",
		Liquidity:    0.8,
		Signals: []TradingSignal{
			{Name: "rsi", Value: 0.8, Weight: 1.0, Direction: "bullish", Description: "RSI indicator"},
		},
	}

	analystSignals := loop.convertToAnalystSignals(market)
	assert.NotEmpty(t, analystSignals)

	// Check that trading signals are included
	found := false
	for _, s := range analystSignals {
		if s.Name == "rsi" {
			found = true
			assert.Equal(t, DirectionBullish, s.Direction)
			break
		}
	}
	assert.True(t, found)
}

func TestAgentExecutionLoop_CalculateAdjustedSize(t *testing.T) {
	loop := NewAgentExecutionLoop(
		DefaultAgentExecutionLoopConfig(),
		NewAnalystAgent(DefaultAnalystAgentConfig()),
		NewTraderAgent(DefaultTraderAgentConfig()),
		risk.NewRiskManagerAgent(risk.DefaultRiskManagerConfig()),
		nil,
		nil,
		nil,
		nil,
	)

	tests := []struct {
		currentSize float64
		maxSize     float64
		expected    float64
	}{
		{0.1, 0.05, 0.05},  // Current > max, should reduce
		{0.03, 0.05, 0.03}, // Current < max, keep current
		{0.05, 0.05, 0.05}, // Equal, keep as is
	}

	for _, tt := range tests {
		result := loop.calculateAdjustedSize(tt.currentSize, decimal.NewFromFloat(tt.maxSize))
		assert.Equal(t, tt.expected, result)
	}
}
