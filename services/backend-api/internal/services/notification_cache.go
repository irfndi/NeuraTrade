package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	userModels "github.com/irfndi/neuratrade/internal/models"
	"github.com/irfndi/neuratrade/internal/telemetry"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
)

// PublishOpportunityUpdate publishes arbitrage opportunity updates via Redis pub/sub.
//
// Parameters:
//
//	ctx: Context.
//	opportunities: List of opportunities.
func (ns *NotificationService) PublishOpportunityUpdate(ctx context.Context, opportunities []ArbitrageOpportunity) {
	if ns.redis == nil || len(opportunities) == 0 {
		return
	}

	channel := "arbitrage_opportunities"

	// Note: Redis pub/sub would require additional Redis client methods
	// For now, we'll use the cache mechanism as the primary distribution method
	telemetry.Logger().Info("Would publish opportunities to Redis channel", "opportunity_count", len(opportunities), "channel", channel)
}

// GetCacheStats returns statistics about Redis cache usage.
//
// Parameters:
//
//	ctx: Context.
//
// Returns:
//
//	map[string]interface{}: Cache stats.
func (ns *NotificationService) GetCacheStats(ctx context.Context) map[string]interface{} {
	stats := make(map[string]interface{})

	if ns.redis == nil {
		stats["redis_available"] = false
		return stats
	}

	stats["redis_available"] = true

	// Check if eligible users are cached
	usersCacheKey := "eligible_users:arbitrage"
	if exists, err := ns.redis.Exists(ctx, usersCacheKey); err == nil {
		stats["users_cached"] = exists
	}

	// Check if opportunities are cached
	oppsCacheKey := "arbitrage_opportunities:latest"
	if exists, err := ns.redis.Exists(ctx, oppsCacheKey); err == nil {
		stats["opportunities_cached"] = exists
	}

	return stats
}

// cacheArbitrageOpportunities stores arbitrage opportunities in Redis with 30-second TTL
func (ns *NotificationService) cacheArbitrageOpportunities(ctx context.Context, opportunities []ArbitrageOpportunity) {
	if ns.redis == nil || len(opportunities) == 0 {
		return
	}

	cacheKey := "arbitrage_opportunities:latest"
	oppsJSON, err := json.Marshal(opportunities)
	if err != nil {
		telemetry.Logger().Error("Failed to marshal opportunities for caching", "error", err)
		return
	}

	if err := ns.redis.Set(ctx, cacheKey, string(oppsJSON), 30*time.Second); err != nil {
		telemetry.Logger().Error("Failed to cache arbitrage opportunities", "error", err)
	} else {
		telemetry.Logger().Info("Cached arbitrage opportunities in Redis", "opportunity_count", len(opportunities), "ttl_seconds", 30)
	}
}

// CacheMarketData stores market data in Redis with 10-second TTL for API performance.
//
// Parameters:
//
//	ctx: Context.
//	exchange: Exchange name.
//	data: Market data to cache.
func (ns *NotificationService) CacheMarketData(ctx context.Context, exchange string, data interface{}) {
	if ns.redis == nil {
		return
	}

	cacheKey := fmt.Sprintf("market_data:%s", exchange)
	dataJSON, err := json.Marshal(data)
	if err != nil {
		telemetry.Logger().Error("Failed to marshal market data for caching", "error", err)
		return
	}

	if err := ns.redis.Set(ctx, cacheKey, string(dataJSON), 10*time.Second); err != nil {
		telemetry.Logger().Error("Failed to cache market data", "exchange", exchange, "error", err)
	} else {
		telemetry.Logger().Info("Cached market data in Redis", "exchange", exchange, "ttl_seconds", 10)
	}
}

// GetCachedMarketData retrieves cached market data from Redis.
//
// Parameters:
//
//	ctx: Context.
//	exchange: Exchange name.
//	result: Pointer to struct to unmarshal data into.
//
// Returns:
//
//	error: Error if retrieval fails.
func (ns *NotificationService) GetCachedMarketData(ctx context.Context, exchange string, result interface{}) error {
	if ns.redis == nil {
		return fmt.Errorf("redis not available")
	}

	cacheKey := fmt.Sprintf("market_data:%s", exchange)
	cachedData, err := ns.redis.Get(ctx, cacheKey)
	if err != nil || cachedData == "" {
		return fmt.Errorf("no cached market data found for %s", exchange)
	}

	if err := json.Unmarshal([]byte(cachedData), result); err != nil {
		return fmt.Errorf("failed to unmarshal cached market data: %w", err)
	}

	telemetry.Logger().Info("Retrieved cached market data from Redis", "exchange", exchange)
	return nil
}

