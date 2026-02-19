package services

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/irfndi/neuratrade/internal/models"
	"github.com/shopspring/decimal"
)

type AIEvaluator interface {
	EvaluateOpportunity(ctx context.Context, opportunity *models.ArbitrageOpportunity, context string) (shouldExecute bool, confidence float64, reasoning string, err error)
}

type ArbitrageExecutionBridge struct {
	db               DBPool
	questEngine      *QuestEngine
	signalAggregator SignalAggregatorInterface
	qualityScorer    SignalQualityScorerInterface
	aiEvaluator      AIEvaluator
	logger           interface{ Info(string) }
}

func NewArbitrageExecutionBridge(
	db DBPool,
	questEngine *QuestEngine,
	signalAggregator SignalAggregatorInterface,
	qualityScorer SignalQualityScorerInterface,
) *ArbitrageExecutionBridge {
	return &ArbitrageExecutionBridge{
		db:               db,
		questEngine:      questEngine,
		signalAggregator: signalAggregator,
		qualityScorer:    qualityScorer,
		logger:           &BasicLogger{},
	}
}

func (aeb *ArbitrageExecutionBridge) SetAIEvaluator(evaluator AIEvaluator) {
	aeb.aiEvaluator = evaluator
	aeb.logger.Info("AI evaluator configured for arbitrage execution bridge")
}

// BasicLogger implements a simple logger for the bridge
type BasicLogger struct{}

func (l *BasicLogger) Info(msg string) {
	log.Printf("[ArbitrageBridge] %s", msg)
}

// Start begins monitoring for arbitrage opportunities and triggering executions
func (aeb *ArbitrageExecutionBridge) Start(ctx context.Context) error {
	aeb.logger.Info("Starting arbitrage execution bridge")

	ticker := time.NewTicker(30 * time.Second) // Check every 30 seconds
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			aeb.logger.Info("Arbitrage execution bridge stopped")
			return nil
		case <-ticker.C:
			if err := aeb.processNewArbitrageOpportunities(ctx); err != nil {
				log.Printf("Error processing arbitrage opportunities: %v", err)
			}
		}
	}
}

func (aeb *ArbitrageExecutionBridge) processNewArbitrageOpportunities(ctx context.Context) error {
	opportunities, err := aeb.getRecentHighQualityOpportunities(ctx, 10*time.Minute)
	if err != nil {
		return fmt.Errorf("failed to get recent opportunities: %w", err)
	}

	if len(opportunities) == 0 {
		return nil
	}

	log.Printf("Found %d potential arbitrage opportunities", len(opportunities))

	for _, opportunity := range opportunities {
		shouldExecute := true
		var reasoning string

		if aeb.aiEvaluator != nil {
			contextStr := fmt.Sprintf("Profit: %s%%, Buy: %s, Sell: %s",
				opportunity.ProfitPercentage.String(),
				opportunity.BuyPrice.String(),
				opportunity.SellPrice.String())

			var confidence float64
			shouldExecute, confidence, reasoning, err = aeb.aiEvaluator.EvaluateOpportunity(ctx, &opportunity, contextStr)
			if err != nil {
				log.Printf("AI evaluation failed for opportunity %s: %v", opportunity.ID, err)
				continue
			}

			log.Printf("AI evaluated opportunity %s: execute=%v confidence=%.2f reason=%s",
				opportunity.ID, shouldExecute, confidence, reasoning)

			if !shouldExecute {
				continue
			}
		}

		if err := aeb.createArbitrageQuest(ctx, opportunity); err != nil {
			log.Printf("Failed to create arbitrage quest for opportunity %s: %v", opportunity.ID, err)
			continue
		}

		log.Printf("Created arbitrage execution quest for opportunity %s", opportunity.ID)
	}

	return nil
}

