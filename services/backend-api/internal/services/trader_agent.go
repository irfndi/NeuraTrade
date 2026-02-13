package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

type TradingAction string

const (
	ActionOpenLong   TradingAction = "open_long"
	ActionOpenShort  TradingAction = "open_short"
	ActionCloseLong  TradingAction = "close_long"
	ActionCloseShort TradingAction = "close_short"
	ActionAddToPos   TradingAction = "add_to_position"
	ActionReducePos  TradingAction = "reduce_position"
	ActionHold       TradingAction = "hold"
	ActionWait       TradingAction = "wait"
)

type TradingMode string

const (
	ModeConservative TradingMode = "conservative"
	ModeModerate     TradingMode = "moderate"
	ModeAggressive   TradingMode = "aggressive"
	ModePaper        TradingMode = "paper"
)

type PositionSide string

const (
	SideLong  PositionSide = "long"
	SideShort PositionSide = "short"
	SideFlat  PositionSide = "flat"
)

type TradingSignal struct {
	Name        string  `json:"name"`
	Value       float64 `json:"value"`
	Weight      float64 `json:"weight"`
	Direction   string  `json:"direction"`
	Description string  `json:"description"`
}

type MarketContext struct {
	Symbol       string          `json:"symbol"`
	CurrentPrice float64         `json:"current_price"`
	Volatility   float64         `json:"volatility"`
	Trend        string          `json:"trend"`
	Liquidity    float64         `json:"liquidity"`
	FundingRate  float64         `json:"funding_rate"`
	OpenInterest float64         `json:"open_interest"`
	Volume24h    float64         `json:"volume_24h"`
	Signals      []TradingSignal `json:"signals"`
}

type PortfolioState struct {
	TotalValue      float64 `json:"total_value"`
	AvailableCash   float64 `json:"available_cash"`
	OpenPositions   int     `json:"open_positions"`
	UnrealizedPnL   float64 `json:"unrealized_pnl"`
	RealizedPnL     float64 `json:"realized_pnl"`
	MaxDrawdown     float64 `json:"max_drawdown"`
	CurrentDrawdown float64 `json:"current_drawdown"`
}

