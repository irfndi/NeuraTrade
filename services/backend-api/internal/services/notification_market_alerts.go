package services

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/getsentry/sentry-go"
	userModels "github.com/irfndi/neuratrade/internal/models"
	"github.com/irfndi/neuratrade/internal/observability"
	"github.com/irfndi/neuratrade/internal/telemetry"
	"github.com/shopspring/decimal"
)

// NotifyArbitrageOpportunities sends notifications about arbitrage opportunities to eligible users.
//
// Parameters:
//
//	ctx: Context.
//	opportunities: List of opportunities.
//
// Returns:
//
//	error: Error if notification fails.
func (ns *NotificationService) NotifyArbitrageOpportunities(ctx context.Context, opportunities []ArbitrageOpportunity) error {
	spanCtx, span := observability.StartSpanWithTags(ctx, observability.SpanOpNotification, "NotificationService.NotifyArbitrageOpportunities", map[string]string{
		"opportunity_count": fmt.Sprintf("%d", len(opportunities)),
	})
	defer observability.FinishSpan(span, nil)

	observability.AddBreadcrumb(spanCtx, "notification", "Starting arbitrage opportunity notifications", sentry.LevelInfo)

	// Cache opportunities for faster subsequent access
	ns.cacheArbitrageOpportunities(spanCtx, opportunities)

	// Publish opportunities for real-time updates
	ns.PublishOpportunityUpdate(spanCtx, opportunities)

	// Get eligible users (those with Telegram chat IDs and arbitrage alerts enabled)
	users, err := ns.getEligibleUsers(spanCtx)
	if err != nil {
		observability.CaptureExceptionWithContext(spanCtx, err, "get_eligible_users", nil)
		return fmt.Errorf("failed to get eligible users: %w", err)
	}

	if len(users) == 0 {
		telemetry.Logger().Info("No eligible users found for arbitrage notifications")
		return nil
	}

	span.SetData("eligible_users", len(users))

	// Group opportunities by type
	arbitrageOpps := make([]ArbitrageOpportunity, 0)
	technicalOpps := make([]ArbitrageOpportunity, 0)

	for _, opp := range opportunities {
		// Categorize opportunity based on exchanges
		if opp.BuyExchange != opp.SellExchange {
			opp.OpportunityType = "arbitrage"
			arbitrageOpps = append(arbitrageOpps, opp)
		} else {
			opp.OpportunityType = "technical"
			technicalOpps = append(technicalOpps, opp)
		}
	}

	// Send notifications to each user
	for _, user := range users {
		// Send true arbitrage opportunities
		if len(arbitrageOpps) > 0 {
			if err := ns.sendArbitrageAlert(ctx, user, arbitrageOpps); err != nil {
				telemetry.Logger().Error("Failed to send arbitrage alert", "user_id", user.ID, "error", err)
			} else {
				telemetry.Logger().Info("Sent arbitrage alert", "user_id", user.ID)
			}
		}

		// Send technical analysis opportunities (if any)
		if len(technicalOpps) > 0 {
			if err := ns.sendArbitrageAlert(ctx, user, technicalOpps); err != nil {
				telemetry.Logger().Error("Failed to send technical alert", "user_id", user.ID, "error", err)
			} else {
				telemetry.Logger().Info("Sent technical alert", "user_id", user.ID)
			}
		}
	}

	telemetry.Logger().Info("Sent notifications", "user_count", len(users), "arbitrage_opportunities", len(arbitrageOpps), "technical_opportunities", len(technicalOpps))
	return nil
}

