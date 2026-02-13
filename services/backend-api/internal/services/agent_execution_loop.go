package services

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/irfndi/neuratrade/internal/ai/llm"
	"github.com/irfndi/neuratrade/internal/services/risk"
	"github.com/irfndi/neuratrade/internal/skill"
	"github.com/shopspring/decimal"
)

// ExecutionDecision represents the final decision from the execution loop
type ExecutionDecision string

const (
	ExecutionDecisionApprove   ExecutionDecision = "approve"
	ExecutionDecisionReject    ExecutionDecision = "reject"
	ExecutionDecisionModify    ExecutionDecision = "modify"
	ExecutionDecisionDefer     ExecutionDecision = "defer"
	ExecutionDecisionEmergency ExecutionDecision = "emergency"
)

// ToolExecutor defines the interface for executing tool calls
type ToolExecutor interface {
	// Execute runs a tool with the given name and arguments
	Execute(ctx context.Context, name string, arguments json.RawMessage) (json.RawMessage, error)
}

// OrderExecutor defines the interface for executing trading orders
type OrderExecutor interface {
	ExecuteOrder(ctx context.Context, decision *TradingDecision) error
}

// AgentExecutionLoopConfig holds configuration for the execution loop
type AgentExecutionLoopConfig struct {
	// MaxIterations is the maximum number of LLM iterations per cycle
	MaxIterations int `json:"max_iterations"`
	// Timeout is the maximum time for a single execution cycle
	Timeout time.Duration `json:"timeout"`
	// RequireRiskApproval requires risk manager approval before execution
	RequireRiskApproval bool `json:"require_risk_approval"`
	// MinConfidence is the minimum confidence threshold for execution
	MinConfidence float64 `json:"min_confidence"`
	// EnableToolCalls enables LLM tool calling
	EnableToolCalls bool `json:"enable_tool_calls"`
	// AutoExecute enables automatic order execution after approval
	AutoExecute bool `json:"auto_execute"`
}

// DefaultAgentExecutionLoopConfig returns sensible defaults
func DefaultAgentExecutionLoopConfig() AgentExecutionLoopConfig {
	return AgentExecutionLoopConfig{
		MaxIterations:       5,
		Timeout:             60 * time.Second,
		RequireRiskApproval: true,
		MinConfidence:       0.7,
		EnableToolCalls:     true,
		AutoExecute:         false,
	}
}

// ExecutionLoopResult represents the result of an execution cycle
type ExecutionLoopResult struct {
	LoopID          string               `json:"loop_id"`
	Symbol          string               `json:"symbol"`
	Decision        ExecutionDecision    `json:"decision"`
	TradingDecision *TradingDecision     `json:"trading_decision,omitempty"`
	RiskAssessment  *risk.RiskAssessment `json:"risk_assessment,omitempty"`
	Analysis        *AnalystAnalysis     `json:"analysis,omitempty"`
	ToolCallsMade   []llm.ToolCall       `json:"tool_calls_made,omitempty"`
	ToolResults     []ToolCallResult     `json:"tool_results,omitempty"`
	Confidence      float64              `json:"confidence"`
	Reasoning       string               `json:"reasoning"`
	Iterations      int                  `json:"iterations"`
	ExecutionTime   time.Duration        `json:"execution_time"`
	Errors          []string             `json:"errors,omitempty"`
	Metadata        map[string]string    `json:"metadata,omitempty"`
}

// ToolCallResult represents the result of a tool call
type ToolCallResult struct {
	ToolID    string          `json:"tool_id"`
	ToolName  string          `json:"tool_name"`
	Arguments json.RawMessage `json:"arguments"`
	Result    json.RawMessage `json:"result"`
	Error     string          `json:"error,omitempty"`
	Duration  time.Duration   `json:"duration"`
}

// ExecutionLoopMetrics tracks execution loop performance
type ExecutionLoopMetrics struct {
	mu                   sync.RWMutex
	TotalCycles          int64            `json:"total_cycles"`
	ApprovedExecutions   int64            `json:"approved_executions"`
	RejectedExecutions   int64            `json:"rejected_executions"`
	DeferredExecutions   int64            `json:"deferred_executions"`
	EmergencyTriggers    int64            `json:"emergency_triggers"`
	TotalToolCalls       int64            `json:"total_tool_calls"`
	FailedToolCalls      int64            `json:"failed_tool_calls"`
	AverageIterations    float64          `json:"average_iterations"`
	AverageExecutionTime float64          `json:"average_execution_time_ms"`
	AverageConfidence    float64          `json:"average_confidence"`
	DecisionsBySymbol    map[string]int64 `json:"decisions_by_symbol"`
	DecisionsByAction    map[string]int64 `json:"decisions_by_action"`
}

