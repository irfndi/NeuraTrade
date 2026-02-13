package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"
	"github.com/irfndi/neuratrade/internal/services/risk"
)

// RoundRobinDebatePhase represents the current phase of the round-robin debate.
type RoundRobinDebatePhase string

const (
	DebatePhaseAnalyst     RoundRobinDebatePhase = "analyst"
	DebatePhaseRiskManager RoundRobinDebatePhase = "risk_manager"
	DebatePhaseTrader      RoundRobinDebatePhase = "trader"
	DebatePhaseConsensus   RoundRobinDebatePhase = "consensus"
	DebatePhaseComplete    RoundRobinDebatePhase = "complete"
)

// RoundRobinDebateConfig extends DebateConfig with round-robin specific settings.
type RoundRobinDebateConfig struct {
	DebateConfig

	// Round-robin settings
	RequireAllAgents       bool          // All agents must participate before consensus
	AllowAgentRebuttal     bool          // Agents can respond to each other's analysis
	MaxRebuttalRounds      int           // Maximum rebuttal rounds
	MinConsensusConfidence float64       // Minimum confidence for consensus
	DebateTimeout          time.Duration // Overall debate timeout
	AgentResponseTimeout   time.Duration // Timeout for individual agent responses

	// Consensus settings
	ConsensusStrategy string  // "unanimous", "majority", "weighted"
	AnalystWeight     float64 // Weight for analyst votes
	RiskManagerWeight float64 // Weight for risk manager votes
	TraderWeight      float64 // Weight for trader votes

	// Advanced features
	EnableConfidenceVoting bool // Agents vote with confidence scores
	EnableJustification    bool // Require agents to justify decisions
	EnableChallengeMode    bool // Agents can challenge each other's conclusions
}

// DefaultRoundRobinDebateConfig returns default round-robin debate configuration.
func DefaultRoundRobinDebateConfig() RoundRobinDebateConfig {
	return RoundRobinDebateConfig{
		DebateConfig:           DefaultDebateConfig(),
		RequireAllAgents:       true,
		AllowAgentRebuttal:     true,
		MaxRebuttalRounds:      2,
		MinConsensusConfidence: 0.65,
		DebateTimeout:          5 * time.Minute,
		AgentResponseTimeout:   30 * time.Second,
		ConsensusStrategy:      "weighted",
		AnalystWeight:          0.35,
		RiskManagerWeight:      0.35,
		TraderWeight:           0.30,
		EnableConfidenceVoting: true,
		EnableJustification:    true,
		EnableChallengeMode:    true,
	}
}

// AgentRole represents the role of an agent in the debate.
type AgentRole string

const (
	AgentRoleAnalyst     AgentRole = "analyst"
	AgentRoleRiskManager AgentRole = "risk_manager"
	AgentRoleTrader      AgentRole = "trader"
)

// AgentVote represents a single agent's vote in the debate.
type AgentVote struct {
	AgentRole     AgentRole `json:"agent_role"`
	Decision      string    `json:"decision"`   // "buy", "sell", "hold", "reject"
	Confidence    float64   `json:"confidence"` // 0-1 confidence score
	Justification string    `json:"justification"`
	Timestamp     time.Time `json:"timestamp"`
	Round         int       `json:"round"`
}

// RoundRobinDebateRound represents a single round in the round-robin debate.
type RoundRobinDebateRound struct {
	RoundNumber int                   `json:"round_number"`
	Phase       RoundRobinDebatePhase `json:"phase"`
	StartedAt   time.Time             `json:"started_at"`
	EndedAt     *time.Time            `json:"ended_at,omitempty"`

	// Agent contributions
	AnalystVote     *AgentVote `json:"analyst_vote,omitempty"`
	RiskManagerVote *AgentVote `json:"risk_manager_vote,omitempty"`
	TraderVote      *AgentVote `json:"trader_vote,omitempty"`

	// Rebuttals (if enabled)
	Rebuttals []*AgentRebuttal `json:"rebuttals,omitempty"`

	// Consensus
	ConsensusScore float64 `json:"consensus_score"`
	Consensus      string  `json:"consensus"`
	FinalDecision  string  `json:"final_decision"`
	Confidence     float64 `json:"confidence"`
}

