package autonomous

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

var (
	// ErrProposalExpired indicates the proposal has expired.
	ErrProposalExpired = errors.New("proposal expired")
	// ErrNilProposal indicates no proposal payload was provided.
	ErrNilProposal = errors.New("proposal is nil")
	// ErrProposalRejected indicates the proposal was rejected by policy.
	ErrProposalRejected = errors.New("proposal rejected by policy")
	// ErrLowConfidence indicates the proposal confidence is too low.
	ErrLowConfidence = errors.New("proposal confidence below threshold")
	// ErrHighRisk indicates the proposal risk is too high.
	ErrHighRisk = errors.New("proposal risk exceeds threshold")
)

// StrategyProposalConfig configures the proposal engine.
type StrategyProposalConfig struct {
	// MinConfidence is the minimum confidence required (0.0-1.0).
	MinConfidence float64 `json:"min_confidence"`
	// MaxRiskScore is the maximum acceptable risk score (0.0-1.0).
	MaxRiskScore float64 `json:"max_risk_score"`
	// ProposalTTL is the time-to-live for proposals.
	ProposalTTL time.Duration `json:"proposal_ttl"`
	// MaxActiveProposals is the maximum concurrent proposals per strategy.
	MaxActiveProposals int `json:"max_active_proposals"`
}

// DefaultStrategyProposalConfig returns the default configuration.
func DefaultStrategyProposalConfig() StrategyProposalConfig {
	return StrategyProposalConfig{
		MinConfidence:      0.6,
		MaxRiskScore:       0.7,
		ProposalTTL:        15 * time.Minute,
		MaxActiveProposals: 3,
	}
}

// StrategyProposalEngine generates and validates trading strategies.
type StrategyProposalEngine struct {
	config    StrategyProposalConfig
	validator PolicyValidator
	risk      RiskManager
}

// NewStrategyProposalEngine creates a new proposal engine.
func NewStrategyProposalEngine(
	config StrategyProposalConfig,
	validator PolicyValidator,
	risk RiskManager,
) *StrategyProposalEngine {
	return &StrategyProposalEngine{
		config:    config,
		validator: validator,
		risk:      risk,
	}
}

// GenerateProposal generates a new strategy proposal from AI output.
func (e *StrategyProposalEngine) GenerateProposal(
	ctx context.Context,
	strategyID, symbol, exchange, side string,
	confidence float64,
	reasoning string,
	parameters map[string]any,
	expectedReturn, maxDrawdown decimal.Decimal,
) (*StrategyProposal, error) {
	// Validate confidence threshold
	if confidence < e.config.MinConfidence {
		return nil, fmt.Errorf("%w: got %.2f, need %.2f",
			ErrLowConfidence, confidence, e.config.MinConfidence)
	}

	// Calculate risk score based on parameters
	riskScore := e.calculateRiskScore(parameters, maxDrawdown)
	if riskScore > e.config.MaxRiskScore {
		return nil, fmt.Errorf("%w: got %.2f, max %.2f",
			ErrHighRisk, riskScore, e.config.MaxRiskScore)
	}

	now := time.Now()
	proposal := &StrategyProposal{
		ID:             e.generateProposalID(strategyID, symbol, now),
		StrategyID:     strategyID,
		Symbol:         symbol,
		Exchange:       exchange,
		Side:           side,
		Confidence:     confidence,
		Reasoning:      reasoning,
		Parameters:     parameters,
		RiskScore:      riskScore,
		ExpectedReturn: expectedReturn,
		MaxDrawdown:    maxDrawdown,
		CreatedAt:      now,
		ExpiresAt:      now.Add(e.config.ProposalTTL),
	}

	return proposal, nil
}

// ValidateProposal validates a proposal against policy and risk limits.
func (e *StrategyProposalEngine) ValidateProposal(ctx context.Context, proposal *StrategyProposal) error {
	if proposal == nil {
		return ErrNilProposal
	}

	// Check expiration
	if time.Now().After(proposal.ExpiresAt) {
		return ErrProposalExpired
	}

	// Validate against policy
	if e.validator != nil {
		passes, reason, err := e.validator.ValidateProposal(ctx, proposal)
		if err != nil {
			return fmt.Errorf("policy validation failed: %w", err)
		}
		if !passes {
			return fmt.Errorf("%w: %s", ErrProposalRejected, reason)
		}
	}

	// Check risk limits
	if e.risk != nil {
		passes, reason, err := e.risk.CheckRiskLimits(ctx, proposal)
		if err != nil {
			return fmt.Errorf("risk check failed: %w", err)
		}
		if !passes {
			return fmt.Errorf("%w: %s", ErrProposalRejected, reason)
		}
	}

	return nil
}

// IsProposalValid checks if a proposal is still valid without returning errors.
func (e *StrategyProposalEngine) IsProposalValid(ctx context.Context, proposal *StrategyProposal) bool {
	return e.ValidateProposal(ctx, proposal) == nil
}

// calculateRiskScore calculates a risk score based on parameters.
func (e *StrategyProposalEngine) calculateRiskScore(parameters map[string]any, maxDrawdown decimal.Decimal) float64 {
	score := 0.0

	// Factor in max drawdown (higher drawdown = higher risk)
	if !maxDrawdown.IsZero() {
		drawdownFloat := maxDrawdown.Abs().InexactFloat64()
		score += drawdownFloat * 0.4 // 40% weight on drawdown
	}

	// Factor in leverage if present
	if leverage, ok := numericValue(parameters["leverage"]); ok {
		if leverage > 1 {
			score += (leverage / 20) * 0.3 // 30% weight on leverage
		}
	}

	// Factor in position size if present
	if posSize, ok := numericValue(parameters["position_size_percent"]); ok {
		score += (posSize / 100) * 0.3 // 30% weight on position size
	}

	// Cap at 1.0
	if score > 1.0 {
		score = 1.0
	}

	return score
}

func numericValue(raw any) (float64, bool) {
	switch value := raw.(type) {
	case float64:
		return value, true
	case float32:
		return float64(value), true
	case int:
		return float64(value), true
	case int32:
		return float64(value), true
	case int64:
		return float64(value), true
	case uint:
		return float64(value), true
	case uint32:
		return float64(value), true
	case uint64:
		return float64(value), true
	case json.Number:
		parsed, err := value.Float64()
		return parsed, err == nil
	case decimal.Decimal:
		return value.InexactFloat64(), true
	default:
		return 0, false
	}
}

// generateProposalID generates a unique proposal ID.
func (e *StrategyProposalEngine) generateProposalID(strategyID, symbol string, t time.Time) string {
	data := fmt.Sprintf("%s:%s:%s:%s", strategyID, symbol, t.UTC().Format(time.RFC3339Nano), uuid.New().String())
	hash := sha256.Sum256([]byte(data))
	return "prop_" + hex.EncodeToString(hash[:8])
}