func (m *ExecutionLoopMetrics) IncrementCycles() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.TotalCycles++
}

func (m *ExecutionLoopMetrics) IncrementApproved() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ApprovedExecutions++
}

func (m *ExecutionLoopMetrics) IncrementRejected() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.RejectedExecutions++
}

func (m *ExecutionLoopMetrics) IncrementDeferred() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.DeferredExecutions++
}

func (m *ExecutionLoopMetrics) IncrementEmergency() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.EmergencyTriggers++
}

func (m *ExecutionLoopMetrics) RecordToolCall(failed bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.TotalToolCalls++
	if failed {
		m.FailedToolCalls++
	}
}

func (m *ExecutionLoopMetrics) UpdateAverages(iterations int, execTimeMs float64, confidence float64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.TotalCycles == 1 {
		m.AverageIterations = float64(iterations)
		m.AverageExecutionTime = execTimeMs
		m.AverageConfidence = confidence
	} else {
		n := float64(m.TotalCycles)
		m.AverageIterations = (m.AverageIterations*(n-1) + float64(iterations)) / n
		m.AverageExecutionTime = (m.AverageExecutionTime*(n-1) + execTimeMs) / n
		m.AverageConfidence = (m.AverageConfidence*(n-1) + confidence) / n
	}
}

func (m *ExecutionLoopMetrics) IncrementBySymbol(symbol string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.DecisionsBySymbol == nil {
		m.DecisionsBySymbol = make(map[string]int64)
	}
	m.DecisionsBySymbol[symbol]++
}

func (m *ExecutionLoopMetrics) IncrementByAction(action ExecutionDecision) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.DecisionsByAction == nil {
		m.DecisionsByAction = make(map[string]int64)
	}
	m.DecisionsByAction[string(action)]++
}

func (m *ExecutionLoopMetrics) GetMetrics() ExecutionLoopMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	symbolCopy := make(map[string]int64, len(m.DecisionsBySymbol))
	for k, v := range m.DecisionsBySymbol {
		symbolCopy[k] = v
	}

	actionCopy := make(map[string]int64, len(m.DecisionsByAction))
	for k, v := range m.DecisionsByAction {
		actionCopy[k] = v
	}

	return ExecutionLoopMetrics{
		TotalCycles:          m.TotalCycles,
		ApprovedExecutions:   m.ApprovedExecutions,
		RejectedExecutions:   m.RejectedExecutions,
		DeferredExecutions:   m.DeferredExecutions,
		EmergencyTriggers:    m.EmergencyTriggers,
		TotalToolCalls:       m.TotalToolCalls,
		FailedToolCalls:      m.FailedToolCalls,
		AverageIterations:    m.AverageIterations,
		AverageExecutionTime: m.AverageExecutionTime,
		AverageConfidence:    m.AverageConfidence,
		DecisionsBySymbol:    symbolCopy,
		DecisionsByAction:    actionCopy,
	}
}

// AgentExecutionLoop wires together the agent execution pipeline:
// AnalystAgent → LLM → Tool Calls → RiskManagerAgent → Execution
type AgentExecutionLoop struct {
	config        AgentExecutionLoopConfig
	analystAgent  *AnalystAgent
	traderAgent   *TraderAgent
	riskManager   *risk.RiskManagerAgent
	llmClient     llm.Client
	skillRegistry *skill.Registry
	toolExecutor  ToolExecutor
	orderExecutor OrderExecutor
	metrics       ExecutionLoopMetrics
	mu            sync.RWMutex
}