// InvalidateUserCache invalidates the eligible users cache when user settings change.
//
// Parameters:
//
//	ctx: Context.
func (ns *NotificationService) InvalidateUserCache(ctx context.Context) {
	if ns.redis == nil {
		return
	}

	cacheKey := "eligible_users:arbitrage"
	if err := ns.redis.Delete(ctx, cacheKey); err != nil {
		telemetry.Logger().Error("Failed to invalidate user cache", "error", err)
	} else {
		telemetry.Logger().Info("Invalidated eligible users cache")
	}
}

// InvalidateOpportunityCache invalidates the arbitrage opportunities cache.
//
// Parameters:
//
//	ctx: Context.
func (ns *NotificationService) InvalidateOpportunityCache(ctx context.Context) {
	if ns.redis == nil {
		return
	}

	cacheKey := "arbitrage_opportunities:latest"
	if err := ns.redis.Delete(ctx, cacheKey); err != nil {
		telemetry.Logger().Error("Failed to invalidate opportunity cache", "error", err)
	} else {
		telemetry.Logger().Info("Invalidated arbitrage opportunities cache")
	}
}

// getEligibleUsers returns all users who should receive arbitrage alerts with Redis caching
func (ns *NotificationService) getEligibleUsers(ctx context.Context) ([]userModels.User, error) {
	cacheKey := "eligible_users:arbitrage"

	// Try to get from Redis cache first
	if ns.redis != nil {
		cachedData, err := ns.redis.Get(ctx, cacheKey)
		if err == nil && cachedData != "" {
			var users []userModels.User
			unmarshalErr := json.Unmarshal([]byte(cachedData), &users)
			if unmarshalErr == nil {
				ns.logger.Info("Retrieved eligible users from Redis cache", "count", len(users))
				return users, nil
			}
			ns.logger.Error("Failed to unmarshal cached users", "error", unmarshalErr)
		}
	}

	// Cache miss or Redis unavailable, query database
	// Exclude blocked users (telegram_blocked = true)
	query := `
		SELECT id, email, telegram_chat_id, subscription_tier, created_at, updated_at
		FROM users
		WHERE telegram_chat_id IS NOT NULL
		  AND telegram_chat_id != ''
		  AND (telegram_blocked IS NULL OR telegram_blocked = false)
		  AND id NOT IN (
			  SELECT DISTINCT user_id
			  FROM user_alerts
			  WHERE alert_type = 'arbitrage'
			    AND is_active = false
			    AND conditions->>'notifications_enabled' = 'false'
		  )
	`

	rows, err := ns.db.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query eligible users: %w", err)
	}
	defer rows.Close()

	var users []userModels.User
	for rows.Next() {
		var user userModels.User
		if err := rows.Scan(&user.ID, &user.Email, &user.TelegramChatID, &user.SubscriptionTier, &user.CreatedAt, &user.UpdatedAt); err != nil {
			ns.logger.Error("Failed to scan user row", "error", err)
			continue
		}
		users = append(users, user)
	}

	// Cache the result in Redis with 5-minute TTL
	if ns.redis != nil && len(users) > 0 {
		usersJSON, err := json.Marshal(users)
		if err == nil {
			if setErr := ns.redis.Set(ctx, cacheKey, string(usersJSON), 5*time.Minute); setErr != nil {
				ns.logger.Error("Failed to cache eligible users", "error", setErr)
			} else {
				ns.logger.Info("Cached eligible users in Redis", "count", len(users), "ttl_minutes", 5)
			}
		} else {
			ns.logger.Error("Failed to marshal users for caching", "error", err)
		}
	}

	return users, nil
}

