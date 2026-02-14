package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/irfndi/neuratrade/internal/services/risk"
)

// RoundRobinDebateConfig holds configuration for round-robin debate coordination
type RoundRobinDebateConfig struct {
	MaxRounds          int                    `json:"max_rounds"`
	ConsensusThreshold float64                `json:"consensus_threshold"`
	RoundTimeout       time.Duration          `json:"round_timeout"`
	DebateTimeout      time.Duration          `json:"debate_timeout"`
	RequireUnanimity   bool                   `json:"require_unanimity"`
	AnalystConfig      AnalystAgentConfig     `json:"analyst_config"`
	TraderConfig       TraderAgentConfig      `json:"trader_config"`
	RiskConfig         risk.RiskManagerConfig `json:"risk_config"`
}

// DefaultRoundRobinDebateConfig returns sensible defaults
func DefaultRoundRobinDebateConfig() RoundRobinDebateConfig {
	return RoundRobinDebateConfig{
		MaxRounds:          5,
		ConsensusThreshold: 0.7,
		RoundTimeout:       30 * time.Second,
		DebateTimeout:      5 * time.Minute,
		RequireUnanimity:   false,
		AnalystConfig:      DefaultAnalystAgentConfig(),
		TraderConfig:       DefaultTraderAgentConfig(),
		RiskConfig:         risk.DefaultRiskManagerConfig(),
	}
}

// RoundRobinDebateRound represents a single round in the debate
type RoundRobinDebateRound struct {
	RoundNumber    int               `json:"round_number"`
	StartedAt      time.Time         `json:"started_at"`
	EndedAt        *time.Time        `json:"ended_at,omitempty"`
	AnalystTurn    *DebateTurnResult `json:"analyst_turn,omitempty"`
	RiskTurn       *DebateTurnResult `json:"risk_turn,omitempty"`
	TraderTurn     *DebateTurnResult `json:"trader_turn,omitempty"`
	RoundConsensus string            `json:"round_consensus"`
	Confidence     float64           `json:"confidence"`
}

// DebateTurnResult represents the result of an agent's turn
type DebateTurnResult struct {
	AgentType       string        `json:"agent_type"`
	Decision        string        `json:"decision"`
	Confidence      float64       `json:"confidence"`
	Reasoning       string        `json:"reasoning"`
	Duration        time.Duration `json:"duration"`
	Signals         []string      `json:"signals,omitempty"`
	Recommendations []string      `json:"recommendations,omitempty"`
}

// RoundRobinDebateResult represents the final result of a round-robin debate
type RoundRobinDebateResult struct {
	DebateID        string                   `json:"debate_id"`
	StartedAt       time.Time                `json:"started_at"`
	EndedAt         time.Time                `json:"ended_at"`
	Rounds          []*RoundRobinDebateRound `json:"rounds"`
	FinalConsensus  string                   `json:"final_consensus"`
	FinalDecision   string                   `json:"final_decision"`
	FinalConfidence float64                  `json:"final_confidence"`
	TotalRounds     int                      `json:"total_rounds"`
	MarketContext   MarketContext            `json:"market_context"`
	PortfolioState  PortfolioState           `json:"portfolio_state"`
}

// DebateCoordinator manages round-robin debates between agents
type DebateCoordinator struct {
	config        RoundRobinDebateConfig
	debateLoop    *AgentDebateLoop
	mu            sync.RWMutex
	activeDebates map[string]*RoundRobinDebateResult
}

// NewDebateCoordinator creates a new debate coordinator
func NewDebateCoordinator(config RoundRobinDebateConfig) *DebateCoordinator {
	debateConfig := DebateConfig{
		MaxRounds:          config.MaxRounds,
		ConsensusThreshold: config.ConsensusThreshold,
		AnalystConfig:      config.AnalystConfig,
		TraderConfig:       config.TraderConfig,
		RiskConfig:         config.RiskConfig,
	}

	return &DebateCoordinator{
		config:        config,
		debateLoop:    NewAgentDebateLoop(debateConfig),
		activeDebates: make(map[string]*RoundRobinDebateResult),
	}
}

// RunRoundRobinDebate executes a complete round-robin debate with multiple rounds
func (dc *DebateCoordinator) RunRoundRobinDebate(ctx context.Context, market MarketContext, portfolio PortfolioState) (*RoundRobinDebateResult, error) {
	debateID := fmt.Sprintf("debate_%s_%d", market.Symbol, time.Now().UnixNano())

	result := &RoundRobinDebateResult{
		DebateID:       debateID,
		StartedAt:      time.Now(),
		Rounds:         make([]*RoundRobinDebateRound, 0),
		MarketContext:  market,
		PortfolioState: portfolio,
	}

	dc.mu.Lock()
	dc.activeDebates[debateID] = result
	dc.mu.Unlock()

	defer func() {
		dc.mu.Lock()
		delete(dc.activeDebates, debateID)
		dc.mu.Unlock()
	}()

	// Create timeout context for entire debate
	debateCtx, cancel := context.WithTimeout(ctx, dc.config.DebateTimeout)
	defer cancel()

	for roundNum := 1; roundNum <= dc.config.MaxRounds; roundNum++ {
		round, err := dc.executeDebateRound(debateCtx, roundNum, market, portfolio, result)
		if err != nil {
			return result, fmt.Errorf("round %d failed: %w", roundNum, err)
		}

		result.Rounds = append(result.Rounds, round)
		result.TotalRounds = roundNum

		// Check if we've reached consensus
		if dc.hasAchievedConsensus(round) {
			break
		}

		// Check if context is cancelled
		select {
		case <-debateCtx.Done():
			return result, fmt.Errorf("debate timeout or cancelled: %w", debateCtx.Err())
		default:
		}
	}

	// Determine final result
	dc.finalizeDebate(result)

	return result, nil
}

