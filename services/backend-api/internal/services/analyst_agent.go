package services

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type AnalystRole string

const (
	AnalystRoleTechnical   AnalystRole = "technical"
	AnalystRoleSentiment   AnalystRole = "sentiment"
	AnalystRoleOnChain     AnalystRole = "onchain"
	AnalystRoleFundamental AnalystRole = "fundamental"
)

type AnalystRecommendation string

const (
	RecommendationBuy   AnalystRecommendation = "buy"
	RecommendationSell  AnalystRecommendation = "sell"
	RecommendationHold  AnalystRecommendation = "hold"
	RecommendationWatch AnalystRecommendation = "watch"
	RecommendationAvoid AnalystRecommendation = "avoid"
)

type MarketCondition string

const (
	ConditionBullish  MarketCondition = "bullish"
	ConditionBearish  MarketCondition = "bearish"
	ConditionNeutral  MarketCondition = "neutral"
	ConditionVolatile MarketCondition = "volatile"
	ConditionTrending MarketCondition = "trending"
)

type AnalystAnalysis struct {
	ID             string                `json:"id"`
	Symbol         string                `json:"symbol"`
	Role           AnalystRole           `json:"role"`
	Condition      MarketCondition       `json:"condition"`
	Recommendation AnalystRecommendation `json:"recommendation"`
	Confidence     float64               `json:"confidence"`
	Score          float64               `json:"score"`
	Signals        []AnalystSignal       `json:"signals"`
	Summary        string                `json:"summary"`
	RiskLevel      string                `json:"risk_level"`
	AnalyzedAt     time.Time             `json:"analyzed_at"`
	Metadata       map[string]string     `json:"metadata,omitempty"`
}

type AnalystSignal struct {
	Name        string  `json:"name"`
	Value       float64 `json:"value"`
	Weight      float64 `json:"weight"`
	Direction   string  `json:"direction"`
	Description string  `json:"description"`
}

type AnalystAgentConfig struct {
	MinConfidence    float64       `json:"min_confidence"`
	SignalThreshold  float64       `json:"signal_threshold"`
	MaxRiskScore     float64       `json:"max_risk_score"`
	AnalysisCooldown time.Duration `json:"analysis_cooldown"`
}

func DefaultAnalystAgentConfig() AnalystAgentConfig {
	return AnalystAgentConfig{
		MinConfidence:    0.6,
		SignalThreshold:  0.5,
		MaxRiskScore:     0.8,
		AnalysisCooldown: 5 * time.Minute,
	}
}

type AnalystAgentMetrics struct {
	mu               sync.RWMutex
	TotalAnalyses    int64            `json:"total_analyses"`
	BuySignals       int64            `json:"buy_signals"`
	SellSignals      int64            `json:"sell_signals"`
	HoldSignals      int64            `json:"hold_signals"`
	AvgConfidence    float64          `json:"avg_confidence"`
	AnalysesBySymbol map[string]int64 `json:"analyses_by_symbol"`
	AnalysesByRole   map[string]int64 `json:"analyses_by_role"`
}

func (m *AnalystAgentMetrics) IncrementTotal() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.TotalAnalyses++
}

func (m *AnalystAgentMetrics) IncrementBuy() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.BuySignals++
}

func (m *AnalystAgentMetrics) IncrementSell() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SellSignals++
}

func (m *AnalystAgentMetrics) IncrementHold() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.HoldSignals++
}

func (m *AnalystAgentMetrics) UpdateAvgConfidence(confidence float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.TotalAnalyses == 1 {
		m.AvgConfidence = confidence
	} else {
		m.AvgConfidence = (m.AvgConfidence*float64(m.TotalAnalyses-1) + confidence) / float64(m.TotalAnalyses)
	}
}

func (m *AnalystAgentMetrics) IncrementBySymbol(symbol string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.AnalysesBySymbol == nil {
		m.AnalysesBySymbol = make(map[string]int64)
	}
	m.AnalysesBySymbol[symbol]++
}

func (m *AnalystAgentMetrics) IncrementByRole(role AnalystRole) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.AnalysesByRole == nil {
		m.AnalysesByRole = make(map[string]int64)
	}
	m.AnalysesByRole[string(role)]++
}

func (m *AnalystAgentMetrics) GetMetrics() AnalystAgentMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return AnalystAgentMetrics{
		TotalAnalyses:    m.TotalAnalyses,
		BuySignals:       m.BuySignals,
		SellSignals:      m.SellSignals,
		HoldSignals:      m.HoldSignals,
		AvgConfidence:    m.AvgConfidence,
		AnalysesBySymbol: m.AnalysesBySymbol,
		AnalysesByRole:   m.AnalysesByRole,
	}
}

type AnalystAgent struct {
	config  AnalystAgentConfig
	metrics AnalystAgentMetrics
	mu      sync.RWMutex
}

func NewAnalystAgent(config AnalystAgentConfig) *AnalystAgent {
	return &AnalystAgent{
		config:  config,
		metrics: AnalystAgentMetrics{AnalysesBySymbol: make(map[string]int64), AnalysesByRole: make(map[string]int64)},
	}
}

