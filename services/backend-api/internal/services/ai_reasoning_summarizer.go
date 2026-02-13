package services

import (
	"context"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
)

// AIReasoningCategory categorizes the type of AI reasoning.
type AIReasoningCategory string

const (
	AICategorySignalGeneration  AIReasoningCategory = "signal_generation"
	AICategoryRiskAssessment    AIReasoningCategory = "risk_assessment"
	AICategoryTradeExecution    AIReasoningCategory = "trade_execution"
	AICategoryPortfolioDecision AIReasoningCategory = "portfolio_decision"
	AICategoryMarketAnalysis    AIReasoningCategory = "market_analysis"
)

// AIReasoningSummary represents an AI reasoning summary.
type AIReasoningSummary struct {
	ID            string              `json:"id" db:"id"`
	UserID        string              `json:"user_id" db:"user_id"`
	QuestID       *int64              `json:"quest_id" db:"quest_id"`
	TradeID       *int64              `json:"trade_id" db:"trade_id"`
	SessionID     string              `json:"session_id" db:"session_id"`
	Category      AIReasoningCategory `json:"category" db:"category"`
	Decision      string              `json:"decision" db:"decision"`
	Reasoning     string              `json:"reasoning" db:"reasoning"`
	Confidence    decimal.Decimal     `json:"confidence" db:"confidence"`
	Factors       []string            `json:"factors" db:"factors"`
	MarketContext string              `json:"market_context" db:"market_context"`
	RiskLevel     string              `json:"risk_level" db:"risk_level"`
	ModelUsed     string              `json:"model_used" db:"model_used"`
	TokensUsed    int                 `json:"tokens_used" db:"tokens_used"`
	LatencyMs     int                 `json:"latency_ms" db:"latency_ms"`
	CreatedAt     time.Time           `json:"created_at" db:"created_at"`
}

// AIReasoningFactors represents the factors considered in AI reasoning.
type AIReasoningFactors struct {
	TechnicalIndicators []string         `json:"technical_indicators"`
	SentimentScore      decimal.Decimal  `json:"sentiment_score"`
	MarketTrend         string           `json:"market_trend"`
	Volatility          decimal.Decimal  `json:"volatility"`
	VolumeAnalysis      string           `json:"volume_analysis"`
	RiskRewardRatio     decimal.Decimal  `json:"risk_reward_ratio"`
	PositionSize        decimal.Decimal  `json:"position_size"`
	StopLoss            *decimal.Decimal `json:"stop_loss,omitempty"`
	TakeProfit          *decimal.Decimal `json:"take_profit,omitempty"`
}

// AIReasoningService generates and stores AI reasoning summaries.
type AIReasoningService struct {
	db     DBPool
	Logger Logger
}

// NewAIReasoningService creates a new AI reasoning service.
func NewAIReasoningService(db DBPool, logger Logger) *AIReasoningService {
	return &AIReasoningService{
		db:     db,
		Logger: logger,
	}
}

// GenerateSignalSummary generates a reasoning summary for a signal generation decision.
func (s *AIReasoningService) GenerateSignalSummary(ctx context.Context, req *AIReasoningRequest) (*AIReasoningSummary, error) {
	if req.UserID == "" {
		return nil, fmt.Errorf("user_id is required")
	}
	if req.Decision == "" {
		return nil, fmt.Errorf("decision is required")
	}
	if req.Reasoning == "" {
		return nil, fmt.Errorf("reasoning is required")
	}

	// Default category to signal_generation if not specified
	category := req.Category
	if category == "" {
		category = AICategorySignalGeneration
	}

	// Default confidence to 0 if not specified
	confidence := req.Confidence
	if confidence.IsZero() {
		confidence = decimal.NewFromFloat(0.5)
	}

	summary := &AIReasoningSummary{
		ID:            fmt.Sprintf("reason-%d-%d", time.Now().UnixNano(), s.randInt63()),
		UserID:        req.UserID,
		QuestID:       req.QuestID,
		TradeID:       req.TradeID,
		SessionID:     req.SessionID,
		Category:      category,
		Decision:      req.Decision,
		Reasoning:     req.Reasoning,
		Confidence:    confidence,
		Factors:       req.Factors,
		MarketContext: req.MarketContext,
		RiskLevel:     req.RiskLevel,
		ModelUsed:     req.ModelUsed,
		TokensUsed:    req.TokensUsed,
		LatencyMs:     req.LatencyMs,
		CreatedAt:     time.Now(),
	}

	// Store in database
	return s.storeSummary(ctx, summary)
}