// executeDebateRound executes a single round of the debate
func (dc *DebateCoordinator) executeDebateRound(ctx context.Context, roundNum int, market MarketContext, portfolio PortfolioState, debate *RoundRobinDebateResult) (*RoundRobinDebateRound, error) {
	round := &RoundRobinDebateRound{
		RoundNumber: roundNum,
		StartedAt:   time.Now(),
	}

	// Execute debate through the existing debate loop
	debateResult, err := dc.debateLoop.RunDebate(ctx, market, portfolio)
	if err != nil {
		return round, err
	}

	// Record turns based on debate result
	if debateResult.Analyst != nil {
		round.AnalystTurn = &DebateTurnResult{
			AgentType:  "analyst",
			Decision:   debateResult.Analyst.Recommendation,
			Confidence: debateResult.Analyst.Confidence,
			Reasoning:  debateResult.Analyst.Summary,
			Signals:    debateResult.Analyst.Signals,
		}
	}

	if debateResult.RiskManager != nil {
		round.RiskTurn = &DebateTurnResult{
			AgentType:       "risk_manager",
			Decision:        map[bool]string{true: "approved", false: "rejected"}[debateResult.RiskManager.Approved],
			Confidence:      1.0 - debateResult.RiskManager.RiskScore,
			Reasoning:       debateResult.RiskManager.Summary,
			Recommendations: debateResult.RiskManager.Recommendations,
		}
	}

	if debateResult.Trader != nil {
		round.TraderTurn = &DebateTurnResult{
			AgentType:  "trader",
			Decision:   debateResult.Trader.Action,
			Confidence: debateResult.Trader.Confidence,
			Reasoning:  debateResult.Trader.Reasoning,
		}
	}

	// Calculate round consensus
	round.RoundConsensus = debateResult.Consensus
	round.Confidence = dc.calculateRoundConfidence(round)

	now := time.Now()
	round.EndedAt = &now

	return round, nil
}

// calculateRoundConfidence calculates the confidence level for a round
func (dc *DebateCoordinator) calculateRoundConfidence(round *RoundRobinDebateRound) float64 {
	var confidences []float64

	if round.AnalystTurn != nil {
		confidences = append(confidences, round.AnalystTurn.Confidence)
	}
	if round.RiskTurn != nil {
		confidences = append(confidences, round.RiskTurn.Confidence)
	}
	if round.TraderTurn != nil {
		confidences = append(confidences, round.TraderTurn.Confidence)
	}

	if len(confidences) == 0 {
		return 0
	}

	// Average confidence
	sum := 0.0
	for _, c := range confidences {
		sum += c
	}
	return sum / float64(len(confidences))
}

// hasAchievedConsensus checks if the debate has reached consensus
func (dc *DebateCoordinator) hasAchievedConsensus(round *RoundRobinDebateRound) bool {
	if dc.config.RequireUnanimity {
		// All agents must agree
		decisions := make(map[string]bool)
		if round.AnalystTurn != nil {
			decisions[round.AnalystTurn.Decision] = true
		}
		if round.RiskTurn != nil {
			decisions[round.RiskTurn.Decision] = true
		}
		if round.TraderTurn != nil {
			decisions[round.TraderTurn.Decision] = true
		}
		return len(decisions) == 1 && round.Confidence >= dc.config.ConsensusThreshold
	}

	// Simple majority with threshold
	return round.Confidence >= dc.config.ConsensusThreshold && round.RoundConsensus == "approved"
}

// finalizeDebate determines the final result of the debate
func (dc *DebateCoordinator) finalizeDebate(result *RoundRobinDebateResult) {
	result.EndedAt = time.Now()

	if len(result.Rounds) == 0 {
		result.FinalConsensus = "no_consensus"
		result.FinalDecision = "hold"
		result.FinalConfidence = 0
		return
	}

	// Use the last round's result
	lastRound := result.Rounds[len(result.Rounds)-1]
	result.FinalConsensus = lastRound.RoundConsensus
	result.FinalConfidence = lastRound.Confidence

	// Determine final decision
	if lastRound.TraderTurn != nil {
		result.FinalDecision = lastRound.TraderTurn.Decision
	} else if lastRound.AnalystTurn != nil {
		result.FinalDecision = lastRound.AnalystTurn.Decision
	} else {
		result.FinalDecision = "hold"
	}

	// Override if risk rejected
	if lastRound.RiskTurn != nil && lastRound.RiskTurn.Decision == "rejected" {
		result.FinalDecision = "hold"
		result.FinalConsensus = "rejected"
	}
}

// GetActiveDebates returns a list of currently active debates
func (dc *DebateCoordinator) GetActiveDebates() []*RoundRobinDebateResult {
	dc.mu.RLock()
	defer dc.mu.RUnlock()

	debates := make([]*RoundRobinDebateResult, 0, len(dc.activeDebates))
	for _, debate := range dc.activeDebates {
		debates = append(debates, debate)
	}
	return debates
}

// GetDebateMetrics returns metrics for debate performance
func (dc *DebateCoordinator) GetDebateMetrics() map[string]interface{} {
	dc.mu.RLock()
	defer dc.mu.RUnlock()

	return map[string]interface{}{
		"active_debates":      len(dc.activeDebates),
		"max_rounds":          dc.config.MaxRounds,
		"consensus_threshold": dc.config.ConsensusThreshold,
	}
}
