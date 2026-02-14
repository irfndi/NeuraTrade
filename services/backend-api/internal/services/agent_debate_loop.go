package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/irfndi/neuratrade/internal/services/risk"
)

type DebateRound struct {
	RoundNumber   int                  `json:"round_number"`
	StartedAt     time.Time            `json:"started_at"`
	EndedAt       *time.Time           `json:"ended_at,omitempty"`
	Analyst       *DebateAnalystResult `json:"analyst,omitempty"`
	RiskManager   *DebateRiskResult    `json:"risk_manager,omitempty"`
	Trader        *DebateTraderResult  `json:"trader,omitempty"`
	Consensus     string               `json:"consensus,omitempty"`
	FinalDecision string               `json:"final_decision,omitempty"`
}

type DebateAnalystResult struct {
	Recommendation string   `json:"recommendation"`
	Confidence     float64  `json:"confidence"`
	Condition      string   `json:"condition"`
	Summary        string   `json:"summary"`
	Signals        []string `json:"signals"`
}

type DebateRiskResult struct {
	Approved        bool     `json:"approved"`
	RiskLevel       string   `json:"risk_level"`
	RiskScore       float64  `json:"risk_score"`
	Recommendations []string `json:"recommendations"`
	Summary         string   `json:"summary"`
}

type DebateTraderResult struct {
	Action       string  `json:"action"`
	Confidence   float64 `json:"confidence"`
	PositionSize float64 `json:"position_size"`
	EntryPrice   float64 `json:"entry_price"`
	StopLoss     float64 `json:"stop_loss,omitempty"`
	TakeProfit   float64 `json:"take_profit,omitempty"`
	Reasoning    string  `json:"reasoning"`
}

type DebateConfig struct {
	MaxRounds          int                    `json:"max_rounds"`
	ConsensusThreshold float64                `json:"consensus_threshold"`
	AnalystConfig      AnalystAgentConfig     `json:"analyst_config"`
	TraderConfig       TraderAgentConfig      `json:"trader_config"`
	RiskConfig         risk.RiskManagerConfig `json:"risk_config"`
}

func DefaultDebateConfig() DebateConfig {
	return DebateConfig{
		MaxRounds:          3,
		ConsensusThreshold: 0.7,
		AnalystConfig:      DefaultAnalystAgentConfig(),
		TraderConfig:       DefaultTraderAgentConfig(),
		RiskConfig:         risk.DefaultRiskManagerConfig(),
	}
}

type AgentDebateLoop struct {
	config       DebateConfig
	analystAgent *AnalystAgent
	riskAgent    *risk.RiskManagerAgent
	traderAgent  *TraderAgent
	mu           sync.RWMutex
}

func NewAgentDebateLoop(config DebateConfig) *AgentDebateLoop {
	return &AgentDebateLoop{
		config:       config,
		analystAgent: NewAnalystAgent(config.AnalystConfig),
		riskAgent:    risk.NewRiskManagerAgent(config.RiskConfig),
		traderAgent:  NewTraderAgent(config.TraderConfig),
	}
}

func (d *AgentDebateLoop) RunDebate(ctx context.Context, market MarketContext, portfolio PortfolioState) (*DebateRound, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.config.MaxRounds <= 0 {
		return nil, fmt.Errorf("invalid MaxRounds: must be greater than 0, got %d", d.config.MaxRounds)
	}

	rounds := make([]*DebateRound, 0, d.config.MaxRounds)

	for round := 1; round <= d.config.MaxRounds; round++ {
		debateRound := &DebateRound{
			RoundNumber: round,
			StartedAt:   time.Now().UTC(),
		}

		analystSignals := d.buildAnalystSignals(market)
		analystResult, err := d.runAnalyst(ctx, market.Symbol, analystSignals)
		if err != nil {
			return nil, fmt.Errorf("analyst failed: %w", err)
		}
		debateRound.Analyst = analystResult

		riskSignals := d.buildRiskSignals(analystResult, market)
		riskResult, err := d.runRiskManager(ctx, market, riskSignals)
		if err != nil {
			return nil, fmt.Errorf("risk manager failed: %w", err)
		}
		debateRound.RiskManager = riskResult

		approved := riskResult.Approved
		if !approved {
			now := time.Now()
			debateRound.EndedAt = &now
			debateRound.Consensus = "rejected"
			debateRound.FinalDecision = "rejected_by_risk"
			rounds = append(rounds, debateRound)
			break
		}

		traderResult, err := d.runTrader(ctx, market, portfolio, analystResult, riskResult)
		if err != nil {
			return nil, fmt.Errorf("trader failed: %w", err)
		}
		debateRound.Trader = traderResult

		now := time.Now()
		debateRound.EndedAt = &now
		debateRound.Consensus = d.determineConsensus(analystResult, riskResult, traderResult)
		debateRound.FinalDecision = traderResult.Action
		rounds = append(rounds, debateRound)

		if debateRound.Consensus == "approved" || round == d.config.MaxRounds {
			break
		}
	}

	if len(rounds) == 0 {
		return nil, fmt.Errorf("no debate rounds were executed")
	}

	return rounds[len(rounds)-1], nil
}