// formatTechnicalSignalMessage creates a formatted message for technical analysis signals
func (ns *NotificationService) formatTechnicalSignalMessage(signals []TechnicalSignalNotification) string {
	if len(signals) == 0 {
		return "No technical analysis signals found."
	}

	// Take top 3 signals for the alert
	topSignals := signals
	if len(signals) > 3 {
		topSignals = signals[:3]
	}

	header := "📊 *Technical Analysis Signals*\n\n"
	message := header
	message += fmt.Sprintf("Found %d high-confidence signals:\n\n", len(signals))

	for i, signal := range topSignals {
		message += fmt.Sprintf("📊 *TA SIGNAL: %s*\n", signal.Symbol)
		message += fmt.Sprintf("🎯 *Signal:* %s\n", signal.SignalText)
		message += fmt.Sprintf("💲 *Current Price:* $%.4f\n", signal.CurrentPrice)
		message += fmt.Sprintf("📈 *Entry:* %s\n", signal.EntryRange)

		// Add targets
		for j, target := range signal.Targets {
			message += fmt.Sprintf("🎯 *Target %d:* $%.4f (%.1f%% profit)\n", j+1, target.Price, target.Profit)
		}

		// Add stop loss
		message += fmt.Sprintf("🛑 *Stop Loss:* $%.4f (%.1f%% risk)\n", signal.StopLoss.Price, signal.StopLoss.Risk)
		message += fmt.Sprintf("📊 *Risk/Reward:* %s\n", signal.RiskReward)

		// Add exchanges
		if len(signal.Exchanges) > 0 {
			exchangeList := strings.Join(signal.Exchanges, ", ")
			message += fmt.Sprintf("🏪 *Exchanges:* %s\n", exchangeList)
		}

		message += fmt.Sprintf("⏰ *Timeframe:* %s\n", signal.Timeframe)
		message += fmt.Sprintf("🎯 *Confidence:* %.1f%%\n", signal.Confidence*100)

		if i < len(topSignals)-1 {
			message += "\n---\n\n"
		}
	}

	if len(signals) > 3 {
		message += fmt.Sprintf("\n...and %d more signals\n\n", len(signals)-3)
	}

	message += "\n⚡ *Trade wisely!* Always manage your risk and position size.\n\n"
	message += "Use /signals to see all current technical signals\n"
	message += "Use /stop to pause these alerts"

	return message
}

// sendArbitrageAlert sends a formatted arbitrage alert to a specific user
func (ns *NotificationService) sendArbitrageAlert(ctx context.Context, user userModels.User, opportunities []ArbitrageOpportunity) error {
	// Check if user has disabled notifications via Redis
	if ns.redis != nil && user.TelegramChatID != nil {
		key := fmt.Sprintf("telegram:user:%s:notifications_enabled", *user.TelegramChatID)
		val, err := ns.redis.Get(ctx, key)
		if err == nil && val == "false" {
			ns.logger.Info("User has disabled notifications, skipping", "user_id", user.ID)
			return nil
		}
	}

	// Check rate limit before sending
	allowed, err := ns.checkRateLimit(ctx, user.ID)
	if err != nil {
		ns.logger.Error("Rate limit check failed", "user_id", user.ID, "error", err)
	}
	if !allowed {
		ns.logger.Info("Rate limit exceeded, skipping notification", "user_id", user.ID)
		return fmt.Errorf("rate limit exceeded for user %s", user.ID)
	}

	chatID, err := strconv.ParseInt(*user.TelegramChatID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid chat ID: %w", err)
	}

	// Generate hash for opportunities to check cache
	oppHash := ns.generateOpportunityHash(opportunities)

	// Try to get cached message first
	var message string
	if cachedMsg, found := ns.getCachedMessage(ctx, "arbitrage", oppHash); found {
		message = cachedMsg
		ns.logger.Info("Using cached arbitrage message", "hash", oppHash[:8])
	} else {
		// Format the alert message and cache it
		message = ns.formatArbitrageMessage(opportunities)
		ns.setCachedMessage(ctx, "arbitrage", oppHash, message)
		ns.logger.Info("Formatted and cached new arbitrage message", "hash", oppHash[:8])
	}

	// Send the message with retry logic
	err = ns.sendTelegramMessageWithRetry(ctx, chatID, message, user.ID)

	if err != nil {
		return fmt.Errorf("failed to send telegram message: %w", err)
	}

	// Log the notification
	if err := ns.logNotification(ctx, user.ID, "telegram", "arbitrage_alert"); err != nil {
		ns.logger.Error("Failed to log notification", "user_id", user.ID, "error", err)
	}

	return nil
}

