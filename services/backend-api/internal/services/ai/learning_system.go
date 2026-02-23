package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// InMemoryLearningSystem implements LearningSystem with in-memory storage
type InMemoryLearningSystem struct {
	mu        sync.RWMutex
	decisions map[string]*DecisionRecord
	optimalDB *OptimalStrategyDB
	dataDir   string
}

// OptimalStrategyDB stores learned optimal strategies
type OptimalStrategyDB struct {
	mu         sync.RWMutex
	strategies map[string]*OptimalStrategy
}

// OptimalStrategy represents learned optimal parameters for a strategy
type OptimalStrategy struct {
	Strategy        string                 `json:"strategy"`
	Symbol          string                 `json:"symbol"`
	WinRate         float64                `json:"win_rate"`
	AvgProfit       float64                `json:"avg_profit"`
	OptimalParams   map[string]interface{} `json:"optimal_params"`
	BestConditions  []string               `json:"best_conditions"`
	AvoidConditions []string               `json:"avoid_conditions"`
	UpdatedAt       time.Time              `json:"updated_at"`
}

// NewInMemoryLearningSystem creates a new learning system
func NewInMemoryLearningSystem() *InMemoryLearningSystem {
	dataDir := filepath.Join("data", "ai_learning")
	if err := os.MkdirAll(dataDir, 0750); err != nil {
		log.Printf("Failed to create AI learning data directory: %v", err)
	}

	return &InMemoryLearningSystem{
		decisions: make(map[string]*DecisionRecord),
		optimalDB: &OptimalStrategyDB{
			strategies: make(map[string]*OptimalStrategy),
		},
		dataDir: dataDir,
	}
}

// RecordDecision stores a trading decision
func (ls *InMemoryLearningSystem) RecordDecision(ctx context.Context, record *DecisionRecord) error {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	ls.decisions[record.ID] = record

	// Persist to disk
	return ls.persistDecision(record)
}

// persistDecision saves decision to disk
func (ls *InMemoryLearningSystem) persistDecision(record *DecisionRecord) error {
	filename := filepath.Join(ls.dataDir, fmt.Sprintf("decision_%s.json", record.ID))
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filename, data, 0600)
}

// GetSimilarDecisions retrieves similar past decisions
func (ls *InMemoryLearningSystem) GetSimilarDecisions(ctx context.Context, symbol string, limit int) ([]*DecisionRecord, error) {
	ls.mu.RLock()
	defer ls.mu.RUnlock()

	var matches []*DecisionRecord

	for _, record := range ls.decisions {
		if record.MarketState.Symbol == symbol && record.Outcome != "" {
			matches = append(matches, record)
		}
	}

	// Sort by most recent
	for i := 0; i < len(matches)-1; i++ {
		for j := i + 1; j < len(matches); j++ {
			if matches[j].Timestamp.After(matches[i].Timestamp) {
				matches[i], matches[j] = matches[j], matches[i]
			}
		}
	}

	if len(matches) > limit {
		matches = matches[:limit]
	}

	return matches, nil
}

// RecordOutcome stores the outcome of a trade
func (ls *InMemoryLearningSystem) RecordOutcome(ctx context.Context, decisionID string, outcome *TradeOutcome) error {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	decision, ok := ls.decisions[decisionID]
	if !ok {
		return fmt.Errorf("decision not found: %s", decisionID)
	}

	decision.Outcome = outcome.Result
	decision.PnL = outcome.PnL
	now := time.Now()
	decision.CompletedAt = &now

	// Update optimal strategy
	ls.updateOptimalStrategy(decision)

	// Re-persist updated decision
	return ls.persistDecision(decision)
}