func (d *AgentDebateLoop) buildAnalystSignals(market MarketContext) []AnalystSignal {
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

func (d *AgentDebateLoop) buildRiskSignals(analystResult *DebateAnalystResult, market MarketContext) []risk.RiskSignal {
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

	if analystResult.Confidence < d.config.ConsensusThreshold {
		signals = append(signals, risk.RiskSignal{
			Name:        "confidence_risk",
			Value:       1.0 - analystResult.Confidence,
			Weight:      0.2,
			Threshold:   0.3,
			Description: "Confidence risk",
		})
	}

	return signals
}

func (d *AgentDebateLoop) runAnalyst(ctx context.Context, symbol string, signals []AnalystSignal) (*DebateAnalystResult, error) {
	analysis, err := d.analystAgent.Analyze(ctx, symbol, AnalystRoleTechnical, signals)
	if err != nil {
		return nil, err
	}

	signalStrings := make([]string, len(analysis.Signals))
	for i, s := range analysis.Signals {
		signalStrings[i] = fmt.Sprintf("%s: %.2f (%s)", s.Name, s.Value, s.Direction)
	}

	return &DebateAnalystResult{
		Recommendation: string(analysis.Recommendation),
		Confidence:     analysis.Confidence,
		Condition:      string(analysis.Condition),
		Summary:        analysis.Summary,
		Signals:        signalStrings,
	}, nil
}

func (d *AgentDebateLoop) runRiskManager(ctx context.Context, market MarketContext, signals []risk.RiskSignal) (*DebateRiskResult, error) {
	assessment, err := d.riskAgent.AssessTradingRisk(ctx, market.Symbol, "buy", signals)
	if err != nil {
		return nil, err
	}

	approved := d.riskAgent.ShouldTrade(assessment)
	recommendations := make([]string, 0)
	if assessment.Action == risk.RiskActionBlock {
		recommendations = append(recommendations, "Trade blocked by risk manager")
	}
	if assessment.Action == risk.RiskActionReduce {
		recommendations = append(recommendations, "Reduce position size recommended")
	}
	recommendations = append(recommendations, assessment.Recommendations...)

	return &DebateRiskResult{
		Approved:        approved,
		RiskLevel:       string(assessment.RiskLevel),
		RiskScore:       assessment.Score,
		Recommendations: recommendations,
		Summary:         getReasonsSummary(assessment.Reasons),
	}, nil
}

func (d *AgentDebateLoop) runTrader(ctx context.Context, market MarketContext, portfolio PortfolioState, analyst *DebateAnalystResult, riskResult *DebateRiskResult) (*DebateTraderResult, error) {
	decision, err := d.traderAgent.MakeDecision(ctx, market, portfolio)
	if err != nil {
		return nil, err
	}

	result := &DebateTraderResult{
		Action:       string(decision.Action),
		Confidence:   decision.Confidence,
		PositionSize: decision.SizePercent,
		EntryPrice:   decision.EntryPrice,
		Reasoning:    decision.Reasoning,
	}

	if decision.StopLoss > 0 {
		result.StopLoss = decision.StopLoss
	}
	if decision.TakeProfit > 0 {
		result.TakeProfit = decision.TakeProfit
	}

	return result, nil
}

func (d *AgentDebateLoop) determineConsensus(analyst *DebateAnalystResult, risk *DebateRiskResult, trader *DebateTraderResult) string {
	if !risk.Approved {
		return "rejected"
	}

	buySignals := map[string]bool{"buy": true, "strong_buy": true}
	if buySignals[analyst.Recommendation] && trader.Action == "buy" {
		return "approved"
	}

	sellSignals := map[string]bool{"sell": true, "strong_sell": true}
	if sellSignals[analyst.Recommendation] && trader.Action == "sell" {
		return "approved"
	}

	return "hold"
}

func getSignalDirection(value float64) SignalDirection {
	if value > 0.1 {
		return DirectionBullish
	}
	if value < -0.1 {
		return DirectionBearish
	}
	return DirectionNeutral
}

func getReasonsSummary(reasons []string) string {
	if len(reasons) == 0 {
		return ""
	}
	result := ""
	for i, r := range reasons {
		if i > 0 {
			result += "; "
		}
		result += r
	}
	return result
}