// storeSummary stores the reasoning summary in the database.
func (s *AIReasoningService) storeSummary(ctx context.Context, summary *AIReasoningSummary) (*AIReasoningSummary, error) {
	query := `
		INSERT INTO ai_reasoning_summaries 
			(user_id, quest_id, trade_id, session_id, category, decision, reasoning, confidence, 
			 factors, market_context, risk_level, model_used, tokens_used, latency_ms)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		RETURNING id, user_id, quest_id, trade_id, session_id, category, decision, reasoning, 
			confidence, factors, market_context, risk_level, model_used, tokens_used, latency_ms, created_at
	`

	var factors []string
	err := s.db.QueryRow(ctx, query,
		summary.UserID,
		summary.QuestID,
		summary.TradeID,
		summary.SessionID,
		summary.Category,
		summary.Decision,
		summary.Reasoning,
		summary.Confidence,
		summary.Factors,
		summary.MarketContext,
		summary.RiskLevel,
		summary.ModelUsed,
		summary.TokensUsed,
		summary.LatencyMs,
	).Scan(
		&summary.ID,
		&summary.UserID,
		&summary.QuestID,
		&summary.TradeID,
		&summary.SessionID,
		&summary.Category,
		&summary.Decision,
		&summary.Reasoning,
		&summary.Confidence,
		&factors,
		&summary.MarketContext,
		&summary.RiskLevel,
		&summary.ModelUsed,
		&summary.TokensUsed,
		&summary.LatencyMs,
		&summary.CreatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to store reasoning summary: %w", err)
	}

	summary.Factors = factors

	s.Logger.WithFields(map[string]interface{}{
		"summary_id": summary.ID,
		"user_id":    summary.UserID,
		"category":   summary.Category,
		"decision":   summary.Decision,
	}).Info("AI reasoning summary stored")

	return summary, nil
}

// GetUserReasoning retrieves reasoning summaries for a user.
func (s *AIReasoningService) GetUserReasoning(ctx context.Context, userID string, limit int) ([]*AIReasoningSummary, error) {
	if limit <= 0 {
		limit = 50
	}

	query := `
		SELECT id, user_id, quest_id, trade_id, session_id, category, decision, reasoning, 
			confidence, factors, market_context, risk_level, model_used, tokens_used, latency_ms, created_at
		FROM ai_reasoning_summaries
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`

	rows, err := s.db.Query(ctx, query, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get user reasoning: %w", err)
	}
	defer rows.Close()

	var summaries []*AIReasoningSummary
	for rows.Next() {
		var summary AIReasoningSummary
		var factors []string
		err := rows.Scan(
			&summary.ID,
			&summary.UserID,
			&summary.QuestID,
			&summary.TradeID,
			&summary.SessionID,
			&summary.Category,
			&summary.Decision,
			&summary.Reasoning,
			&summary.Confidence,
			&factors,
			&summary.MarketContext,
			&summary.RiskLevel,
			&summary.ModelUsed,
			&summary.TokensUsed,
			&summary.LatencyMs,
			&summary.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan reasoning summary: %w", err)
		}
		summary.Factors = factors
		summaries = append(summaries, &summary)
	}

	return summaries, nil
}