// checkRateLimit checks if a user has exceeded the notification rate limit (5 notifications per minute)
// Uses fail-closed strategy: denies requests when Redis is unavailable to prevent abuse
func (ns *NotificationService) checkRateLimit(ctx context.Context, userID string) (bool, error) {
	if ns.redis == nil {
		ns.logger.Warn("Rate limit check denied: Redis not available", "user_id", userID)
		return false, fmt.Errorf("rate limiting unavailable: Redis not configured")
	}

	// Use sliding window with Redis sorted set
	rateKey := fmt.Sprintf("rate_limit:notifications:%s", userID)
	now := time.Now().Unix()
	oneMinuteAgo := now - 60

	// Remove old entries (older than 1 minute)
	if err := ns.redis.Client.ZRemRangeByScore(ctx, rateKey, "0", fmt.Sprintf("%d", oneMinuteAgo)).Err(); err != nil {
		ns.logger.Error("Failed to clean old rate limit entries", "user_id", userID, "error", err)
		// Continue - non-critical cleanup operation
	}

	// Count current notifications in the last minute
	count, err := ns.redis.Client.ZCard(ctx, rateKey).Result()
	if err != nil {
		ns.logger.Error("Rate limit check denied: Redis operation failed", "user_id", userID, "error", err)
		return false, fmt.Errorf("rate limiting unavailable: %w", err)
	}

	// Check if user has exceeded the limit (5 notifications per minute)
	if count >= 5 {
		return false, nil
	}

	// Add current notification to the sliding window
	if err := ns.redis.Client.ZAdd(ctx, rateKey, redis.Z{
		Score:  float64(now),
		Member: fmt.Sprintf("%d", now),
	}).Err(); err != nil {
		ns.logger.Error("Failed to add rate limit entry", "user_id", userID, "error", err)
	}

	// Set expiration for the key (2 minutes to be safe)
	if err := ns.redis.Client.Expire(ctx, rateKey, 2*time.Minute).Err(); err != nil {
		ns.logger.Error("Failed to set expiration for rate limit key", "key", rateKey, "error", err)
	}

	return true, nil
}

// generateOpportunityHash creates a hash for opportunities to use as cache key
func (ns *NotificationService) generateOpportunityHash(opportunities []ArbitrageOpportunity) string {
	// Create a consistent string representation of opportunities
	var hashData strings.Builder
	for _, opp := range opportunities {
		fmt.Fprintf(&hashData, "%s:%s:%s:%.4f:%.4f:%.2f",
			opp.Symbol, opp.BuyExchange, opp.SellExchange,
			opp.BuyPrice, opp.SellPrice, opp.ProfitPercent)
	}

	return stableHash(hashData.String())
}

// generateTechnicalSignalsHash creates a consistent hash for technical signals
func (ns *NotificationService) generateTechnicalSignalsHash(signals []TechnicalSignalNotification) string {
	var hashData strings.Builder
	for _, signal := range signals {
		fmt.Fprintf(&hashData, "%s:%s:%s:%.4f:%.2f",
			signal.Symbol, signal.SignalType, signal.Action, signal.CurrentPrice, signal.Confidence)
	}

	return stableHash(hashData.String())
}

func stableHash(input string) string {
	sum := sha256.Sum256([]byte(input))
	return hex.EncodeToString(sum[:])
}

// getCachedMessage retrieves a cached message from Redis
func (ns *NotificationService) getCachedMessage(ctx context.Context, msgType, hash string) (string, bool) {
	if ns.redis == nil {
		return "", false
	}

	cacheKey := fmt.Sprintf("msg_cache:%s:%s", msgType, hash)
	message, err := ns.redis.Get(ctx, cacheKey)
	if err != nil {
		return "", false
	}
	return message, true
}

// setCachedMessage stores a formatted message in Redis with TTL
func (ns *NotificationService) setCachedMessage(ctx context.Context, msgType, hash, message string) {
	if ns.redis == nil {
		return
	}

	cacheKey := fmt.Sprintf("msg_cache:%s:%s", msgType, hash)
	if err := ns.redis.Set(ctx, cacheKey, message, 5*time.Minute); err != nil {
		ns.logger.Error("Failed to cache message", "key", cacheKey, "error", err)
	}
}