type TradingDecision struct {
	ID             string            `json:"id"`
	Symbol         string            `json:"symbol"`
	Action         TradingAction     `json:"action"`
	Side           PositionSide      `json:"side"`
	SizePercent    float64           `json:"size_percent"`
	EntryPrice     float64           `json:"entry_price,omitempty"`
	StopLoss       float64           `json:"stop_loss,omitempty"`
	TakeProfit     float64           `json:"take_profit,omitempty"`
	Confidence     float64           `json:"confidence"`
	Reasoning      string            `json:"reasoning"`
	RiskScore      float64           `json:"risk_score"`
	ExpectedReturn float64           `json:"expected_return"`
	CreatedAt      time.Time         `json:"created_at"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

type TraderAgentConfig struct {
	Mode                TradingMode   `json:"mode"`
	MaxPositionSize     float64       `json:"max_position_size"`
	MaxOpenPositions    int           `json:"max_open_positions"`
	StopLossPercent     float64       `json:"stop_loss_percent"`
	TakeProfitPercent   float64       `json:"take_profit_percent"`
	MinConfidence       float64       `json:"min_confidence"`
	MaxRiskPerTrade     float64       `json:"max_risk_per_trade"`
	CooldownPeriod      time.Duration `json:"cooldown_period"`
	RequireConfirmation bool          `json:"require_confirmation"`
}

func DefaultTraderAgentConfig() TraderAgentConfig {
	return TraderAgentConfig{
		Mode:                ModeModerate,
		MaxPositionSize:     0.1,
		MaxOpenPositions:    5,
		StopLossPercent:     2.0,
		TakeProfitPercent:   6.0,
		MinConfidence:       0.7,
		MaxRiskPerTrade:     0.02,
		CooldownPeriod:      5 * time.Minute,
		RequireConfirmation: false,
	}
}

type TraderAgentMetrics struct {
	mu                sync.RWMutex
	TotalDecisions    int64            `json:"total_decisions"`
	TradesExecuted    int64            `json:"trades_executed"`
	TradesSkipped     int64            `json:"trades_skipped"`
	Wins              int64            `json:"wins"`
	Losses            int64            `json:"losses"`
	TotalPnL          float64          `json:"total_pnl"`
	AvgConfidence     float64          `json:"avg_confidence"`
	DecisionsByAction map[string]int64 `json:"decisions_by_action"`
	DecisionsBySymbol map[string]int64 `json:"decisions_by_symbol"`
}

func (m *TraderAgentMetrics) IncrementTotal() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.TotalDecisions++
}

func (m *TraderAgentMetrics) IncrementExecuted() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.TradesExecuted++
}

func (m *TraderAgentMetrics) IncrementSkipped() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.TradesSkipped++
}

func (m *TraderAgentMetrics) RecordWin(pnl float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Wins++
	m.TotalPnL += pnl
}

func (m *TraderAgentMetrics) RecordLoss(pnl float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Losses++
	m.TotalPnL += pnl
}

func (m *TraderAgentMetrics) UpdateAvgConfidence(confidence float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.TotalDecisions == 1 {
		m.AvgConfidence = confidence
	} else {
		m.AvgConfidence = (m.AvgConfidence*float64(m.TotalDecisions-1) + confidence) / float64(m.TotalDecisions)
	}
}

func (m *TraderAgentMetrics) IncrementByAction(action TradingAction) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.DecisionsByAction == nil {
		m.DecisionsByAction = make(map[string]int64)
	}
	m.DecisionsByAction[string(action)]++
}

func (m *TraderAgentMetrics) IncrementBySymbol(symbol string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.DecisionsBySymbol == nil {
		m.DecisionsBySymbol = make(map[string]int64)
	}
	m.DecisionsBySymbol[symbol]++
}

func (m *TraderAgentMetrics) GetMetrics() TraderAgentMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	actionCopy := make(map[string]int64, len(m.DecisionsByAction))
	for k, v := range m.DecisionsByAction {
		actionCopy[k] = v
	}

	symbolCopy := make(map[string]int64, len(m.DecisionsBySymbol))
	for k, v := range m.DecisionsBySymbol {
		symbolCopy[k] = v
	}

	return TraderAgentMetrics{
		TotalDecisions:    m.TotalDecisions,
		TradesExecuted:    m.TradesExecuted,
		TradesSkipped:     m.TradesSkipped,
		Wins:              m.Wins,
		Losses:            m.Losses,
		TotalPnL:          m.TotalPnL,
		AvgConfidence:     m.AvgConfidence,
		DecisionsByAction: actionCopy,
		DecisionsBySymbol: symbolCopy,
	}
}

type TraderAgent struct {
	config       TraderAgentConfig
	metrics      TraderAgentMetrics
	lastDecision map[string]time.Time
	mu           sync.RWMutex
}

func NewTraderAgent(config TraderAgentConfig) *TraderAgent {
	return &TraderAgent{
		config:       config,
		lastDecision: make(map[string]time.Time),
		metrics:      TraderAgentMetrics{DecisionsByAction: make(map[string]int64), DecisionsBySymbol: make(map[string]int64)},
	}
}

func (t *TraderAgent) MakeDecision(ctx context.Context, market MarketContext, portfolio PortfolioState) (*TradingDecision, error) {
	t.metrics.IncrementTotal()
	t.metrics.IncrementBySymbol(market.Symbol)

	decision := &TradingDecision{
		ID:        generateTraderID(),
		Symbol:    market.Symbol,
		CreatedAt: time.Now().UTC(),
		Metadata:  make(map[string]string),
	}

	decision.Confidence = t.calculateConfidence(market)
	decision.RiskScore = calculateTradeRiskScore(market, portfolio)

	if decision.Confidence < t.config.MinConfidence {
		decision.Action = ActionWait
		decision.Reasoning = fmt.Sprintf("Confidence %.2f below minimum %.2f", decision.Confidence, t.config.MinConfidence)
		t.metrics.IncrementSkipped()
		return decision, nil
	}

	if portfolio.OpenPositions >= t.config.MaxOpenPositions {
		decision.Action = ActionHold
		decision.Reasoning = fmt.Sprintf("Maximum positions (%d) reached", t.config.MaxOpenPositions)
		t.metrics.IncrementSkipped()
		return decision, nil
	}

	if portfolio.AvailableCash < portfolio.TotalValue*t.config.MaxRiskPerTrade {
		decision.Action = ActionHold
		decision.Reasoning = "Insufficient capital for trade"
		t.metrics.IncrementSkipped()
		return decision, nil
	}

	if t.isInCooldown(market.Symbol) {
		decision.Action = ActionWait
		decision.Reasoning = "In cooldown period"
		t.metrics.IncrementSkipped()
		return decision, nil
	}

	decision = t.determineAction(decision, market, portfolio)
	decision = t.calculatePositionSizing(decision, market, portfolio)

	t.metrics.UpdateAvgConfidence(decision.Confidence)
	t.metrics.IncrementByAction(decision.Action)
	t.metrics.IncrementExecuted()

	t.mu.Lock()
	t.lastDecision[market.Symbol] = time.Now()
	t.mu.Unlock()

	return decision, nil
}

func (t *TraderAgent) calculateConfidence(market MarketContext) float64 {
	if len(market.Signals) == 0 {
		return 0.0
	}

	weightedSum := 0.0
	totalWeight := 0.0
	for _, signal := range market.Signals {
		weightedSum += signal.Value * signal.Weight
		totalWeight += signal.Weight
	}

	if totalWeight == 0 {
		return 0.0
	}

	score := weightedSum / totalWeight
	confidence := (score + 1) / 2
	if confidence > 1.0 {
		confidence = 1.0
	}
	if confidence < 0.0 {
		confidence = 0.0
	}
	return confidence
}

func (t *TraderAgent) determineAction(decision *TradingDecision, market MarketContext, portfolio PortfolioState) *TradingDecision {
	bullishCount := 0
	bearishCount := 0

	for _, signal := range market.Signals {
		switch signal.Direction {
		case "bullish":
			bullishCount++
		case "bearish":
			bearishCount++
		}
	}

	if bullishCount > bearishCount && decision.Confidence >= t.config.MinConfidence {
		decision.Action = ActionOpenLong
		decision.Side = SideLong
		decision.Reasoning = fmt.Sprintf("Bullish signal with %.0f%% confidence", decision.Confidence*100)
	} else if bearishCount > bullishCount && decision.Confidence >= t.config.MinConfidence {
		decision.Action = ActionOpenShort
		decision.Side = SideShort
		decision.Reasoning = fmt.Sprintf("Bearish signal with %.0f%% confidence", decision.Confidence*100)
	} else {
		decision.Action = ActionHold
		decision.Reasoning = "Market conditions suggest holding"
	}

	return decision
}

func (t *TraderAgent) calculatePositionSizing(decision *TradingDecision, market MarketContext, portfolio PortfolioState) *TradingDecision {
	if decision.Action == ActionHold || decision.Action == ActionWait {
		return decision
	}

	baseSize := t.config.MaxPositionSize
	switch t.config.Mode {
	case ModeConservative:
		baseSize *= 0.5
	case ModeAggressive:
		baseSize *= 1.5
	}

	riskAdjustment := 1.0 - decision.RiskScore
	decision.SizePercent = baseSize * riskAdjustment * decision.Confidence

	if decision.SizePercent > t.config.MaxPositionSize {
		decision.SizePercent = t.config.MaxPositionSize
	}

	decision.EntryPrice = market.CurrentPrice

	// CRITICAL FIX: StopLoss/TakeProfit calculations must account for position side
	if decision.Side == SideShort {
		// For short positions: stop loss is above entry, take profit is below entry
		decision.StopLoss = decision.EntryPrice * (1 + t.config.StopLossPercent/100)
		decision.TakeProfit = decision.EntryPrice * (1 - t.config.TakeProfitPercent/100)
		// Expected return is positive when price goes down for shorts
		decision.ExpectedReturn = t.config.TakeProfitPercent * decision.SizePercent
	} else {
		// For long positions: stop loss is below entry, take profit is above entry
		decision.StopLoss = decision.EntryPrice * (1 - t.config.StopLossPercent/100)
		decision.TakeProfit = decision.EntryPrice * (1 + t.config.TakeProfitPercent/100)
		decision.ExpectedReturn = t.config.TakeProfitPercent * decision.SizePercent
	}

	return decision
}

func (t *TraderAgent) isInCooldown(symbol string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	lastTime, exists := t.lastDecision[symbol]
	if !exists {
		return false
	}
	return time.Since(lastTime) < t.config.CooldownPeriod
}

func (t *TraderAgent) GetMetrics() TraderAgentMetrics {
	return t.metrics.GetMetrics()
}

func (t *TraderAgent) SetConfig(config TraderAgentConfig) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.config = config
}

func (t *TraderAgent) GetConfig() TraderAgentConfig {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.config
}

func (t *TraderAgent) ShouldExecute(decision *TradingDecision) bool {
	if decision.Action == ActionHold || decision.Action == ActionWait {
		return false
	}
	if decision.Confidence < t.config.MinConfidence {
		return false
	}
	if decision.SizePercent <= 0 {
		return false
	}
	return true
}

func (t *TraderAgent) GetWinRate() float64 {
	metrics := t.metrics.GetMetrics()
	total := metrics.Wins + metrics.Losses
	if total == 0 {
		return 0
	}
	return float64(metrics.Wins) / float64(total)
}

func generateTraderID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("trade_%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("trade_%d_%s", time.Now().UnixNano(), hex.EncodeToString(b))
}

func calculateTradeRiskScore(market MarketContext, portfolio PortfolioState) float64 {
	riskScore := 0.0

	if market.Volatility > 0.5 {
		riskScore += 0.3
	}
	if market.Liquidity < 0.3 {
		riskScore += 0.2
	}
	if portfolio.CurrentDrawdown > 0.1 {
		riskScore += 0.3
	}

	if riskScore > 1.0 {
		riskScore = 1.0
	}
	return riskScore
}