// AgentRebuttal represents a rebuttal between agents.
type AgentRebuttal struct {
	FromAgent AgentRole `json:"from_agent"`
	ToAgent   AgentRole `json:"to_agent"`
	Challenge string    `json:"challenge"`
	Response  string    `json:"response,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// RoundRobinDebateResult represents the complete result of a round-robin debate.
type RoundRobinDebateResult struct {
	ID             string                   `json:"id"`
	Symbol         string                   `json:"symbol"`
	MarketContext  MarketContext            `json:"market_context"`
	PortfolioState PortfolioState           `json:"portfolio_state"`
	Rounds         []*RoundRobinDebateRound `json:"rounds"`
	FinalRound     *RoundRobinDebateRound   `json:"final_round"`
	TotalRounds    int                      `json:"total_rounds"`
	DurationMs     int64                    `json:"duration_ms"`
	StartedAt      time.Time                `json:"started_at"`
	CompletedAt    time.Time                `json:"completed_at"`
	Success        bool                     `json:"success"`
	Error          string                   `json:"error,omitempty"`
}

// RoundRobinDebateLoop implements a sophisticated round-robin debate system.
type RoundRobinDebateLoop struct {
	config       RoundRobinDebateConfig
	analystAgent *AnalystAgent
	riskAgent    *risk.RiskManagerAgent
	traderAgent  *TraderAgent
	logger       *zaplogrus.Logger

	// Debate history
	history   map[string]*RoundRobinDebateResult
	historyMu sync.RWMutex

	mu sync.RWMutex
}

// NewRoundRobinDebateLoop creates a new round-robin debate loop.
func NewRoundRobinDebateLoop(config RoundRobinDebateConfig, logger *zaplogrus.Logger) *RoundRobinDebateLoop {
	return &RoundRobinDebateLoop{
		config:       config,
		analystAgent: NewAnalystAgent(config.AnalystConfig),
		riskAgent:    risk.NewRiskManagerAgent(config.RiskConfig),
		traderAgent:  NewTraderAgent(config.TraderConfig),
		logger:       logger,
		history:      make(map[string]*RoundRobinDebateResult),
	}
}

// RunDebate executes a complete round-robin debate.
func (r *RoundRobinDebateLoop) RunDebate(ctx context.Context, market MarketContext, portfolio PortfolioState) (*RoundRobinDebateResult, error) {
	startTime := time.Now().UTC()

	result := &RoundRobinDebateResult{
		ID:             uuid.New().String(),
		Symbol:         market.Symbol,
		MarketContext:  market,
		PortfolioState: portfolio,
		Rounds:         make([]*RoundRobinDebateRound, 0),
		StartedAt:      startTime,
		Success:        false,
	}

	// Create debate context with timeout
	debateCtx, cancel := context.WithTimeout(ctx, r.config.DebateTimeout)
	defer cancel()

	r.logger.Info("Starting round-robin debate",
		"debate_id", result.ID,
		"symbol", market.Symbol,
		"max_rounds", r.config.MaxRounds)

	for roundNum := 1; roundNum <= r.config.MaxRounds; roundNum++ {
		round, err := r.executeDebateRound(debateCtx, market, portfolio, roundNum, result.Rounds)
		if err != nil {
			result.Error = fmt.Sprintf("Round %d failed: %v", roundNum, err)
			r.logger.WithError(err).Error("Debate round failed",
				"debate_id", result.ID,
				"round", roundNum)
			break
		}

		result.Rounds = append(result.Rounds, round)

		// Check for early consensus
		if round.Consensus == "approved" || round.Consensus == "rejected" {
			r.logger.Info("Debate reached consensus",
				"debate_id", result.ID,
				"round", roundNum,
				"consensus", round.Consensus)
			result.FinalRound = round
			result.Success = true
			break
		}

		// Check for timeout
		if debateCtx.Err() != nil {
			result.Error = "Debate timeout reached"
			r.logger.Warn("Debate timeout", "debate_id", result.ID)
			break
		}
	}

	// If no consensus reached, use last round
	if result.FinalRound == nil && len(result.Rounds) > 0 {
		result.FinalRound = result.Rounds[len(result.Rounds)-1]
	}

	result.CompletedAt = time.Now().UTC()
	result.DurationMs = result.CompletedAt.Sub(startTime).Milliseconds()
	result.TotalRounds = len(result.Rounds)

	if result.FinalRound != nil && result.FinalRound.Consensus == "approved" {
		result.Success = true
	}

	// Store in history
	r.historyMu.Lock()
	r.history[result.ID] = result
	r.historyMu.Unlock()

	r.logger.Info("Round-robin debate completed",
		"debate_id", result.ID,
		"success", result.Success,
		"rounds", result.TotalRounds,
		"duration_ms", result.DurationMs)

	return result, nil
}

// executeDebateRound executes a single round of the debate.
func (r *RoundRobinDebateLoop) executeDebateRound(
	ctx context.Context,
	market MarketContext,
	portfolio PortfolioState,
	roundNum int,
	previousRounds []*RoundRobinDebateRound,
) (*RoundRobinDebateRound, error) {

	round := &RoundRobinDebateRound{
		RoundNumber: roundNum,
		Phase:       DebatePhaseAnalyst,
		StartedAt:   time.Now().UTC(),
		Rebuttals:   make([]*AgentRebuttal, 0),
	}

	// Step 1: Analyst Analysis
	analystVote, err := r.runAnalystPhase(ctx, market, previousRounds)
	if err != nil {
		return nil, fmt.Errorf("analyst phase failed: %w", err)
	}
	round.AnalystVote = analystVote
	round.Phase = DebatePhaseRiskManager

	// Step 2: Risk Manager Assessment
	riskVote, err := r.runRiskManagerPhase(ctx, market, analystVote, previousRounds)
	if err != nil {
		return nil, fmt.Errorf("risk manager phase failed: %w", err)
	}
	round.RiskManagerVote = riskVote
	round.Phase = DebatePhaseTrader

	// Step 3: Trader Decision
	traderVote, err := r.runTraderPhase(ctx, market, portfolio, analystVote, riskVote, previousRounds)
	if err != nil {
		return nil, fmt.Errorf("trader phase failed: %w", err)
	}
	round.TraderVote = traderVote
	round.Phase = DebatePhaseConsensus

	// Step 4: Rebuttals (if enabled and needed)
	if r.config.AllowAgentRebuttal && roundNum <= r.config.MaxRebuttalRounds {
		rebuttals := r.generateRebuttals(analystVote, riskVote, traderVote)
		round.Rebuttals = rebuttals
	}

	// Step 5: Calculate Consensus
	consensus, score, confidence := r.calculateConsensus(analystVote, riskVote, traderVote)
	round.Consensus = consensus
	round.ConsensusScore = score
	round.Confidence = confidence

	// Determine final decision
	if consensus == "approved" {
		round.FinalDecision = traderVote.Decision
	} else if consensus == "rejected" {
		round.FinalDecision = "rejected"
	} else {
		round.FinalDecision = "hold"
	}

	now := time.Now().UTC()
	round.EndedAt = &now
	round.Phase = DebatePhaseComplete

	return round, nil
}

// runAnalystPhase executes the analyst agent phase.
func (r *RoundRobinDebateLoop) runAnalystPhase(ctx context.Context, market MarketContext, previousRounds []*RoundRobinDebateRound) (*AgentVote, error) {
	// Build signals based on market context
	signals := r.buildAnalystSignals(market)

	// Add context from previous rounds if available
	if len(previousRounds) > 0 {
		lastRound := previousRounds[len(previousRounds)-1]
		if lastRound.Consensus == "hold" {
			signals = append(signals, AnalystSignal{
				Name:        "previous_consensus",
				Value:       0.5,
				Weight:      0.1,
				Direction:   DirectionNeutral,
				Description: "Previous round resulted in hold decision",
			})
		}
	}

	analysis, err := r.analystAgent.Analyze(ctx, market.Symbol, AnalystRoleTechnical, signals)
	if err != nil {
		return nil, err
	}

	decision := "hold"
	if analysis.Recommendation == RecommendationBuy {
		decision = "buy"
	} else if analysis.Recommendation == RecommendationSell {
		decision = "sell"
	}

	return &AgentVote{
		AgentRole:     AgentRoleAnalyst,
		Decision:      decision,
		Confidence:    analysis.Confidence,
		Justification: analysis.Summary,
		Timestamp:     time.Now().UTC(),
		Round:         len(previousRounds) + 1,
	}, nil
}

// runRiskManagerPhase executes the risk manager agent phase.
func (r *RoundRobinDebateLoop) runRiskManagerPhase(
	ctx context.Context,
	market MarketContext,
	analystVote *AgentVote,
	previousRounds []*RoundRobinDebateRound,
) (*AgentVote, error) {

	signals := r.buildRiskSignals(analystVote, market)

	assessment, err := r.riskAgent.AssessTradingRisk(ctx, market.Symbol, analystVote.Decision, signals)
	if err != nil {
		return nil, err
	}

	approved := r.riskAgent.ShouldTrade(assessment)

	decision := analystVote.Decision
	if !approved {
		decision = "reject"
	} else if assessment.Action == risk.RiskActionReduce {
		decision = "hold" // Reduce risk by holding
	}

	justification := getReasonsSummary(assessment.Reasons)
	if assessment.Recommendations != nil && len(assessment.Recommendations) > 0 {
		justification = assessment.Recommendations[0]
	}

	return &AgentVote{
		AgentRole:     AgentRoleRiskManager,
		Decision:      decision,
		Confidence:    1.0 - assessment.Score, // Lower risk score = higher confidence
		Justification: justification,
		Timestamp:     time.Now().UTC(),
		Round:         len(previousRounds) + 1,
	}, nil
}

// runTraderPhase executes the trader agent phase.
func (r *RoundRobinDebateLoop) runTraderPhase(
	ctx context.Context,
	market MarketContext,
	portfolio PortfolioState,
	analystVote *AgentVote,
	riskVote *AgentVote,
	previousRounds []*RoundRobinDebateRound,
) (*AgentVote, error) {

	// Don't trade if risk manager rejected
	if riskVote.Decision == "reject" {
		return &AgentVote{
			AgentRole:     AgentRoleTrader,
			Decision:      "hold",
			Confidence:    riskVote.Confidence,
			Justification: "Trade rejected by risk manager",
			Timestamp:     time.Now().UTC(),
			Round:         len(previousRounds) + 1,
		}, nil
	}

	decision, err := r.traderAgent.MakeDecision(ctx, market, portfolio)
	if err != nil {
		return nil, err
	}

	return &AgentVote{
		AgentRole:     AgentRoleTrader,
		Decision:      string(decision.Action),
		Confidence:    decision.Confidence,
		Justification: decision.Reasoning,
		Timestamp:     time.Now().UTC(),
		Round:         len(previousRounds) + 1,
	}, nil
}

// generateRebuttals generates rebuttals between agents when there are disagreements.
func (r *RoundRobinDebateLoop) generateRebuttals(analystVote, riskVote, traderVote *AgentVote) []*AgentRebuttal {
	rebuttals := make([]*AgentRebuttal, 0)

	if !r.config.EnableChallengeMode {
		return rebuttals
	}

	// Example: Risk manager challenges analyst if confidence is low
	if analystVote.Confidence < 0.6 && riskVote.Confidence > 0.7 {
		rebuttals = append(rebuttals, &AgentRebuttal{
			FromAgent: AgentRoleRiskManager,
			ToAgent:   AgentRoleAnalyst,
			Challenge: "Low confidence in analysis despite market conditions",
			Timestamp: time.Now().UTC(),
		})
	}

	// Example: Trader challenges if decision differs from recommendation
	if analystVote.Decision != traderVote.Decision && analystVote.Confidence > 0.8 {
		rebuttals = append(rebuttals, &AgentRebuttal{
			FromAgent: AgentRoleTrader,
			ToAgent:   AgentRoleAnalyst,
			Challenge: "Decision diverges from high-confidence analyst recommendation",
			Timestamp: time.Now().UTC(),
		})
	}

	return rebuttals
}

// calculateConsensus calculates the consensus score and decision.
func (r *RoundRobinDebateLoop) calculateConsensus(analystVote, riskVote, traderVote *AgentVote) (string, float64, float64) {
	// Check for rejection first
	if riskVote.Decision == "reject" {
		return "rejected", 0.0, riskVote.Confidence
	}

	// Count weighted votes
	votes := make(map[string]float64)

	// Add analyst vote
	votes[analystVote.Decision] += r.config.AnalystWeight * analystVote.Confidence

	// Add risk manager vote (if not rejected)
	if riskVote.Decision != "reject" {
		votes[riskVote.Decision] += r.config.RiskManagerWeight * riskVote.Confidence
	}

	// Add trader vote
	votes[traderVote.Decision] += r.config.TraderWeight * traderVote.Confidence

	// Find winning decision
	var winningDecision string
	var maxScore float64
	for decision, score := range votes {
		if score > maxScore {
			maxScore = score
			winningDecision = decision
		}
	}

	// Calculate average confidence
	avgConfidence := (analystVote.Confidence + riskVote.Confidence + traderVote.Confidence) / 3.0

	// Determine consensus
	consensus := "hold"
	if winningDecision == "buy" || winningDecision == "sell" {
		if maxScore >= r.config.MinConsensusConfidence && avgConfidence >= r.config.MinConsensusConfidence {
			consensus = "approved"
		}
	}

	return consensus, maxScore, avgConfidence
}

// buildAnalystSignals builds signals for the analyst agent.
func (r *RoundRobinDebateLoop) buildAnalystSignals(market MarketContext) []AnalystSignal {
	signals := make([]AnalystSignal, 0)

	signals = append(signals, AnalystSignal{
		Name:        "price_momentum",
		Value:       market.Volume24h,
		Weight:      0.3,
		Direction:   getSignalDirection(market.Volume24h),
		Description: "24h volume as momentum indicator",
	})

	signals = append(signals, AnalystSignal{
		Name:        "volume_activity",
		Value:       market.Liquidity,
		Weight:      0.2,
		Direction:   DirectionNeutral,
		Description: "Liquidity indicator",
	})

	signals = append(signals, AnalystSignal{
		Name:        "volatility",
		Value:       market.Volatility,
		Weight:      0.2,
		Direction:   getSignalDirection(market.Volatility - 0.5),
		Description: "Price volatility",
	})

	signals = append(signals, AnalystSignal{
		Name:        "funding_rate",
		Value:       market.FundingRate,
		Weight:      0.15,
		Direction:   getSignalDirection(market.FundingRate),
		Description: "Funding rate indicator",
	})

	return signals
}

// buildRiskSignals builds risk signals for the risk manager.
func (r *RoundRobinDebateLoop) buildRiskSignals(analystVote *AgentVote, market MarketContext) []risk.RiskSignal {
	signals := make([]risk.RiskSignal, 0)

	signals = append(signals, risk.RiskSignal{
		Name:        "market_risk",
		Value:       market.Volatility,
		Weight:      0.5,
		Threshold:   0.8,
		Description: "Market volatility risk",
	})

	signals = append(signals, risk.RiskSignal{
		Name:        "liquidity_risk",
		Value:       1.0 - market.Liquidity,
		Weight:      0.3,
		Threshold:   0.3,
		Description: "Liquidity risk",
	})

	if analystVote.Confidence < r.config.MinConsensusConfidence {
		signals = append(signals, risk.RiskSignal{
			Name:        "confidence_risk",
			Value:       1.0 - analystVote.Confidence,
			Weight:      0.2,
			Threshold:   0.3,
			Description: "Confidence risk",
		})
	}

	return signals
}

// GetDebateHistory returns debate history for a specific symbol.
func (r *RoundRobinDebateLoop) GetDebateHistory(symbol string, limit int) []*RoundRobinDebateResult {
	r.historyMu.RLock()
	defer r.historyMu.RUnlock()

	results := make([]*RoundRobinDebateResult, 0)
	for _, result := range r.history {
		if result.Symbol == symbol {
			results = append(results, result)
		}
	}

	// Sort by completed time (most recent first)
	for i := 0; i < len(results)-1; i++ {
		for j := i + 1; j < len(results); j++ {
			if results[i].CompletedAt.Before(results[j].CompletedAt) {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	// Apply limit
	if limit > 0 && limit < len(results) {
		results = results[:limit]
	}

	return results
}