// CheckUserNotificationPreferences checks if a user wants to receive arbitrage notifications with Redis caching.
//
// Parameters:
//
//	ctx: Context.
//	userID: User ID.
//
// Returns:
//
//	bool: True if notifications are enabled.
//	error: Error if check fails.
func (ns *NotificationService) CheckUserNotificationPreferences(ctx context.Context, userID string) (bool, error) {
	cacheKey := fmt.Sprintf("user_preferences:%s:arbitrage", userID)

	// Try to get from Redis cache first
	if ns.redis != nil {
		cachedData, err := ns.redis.Get(ctx, cacheKey)
		if err == nil && cachedData != "" {
			switch cachedData {
			case "true":
				return true, nil
			case "false":
				return false, nil
			}
		}
	}

	// Cache miss or Redis unavailable, query database
	query := `
		SELECT COUNT(*) 
		FROM user_alerts 
		WHERE user_id = $1 
		  AND alert_type = 'arbitrage' 
		  AND is_active = false
		  AND conditions->>'notifications_enabled' = 'false'
	`

	var count int
	err := ns.db.QueryRow(ctx, query, userID).Scan(&count)
	if err != nil {
		return true, fmt.Errorf("failed to check user preferences: %w", err) // Default to enabled on error
	}

	result := count == 0 // Return true if no disabled alerts found

	// Cache the result in Redis with 5-minute TTL
	if ns.redis != nil {
		cacheValue := "false"
		if result {
			cacheValue = "true"
		}
		if err := ns.redis.Set(ctx, cacheKey, cacheValue, 5*time.Minute); err != nil {
			ns.logger.Error("Failed to cache user preferences", "user_id", userID, "error", err)
		} else {
			ns.logger.Info("Cached user preferences", "user_id", userID, "preferences", result)
		}
	}

	return result, nil
}

// generateAggregatedSignalsHash generates a consistent hash for a slice of aggregated signals
func (ns *NotificationService) generateAggregatedSignalsHash(signals []*AggregatedSignal) string {
	h := sha256.New()

	// Sort signals by symbol and signal type for consistent hashing
	sortedSignals := make([]*AggregatedSignal, len(signals))
	copy(sortedSignals, signals)
	sort.Slice(sortedSignals, func(i, j int) bool {
		if sortedSignals[i].Symbol != sortedSignals[j].Symbol {
			return sortedSignals[i].Symbol < sortedSignals[j].Symbol
		}
		return sortedSignals[i].SignalType < sortedSignals[j].SignalType
	})

	for _, signal := range sortedSignals {
		_, _ = fmt.Fprintf(h, "%s:%s:%s:%s:%.2f",
			signal.Symbol,
			signal.SignalType,
			signal.Action,
			string(signal.Strength),
			signal.Confidence.InexactFloat64())
	}

	return fmt.Sprintf("%x", h.Sum(nil))
}

// handleBlockedUser marks a user as blocked in the database
func (ns *NotificationService) handleBlockedUser(ctx context.Context, userID, reason string) error {
	ns.logger.Info("Marking user as blocked", "user_id", userID, "reason", reason)

	query := `
		UPDATE users
		SET telegram_blocked = true,
		    telegram_blocked_at = NOW(),
		    updated_at = NOW()
		WHERE id = $1
	`
	_, err := ns.db.Exec(ctx, query, userID)
	if err != nil {
		return fmt.Errorf("failed to update user blocked status: %w", err)
	}

	// Invalidate cache
	ns.InvalidateUserCache(ctx)

	ns.logger.Info("User marked as blocked", "user_id", userID)
	return nil
}