// sendEnhancedArbitrageAlert sends a formatted enhanced arbitrage alert to a specific user
func (ns *NotificationService) sendEnhancedArbitrageAlert(ctx context.Context, user userModels.User, signal *AggregatedSignal) error {
	// Check if user has disabled notifications via Redis
	if ns.redis != nil && user.TelegramChatID != nil {
		key := fmt.Sprintf("telegram:user:%s:notifications_enabled", *user.TelegramChatID)
		val, err := ns.redis.Get(ctx, key)
		if err == nil && val == "false" {
			ns.logger.Info("User has disabled notifications, skipping", "user_id", user.ID)
			return nil
		}
	}

	// Check rate limit before sending
	allowed, err := ns.checkRateLimit(ctx, user.ID)
	if err != nil {
		ns.logger.Error("Rate limit check failed", "user_id", user.ID, "error", err)
	}
	if !allowed {
		ns.logger.Info("Rate limit exceeded, skipping enhanced arbitrage notification", "user_id", user.ID)
		return fmt.Errorf("rate limit exceeded for user %s", user.ID)
	}

	chatID, err := strconv.ParseInt(*user.TelegramChatID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid chat ID: %w", err)
	}

	// Generate hash for signal to check cache
	signalHash := stableHash(fmt.Sprintf("%s:%s:%.4f", signal.Symbol, signal.SignalType, signal.Confidence.InexactFloat64()))

	// Try to get cached message first
	var message string
	if cachedMsg, found := ns.getCachedMessage(ctx, "enhanced_arbitrage", signalHash); found {
		message = cachedMsg
		ns.logger.Info("Using cached enhanced arbitrage message", "hash", signalHash[:8])
	} else {
		// Format the enhanced alert message and cache it
		message = ns.formatEnhancedArbitrageMessage(signal)
		ns.setCachedMessage(ctx, "enhanced_arbitrage", signalHash, message)
		ns.logger.Info("Formatted and cached new enhanced arbitrage message", "hash", signalHash[:8])
	}

	// Send the message with retry logic
	err = ns.sendTelegramMessageWithRetry(ctx, chatID, message, user.ID)
	if err != nil {
		return fmt.Errorf("failed to send telegram message: %w", err)
	}

	// Log the notification
	if err := ns.logNotification(ctx, user.ID, "telegram", "enhanced_arbitrage_alert"); err != nil {
		ns.logger.Error("Failed to log notification", "user_id", user.ID, "error", err)
	}

	return nil
}

// NotifyEnhancedArbitrageSignals sends notifications about enhanced arbitrage signals to eligible users.
//
// Parameters:
//
//	ctx: Context.
//	signals: List of aggregated signals.
//
// Returns:
//
//	error: Error if notification fails.
func (ns *NotificationService) NotifyEnhancedArbitrageSignals(ctx context.Context, signals []*AggregatedSignal) error {
	// Get eligible users (those with Telegram chat IDs and arbitrage alerts enabled)
	users, err := ns.getEligibleUsers(ctx)
	if err != nil {
		return fmt.Errorf("failed to get eligible users: %w", err)
	}

	if len(users) == 0 {
		ns.logger.Info("No eligible users found for enhanced arbitrage notifications")
		return nil
	}

	// Filter arbitrage signals
	arbitrageSignals := make([]*AggregatedSignal, 0)
	for _, signal := range signals {
		if signal.SignalType == SignalTypeArbitrage {
			arbitrageSignals = append(arbitrageSignals, signal)
		}
	}

	if len(arbitrageSignals) == 0 {
		ns.logger.Info("No arbitrage signals found to notify")
		return nil
	}

	// Send notifications to each user
	for _, user := range users {
		for _, signal := range arbitrageSignals {
			if err := ns.sendEnhancedArbitrageAlert(ctx, user, signal); err != nil {
				ns.logger.Error("Failed to send enhanced arbitrage alert", "user_id", user.ID, "error", err)
			} else {
				ns.logger.Info("Sent enhanced arbitrage alert", "user_id", user.ID, "symbol", signal.Symbol)
			}
		}
	}

	ns.logger.Info("Sent enhanced arbitrage notifications", "user_count", len(users), "signal_count", len(arbitrageSignals))
	return nil
}

