package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"time"

	"github.com/irfndi/neuratrade/internal/ai/llm"
)

// TradingAction represents the action AI decides to take
type TradingAction string

const (
	ActionBuy       TradingAction = "buy"
	ActionSell      TradingAction = "sell"
	ActionHold      TradingAction = "hold"
	ActionClose     TradingAction = "close"
	ActionScalp     TradingAction = "scalp"
	ActionArbitrage TradingAction = "arbitrage"
)

// MarketState represents current market conditions
type MarketState struct {
	Symbol     string                 `json:"symbol"`
	Exchange   string                 `json:"exchange"`
	Price      float64                `json:"price"`
	Bid        float64                `json:"bid"`
	Ask        float64                `json:"ask"`
	Spread     float64                `json:"spread"`
	Volume24h  float64                `json:"volume_24h"`
	Change24h  float64                `json:"change_24h"`
	Volatility float64                `json:"volatility"`
	OrderBook  OrderBookState         `json:"order_book"`
	Indicators map[string]interface{} `json:"indicators,omitempty"`
	Timestamp  time.Time              `json:"timestamp"`
}

// OrderBookState represents order book snapshot
type OrderBookState struct {
	Bids      []PriceLevel `json:"bids"`
	Asks      []PriceLevel `json:"asks"`
	BidDepth  float64      `json:"bid_depth"`
	AskDepth  float64      `json:"ask_depth"`
	Imbalance float64      `json:"imbalance"` // -1 to 1, negative = more asks
}

// PriceLevel represents a price level in order book
type PriceLevel struct {
	Price  float64 `json:"price"`
	Volume float64 `json:"volume"`
}

// PortfolioState represents current portfolio
type PortfolioState struct {
	Balance        float64         `json:"balance"`
	AvailableFunds float64         `json:"available_funds"`
	Positions      []PositionState `json:"positions"`
	OpenOrders     []OrderState    `json:"open_orders"`
	DailyPnL       float64         `json:"daily_pnl"`
	TotalPnL       float64         `json:"total_pnl"`
	RiskLevel      string          `json:"risk_level"`
}

// PositionState represents an open position
type PositionState struct {
	Symbol        string  `json:"symbol"`
	Side          string  `json:"side"`
	Size          float64 `json:"size"`
	EntryPrice    float64 `json:"entry_price"`
	UnrealizedPnL float64 `json:"unrealized_pnl"`
}

// OrderState represents an open order
type OrderState struct {
	ID     string  `json:"id"`
	Symbol string  `json:"symbol"`
	Side   string  `json:"side"`
	Type   string  `json:"type"`
	Size   float64 `json:"size"`
	Price  float64 `json:"price,omitempty"`
	Status string  `json:"status"`
}

// ReasoningRequest is the input to AI Brain
type ReasoningRequest struct {
	RequestID        string           `json:"request_id"`
	Timestamp        time.Time        `json:"timestamp"`
	Strategy         string           `json:"strategy"` // "scalping", "arbitrage", etc.
	MarketState      MarketState      `json:"market_state"`
	PortfolioState   PortfolioState   `json:"portfolio_state"`
	Context          string           `json:"context,omitempty"`
	PreviousDecision *TradingDecision `json:"previous_decision,omitempty"`
}

// ReasoningResponse is the AI Brain output
type ReasoningResponse struct {
	RequestID      string          `json:"request_id"`
	Decision       TradingDecision `json:"decision"`
	Reasoning      string          `json:"reasoning"`
	Confidence     float64         `json:"confidence"`
	ToolsUsed      []ToolCall      `json:"tools_used"`
	MarketAnalysis string          `json:"market_analysis"`
	RiskAssessment string          `json:"risk_assessment"`
	ExecutionTime  time.Duration   `json:"execution_time"`
	ModelUsed      string          `json:"model_used"`
	TokensUsed     int             `json:"tokens_used"`
}

