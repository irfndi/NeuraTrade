package ai

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/ai/llm"
)

// MockLLMClient implements llm.Client for testing
type MockLLMClient struct {
	response *llm.CompletionResponse
	err      error
}

func (m *MockLLMClient) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.response, nil
}
func (m *MockLLMClient) Stream(ctx context.Context, req *llm.CompletionRequest) (<-chan llm.StreamEvent, error) {
	ch := make(chan llm.StreamEvent)
	close(ch)
	return ch, nil
}

func (m *MockLLMClient) Provider() llm.Provider {
	return llm.ProviderOpenAI
}

func (m *MockLLMClient) Close() error {
	return nil
}

// MockToolRegistry implements ToolRegistry for testing
type MockToolRegistry struct {
	tools map[string]Tool
}

func NewMockToolRegistry() *MockToolRegistry {
	return &MockToolRegistry{
		tools: make(map[string]Tool),
	}
}

func (m *MockToolRegistry) GetToolsForStrategy(strategy string) []llm.ToolDefinition {
	return []llm.ToolDefinition{}
}

func (m *MockToolRegistry) GetTool(name string) (Tool, bool) {
	t, ok := m.tools[name]
	return t, ok
}

func (m *MockToolRegistry) Register(tool Tool) error {
	m.tools[tool.Name()] = tool
	return nil
}

// MockTool implements Tool for testing
type MockTool struct {
	name        string
	description string
}

func (m *MockTool) Name() string        { return m.name }
func (m *MockTool) Description() string { return m.description }
func (m *MockTool) Execute(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage(`{"result": "mock"}`), nil
}

// MockLearningSystem implements LearningSystem for testing
type MockLearningSystem struct {
	decisions map[string]*DecisionRecord
}

func NewMockLearningSystem() *MockLearningSystem {
	return &MockLearningSystem{
		decisions: make(map[string]*DecisionRecord),
	}
}

func (m *MockLearningSystem) RecordDecision(ctx context.Context, record *DecisionRecord) error {
	m.decisions[record.ID] = record
	return nil
}

func (m *MockLearningSystem) GetSimilarDecisions(ctx context.Context, symbol string, limit int) ([]*DecisionRecord, error) {
	var matches []*DecisionRecord
	for _, d := range m.decisions {
		if d.MarketState.Symbol == symbol && d.Outcome != "" {
			matches = append(matches, d)
		}
	}
	if len(matches) > limit {
		matches = matches[:limit]
	}
	return matches, nil
}

func (m *MockLearningSystem) RecordOutcome(ctx context.Context, decisionID string, outcome *TradeOutcome) error {
	if d, ok := m.decisions[decisionID]; ok {
		d.Outcome = outcome.Result
		d.PnL = outcome.PnL
	}
	return nil
}

func TestMain(m *testing.M) {
	// Disable logging during tests
	log.SetOutput(os.NewFile(0, os.DevNull))
	os.Exit(m.Run())
}

func TestDefaultAIBrainConfig(t *testing.T) {
	config := DefaultAIBrainConfig()

	if config.Model != "gpt-4o" {
		t.Errorf("Expected model 'gpt-4o', got '%s'", config.Model)
	}
	if config.Temperature != 0.2 {
		t.Errorf("Expected temperature 0.2, got %f", config.Temperature)
	}
	if config.MaxTokens != 2000 {
		t.Errorf("Expected max tokens 2000, got %d", config.MaxTokens)
	}
	if config.MinConfidence != 0.7 {
		t.Errorf("Expected min confidence 0.7, got %f", config.MinConfidence)
	}
	if config.Timeout != 30*time.Second {
		t.Errorf("Expected timeout 30s, got %v", config.Timeout)
	}
	if config.MaxDailyTrades != 50 {
		t.Errorf("Expected max daily trades 50, got %d", config.MaxDailyTrades)
	}
	if !config.EnableLearning {
		t.Error("Expected learning to be enabled by default")
	}
}

func TestNewAITradingBrain(t *testing.T) {
	mockLLM := &MockLLMClient{}
	mockRegistry := NewMockToolRegistry()
	mockLearning := NewMockLearningSystem()
	config := DefaultAIBrainConfig()

	brain := NewAITradingBrain(mockLLM, mockRegistry, mockLearning, config)

	if brain == nil {
		t.Fatal("Expected non-nil brain")
	}
	if brain.llmClient == nil {
		t.Error("Expected LLM client to be set")
	}
	if brain.toolRegistry == nil {
		t.Error("Expected tool registry to be set")
	}
	if brain.learningSystem == nil {
		t.Error("Expected learning system to be set")
	}
}