// formatArbitrageMessage creates a formatted message for arbitrage opportunities
func (ns *NotificationService) formatArbitrageMessage(opportunities []ArbitrageOpportunity) string {
	if len(opportunities) == 0 {
		return "No arbitrage opportunities found."
	}

	// Take top 3 opportunities for the alert
	topOpportunities := opportunities
	if len(opportunities) > 3 {
		topOpportunities = opportunities[:3]
	}

	// Determine message header based on opportunity type
	header := "🚨 *Arbitrage Alert!*\n\n"
	if len(opportunities) > 0 {
		switch opportunities[0].OpportunityType {
		case "arbitrage":
			header = "🚀 *True Arbitrage Opportunities*\n\n"
		case "technical":
			header = "📊 *Technical Analysis Signals*\n\n"
		case "ai_generated":
			header = "🤖 *AI-Generated Opportunities*\n\n"
		}
	}

	message := header
	message += fmt.Sprintf("Found %d profitable opportunities:\n\n", len(opportunities))

	for i, opp := range topOpportunities {
		message += fmt.Sprintf("*%d. %s*\n", i+1, opp.Symbol)
		message += fmt.Sprintf("💰 Profit: *%.2f%%*\n", opp.ProfitPercent)
		message += fmt.Sprintf("📈 Buy: %s @ $%.4f\n", opp.BuyExchange, opp.BuyPrice)
		message += fmt.Sprintf("📉 Sell: %s @ $%.4f\n", opp.SellExchange, opp.SellPrice)
		message += "\n"
	}

	if len(opportunities) > 3 {
		message += fmt.Sprintf("...and %d more opportunities\n\n", len(opportunities)-3)
	}

	message += "⚡ *Act fast!* These opportunities may disappear quickly.\n\n"
	message += "Use /opportunities to see all current opportunities\n"
	message += "Use /stop to pause these alerts"

	return message
}