func (a *AnalystAgent) Analyze(ctx context.Context, symbol string, role AnalystRole, signals []AnalystSignal) (*AnalystAnalysis, error) {
	a.metrics.IncrementTotal()
	a.metrics.IncrementBySymbol(symbol)
	a.metrics.IncrementByRole(role)

	analysis := &AnalystAnalysis{
		ID:         generateAnalystID(),
		Symbol:     symbol,
		Role:       role,
		Signals:    signals,
		AnalyzedAt: time.Now().UTC(),
		Metadata:   make(map[string]string),
	}

	weightedScore := 0.0
	totalWeight := 0.0
	bullishCount := 0
	bearishCount := 0

	for _, signal := range signals {
		weightedScore += signal.Value * signal.Weight
		totalWeight += signal.Weight
		switch signal.Direction {
		case "bullish":
			bullishCount++
		case "bearish":
			bearishCount++
		}
	}

	if totalWeight > 0 {
		analysis.Score = weightedScore / totalWeight
	}

	analysis.Confidence = calculateConfidence(signals, analysis.Score)
	a.metrics.UpdateAvgConfidence(analysis.Confidence)

	if bullishCount > bearishCount && analysis.Score > a.config.SignalThreshold {
		analysis.Recommendation = RecommendationBuy
		a.metrics.IncrementBuy()
	} else if bearishCount > bullishCount && analysis.Score < -a.config.SignalThreshold {
		analysis.Recommendation = RecommendationSell
		a.metrics.IncrementSell()
	} else {
		analysis.Recommendation = RecommendationHold
		a.metrics.IncrementHold()
	}

	analysis.Condition = determineCondition(analysis.Score, signals)
	analysis.RiskLevel = calculateRiskLevel(signals)

	if analysis.Confidence < a.config.MinConfidence {
		analysis.Recommendation = RecommendationWatch
	}

	analysis.Summary = generateSummary(analysis)

	return analysis, nil
}

func (a *AnalystAgent) GetMetrics() AnalystAgentMetrics {
	return a.metrics.GetMetrics()
}

func (a *AnalystAgent) SetConfig(config AnalystAgentConfig) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.config = config
}

func (a *AnalystAgent) GetConfig() AnalystAgentConfig {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.config
}

func (a *AnalystAgent) QuickAnalyze(symbol string, signals []AnalystSignal) AnalystRecommendation {
	analysis, err := a.Analyze(context.Background(), symbol, AnalystRoleTechnical, signals)
	if err != nil {
		return RecommendationHold
	}
	return analysis.Recommendation
}

func (a *AnalystAgent) ShouldTrade(analysis *AnalystAnalysis) bool {
	if analysis.Confidence < a.config.MinConfidence {
		return false
	}
	if analysis.RiskLevel == "high" {
		return false
	}
	return analysis.Recommendation == RecommendationBuy || analysis.Recommendation == RecommendationSell
}

func generateAnalystID() string {
	return fmt.Sprintf("analyst_%d", time.Now().UnixNano())
}

func calculateConfidence(signals []AnalystSignal, score float64) float64 {
	if len(signals) == 0 {
		return 0.0
	}

	signalAgreement := 0.0
	expectedDirection := "neutral"
	if score > 0 {
		expectedDirection = "bullish"
	} else if score < 0 {
		expectedDirection = "bearish"
	}

	for _, s := range signals {
		if s.Direction == expectedDirection {
			signalAgreement++
		}
	}

	agreementRatio := signalAgreement / float64(len(signals))
	confidence := agreementRatio * 0.7

	absScore := score
	if absScore < 0 {
		absScore = -absScore
	}
	confidence += absScore * 0.3

	if confidence > 1.0 {
		confidence = 1.0
	}

	return confidence
}

func determineCondition(score float64, signals []AnalystSignal) MarketCondition {
	volatility := 0.0
	for _, s := range signals {
		if s.Name == "volatility" || s.Name == "atr" {
			volatility = s.Value
		}
	}

	if volatility > 0.7 {
		return ConditionVolatile
	}

	if score > 0.5 {
		return ConditionBullish
	} else if score < -0.5 {
		return ConditionBearish
	}

	trendingCount := 0
	for _, s := range signals {
		if s.Name == "trend" || s.Name == "adx" {
			trendingCount++
		}
	}
	if trendingCount > 0 {
		return ConditionTrending
	}

	return ConditionNeutral
}

func calculateRiskLevel(signals []AnalystSignal) string {
	riskScore := 0.0
	for _, s := range signals {
		if s.Name == "volatility" {
			riskScore += s.Value * 0.4
		}
		if s.Name == "volume" && s.Value < 0.3 {
			riskScore += 0.2
		}
		if s.Name == "liquidity" && s.Value < 0.5 {
			riskScore += 0.2
		}
	}

	if riskScore > 0.6 {
		return "high"
	} else if riskScore > 0.3 {
		return "medium"
	}
	return "low"
}

func generateSummary(analysis *AnalystAnalysis) string {
	condition := string(analysis.Condition)
	recommendation := string(analysis.Recommendation)
	confidence := fmt.Sprintf("%.0f%%", analysis.Confidence*100)

	return fmt.Sprintf("%s analysis for %s: %s condition, recommend %s with %s confidence",
		analysis.Role, analysis.Symbol, condition, recommendation, confidence)
}