// TradingDecision is the concrete trading decision
type TradingDecision struct {
	ID             string        `json:"id"`
	Action         TradingAction `json:"action"`
	Symbol         string        `json:"symbol"`
	Side           string        `json:"side,omitempty"`
	Size           float64       `json:"size,omitempty"`
	SizePercent    float64       `json:"size_percent,omitempty"` // % of available funds
	EntryPrice     float64       `json:"entry_price,omitempty"`
	StopLoss       float64       `json:"stop_loss,omitempty"`
	TakeProfit     float64       `json:"take_profit,omitempty"`
	OrderType      string        `json:"order_type,omitempty"`
	TimeInForce    string        `json:"time_in_force,omitempty"`
	MaxSlippage    float64       `json:"max_slippage,omitempty"`
	HoldDuration   time.Duration `json:"hold_duration,omitempty"`
	ExitConditions []string      `json:"exit_conditions,omitempty"`
	Confidence     float64       `json:"confidence"`
	Reasoning      string        `json:"reasoning,omitempty"`
	MarketAnalysis string        `json:"market_analysis,omitempty"`
	RiskAssessment string        `json:"risk_assessment,omitempty"`
}

// ToolCall represents a tool invocation by AI
type ToolCall struct {
	ToolName   string          `json:"tool_name"`
	Parameters json.RawMessage `json:"parameters"`
	Result     json.RawMessage `json:"result,omitempty"`
	Error      string          `json:"error,omitempty"`
}

// AITradingBrain is the central AI decision-making system
type AITradingBrain struct {
	llmClient      llm.Client
	toolRegistry   ToolRegistry
	learningSystem LearningSystem
	config         AIBrainConfig
	logger         *log.Logger
}

// AIBrainConfig holds configuration for AI Brain
type AIBrainConfig struct {
	Model          string        `json:"model"`
	Temperature    float64       `json:"temperature"`
	MaxTokens      int           `json:"max_tokens"`
	Timeout        time.Duration `json:"timeout"`
	MinConfidence  float64       `json:"min_confidence"` // Minimum confidence to execute
	MaxDailyTrades int           `json:"max_daily_trades"`
	EnableLearning bool          `json:"enable_learning"`
}

// DefaultAIBrainConfig returns default configuration
func DefaultAIBrainConfig() AIBrainConfig {
	return AIBrainConfig{
		Model:          "gpt-4o",
		Temperature:    0.2, // Low temperature for consistent decisions
		MaxTokens:      2000,
		Timeout:        30 * time.Second,
		MinConfidence:  0.7, // 70% minimum confidence
		MaxDailyTrades: 50,
		EnableLearning: true,
	}
}

// NewAITradingBrain creates a new AI trading brain
func NewAITradingBrain(
	llmClient llm.Client,
	toolRegistry ToolRegistry,
	learningSystem LearningSystem,
	config AIBrainConfig,
) *AITradingBrain {
	return &AITradingBrain{
		llmClient:      llmClient,
		toolRegistry:   toolRegistry,
		learningSystem: learningSystem,
		config:         config,
		logger:         log.Default(),
	}
}