// formatEnhancedArbitrageMessage creates a formatted message for enhanced arbitrage signals with price ranges
func (ns *NotificationService) formatEnhancedArbitrageMessage(signal *AggregatedSignal) string {
	if signal == nil || signal.SignalType != SignalTypeArbitrage {
		return "No arbitrage signal found."
	}

	// Extract metadata
	metadata := signal.Metadata
	buyPriceRange, _ := metadata["buy_price_range"].(map[string]interface{})
	sellPriceRange, _ := metadata["sell_price_range"].(map[string]interface{})
	profitRange, _ := metadata["profit_range"].(map[string]interface{})
	buyExchanges, _ := metadata["buy_exchanges"].([]string)
	sellExchanges, _ := metadata["sell_exchanges"].([]string)
	opportunityCount, _ := metadata["opportunity_count"].(int)
	minVolume, _ := metadata["min_volume"].(decimal.Decimal)
	validityMinutes, _ := metadata["validity_minutes"].(int)

	// Build the message
	message := fmt.Sprintf("🔄 *ARBITRAGE ALERT: %s*\n\n", signal.Symbol)

	// Profit range
	if profitRange != nil {
		minPercent, _ := profitRange["min_percent"].(decimal.Decimal)
		maxPercent, _ := profitRange["max_percent"].(decimal.Decimal)
		minDollar, _ := profitRange["min_dollar"].(decimal.Decimal)
		maxDollar, _ := profitRange["max_dollar"].(decimal.Decimal)
		baseAmount, _ := profitRange["base_amount"].(decimal.Decimal)

		if minPercent.Equal(maxPercent) {
			message += fmt.Sprintf("💰 Profit: *%.2f%%* ($%.0f on $%.0f)\n",
				minPercent.InexactFloat64(), minDollar.InexactFloat64(), baseAmount.InexactFloat64())
		} else {
			message += fmt.Sprintf("💰 Profit: *%.2f%% - %.2f%%* ($%.0f - $%.0f on $%.0f)\n",
				minPercent.InexactFloat64(), maxPercent.InexactFloat64(),
				minDollar.InexactFloat64(), maxDollar.InexactFloat64(), baseAmount.InexactFloat64())
		}
	}

	// Buy price range
	if buyPriceRange != nil && len(buyExchanges) > 0 {
		buyMin, _ := buyPriceRange["min"].(decimal.Decimal)
		buyMax, _ := buyPriceRange["max"].(decimal.Decimal)

		exchangeList := strings.Join(buyExchanges, ", ")
		if buyMin.Equal(buyMax) {
			message += fmt.Sprintf("📈 BUY: $%.4f (%s)\n", buyMin.InexactFloat64(), exchangeList)
		} else {
			message += fmt.Sprintf("📈 BUY: $%.4f - $%.4f (%s)\n",
				buyMin.InexactFloat64(), buyMax.InexactFloat64(), exchangeList)
		}
	}

	// Sell price range
	if sellPriceRange != nil && len(sellExchanges) > 0 {
		sellMin, _ := sellPriceRange["min"].(decimal.Decimal)
		sellMax, _ := sellPriceRange["max"].(decimal.Decimal)

		exchangeList := strings.Join(sellExchanges, ", ")
		if sellMin.Equal(sellMax) {
			message += fmt.Sprintf("📉 SELL: $%.4f (%s)\n", sellMax.InexactFloat64(), exchangeList)
		} else {
			message += fmt.Sprintf("📉 SELL: $%.4f - $%.4f (%s)\n",
				sellMin.InexactFloat64(), sellMax.InexactFloat64(), exchangeList)
		}
	}

	// Validity and volume info
	if validityMinutes > 0 {
		message += fmt.Sprintf("⏰ Valid for: *%d minutes*\n", validityMinutes)
	}

	if !minVolume.IsZero() {
		message += fmt.Sprintf("🎯 Min Volume: *$%.0f*\n", minVolume.InexactFloat64())
	}

	// Additional info
	if opportunityCount > 1 {
		message += fmt.Sprintf("📊 Opportunities: *%d*\n", opportunityCount)
	}

	message += fmt.Sprintf("🎯 Confidence: *%.1f%%*\n", signal.Confidence.Mul(decimal.NewFromFloat(100)).InexactFloat64())

	message += "\n⚡ *Act fast!* Arbitrage opportunities disappear quickly.\n"
	message += "💡 *Min Volume* helps filter out low-liquidity fake signals."

	return message
}