// getRecentHighQualityOpportunities queries the database for recent high-quality arbitrage opportunities
func (aeb *ArbitrageExecutionBridge) getRecentHighQualityOpportunities(ctx context.Context, since time.Duration) ([]models.ArbitrageOpportunity, error) {
	cutoff := time.Now().UTC().Add(-since)
	now := time.Now().UTC()

	query := `
		SELECT ao.id, ao.trading_pair_id, ao.buy_exchange_id, ao.sell_exchange_id,
		       ao.buy_price, ao.sell_price, ao.profit_percentage, ao.detected_at, ao.expires_at
		FROM arbitrage_opportunities ao
		JOIN trading_pairs tp ON ao.trading_pair_id = tp.id
		WHERE ao.detected_at > $1
		  AND ao.expires_at > $2
		  AND ao.profit_percentage > 0.01  -- At least 1% profit threshold
		ORDER BY ao.profit_percentage DESC
		LIMIT 10
	`

	rows, err := aeb.db.Query(ctx, query, cutoff, now)
	if err != nil {
		return nil, fmt.Errorf("failed to query arbitrage opportunities: %w", err)
	}
	defer rows.Close()

	var opportunities []models.ArbitrageOpportunity
	for rows.Next() {
		var opp models.ArbitrageOpportunity
		err := rows.Scan(&opp.ID, &opp.TradingPairID, &opp.BuyExchangeID, &opp.SellExchangeID,
			&opp.BuyPrice, &opp.SellPrice, &opp.ProfitPercentage, &opp.DetectedAt, &opp.ExpiresAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan arbitrage opportunity: %w", err)
		}
		opportunities = append(opportunities, opp)
	}

	return opportunities, nil
}

// createArbitrageQuest creates a new quest to execute the arbitrage opportunity
func (aeb *ArbitrageExecutionBridge) createArbitrageQuest(ctx context.Context, opportunity models.ArbitrageOpportunity) error {
	// Get exchange names for the arbitrage opportunity
	buyExchangeName, err := aeb.getExchangeName(ctx, opportunity.BuyExchangeID)
	if err != nil {
		return fmt.Errorf("failed to get buy exchange name: %w", err)
	}

	sellExchangeName, err := aeb.getExchangeName(ctx, opportunity.SellExchangeID)
	if err != nil {
		return fmt.Errorf("failed to get sell exchange name: %w", err)
	}

	// Get trading pair symbol
	symbol, err := aeb.getTradingPairSymbol(ctx, opportunity.TradingPairID)
	if err != nil {
		return fmt.Errorf("failed to get trading pair symbol: %w", err)
	}

	// Calculate potential profit
	profitAmount := opportunity.SellPrice.Sub(opportunity.BuyPrice).Mul(decimal.NewFromFloat(0.1)) // Using 0.1 as placeholder amount

	// Create quest with arbitrage details in checkpoint
	questCheckpoint := map[string]interface{}{
		"type":          "arbitrage_execution",
		"symbol":        symbol,
		"buy_exchange":  buyExchangeName,
		"sell_exchange": sellExchangeName,
		"buy_price":     opportunity.BuyPrice.String(),
		"sell_price":    opportunity.SellPrice.String(),
		"profit_pct":    opportunity.ProfitPercentage.String(),
		"profit_amount": profitAmount.String(),
		"detected_at":   opportunity.DetectedAt,
		"expires_at":    opportunity.ExpiresAt,
	}

	aeb.questEngine.RegisterDefinition(&QuestDefinition{
		ID:          "arbitrage_execution",
		Name:        "Arbitrage Execution",
		Description: "Execute detected arbitrage opportunity",
		Type:        QuestTypeArbitrage,
		Cadence:     CadenceOnetime,
		TargetCount: 1,
	})

	quest, err := aeb.questEngine.CreateQuest("arbitrage_execution", "system", 1)
	if err != nil {
		return fmt.Errorf("failed to create arbitrage quest: %w", err)
	}

	quest.Checkpoint = questCheckpoint
	quest.Status = QuestStatusActive

	return aeb.questEngine.UpdateQuestProgress(quest.ID, quest.CurrentCount, questCheckpoint)
}

// getExchangeName retrieves the exchange name from the database
func (aeb *ArbitrageExecutionBridge) getExchangeName(ctx context.Context, exchangeID int) (string, error) {
	var name string
	query := `SELECT name FROM exchanges WHERE id = $1`

	err := aeb.db.QueryRow(ctx, query, exchangeID).Scan(&name)
	if err != nil {
		return "", fmt.Errorf("failed to get exchange name: %w", err)
	}
	return name, nil
}

// getTradingPairSymbol retrieves the trading pair symbol from the database
func (aeb *ArbitrageExecutionBridge) getTradingPairSymbol(ctx context.Context, pairID int) (string, error) {
	var symbol string
	query := `SELECT symbol FROM trading_pairs WHERE id = $1`

	err := aeb.db.QueryRow(ctx, query, pairID).Scan(&symbol)
	if err != nil {
		return "", fmt.Errorf("failed to get trading pair symbol: %w", err)
	}
	return symbol, nil
}