// formatAggregatedArbitrageMessage formats multiple arbitrage signals into a single message
func (ns *NotificationService) formatAggregatedArbitrageMessage(signals []*AggregatedSignal) string {
	if len(signals) == 0 {
		return "🔍 No arbitrage opportunities available"
	}

	// Sort signals by profit potential (highest first)
	sort.Slice(signals, func(i, j int) bool {
		return signals[i].ProfitPotential.GreaterThan(signals[j].ProfitPotential)
	})

	var message strings.Builder
	message.WriteString("🚀 Aggregated Arbitrage Opportunities\n\n")

	// Limit to top 5 opportunities to keep message manageable
	maxSignals := len(signals)
	if maxSignals > 5 {
		maxSignals = 5
	}

	for i, signal := range signals[:maxSignals] {
		fmt.Fprintf(&message, "%d. %s\n", i+1, signal.Symbol)
		fmt.Fprintf(&message, "💰 Profit: %.2f%%\n", signal.ProfitPotential.InexactFloat64())
		fmt.Fprintf(&message, "🎯 Confidence: %.1f%%\n", signal.Confidence.InexactFloat64())
		fmt.Fprintf(&message, "⚡ Action: %s\n", strings.ToUpper(signal.Action))
		fmt.Fprintf(&message, "🏪 Exchanges: %s\n", strings.Join(signal.Exchanges, ", "))

		// Add metadata if available
		if signal.Metadata != nil {
			if buyPrice, ok := signal.Metadata["buy_price"]; ok {
				fmt.Fprintf(&message, "📈 Buy Price: %v\n", buyPrice)
			}
			if sellPrice, ok := signal.Metadata["sell_price"]; ok {
				fmt.Fprintf(&message, "📉 Sell Price: %v\n", sellPrice)
			}
		}

		message.WriteString("\n")
	}

	if len(signals) > maxSignals {
		fmt.Fprintf(&message, "... and %d more opportunities\n\n", len(signals)-maxSignals)
	}

	message.WriteString("⏰ Generated: ")
	message.WriteString(time.Now().Format("15:04:05 MST"))
	message.WriteString("\n\n⚠️ Trade at your own risk")

	return message.String()
}

// formatAggregatedTechnicalMessage formats multiple technical analysis signals into a single message
func (ns *NotificationService) formatAggregatedTechnicalMessage(signals []*AggregatedSignal) string {
	if len(signals) == 0 {
		return "📊 No technical analysis signals available"
	}

	// Sort signals by strength (highest first)
	sort.Slice(signals, func(i, j int) bool {
		return string(signals[i].Strength) > string(signals[j].Strength)
	})

	var message strings.Builder
	message.WriteString("📊 Aggregated Technical Analysis\n\n")

	// Limit to top 5 signals to keep message manageable
	maxSignals := len(signals)
	if maxSignals > 5 {
		maxSignals = 5
	}

	for i, signal := range signals[:maxSignals] {
		fmt.Fprintf(&message, "%d. %s\n", i+1, signal.Symbol)
		fmt.Fprintf(&message, "📈 Signal: %s\n", strings.ToUpper(signal.Action))
		fmt.Fprintf(&message, "💪 Strength: %s\n", signal.Strength)
		fmt.Fprintf(&message, "🎯 Confidence: %.1f%%\n", signal.Confidence.InexactFloat64())
		fmt.Fprintf(&message, "⚠️ Risk: %.2f%%\n", signal.RiskLevel.InexactFloat64())

		// Add indicators if available
		if len(signal.Indicators) > 0 {
			message.WriteString("📊 Indicators: ")
			message.WriteString(strings.Join(signal.Indicators, ", "))
			message.WriteString("\n")
		}

		// Add metadata if available
		if signal.Metadata != nil {
			if entryPrice, ok := signal.Metadata["entry_price"]; ok {
				fmt.Fprintf(&message, "🎯 Entry: %v\n", entryPrice)
			}
			if stopLoss, ok := signal.Metadata["stop_loss"]; ok {
				fmt.Fprintf(&message, "🛑 Stop Loss: %v\n", stopLoss)
			}
			if target, ok := signal.Metadata["target"]; ok {
				fmt.Fprintf(&message, "🎯 Target: %v\n", target)
			}
		}

		message.WriteString("\n")
	}

	if len(signals) > maxSignals {
		fmt.Fprintf(&message, "... and %d more signals\n\n", len(signals)-maxSignals)
	}

	message.WriteString("⏰ Generated: ")
	message.WriteString(time.Now().Format("15:04:05 MST"))
	message.WriteString("\n\n⚠️ Trade at your own risk")

	return message.String()
}