// NewAgentExecutionLoop creates a new agent execution loop
func NewAgentExecutionLoop(
	config AgentExecutionLoopConfig,
	analystAgent *AnalystAgent,
	traderAgent *TraderAgent,
	riskManager *risk.RiskManagerAgent,
	llmClient llm.Client,
	skillRegistry *skill.Registry,
	toolExecutor ToolExecutor,
	orderExecutor OrderExecutor,
) *AgentExecutionLoop {
	return &AgentExecutionLoop{
		config:        config,
		analystAgent:  analystAgent,
		traderAgent:   traderAgent,
		riskManager:   riskManager,
		llmClient:     llmClient,
		skillRegistry: skillRegistry,
		toolExecutor:  toolExecutor,
		orderExecutor: orderExecutor,
		metrics:       ExecutionLoopMetrics{},
	}
}

// Execute runs the full agent execution loop for a given symbol and context
func (l *AgentExecutionLoop) Execute(ctx context.Context, symbol string, marketContext MarketContext, portfolio PortfolioState) (*ExecutionLoopResult, error) {
	startTime := time.Now()

	result := &ExecutionLoopResult{
		LoopID:        generateLoopID(),
		Symbol:        symbol,
		Decision:      ExecutionDecisionDefer,
		ToolCallsMade: []llm.ToolCall{},
		ToolResults:   []ToolCallResult{},
		Errors:        []string{},
		Metadata:      make(map[string]string),
	}

	defer func() {
		result.ExecutionTime = time.Since(startTime)
		l.metrics.IncrementCycles()
		l.metrics.UpdateAverages(result.Iterations, float64(result.ExecutionTime.Milliseconds()), result.Confidence)
		l.metrics.IncrementBySymbol(symbol)
	}()

	// Set timeout context
	if l.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, l.config.Timeout)
		defer cancel()
	}

	// Step 1: Run AnalystAgent analysis
	analysis, err := l.runAnalysis(ctx, symbol, marketContext)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("analysis failed: %v", err))
		result.Decision = ExecutionDecisionReject
		l.metrics.IncrementRejected()
		return result, fmt.Errorf("analysis phase failed: %w", err)
	}
	result.Analysis = analysis

	// Step 2: If LLM is configured, run LLM with tool calling
	if l.llmClient != nil && l.config.EnableToolCalls {
		toolCalls, toolResults, err := l.runLLMWithTools(ctx, symbol, analysis, marketContext)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("llm tool calling failed: %v", err))
			// Continue without tool results - not a fatal error
		} else {
			result.ToolCallsMade = toolCalls
			result.ToolResults = toolResults
			result.Iterations = len(toolCalls)
		}
	}

	// Step 3: Generate trading decision using TraderAgent
	tradingDecision, err := l.traderAgent.MakeDecision(ctx, marketContext, portfolio)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("trading decision failed: %v", err))
		result.Decision = ExecutionDecisionReject
		l.metrics.IncrementRejected()
		return result, fmt.Errorf("trading decision phase failed: %w", err)
	}
	result.TradingDecision = tradingDecision
	result.Confidence = tradingDecision.Confidence

	// Check if we should even proceed (hold/wait decisions)
	if tradingDecision.Action == ActionHold || tradingDecision.Action == ActionWait {
		result.Decision = ExecutionDecisionDefer
		result.Reasoning = tradingDecision.Reasoning
		l.metrics.IncrementDeferred()
		return result, nil
	}

	// Step 4: Run risk assessment
	if l.config.RequireRiskApproval && l.riskManager != nil {
		riskSignals := l.convertToRiskSignals(analysis, marketContext, portfolio)
		riskAssessment, err := l.riskManager.AssessTradingRisk(ctx, symbol, string(tradingDecision.Side), riskSignals)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("risk assessment failed: %v", err))
			result.Decision = ExecutionDecisionReject
			l.metrics.IncrementRejected()
			return result, fmt.Errorf("risk assessment failed: %w", err)
		}
		result.RiskAssessment = riskAssessment

		// Check risk action
		switch riskAssessment.Action {
		case risk.RiskActionBlock, risk.RiskActionEmergency:
			result.Decision = ExecutionDecisionReject
			result.Reasoning = fmt.Sprintf("blocked by risk manager: %v", riskAssessment.Reasons)
			l.metrics.IncrementRejected()
			if riskAssessment.Action == risk.RiskActionEmergency {
				l.metrics.IncrementEmergency()
				result.Decision = ExecutionDecisionEmergency
			}
			return result, nil
		case risk.RiskActionReduce:
			result.Decision = ExecutionDecisionModify
			// Adjust position size based on risk recommendation
			if !riskAssessment.MaxPositionSize.IsZero() {
				tradingDecision.SizePercent = l.calculateAdjustedSize(tradingDecision.SizePercent, riskAssessment.MaxPositionSize)
			}
			result.Reasoning = fmt.Sprintf("reduced by risk manager: %v", riskAssessment.Recommendations)
		case risk.RiskActionWarning:
			// Continue with warning
			result.Reasoning = fmt.Sprintf("approved with warning: %v", riskAssessment.Recommendations)
		}
	}

	// Step 5: Final confidence check
	if result.Confidence < l.config.MinConfidence {
		result.Decision = ExecutionDecisionReject
		result.Reasoning = fmt.Sprintf("confidence %.2f below minimum %.2f", result.Confidence, l.config.MinConfidence)
		l.metrics.IncrementRejected()
		return result, nil
	}

	// Step 6: Approve execution
	result.Decision = ExecutionDecisionApprove
	result.Reasoning = tradingDecision.Reasoning
	l.metrics.IncrementApproved()
	l.metrics.IncrementByAction(result.Decision)

	// Step 7: Execute if auto-execute is enabled
	if l.config.AutoExecute && l.orderExecutor != nil {
		if err := l.orderExecutor.ExecuteOrder(ctx, tradingDecision); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("order execution failed: %v", err))
			// Don't change decision - it was approved, just execution failed
		}
	}

	return result, nil
}