func TestAITradingBrain_Reason_Success(t *testing.T) {
	mockLLM := &MockLLMClient{
		response: &llm.CompletionResponse{
			Message: llm.Message{
				Role:    "assistant",
				Content: `{"action": "buy", "symbol": "BTC/USDT", "side": "buy", "size_percent": 1.5, "confidence": 0.85, "reasoning": "Strong uptrend"}`,
			},
			Usage: llm.UsageMetrics{TotalTokens: 100},
		},
	}
	mockRegistry := NewMockToolRegistry()
	mockLearning := NewMockLearningSystem()
	config := DefaultAIBrainConfig()

	brain := NewAITradingBrain(mockLLM, mockRegistry, mockLearning, config)

	// Create request
	req := &ReasoningRequest{
		RequestID: "test-1",
		Timestamp: time.Now(),
		Strategy:  "scalping",
		MarketState: MarketState{
			Symbol:    "BTC/USDT",
			Exchange:  "binance",
			Price:     45000.00,
			Volume24h: 1000000,
			Timestamp: time.Now(),
		},
		PortfolioState: PortfolioState{
			Balance:        10000.00,
			AvailableFunds: 8000.00,
		},
	}

	// Execute
	resp, err := brain.Reason(context.Background(), req)

	// Assert
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if resp == nil {
		t.Fatal("Expected non-nil response")
	}
	if resp.Decision.Action != ActionBuy {
		t.Errorf("Expected action 'buy', got '%s'", resp.Decision.Action)
	}
	if resp.Decision.Symbol != "BTC/USDT" {
		t.Errorf("Expected symbol 'BTC/USDT', got '%s'", resp.Decision.Symbol)
	}
	if resp.Confidence < 0 || resp.Confidence > 1 {
		t.Errorf("Expected confidence between 0 and 1, got %f", resp.Confidence)
	}
}

func TestAITradingBrain_Reason_ConfidenceThreshold(t *testing.T) {
	mockLLM := &MockLLMClient{
		response: &llm.CompletionResponse{
			Message: llm.Message{
				Role:    "assistant",
				Content: `{"action": "buy", "symbol": "BTC/USDT", "confidence": 0.5, "reasoning": "Low confidence signal"}`,
			},
			Usage: llm.UsageMetrics{TotalTokens: 50},
		},
	}
	mockRegistry := NewMockToolRegistry()
	mockLearning := NewMockLearningSystem()
	config := DefaultAIBrainConfig()
	config.MinConfidence = 0.7 // 70% threshold

	brain := NewAITradingBrain(mockLLM, mockRegistry, mockLearning, config)

	req := &ReasoningRequest{
		RequestID: "test-2",
		Timestamp: time.Now(),
		Strategy:  "scalping",
		MarketState: MarketState{
			Symbol:    "BTC/USDT",
			Price:     45000.00,
			Timestamp: time.Now(),
		},
		PortfolioState: PortfolioState{
			Balance: 10000.00,
		},
	}

	resp, err := brain.Reason(context.Background(), req)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	// Decision should be downgraded to HOLD
	if resp.Decision.Action != ActionHold {
		t.Errorf("Expected action 'hold' due to low confidence, got '%s'", resp.Decision.Action)
	}
}

func TestAITradingBrain_Reason_LLMError(t *testing.T) {
	mockLLM := &MockLLMClient{
		err: context.DeadlineExceeded,
	}
	mockRegistry := NewMockToolRegistry()
	mockLearning := NewMockLearningSystem()
	config := DefaultAIBrainConfig()

	brain := NewAITradingBrain(mockLLM, mockRegistry, mockLearning, config)

	req := &ReasoningRequest{
		RequestID: "test-3",
		Timestamp: time.Now(),
		Strategy:  "scalping",
		MarketState: MarketState{
			Symbol:    "BTC/USDT",
			Timestamp: time.Now(),
		},
	}

	_, err := brain.Reason(context.Background(), req)

	if err == nil {
		t.Error("Expected error when LLM fails")
	}
}

func TestAITradingBrain_parseDecision_ValidJSON(t *testing.T) {
	brain := &AITradingBrain{logger: log.Default()}

	content := `{"action": "sell", "symbol": "ETH/USDT", "confidence": 0.75, "reasoning": "Resistance level hit"}`
	req := &ReasoningRequest{
		MarketState: MarketState{Symbol: "ETH/USDT"},
	}

	decision, err := brain.parseDecision(content, req)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if decision.Action != ActionSell {
		t.Errorf("Expected action 'sell', got '%s'", decision.Action)
	}
	if decision.Symbol != "ETH/USDT" {
		t.Errorf("Expected symbol 'ETH/USDT', got '%s'", decision.Symbol)
	}
	if decision.Confidence != 0.75 {
		t.Errorf("Expected confidence 0.75, got %f", decision.Confidence)
	}
}

