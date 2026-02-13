package risk

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

// RiskManagerAgent roles
type RiskManagerRole string

const (
	RiskManagerRolePortfolio RiskManagerRole = "portfolio"
	RiskManagerRolePosition  RiskManagerRole = "position"
	RiskManagerRoleTrading   RiskManagerRole = "trading"
	RiskManagerRoleEmergency RiskManagerRole = "emergency"
)

// RiskLevel types
type RiskLevel string

const (
	RiskLevelLow     RiskLevel = "low"
	RiskLevelMedium  RiskLevel = "medium"
	RiskLevelHigh    RiskLevel = "high"
	RiskLevelExtreme RiskLevel = "extreme"
)

// RiskAction types
type RiskAction string

const (
	RiskActionApprove   RiskAction = "approve"
	RiskActionWarning   RiskAction = "warning"
	RiskActionBlock     RiskAction = "block"
	RiskActionReduce    RiskAction = "reduce"
	RiskActionClose     RiskAction = "close"
	RiskActionEmergency RiskAction = "emergency"
)

// RiskAssessment contains the result of risk evaluation
type RiskAssessment struct {
	ID              string            `json:"id"`
	Role            RiskManagerRole   `json:"role"`
	Symbol          string            `json:"symbol"`
	Action          RiskAction        `json:"action"`
	RiskLevel       RiskLevel         `json:"risk_level"`
	Confidence      float64           `json:"confidence"`
	Score           float64           `json:"score"`
	Reasons         []string          `json:"reasons"`
	Recommendations []string          `json:"recommendations"`
	MaxPositionSize decimal.Decimal   `json:"max_position_size,omitempty"`
	StopLossPct     float64           `json:"stop_loss_pct,omitempty"`
	TakeProfitPct   float64           `json:"take_profit_pct,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
	AssessedAt      time.Time         `json:"assessed_at"`
}

// RiskSignal represents a risk signal to evaluate
type RiskSignal struct {
	Name        string  `json:"name"`
	Value       float64 `json:"value"`
	Weight      float64 `json:"weight"`
	Threshold   float64 `json:"threshold"`
	Description string  `json:"description"`
}

// RiskManagerConfig for the RiskManagerAgent
type RiskManagerConfig struct {
	MaxPortfolioRisk     float64         `json:"max_portfolio_risk"`     // Max % of portfolio at risk (0.0-1.0)
	MaxPositionRisk      float64         `json:"max_position_risk"`      // Max % of position at risk (0.0-1.0)
	MaxDailyLoss         decimal.Decimal `json:"max_daily_loss"`         // Max daily loss allowed
	MaxDrawdown          float64         `json:"max_drawdown"`           // Max drawdown % (0.0-1.0)
	MinRiskRewardRatio   float64         `json:"min_risk_reward_ratio"`  // Minimum risk:reward ratio
	MaxConcurrentTrades  int             `json:"max_concurrent_trades"`  // Max open positions
	EmergencyThreshold   float64         `json:"emergency_threshold"`    // Trigger emergency at this drawdown
	ConsecutiveLossLimit int             `json:"consecutive_loss_limit"` // Max consecutive losses before pause
	PositionSizeLimit    decimal.Decimal `json:"position_size_limit"`    // Max position size in quote currency
}

// DefaultRiskManagerConfig returns sensible defaults
func DefaultRiskManagerConfig() RiskManagerConfig {
	return RiskManagerConfig{
		MaxPortfolioRisk:     0.1,                        // 10% max portfolio risk
		MaxPositionRisk:      0.02,                       // 2% max position risk
		MaxDailyLoss:         decimal.NewFromFloat(100),  // $100 max daily loss
		MaxDrawdown:          0.15,                       // 15% max drawdown
		MinRiskRewardRatio:   2.0,                        // 1:2 minimum risk:reward
		MaxConcurrentTrades:  5,                          // Max 5 open positions
		EmergencyThreshold:   0.20,                       // 20% drawdown triggers emergency
		ConsecutiveLossLimit: 3,                          // Pause after 3 consecutive losses
		PositionSizeLimit:    decimal.NewFromFloat(1000), // $1000 max position
	}
}

// RiskManagerMetrics tracks agent performance
type RiskManagerMetrics struct {
	mu                  sync.RWMutex
	TotalAssessments    int64            `json:"total_assessments"`
	ApprovedTrades      int64            `json:"approved_trades"`
	BlockedTrades       int64            `json:"blocked_trades"`
	WarningsIssued      int64            `json:"warnings_issued"`
	EmergencyTriggers   int64            `json:"emergency_triggers"`
	AssessmentsByRole   map[string]int64 `json:"assessments_by_role"`
	AssessmentsBySymbol map[string]int64 `json:"assessments_by_symbol"`
}

func NewRiskManagerMetrics() RiskManagerMetrics {
	return RiskManagerMetrics{
		AssessmentsByRole:   make(map[string]int64),
		AssessmentsBySymbol: make(map[string]int64),
	}
}

func (m *RiskManagerMetrics) IncrementTotal() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.TotalAssessments++
}

func (m *RiskManagerMetrics) IncrementApproved() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ApprovedTrades++
}

func (m *RiskManagerMetrics) IncrementBlocked() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.BlockedTrades++
}

func (m *RiskManagerMetrics) IncrementWarnings() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.WarningsIssued++
}

func (m *RiskManagerMetrics) IncrementEmergency() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.EmergencyTriggers++
}

func (m *RiskManagerMetrics) IncrementByRole(role RiskManagerRole) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.AssessmentsByRole[string(role)]++
}

func (m *RiskManagerMetrics) IncrementBySymbol(symbol string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.AssessmentsBySymbol[symbol]++
}

func (m *RiskManagerMetrics) GetMetrics() RiskManagerMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return RiskManagerMetrics{
		TotalAssessments:    m.TotalAssessments,
		ApprovedTrades:      m.ApprovedTrades,
		BlockedTrades:       m.BlockedTrades,
		WarningsIssued:      m.WarningsIssued,
		EmergencyTriggers:   m.EmergencyTriggers,
		AssessmentsByRole:   m.AssessmentsByRole,
		AssessmentsBySymbol: m.AssessmentsBySymbol,
	}
}

// RiskManagerAgent evaluates trading risks and provides recommendations
type RiskManagerAgent struct {
	config  RiskManagerConfig
	metrics RiskManagerMetrics
}

func NewRiskManagerAgent(config RiskManagerConfig) *RiskManagerAgent {
	return &RiskManagerAgent{
		config:  config,
		metrics: NewRiskManagerMetrics(),
	}
}

// AssessPortfolioRisk evaluates overall portfolio risk
func (a *RiskManagerAgent) AssessPortfolioRisk(ctx context.Context, signals []RiskSignal) (*RiskAssessment, error) {
	assessment := &RiskAssessment{
		ID:              generateRiskID(),
		Role:            RiskManagerRolePortfolio,
		AssessedAt:      time.Now(),
		Reasons:         []string{},
		Recommendations: []string{},
		Metadata:        make(map[string]string),
	}

	score := calculateRiskScore(signals)
	assessment.Score = score
	assessment.Confidence = calculateRiskConfidence(signals)
	assessment.RiskLevel = determineRiskLevel(score)
	assessment.Action = determineAction(assessment.RiskLevel, score, a.config)

	// Generate reasons
	for _, signal := range signals {
		if signal.Value > signal.Threshold {
			assessment.Reasons = append(assessment.Reasons, signal.Description)
		}
	}

	// Generate recommendations based on risk level
	switch assessment.RiskLevel {
	case RiskLevelLow:
		assessment.Recommendations = append(assessment.Recommendations, "Portfolio risk is within acceptable limits")
	case RiskLevelMedium:
		assessment.Recommendations = append(assessment.Recommendations, "Consider reducing position sizes", "Monitor portfolio closely")
	case RiskLevelHigh:
		assessment.Recommendations = append(assessment.Recommendations, "Reduce exposure immediately", "Avoid new positions")
	case RiskLevelExtreme:
		assessment.Recommendations = append(assessment.Recommendations, "EMERGENCY: Close positions", "Halt all trading activity")
	}

	a.metrics.IncrementTotal()
	a.metrics.IncrementByRole(assessment.Role)

	return assessment, nil
}

// AssessPositionRisk evaluates risk for a specific position
func (a *RiskManagerAgent) AssessPositionRisk(ctx context.Context, symbol string, signals []RiskSignal, positionSize decimal.Decimal) (*RiskAssessment, error) {
	assessment := &RiskAssessment{
		ID:              generateRiskID(),
		Role:            RiskManagerRolePosition,
		Symbol:          symbol,
		AssessedAt:      time.Now(),
		Reasons:         []string{},
		Recommendations: []string{},
		Metadata:        make(map[string]string),
	}

	score := calculateRiskScore(signals)
	assessment.Score = score
	assessment.Confidence = calculateRiskConfidence(signals)
	assessment.RiskLevel = determineRiskLevel(score)
	assessment.Action = determineAction(assessment.RiskLevel, score, a.config)

	// Check position size limits
	if positionSize.GreaterThan(a.config.PositionSizeLimit) {
		assessment.Reasons = append(assessment.Reasons, fmt.Sprintf("Position size %s exceeds limit %s", positionSize.String(), a.config.PositionSizeLimit.String()))
		assessment.Action = RiskActionReduce
		assessment.MaxPositionSize = a.config.PositionSizeLimit
	}

	// Calculate recommended stop-loss and take-profit
	assessment.StopLossPct = a.config.MaxPositionRisk * 100
	assessment.TakeProfitPct = a.config.MinRiskRewardRatio * assessment.StopLossPct

	// Generate reasons
	for _, signal := range signals {
		if signal.Value > signal.Threshold {
			assessment.Reasons = append(assessment.Reasons, signal.Description)
		}
	}

	// Generate recommendations
	switch assessment.Action {
	case RiskActionApprove:
		assessment.Recommendations = append(assessment.Recommendations, "Position risk is acceptable")
	case RiskActionWarning:
		assessment.Recommendations = append(assessment.Recommendations, "Reduce position size", "Tighten stop-loss")
	case RiskActionBlock:
		assessment.Recommendations = append(assessment.Recommendations, "Do not enter position")
	case RiskActionReduce:
		assessment.Recommendations = append(assessment.Recommendations, fmt.Sprintf("Reduce position to max %s", a.config.PositionSizeLimit.String()))
	}

	a.metrics.IncrementTotal()
	a.metrics.IncrementByRole(assessment.Role)
	a.metrics.IncrementBySymbol(symbol)

	return assessment, nil
}

// AssessTradingRisk evaluates risk for a trade signal
func (a *RiskManagerAgent) AssessTradingRisk(ctx context.Context, symbol string, side string, signals []RiskSignal) (*RiskAssessment, error) {
	assessment := &RiskAssessment{
		ID:              generateRiskID(),
		Role:            RiskManagerRoleTrading,
		Symbol:          symbol,
		AssessedAt:      time.Now(),
		Reasons:         []string{},
		Recommendations: []string{},
		Metadata:        map[string]string{"side": side},
	}

	score := calculateRiskScore(signals)
	assessment.Score = score
	assessment.Confidence = calculateRiskConfidence(signals)
	assessment.RiskLevel = determineRiskLevel(score)
	assessment.Action = determineAction(assessment.RiskLevel, score, a.config)

	// Generate reasons
	for _, signal := range signals {
		if signal.Value > signal.Threshold {
			assessment.Reasons = append(assessment.Reasons, signal.Description)
		}
	}

	// Check risk:reward ratio
	for _, signal := range signals {
		if signal.Name == "risk_reward_ratio" && signal.Value < a.config.MinRiskRewardRatio {
			assessment.Reasons = append(assessment.Reasons, "Risk:reward ratio below minimum threshold")
			if assessment.Action == RiskActionApprove {
				assessment.Action = RiskActionWarning
			}
		}
	}

	// Generate recommendations
	switch assessment.Action {
	case RiskActionApprove:
		assessment.Recommendations = append(assessment.Recommendations, "Trade approved")
		a.metrics.IncrementApproved()
	case RiskActionWarning:
		assessment.Recommendations = append(assessment.Recommendations, "Trade approved with caution", "Monitor closely")
		a.metrics.IncrementWarnings()
	case RiskActionBlock:
		assessment.Recommendations = append(assessment.Recommendations, "Trade blocked - risk too high")
		a.metrics.IncrementBlocked()
	case RiskActionReduce:
		assessment.Recommendations = append(assessment.Recommendations, "Reduce position size to acceptable risk")
		a.metrics.IncrementWarnings()
	}

	a.metrics.IncrementTotal()
	a.metrics.IncrementByRole(assessment.Role)
	a.metrics.IncrementBySymbol(symbol)

	return assessment, nil
}

// CheckEmergencyConditions checks if emergency conditions are met
func (a *RiskManagerAgent) CheckEmergencyConditions(ctx context.Context, currentDrawdown float64, dailyLoss decimal.Decimal) (*RiskAssessment, error) {
	assessment := &RiskAssessment{
		ID:              generateRiskID(),
		Role:            RiskManagerRoleEmergency,
		AssessedAt:      time.Now(),
		Reasons:         []string{},
		Recommendations: []string{},
		Metadata:        make(map[string]string),
	}

	isEmergency := false

	// Check drawdown threshold
	if currentDrawdown >= a.config.EmergencyThreshold {
		assessment.Reasons = append(assessment.Reasons, fmt.Sprintf("Drawdown %.2f%% exceeds emergency threshold %.2f%%", currentDrawdown*100, a.config.EmergencyThreshold*100))
		isEmergency = true
	}

	// Check daily loss
	if dailyLoss.GreaterThanOrEqual(a.config.MaxDailyLoss) {
		assessment.Reasons = append(assessment.Reasons, fmt.Sprintf("Daily loss %s exceeds maximum %s", dailyLoss.String(), a.config.MaxDailyLoss.String()))
		isEmergency = true
	}

	if isEmergency {
		assessment.Action = RiskActionEmergency
		assessment.RiskLevel = RiskLevelExtreme
		assessment.Score = 1.0
		assessment.Confidence = 1.0
		assessment.Recommendations = append(assessment.Recommendations,
			"EMERGENCY: All positions should be closed immediately",
			"Halt all trading activity",
			"Review strategy before resuming")
		a.metrics.IncrementEmergency()
	} else {
		assessment.Action = RiskActionApprove
		assessment.RiskLevel = RiskLevelLow
		assessment.Score = currentDrawdown
		assessment.Confidence = 0.9
		assessment.Recommendations = append(assessment.Recommendations, "Normal operations can continue")
	}

	a.metrics.IncrementTotal()
	a.metrics.IncrementByRole(assessment.Role)

	return assessment, nil
}

// ShouldTrade determines if a trade should be allowed based on risk assessment
func (a *RiskManagerAgent) ShouldTrade(assessment *RiskAssessment) bool {
	switch assessment.Action {
	case RiskActionApprove, RiskActionWarning:
		return true
	default:
		return false
	}
}

// GetMetrics returns the current metrics
func (a *RiskManagerAgent) GetMetrics() RiskManagerMetrics {
	return a.metrics.GetMetrics()
}

// SetConfig updates the agent configuration
func (a *RiskManagerAgent) SetConfig(config RiskManagerConfig) {
	a.config = config
}

// GetConfig returns the current configuration
func (a *RiskManagerAgent) GetConfig() RiskManagerConfig {
	return a.config
}

// Helper functions

func generateRiskID() string {
	return fmt.Sprintf("risk_%d", time.Now().UnixNano())
}

func calculateRiskScore(signals []RiskSignal) float64 {
	if len(signals) == 0 {
		return 0.0
	}

	var weightedSum float64
	var totalWeight float64

	for _, signal := range signals {
		weightedSum += signal.Value * signal.Weight
		totalWeight += signal.Weight
	}

	if totalWeight == 0 {
		return 0.0
	}

	return weightedSum / totalWeight
}

func calculateRiskConfidence(signals []RiskSignal) float64 {
	if len(signals) == 0 {
		return 0.0
	}

	// More signals = higher confidence
	// Normalize to 0.5-1.0 range
	confidence := 0.5 + (float64(len(signals)) * 0.05)
	if confidence > 1.0 {
		confidence = 1.0
	}

	return confidence
}

func determineRiskLevel(score float64) RiskLevel {
	switch {
	case score < 0.2:
		return RiskLevelLow
	case score < 0.5:
		return RiskLevelMedium
	case score < 0.8:
		return RiskLevelHigh
	default:
		return RiskLevelExtreme
	}
}

func determineAction(riskLevel RiskLevel, score float64, config RiskManagerConfig) RiskAction {
	switch riskLevel {
	case RiskLevelLow:
		return RiskActionApprove
	case RiskLevelMedium:
		if score > 0.4 {
			return RiskActionWarning
		}
		return RiskActionApprove
	case RiskLevelHigh:
		if score > 0.7 {
			return RiskActionBlock
		}
		return RiskActionReduce
	default:
		return RiskActionBlock
	}
}