// runAnalysis runs the analyst agent
func (l *AgentExecutionLoop) runAnalysis(ctx context.Context, symbol string, market MarketContext) (*AnalystAnalysis, error) {
	signals := l.convertToAnalystSignals(market)
	return l.analystAgent.Analyze(ctx, symbol, AnalystRoleTechnical, signals)
}

// runLLMWithTools runs the LLM with tool calling capability
func (l *AgentExecutionLoop) runLLMWithTools(ctx context.Context, symbol string, analysis *AnalystAnalysis, market MarketContext) ([]llm.ToolCall, []ToolCallResult, error) {
	var allToolCalls []llm.ToolCall
	var allResults []ToolCallResult

	// Build conversation
	systemPrompt := l.buildSystemPrompt()
	conversation := llm.NewConversationBuilder(systemPrompt).
		AddUser(fmt.Sprintf("Analyze trading opportunity for %s. Current price: %.2f. Analysis recommendation: %s with %.0f%% confidence.",
			symbol, market.CurrentPrice, analysis.Recommendation, analysis.Confidence*100))

	// Build tool definitions from skills
	var tools []llm.ToolDefinition
	if l.skillRegistry != nil {
		skills := l.getAllSkills()
		tools = llm.BuildToolDefinitions(skills)
	}

	// Iterate with LLM
	for i := 0; i < l.config.MaxIterations; i++ {
		req := &llm.CompletionRequest{
			Messages: conversation.Build(),
			Model:    "gpt-4", // TODO: Make configurable
			Tools:    tools,
		}

		resp, err := l.llmClient.Complete(ctx, req)
		if err != nil {
			return allToolCalls, allResults, fmt.Errorf("LLM completion failed: %w", err)
		}

		// Check if we have tool calls
		if len(resp.ToolCalls) == 0 {
			// No more tool calls, we're done
			break
		}

		// Execute each tool call
		for _, tc := range resp.ToolCalls {
			result := ToolCallResult{
				ToolID:    tc.ID,
				ToolName:  tc.Name,
				Arguments: tc.Arguments,
			}

			toolStart := time.Now()
			output, err := l.toolExecutor.Execute(ctx, tc.Name, tc.Arguments)
			result.Duration = time.Since(toolStart)

			if err != nil {
				result.Error = err.Error()
				l.metrics.RecordToolCall(true)
			} else {
				result.Result = output
				l.metrics.RecordToolCall(false)
			}

			allToolCalls = append(allToolCalls, tc)
			allResults = append(allResults, result)

			// Add tool result to conversation
			var resultContent string
			if err != nil {
				resultContent = fmt.Sprintf("Error: %s", err.Error())
			} else {
				resultContent = string(output)
			}
			conversation.AddToolResult(tc.ID, resultContent)
		}

		// Add assistant message with tool calls
		if resp.Message.Content != "" {
			conversation.AddAssistant(resp.Message.Content)
		}
	}

	return allToolCalls, allResults, nil
}