func TestAITradingBrain_parseDecision_InvalidJSON(t *testing.T) {
	brain := &AITradingBrain{logger: log.Default()}

	content := `This is not valid JSON at all`
	req := &ReasoningRequest{
		MarketState: MarketState{Symbol: "BTC/USDT"},
	}

	decision, err := brain.parseDecision(content, req)

	// Should fallback to hold, not error
	if err != nil {
		t.Fatalf("Expected no error on invalid JSON, got: %v", err)
	}
	if decision.Action != ActionHold {
		t.Errorf("Expected fallback to 'hold', got '%s'", decision.Action)
	}
	if decision.Symbol != "BTC/USDT" {
		t.Errorf("Expected symbol from request, got '%s'", decision.Symbol)
	}
}

func TestAITradingBrain_parseDecision_JSONInText(t *testing.T) {
	brain := &AITradingBrain{logger: log.Default()}

	content := `Here is my analysis: {"action": "buy", "confidence": 0.9} and more text after.`
	req := &ReasoningRequest{
		MarketState: MarketState{Symbol: "SOL/USDT"},
	}

	decision, err := brain.parseDecision(content, req)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if decision.Action != ActionBuy {
		t.Errorf("Expected action 'buy', got '%s'", decision.Action)
	}
}

func TestAITradingBrain_buildSystemPrompt(t *testing.T) {
	brain := &AITradingBrain{logger: log.Default()}

	tests := []struct {
		strategy    string
		expectInMsg string
	}{
		{"scalping", "high-frequency scalping"},
		{"arbitrage", "cross-exchange arbitrage"},
		{"unknown", "unknown"},
	}

	for _, tt := range tests {
		prompt := brain.buildSystemPrompt(tt.strategy)
		if prompt == "" {
			t.Errorf("Expected non-empty prompt for strategy '%s'", tt.strategy)
		}
		// Verify key elements are present
		if !contains(prompt, "trading agent") {
			t.Errorf("Expected prompt to contain 'trading agent' for strategy '%s'", tt.strategy)
		}
	}
}

func TestAITradingBrain_buildUserPrompt(t *testing.T) {
	brain := &AITradingBrain{logger: log.Default()}

	req := &ReasoningRequest{
		Strategy: "scalping",
		MarketState: MarketState{
			Symbol:    "BTC/USDT",
			Price:     45000.00,
			Volume24h: 1000000,
		},
		PortfolioState: PortfolioState{
			Balance: 10000.00,
		},
		Context: "Test context",
	}

	prompt := brain.buildUserPrompt(req, "")

	if prompt == "" {
		t.Error("Expected non-empty prompt")
	}
	if !contains(prompt, "BTC/USDT") {
		t.Error("Expected prompt to contain symbol")
	}
	if !contains(prompt, "scalping") {
		t.Error("Expected prompt to contain strategy")
	}
	if !contains(prompt, "Test context") {
		t.Error("Expected prompt to contain context")
	}
}

func TestAITradingBrain_SetLogger(t *testing.T) {
	brain := &AITradingBrain{logger: log.Default()}
	newLogger := log.New(os.Stderr, "test: ", log.LstdFlags)

	brain.SetLogger(newLogger)

	// No assertion needed - just verify it doesn't panic
}

func TestGenerateDecisionID(t *testing.T) {
	id1 := generateDecisionID()
	id2 := generateDecisionID()

	if id1 == "" {
		t.Error("Expected non-empty ID")
	}
	if id1 == id2 {
		t.Error("Expected unique IDs")
	}
	if !contains(id1, "dec_") {
		t.Errorf("Expected ID to start with 'dec_', got '%s'", id1)
	}
}

func TestTradingAction_Constants(t *testing.T) {
	if ActionBuy != "buy" {
		t.Errorf("Expected ActionBuy = 'buy', got '%s'", ActionBuy)
	}
	if ActionSell != "sell" {
		t.Errorf("Expected ActionSell = 'sell', got '%s'", ActionSell)
	}
	if ActionHold != "hold" {
		t.Errorf("Expected ActionHold = 'hold', got '%s'", ActionHold)
	}
	if ActionClose != "close" {
		t.Errorf("Expected ActionClose = 'close', got '%s'", ActionClose)
	}
	if ActionScalp != "scalp" {
		t.Errorf("Expected ActionScalp = 'scalp', got '%s'", ActionScalp)
	}
	if ActionArbitrage != "arbitrage" {
		t.Errorf("Expected ActionArbitrage = 'arbitrage', got '%s'", ActionArbitrage)
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