// GetTradeReasoning retrieves reasoning summaries for a specific trade.
func (s *AIReasoningService) GetTradeReasoning(ctx context.Context, tradeID int64) ([]*AIReasoningSummary, error) {
	query := `
		SELECT id, user_id, quest_id, trade_id, session_id, category, decision, reasoning, 
			confidence, factors, market_context, risk_level, model_used, tokens_used, latency_ms, created_at
		FROM ai_reasoning_summaries
		WHERE trade_id = $1
		ORDER BY created_at DESC
	`

	rows, err := s.db.Query(ctx, query, tradeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get trade reasoning: %w", err)
	}
	defer rows.Close()

	var summaries []*AIReasoningSummary
	for rows.Next() {
		var summary AIReasoningSummary
		var factors []string
		err := rows.Scan(
			&summary.ID,
			&summary.UserID,
			&summary.QuestID,
			&summary.TradeID,
			&summary.SessionID,
			&summary.Category,
			&summary.Decision,
			&summary.Reasoning,
			&summary.Confidence,
			&factors,
			&summary.MarketContext,
			&summary.RiskLevel,
			&summary.ModelUsed,
			&summary.TokensUsed,
			&summary.LatencyMs,
			&summary.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan reasoning summary: %w", err)
		}
		summary.Factors = factors
		summaries = append(summaries, &summary)
	}

	return summaries, nil
}

// GenerateExplanation generates a human-readable explanation for a trading decision.
func (s *AIReasoningService) GenerateExplanation(decision string, factors *AIReasoningFactors, marketContext string) string {
	explanation := fmt.Sprintf("Decision: %s\n\n", decision)

	if factors != nil {
		explanation += "Key Factors:\n"
		if len(factors.TechnicalIndicators) > 0 {
			explanation += fmt.Sprintf("- Technical Indicators: %s\n", joinStrings(factors.TechnicalIndicators, ", "))
		}
		if !factors.SentimentScore.IsZero() {
			explanation += fmt.Sprintf("- Sentiment Score: %.2f\n", factors.SentimentScore.InexactFloat64())
		}
		if factors.MarketTrend != "" {
			explanation += fmt.Sprintf("- Market Trend: %s\n", factors.MarketTrend)
		}
		if !factors.Volatility.IsZero() {
			explanation += fmt.Sprintf("- Volatility: %.2f%%\n", factors.Volatility.Mul(decimal.NewFromInt(100)).InexactFloat64())
		}
		if !factors.RiskRewardRatio.IsZero() {
			explanation += fmt.Sprintf("- Risk/Reward Ratio: %.2f\n", factors.RiskRewardRatio.InexactFloat64())
		}
		if !factors.PositionSize.IsZero() {
			explanation += fmt.Sprintf("- Position Size: %s\n", factors.PositionSize.String())
		}
		if factors.StopLoss != nil {
			explanation += fmt.Sprintf("- Stop Loss: %s\n", factors.StopLoss.String())
		}
		if factors.TakeProfit != nil {
			explanation += fmt.Sprintf("- Take Profit: %s\n", factors.TakeProfit.String())
		}
	}

	if marketContext != "" {
		explanation += fmt.Sprintf("\nMarket Context:\n%s\n", marketContext)
	}

	return explanation
}

// AIReasoningRequest represents a request to generate a reasoning summary.
type AIReasoningRequest struct {
	UserID        string
	QuestID       *int64
	TradeID       *int64
	SessionID     string
	Category      AIReasoningCategory
	Decision      string
	Reasoning     string
	Confidence    decimal.Decimal
	Factors       []string
	MarketContext string
	RiskLevel     string
	ModelUsed     string
	TokensUsed    int
	LatencyMs     int
}

// randInt64 generates a random int64.
func (s *AIReasoningService) randInt63() int64 {
	return time.Now().UnixNano()
}

// joinStrings joins a slice of strings with a separator.
func joinStrings(items []string, sep string) string {
	result := ""
	for i, item := range items {
		if i > 0 {
			result += sep
		}
		result += item
	}
	return result
}