// buildSystemPrompt creates the system prompt for the LLM
func (l *AgentExecutionLoop) buildSystemPrompt() string {
	var skills []*skill.Skill
	if l.skillRegistry != nil {
		skills = l.getAllSkills()
	}
	return llm.BuildSystemPrompt(skills, "You are a trading agent analyzing market opportunities.")
}

// getAllSkills retrieves all skills from the registry
func (l *AgentExecutionLoop) getAllSkills() []*skill.Skill {
	if l.skillRegistry == nil {
		return nil
	}

	skillsMap := l.skillRegistry.GetAll()
	skills := make([]*skill.Skill, 0, len(skillsMap))
	for _, s := range skillsMap {
		skills = append(skills, s)
	}
	return skills
}

// convertToAnalystSignals converts market context to analyst signals
func (l *AgentExecutionLoop) convertToAnalystSignals(market MarketContext) []AnalystSignal {
	signals := []AnalystSignal{
		{Name: "price_momentum", Value: market.CurrentPrice, Weight: 0.2, Direction: l.determineDirection(market.Trend), Description: "Current price level"},
		{Name: "volatility", Value: market.Volatility, Weight: 0.15, Direction: DirectionNeutral, Description: "Market volatility"},
		{Name: "liquidity", Value: market.Liquidity, Weight: 0.15, Direction: DirectionNeutral, Description: "Market liquidity"},
	}

	// Add trading signals
	for _, ts := range market.Signals {
		var direction SignalDirection
		switch ts.Direction {
		case "bullish":
			direction = DirectionBullish
		case "bearish":
			direction = DirectionBearish
		default:
			direction = DirectionNeutral
		}
		signals = append(signals, AnalystSignal{
			Name:        ts.Name,
			Value:       ts.Value,
			Weight:      ts.Weight,
			Direction:   direction,
			Description: ts.Description,
		})
	}

	return signals
}

// convertToRiskSignals converts analysis and context to risk signals
func (l *AgentExecutionLoop) convertToRiskSignals(analysis *AnalystAnalysis, market MarketContext, portfolio PortfolioState) []risk.RiskSignal {
	signals := []risk.RiskSignal{
		{Name: "volatility", Value: market.Volatility, Weight: 0.25, Threshold: 0.5, Description: "Market volatility risk"},
		{Name: "liquidity", Value: 1 - market.Liquidity, Weight: 0.2, Threshold: 0.5, Description: "Low liquidity risk"},
		{Name: "drawdown", Value: portfolio.CurrentDrawdown, Weight: 0.3, Threshold: 0.1, Description: "Current drawdown risk"},
	}

	if analysis != nil {
		signals = append(signals, risk.RiskSignal{
			Name:        "confidence",
			Value:       1 - analysis.Confidence,
			Weight:      0.15,
			Threshold:   0.3,
			Description: "Low confidence risk",
		})
	}

	return signals
}

// determineDirection converts trend string to signal direction
func (l *AgentExecutionLoop) determineDirection(trend string) SignalDirection {
	switch trend {
	case "bullish", "up":
		return DirectionBullish
	case "bearish", "down":
		return DirectionBearish
	default:
		return DirectionNeutral
	}
}

// calculateAdjustedSize calculates position size based on risk limits
func (l *AgentExecutionLoop) calculateAdjustedSize(currentSize float64, maxSize decimal.Decimal) float64 {
	maxSizeFloat, _ := maxSize.Float64()
	if currentSize > maxSizeFloat {
		return maxSizeFloat
	}
	return currentSize
}

// GetMetrics returns the current execution loop metrics
func (l *AgentExecutionLoop) GetMetrics() ExecutionLoopMetrics {
	return l.metrics.GetMetrics()
}

// SetConfig updates the execution loop configuration
func (l *AgentExecutionLoop) SetConfig(config AgentExecutionLoopConfig) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.config = config
}

// GetConfig returns the current configuration
func (l *AgentExecutionLoop) GetConfig() AgentExecutionLoopConfig {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.config
}

// generateLoopID generates a unique loop ID
func generateLoopID() string {
	return fmt.Sprintf("loop_%d", time.Now().UnixNano())
}