// ConvertAggregatedSignalToNotification converts an AggregatedSignal to TechnicalSignalNotification.
//
// Parameters:
//
//	signal: The aggregated signal.
//
// Returns:
//
//	*TechnicalSignalNotification: Notification struct.
func (ns *NotificationService) ConvertAggregatedSignalToNotification(signal *AggregatedSignal) *TechnicalSignalNotification {
	// Extract current price from metadata if available
	currentPrice := 0.0
	if signal.Metadata != nil {
		if price, ok := signal.Metadata["current_price"].(float64); ok {
			currentPrice = price
		}
	}

	// Calculate entry range based on current price and action
	entryRange := ""
	if currentPrice > 0 {
		switch signal.Action {
		case "buy":
			lowEntry := currentPrice * 0.995  // 0.5% below current
			highEntry := currentPrice * 1.005 // 0.5% above current
			entryRange = fmt.Sprintf("$%.4f - $%.4f", lowEntry, highEntry)
		case "sell":
			lowEntry := currentPrice * 0.995
			highEntry := currentPrice * 1.005
			entryRange = fmt.Sprintf("$%.4f - $%.4f", lowEntry, highEntry)
		}
	}

	// Calculate targets based on profit potential
	targets := []Target{}
	if currentPrice > 0 {
		profitFloat, _ := signal.ProfitPotential.Float64()
		switch signal.Action {
		case "buy":
			// Target 1: Half of profit potential
			target1Price := currentPrice * (1 + (profitFloat/2)/100)
			target1Profit := (profitFloat / 2)
			targets = append(targets, Target{Price: target1Price, Profit: target1Profit})

			// Target 2: Full profit potential
			target2Price := currentPrice * (1 + profitFloat/100)
			target2Profit := profitFloat
			targets = append(targets, Target{Price: target2Price, Profit: target2Profit})
		case "sell":
			// For sell signals, targets are lower prices
			target1Price := currentPrice * (1 - (profitFloat/2)/100)
			target1Profit := (profitFloat / 2)
			targets = append(targets, Target{Price: target1Price, Profit: target1Profit})

			target2Price := currentPrice * (1 - profitFloat/100)
			target2Profit := profitFloat
			targets = append(targets, Target{Price: target2Price, Profit: target2Profit})
		}
	}

	// Calculate stop loss based on risk level
	stopLoss := StopLoss{}
	if currentPrice > 0 {
		riskFloat, _ := signal.RiskLevel.Float64()
		switch signal.Action {
		case "buy":
			stopLoss.Price = currentPrice * (1 - riskFloat)
			stopLoss.Risk = riskFloat * 100
		case "sell":
			stopLoss.Price = currentPrice * (1 + riskFloat)
			stopLoss.Risk = riskFloat * 100
		}
	}

	// Calculate risk/reward ratio
	riskReward := "1:1"
	if len(targets) > 0 && stopLoss.Risk > 0 {
		avgProfit := (targets[0].Profit + targets[len(targets)-1].Profit) / 2
		ratio := avgProfit / stopLoss.Risk
		riskReward = fmt.Sprintf("1:%.1f", ratio)
	}

	// Extract signal description from metadata or create from indicators
	signalText := ""
	if signal.Metadata != nil {
		if desc, ok := signal.Metadata["description"].(string); ok {
			signalText = desc
		}
	}
	if signalText == "" && len(signal.Indicators) > 0 {
		signalText = strings.Join(signal.Indicators, " + ")
	}

	// Default timeframe
	timeframe := "4H"
	if signal.Metadata != nil {
		if tf, ok := signal.Metadata["timeframe"].(string); ok {
			timeframe = tf
		}
	}

	confidence, _ := signal.Confidence.Float64()

	return &TechnicalSignalNotification{
		Symbol:       signal.Symbol,
		SignalType:   string(signal.SignalType),
		Action:       signal.Action,
		SignalText:   signalText,
		CurrentPrice: currentPrice,
		EntryRange:   entryRange,
		Targets:      targets,
		StopLoss:     stopLoss,
		RiskReward:   riskReward,
		Exchanges:    signal.Exchanges,
		Timeframe:    timeframe,
		Confidence:   confidence,
		Timestamp:    signal.CreatedAt,
	}
}

// Ensure decimal.Decimal is referenced to avoid unused import issues
var _ = decimal.Decimal{}
