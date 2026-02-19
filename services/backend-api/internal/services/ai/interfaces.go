package ai

import (
	"context"
	"encoding/json"
	"time"

	"github.com/irfndi/neuratrade/internal/ai/llm"
)

// ToolRegistry manages available tools for AI agents
type ToolRegistry interface {
	// GetToolsForStrategy returns tools available for a specific strategy
	GetToolsForStrategy(strategy string) []llm.ToolDefinition

	// GetTool retrieves a tool by name
	GetTool(name string) (Tool, bool)

	// Register registers a new tool
	Register(tool Tool) error
}

// Tool represents a callable tool
type Tool interface {
	// Name returns the tool name
	Name() string

	// Description returns tool description
	Description() string

	// Execute runs the tool with given parameters
	Execute(ctx context.Context, params json.RawMessage) (json.RawMessage, error)
}

// LearningSystem manages AI learning and memory
type LearningSystem interface {
	// RecordDecision stores a trading decision
	RecordDecision(ctx context.Context, record *DecisionRecord) error

	// GetSimilarDecisions retrieves similar past decisions
	GetSimilarDecisions(ctx context.Context, symbol string, limit int) ([]*DecisionRecord, error)

	// RecordOutcome stores the outcome of a trade
	RecordOutcome(ctx context.Context, decisionID string, outcome *TradeOutcome) error
}

// DecisionRecord stores a trading decision for learning
type DecisionRecord struct {
	ID          string          `json:"id"`
	Timestamp   time.Time       `json:"timestamp"`
	Strategy    string          `json:"strategy"`
	MarketState MarketState     `json:"market_state"`
	Decision    TradingDecision `json:"decision"`
	Reasoning   string          `json:"reasoning"`
	Confidence  float64         `json:"confidence"`
	ModelUsed   string          `json:"model_used"`
	TokensUsed  int             `json:"tokens_used"`
	Outcome     string          `json:"outcome,omitempty"` // "win", "loss", "pending"
	PnL         float64         `json:"pnl,omitempty"`
	ExecutedAt  *time.Time      `json:"executed_at,omitempty"`
	CompletedAt *time.Time      `json:"completed_at,omitempty"`
}

// TradeOutcome represents the result of a trade
type TradeOutcome struct {
	DecisionID       string        `json:"decision_id"`
	Result           string        `json:"result"` // "win", "loss", "breakeven"
	PnL              float64       `json:"pnl"`
	PnLPercent       float64       `json:"pnl_percent"`
	ExecutedPrice    float64       `json:"executed_price"`
	ClosedPrice      float64       `json:"closed_price,omitempty"`
	ExecutedAt       time.Time     `json:"executed_at"`
	ClosedAt         time.Time     `json:"closed_at,omitempty"`
	Duration         time.Duration `json:"duration"`
	MarketConditions string        `json:"market_conditions"`
}