// NotifyAggregatedSignals sends notifications about aggregated signals to eligible users.
//
// Parameters:
//
//	ctx: Context.
//	signals: List of aggregated signals.
//
// Returns:
//
//	error: Error if notification fails.
func (ns *NotificationService) NotifyAggregatedSignals(ctx context.Context, signals []*AggregatedSignal) error {
	// Get eligible users (those with Telegram chat IDs and alerts enabled)
	users, err := ns.getEligibleUsers(ctx)
	if err != nil {
		return fmt.Errorf("failed to get eligible users: %w", err)
	}

	if len(users) == 0 {
		ns.logger.Info("No eligible users found for aggregated signal notifications")
		return nil
	}

	if len(signals) == 0 {
		ns.logger.Info("No aggregated signals to notify")
		return nil
	}

	// Group signals by type
	arbitrageSignals := make([]*AggregatedSignal, 0)
	technicalSignals := make([]*AggregatedSignal, 0)

	for _, signal := range signals {
		switch signal.SignalType {
		case SignalTypeArbitrage:
			arbitrageSignals = append(arbitrageSignals, signal)
		case SignalTypeTechnical:
			technicalSignals = append(technicalSignals, signal)
		}
	}

	// Send notifications to each user
	for _, user := range users {
		// Send arbitrage signals
		if len(arbitrageSignals) > 0 {
			if err := ns.sendAggregatedArbitrageAlert(ctx, user, arbitrageSignals); err != nil {
				ns.logger.Error("Failed to send aggregated arbitrage alert", "user_id", user.ID, "error", err)
			} else {
				ns.logger.Info("Sent aggregated arbitrage alert", "user_id", user.ID)
			}
		}

		// Send technical analysis signals
		if len(technicalSignals) > 0 {
			if err := ns.sendAggregatedTechnicalAlert(ctx, user, technicalSignals); err != nil {
				ns.logger.Error("Failed to send aggregated technical alert", "user_id", user.ID, "error", err)
			} else {
				ns.logger.Info("Sent aggregated technical alert", "user_id", user.ID)
			}
		}
	}

	ns.logger.Info("Sent aggregated signal notifications",
		"user_count", len(users), "arbitrage_signals", len(arbitrageSignals), "technical_signals", len(technicalSignals))
	return nil
}

// sendAggregatedArbitrageAlert sends a formatted aggregated arbitrage alert to a specific user
func (ns *NotificationService) sendAggregatedArbitrageAlert(ctx context.Context, user userModels.User, signals []*AggregatedSignal) error {
	// Check rate limit before sending
	allowed, err := ns.checkRateLimit(ctx, user.ID)
	if err != nil {
		ns.logger.Error("Rate limit check failed", "user_id", user.ID, "error", err)
	}
	if !allowed {
		ns.logger.Info("Rate limit exceeded, skipping aggregated arbitrage alert", "user_id", user.ID)
		return fmt.Errorf("rate limit exceeded for user %s", user.ID)
	}

	chatID, err := strconv.ParseInt(*user.TelegramChatID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid chat ID: %w", err)
	}

	// Generate hash for signals to check cache
	signalsHash := ns.generateAggregatedSignalsHash(signals)

	// Try to get cached message first
	var message string
	if cachedMsg, found := ns.getCachedMessage(ctx, "aggregated_arbitrage", signalsHash); found {
		message = cachedMsg
		ns.logger.Info("Using cached aggregated arbitrage message", "hash", signalsHash[:8])
	} else {
		// Format the aggregated arbitrage alert message and cache it
		message = ns.formatAggregatedArbitrageMessage(signals)
		ns.setCachedMessage(ctx, "aggregated_arbitrage", signalsHash, message)
		ns.logger.Info("Formatted and cached new aggregated arbitrage message", "hash", signalsHash[:8])
	}

	// Send the message
	err = ns.sendTelegramMessage(ctx, chatID, message)

	if err != nil {
		return fmt.Errorf("failed to send telegram message: %w", err)
	}

	// Log the notification
	if err := ns.logNotification(ctx, user.ID, "telegram", "aggregated_arbitrage_alert"); err != nil {
		ns.logger.Error("Failed to log notification", "user_id", user.ID, "error", err)
	}

	return nil
}