// Reason processes market data and makes trading decision
func (brain *AITradingBrain) Reason(ctx context.Context, req *ReasoningRequest) (*ReasoningResponse, error) {
	startTime := time.Now()

	brain.logger.Printf("[AI Brain] Starting reasoning for %s on %s", req.Strategy, req.MarketState.Symbol)

	// Get relevant historical context
	var memoryContext string
	if brain.config.EnableLearning {
		similar, err := brain.learningSystem.GetSimilarDecisions(ctx, req.MarketState.Symbol, 5)
		if err == nil && len(similar) > 0 {
			memoryContext = brain.formatSimilarDecisions(similar)
		}
	}

	// Build system prompt based on strategy
	systemPrompt := brain.buildSystemPrompt(req.Strategy)

	// Build user prompt with context
	userPrompt := brain.buildUserPrompt(req, memoryContext)

	// Get available tools for this strategy
	tools := brain.toolRegistry.GetToolsForStrategy(req.Strategy)

	// Call LLM with tools
	llmReq := &llm.CompletionRequest{
		Model:       brain.config.Model,
		Temperature: &brain.config.Temperature,
		MaxTokens:   brain.config.MaxTokens,
		Messages: []llm.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Tools: tools,
	}

	// Set timeout
	ctx, cancel := context.WithTimeout(ctx, brain.config.Timeout)
	defer cancel()

	llmResp, err := brain.llmClient.Complete(ctx, llmReq)
	if err != nil {
		return nil, fmt.Errorf("LLM completion failed: %w", err)
	}

	// Parse decision from response
	decision, err := brain.parseDecision(llmResp.Message.Content, req)
	if err != nil {
		return nil, fmt.Errorf("failed to parse decision: %w", err)
	}

	// Check confidence threshold
	if decision.Confidence < brain.config.MinConfidence {
		brain.logger.Printf("[AI Brain] Decision confidence %.2f below threshold %.2f, holding",
			decision.Confidence, brain.config.MinConfidence)
		decision.Action = ActionHold
	}

	// Record decision for learning
	if brain.config.EnableLearning {
		go func() {
			brain.learningSystem.RecordDecision(context.Background(), &DecisionRecord{
				ID:          decision.ID,
				Timestamp:   time.Now(),
				Strategy:    req.Strategy,
				MarketState: req.MarketState,
				Decision:    *decision,
				Reasoning:   llmResp.Message.Content,
				Confidence:  decision.Confidence,
				ModelUsed:   brain.config.Model,
				TokensUsed:  llmResp.Usage.TotalTokens,
			})
		}()
	}

	executionTime := time.Since(startTime)

	brain.logger.Printf("[AI Brain] Decision made in %v: %s (confidence: %.2f)",
		executionTime, decision.Action, decision.Confidence)

	return &ReasoningResponse{
		RequestID:     req.RequestID,
		Decision:      *decision,
		Reasoning:     llmResp.Message.Content,
		Confidence:    decision.Confidence,
		ExecutionTime: executionTime,
		ModelUsed:     brain.config.Model,
		TokensUsed:    llmResp.Usage.TotalTokens,
	}, nil
}

// buildSystemPrompt creates the system prompt based on strategy
func (brain *AITradingBrain) buildSystemPrompt(strategy string) string {
	basePrompt := `You are an expert AI trading agent specialized in %s.

Your role is to analyze market conditions and make precise trading decisions.

DECISION FRAMEWORK:
1. Analyze market data thoroughly
2. Consider portfolio state and risk
3. Evaluate opportunity quality
4. Make decisive action: BUY, SELL, HOLD, or CLOSE
5. Provide clear reasoning

RISK MANAGEMENT RULES:
- Never risk more than 2% of portfolio per trade
- Always set stop-loss for non-scalp trades
- Consider market volatility in position sizing
- Respect daily loss limits

OUTPUT FORMAT:
You must respond with a JSON object:
{
  "action": "buy|sell|hold|close",
  "symbol": "BTC/USDT",
  "side": "buy",
  "size_percent": 1.5,
  "entry_price": 45000.00,
  "stop_loss": 44000.00,
  "take_profit": 47000.00,
  "order_type": "limit",
  "confidence": 0.85,
  "reasoning": "Detailed explanation of decision",
  "market_analysis": "Brief market assessment",
  "risk_assessment": "Risk evaluation"
}

REQUIREMENTS:
- Confidence must be 0.0 to 1.0
- Size percent must be 0.1 to 5.0
- Provide detailed reasoning
- Be decisive - avoid "maybe" decisions`

	switch strategy {
	case "scalping":
		return fmt.Sprintf(basePrompt, "high-frequency scalping") + `

SCALPING SPECIFICS:
- Target small, quick profits (0.1% to 0.5%)
- Hold positions for seconds to minutes
- Focus on spread and order book imbalance
- Use market orders for fast execution
- Tight stop-losses (0.1% to 0.2%)
- Avoid trading during low volatility

SCALPING SIGNALS TO LOOK FOR:
- Order book imbalance (>60% on one side)
- Tight spread (<0.1%)
- High recent volume
- Price bouncing off support/resistance
- Momentum in order flow`
	case "arbitrage":
		return fmt.Sprintf(basePrompt, "cross-exchange arbitrage") + `

ARBITRAGE SPECIFICS:
- Identify price discrepancies between exchanges
- Account for fees, slippage, and transfer times
- Only execute if profit > fees + buffer
- Consider execution speed and reliability
- Monitor for delayed settlements`
	default:
		return fmt.Sprintf(basePrompt, strategy)
	}
}

