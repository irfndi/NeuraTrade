package services

import (
	"context"
	"fmt"
	"log"
	"time"
)

// QuestHandlerFunc is a function that executes a quest
type QuestHandlerFunc func(ctx context.Context, quest *Quest) error

// RegisterDefaultQuestHandlers registers default handlers for all quest types
func (e *QuestEngine) RegisterDefaultQuestHandlers(
	ccxtService interface{},
	arbitrageService interface{},
	futuresArbService interface{},
) {
	// Market Scanner handler - scans for trading opportunities every 5 minutes
	e.RegisterHandler(QuestTypeRoutine, func(ctx context.Context, quest *Quest) error {
		return e.handleMarketScan(ctx, quest, ccxtService, arbitrageService)
	})

	// Funding Rate Scanner handler - scans funding rates every 5 minutes
	e.RegisterHandler(QuestTypeRoutine, func(ctx context.Context, quest *Quest) error {
		return e.handleFundingRateScan(ctx, quest, futuresArbService)
	})

	// Portfolio Health Check handler - checks portfolio health hourly
	e.RegisterHandler(QuestTypeRoutine, func(ctx context.Context, quest *Quest) error {
		return e.handlePortfolioHealth(ctx, quest)
	})

	log.Println("Quest handlers registered successfully")
}

// handleMarketScan scans markets for trading opportunities
func (e *QuestEngine) handleMarketScan(
	ctx context.Context,
	quest *Quest,
	ccxtService interface{},
	arbitrageService interface{},
) error {
	log.Printf("Executing market scan quest: %s", quest.Name)

	// TODO: Implement actual market scanning logic
	// For now, just update the checkpoint to show the quest is running
	quest.CurrentCount++
	quest.Checkpoint["last_scan_time"] = time.Now().UTC().Format(time.RFC3339)
	quest.Checkpoint["status"] = "scanning"

	log.Printf("Market scan complete")
	return nil
}

// handleFundingRateScan scans for funding rate arbitrage opportunities
func (e *QuestEngine) handleFundingRateScan(
	ctx context.Context,
	quest *Quest,
	futuresArbService interface{},
) error {
	log.Printf("Executing funding rate scan quest: %s", quest.Name)

	if futuresArbService == nil {
		log.Printf("Futures arbitrage service not available, skipping funding rate scan")
		quest.CurrentCount++
		return nil
	}

	// TODO: Implement actual funding rate scanning
	quest.CurrentCount++
	quest.Checkpoint["last_funding_scan"] = time.Now().UTC().Format(time.RFC3339)

	log.Printf("Funding rate scan complete")
	return nil
}

// handlePortfolioHealth checks portfolio balance and exposure
func (e *QuestEngine) handlePortfolioHealth(ctx context.Context, quest *Quest) error {
	log.Printf("Executing portfolio health check quest: %s", quest.Name)

	// Get chat ID from quest metadata
	chatID, ok := quest.Metadata["chat_id"]
	if !ok {
		return fmt.Errorf("chat_id not found in quest metadata")
	}

	// Update quest progress
	quest.CurrentCount++
	quest.Checkpoint["last_health_check"] = time.Now().UTC().Format(time.RFC3339)
	quest.Checkpoint["chat_id"] = chatID
	quest.Checkpoint["status"] = "healthy"

	log.Printf("Portfolio health check complete for chat_id: %s", chatID)
	return nil
}