// updateOptimalStrategy learns from decision outcome
func (ls *InMemoryLearningSystem) updateOptimalStrategy(decision *DecisionRecord) {
	key := fmt.Sprintf("%s_%s", decision.Strategy, decision.MarketState.Symbol)

	ls.optimalDB.mu.Lock()
	defer ls.optimalDB.mu.Unlock()

	strategy, exists := ls.optimalDB.strategies[key]
	if !exists {
		strategy = &OptimalStrategy{
			Strategy:       decision.Strategy,
			Symbol:         decision.MarketState.Symbol,
			OptimalParams:  make(map[string]interface{}),
			BestConditions: []string{},
		}
	}

	// Update win rate
	if decision.Outcome == "win" {
		strategy.WinRate = (strategy.WinRate*float64(len(ls.getStrategyDecisions(key))) + 1) / float64(len(ls.getStrategyDecisions(key))+1)
		strategy.AvgProfit = (strategy.AvgProfit*float64(len(ls.getStrategyDecisions(key))) + decision.PnL) / float64(len(ls.getStrategyDecisions(key))+1)
	}

	strategy.UpdatedAt = time.Now()
	ls.optimalDB.strategies[key] = strategy
}

// getStrategyDecisions gets all decisions for a strategy
func (ls *InMemoryLearningSystem) getStrategyDecisions(key string) []*DecisionRecord {
	var result []*DecisionRecord
	for _, d := range ls.decisions {
		if fmt.Sprintf("%s_%s", d.Strategy, d.MarketState.Symbol) == key {
			result = append(result, d)
		}
	}
	return result
}

// GetOptimalStrategy returns learned optimal strategy
func (ls *InMemoryLearningSystem) GetOptimalStrategy(strategy, symbol string) *OptimalStrategy {
	key := fmt.Sprintf("%s_%s", strategy, symbol)

	ls.optimalDB.mu.RLock()
	defer ls.optimalDB.mu.RUnlock()

	return ls.optimalDB.strategies[key]
}

// GenerateInsights creates insights from learning
func (ls *InMemoryLearningSystem) GenerateInsights(symbol string) *AIInsights {
	ls.mu.RLock()
	defer ls.mu.RUnlock()

	var wins, losses int
	var totalPnL float64

	for _, d := range ls.decisions {
		if d.MarketState.Symbol == symbol && d.Outcome != "" {
			switch d.Outcome {
			case "win":
				wins++
			case "loss":
				losses++
			}
			totalPnL += d.PnL
		}
	}

	total := wins + losses
	if total == 0 {
		return &AIInsights{
			Symbol:      symbol,
			TotalTrades: 0,
			Message:     "No trading history yet",
		}
	}

	winRate := float64(wins) / float64(total) * 100

	return &AIInsights{
		Symbol:         symbol,
		TotalTrades:    total,
		WinRate:        winRate,
		TotalPnL:       totalPnL,
		AvgPnL:         totalPnL / float64(total),
		Message:        ls.generateInsightMessage(winRate, totalPnL),
		Recommendation: ls.generateRecommendation(winRate),
	}
}

// AIInsights represents AI learning insights
type AIInsights struct {
	Symbol         string  `json:"symbol"`
	TotalTrades    int     `json:"total_trades"`
	WinRate        float64 `json:"win_rate"`
	TotalPnL       float64 `json:"total_pnl"`
	AvgPnL         float64 `json:"avg_pnl"`
	Message        string  `json:"message"`
	Recommendation string  `json:"recommendation"`
}

// generateInsightMessage creates human-readable insight
func (ls *InMemoryLearningSystem) generateInsightMessage(winRate, totalPnL float64) string {
	if winRate > 60 && totalPnL > 0 {
		return fmt.Sprintf("Strong performance with %.1f%% win rate and $%.2f profit", winRate, totalPnL)
	} else if winRate > 50 && totalPnL > 0 {
		return fmt.Sprintf("Good performance with %.1f%% win rate and $%.2f profit", winRate, totalPnL)
	} else if winRate < 40 {
		return fmt.Sprintf("Performance needs improvement with %.1f%% win rate", winRate)
	}
	return fmt.Sprintf("Performance is neutral with %.1f%% win rate", winRate)
}

// generateRecommendation creates trading recommendation
func (ls *InMemoryLearningSystem) generateRecommendation(winRate float64) string {
	if winRate > 70 {
		return "Increase position sizes gradually"
	} else if winRate > 55 {
		return "Continue current strategy"
	} else if winRate > 40 {
		return "Review and adjust strategy parameters"
	}
	return "Consider pausing and re-evaluating strategy"
}