// buildUserPrompt creates the user prompt with context
func (brain *AITradingBrain) buildUserPrompt(req *ReasoningRequest, memoryContext string) string {
	marketJSON, _ := json.MarshalIndent(req.MarketState, "", "  ")
	portfolioJSON, _ := json.MarshalIndent(req.PortfolioState, "", "  ")

	prompt := fmt.Sprintf(`MARKET CONTEXT:
%s

PORTFOLIO STATE:
%s

STRATEGY: %s

`, string(marketJSON), string(portfolioJSON), req.Strategy)

	if req.Context != "" {
		prompt += fmt.Sprintf("ADDITIONAL CONTEXT:\n%s\n\n", req.Context)
	}

	if memoryContext != "" {
		prompt += fmt.Sprintf("SIMILAR PAST DECISIONS:\n%s\n\n", memoryContext)
	}

	prompt += `Analyze the market conditions and make a trading decision.

Return your decision as a JSON object following the format specified in your instructions.

Be precise, confident, and risk-aware.`

	return prompt
}

// parseDecision extracts trading decision from LLM response
func (brain *AITradingBrain) parseDecision(content string, req *ReasoningRequest) (*TradingDecision, error) {
	// Try to extract JSON from response
	var decision TradingDecision

	// Look for JSON block
	var jsonStr string
	if idx := strings.Index(content, "{"); idx != -1 {
		if endIdx := strings.LastIndex(content, "}"); endIdx != -1 {
			jsonStr = content[idx : endIdx+1]
		}
	}

	if jsonStr == "" {
		jsonStr = content
	}

	if err := json.Unmarshal([]byte(jsonStr), &decision); err != nil {
		// Fallback: try to parse reasoning and default to hold
		brain.logger.Printf("[AI Brain] Failed to parse JSON decision: %v", err)
		return &TradingDecision{
			ID:         generateDecisionID(),
			Action:     ActionHold,
			Symbol:     req.MarketState.Symbol,
			Confidence: 0.0,
		}, nil
	}

	// Set ID if not provided
	if decision.ID == "" {
		decision.ID = generateDecisionID()
	}

	// Ensure symbol is set
	if decision.Symbol == "" {
		decision.Symbol = req.MarketState.Symbol
	}

	return &decision, nil
}

// formatSimilarDecisions formats past decisions for context
func (brain *AITradingBrain) formatSimilarDecisions(decisions []*DecisionRecord) string {
	var sb strings.Builder
	for i, d := range decisions {
		if i >= 3 { // Limit to 3 examples
			break
		}
		sb.WriteString(fmt.Sprintf("- Decision: %s, Outcome: %s, PnL: %.2f\n",
			d.Decision.Action, d.Outcome, d.PnL))
	}
	return sb.String()
}

// generateDecisionID generates unique decision ID
func generateDecisionID() string {
	return fmt.Sprintf("dec_%d_%d", time.Now().Unix(), rand.Intn(10000))
}

// SetLogger sets the logger for AI Brain
func (brain *AITradingBrain) SetLogger(logger *log.Logger) {
	brain.logger = logger
}