// sendAggregatedTechnicalAlert sends a formatted aggregated technical alert to a specific user
func (ns *NotificationService) sendAggregatedTechnicalAlert(ctx context.Context, user userModels.User, signals []*AggregatedSignal) error {
	// Check rate limit before sending
	allowed, err := ns.checkRateLimit(ctx, user.ID)
	if err != nil {
		ns.logger.Error("Rate limit check failed", "user_id", user.ID, "error", err)
	}
	if !allowed {
		ns.logger.Info("Rate limit exceeded, skipping aggregated technical alert", "user_id", user.ID)
		return fmt.Errorf("rate limit exceeded for user %s", user.ID)
	}

	chatID, err := strconv.ParseInt(*user.TelegramChatID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid chat ID: %w", err)
	}

	// Generate hash for signals to check cache
	signalsHash := ns.generateAggregatedSignalsHash(signals)

	// Try to get cached message first
	var message string
	if cachedMsg, found := ns.getCachedMessage(ctx, "aggregated_technical", signalsHash); found {
		message = cachedMsg
		ns.logger.Info("Using cached aggregated technical message", "hash", signalsHash[:8])
	} else {
		// Format the aggregated technical alert message and cache it
		message = ns.formatAggregatedTechnicalMessage(signals)
		ns.setCachedMessage(ctx, "aggregated_technical", signalsHash, message)
		ns.logger.Info("Formatted and cached new aggregated technical message", "hash", signalsHash[:8])
	}

	// Send the message
	err = ns.sendTelegramMessage(ctx, chatID, message)

	if err != nil {
		return fmt.Errorf("failed to send telegram message: %w", err)
	}

	// Log the notification
	if err := ns.logNotification(ctx, user.ID, "telegram", "aggregated_technical_alert"); err != nil {
		ns.logger.Error("Failed to log notification", "user_id", user.ID, "error", err)
	}

	return nil
}

// NotifyTechnicalSignals sends notifications about technical analysis signals to eligible users.
//
// Parameters:
//
//	ctx: Context.
//	signals: List of technical signals.
//
// Returns:
//
//	error: Error if notification fails.
func (ns *NotificationService) NotifyTechnicalSignals(ctx context.Context, signals []TechnicalSignalNotification) error {
	// Get eligible users (those with Telegram chat IDs and technical alerts enabled)
	users, err := ns.getEligibleUsers(ctx)
	if err != nil {
		return fmt.Errorf("failed to get eligible users: %w", err)
	}

	if len(users) == 0 {
		ns.logger.Info("No eligible users found for technical signal notifications")
		return nil
	}

	// Send notifications to each user
	for _, user := range users {
		if err := ns.sendTechnicalAlert(ctx, user, signals); err != nil {
			ns.logger.Error("Failed to send technical alert", "user_id", user.ID, "error", err)
		} else {
			ns.logger.Info("Sent technical alert", "user_id", user.ID)
		}
	}

	ns.logger.Info("Sent technical signal notifications", "user_count", len(users), "signal_count", len(signals))
	return nil
}

// sendTechnicalAlert sends a formatted technical analysis alert to a specific user
func (ns *NotificationService) sendTechnicalAlert(ctx context.Context, user userModels.User, signals []TechnicalSignalNotification) error {
	// Check rate limit before sending
	allowed, err := ns.checkRateLimit(ctx, user.ID)
	if err != nil {
		ns.logger.Error("Rate limit check failed", "user_id", user.ID, "error", err)
	}
	if !allowed {
		ns.logger.Info("Rate limit exceeded, skipping technical alert", "user_id", user.ID)
		return fmt.Errorf("rate limit exceeded for user %s", user.ID)
	}

	chatID, err := strconv.ParseInt(*user.TelegramChatID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid chat ID: %w", err)
	}

	// Generate hash for signals to check cache
	signalsHash := ns.generateTechnicalSignalsHash(signals)

	// Try to get cached message first
	var message string
	if cachedMsg, found := ns.getCachedMessage(ctx, "technical", signalsHash); found {
		message = cachedMsg
		ns.logger.Info("Using cached technical message", "hash", signalsHash[:8])
	} else {
		// Format the technical alert message and cache it
		message = ns.formatTechnicalSignalMessage(signals)
		ns.setCachedMessage(ctx, "technical", signalsHash, message)
		ns.logger.Info("Formatted and cached new technical message", "hash", signalsHash[:8])
	}

	// Send the message
	err = ns.sendTelegramMessage(ctx, chatID, message)

	if err != nil {
		return fmt.Errorf("failed to send telegram message: %w", err)
	}

	// Log the notification
	if err := ns.logNotification(ctx, user.ID, "telegram", "technical_alert"); err != nil {
		ns.logger.Error("Failed to log notification", "user_id", user.ID, "error", err)
	}

	return nil
}
