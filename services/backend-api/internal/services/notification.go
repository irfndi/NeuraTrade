package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/getsentry/sentry-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/irfndi/neuratrade/internal/observability"
	"github.com/irfndi/neuratrade/internal/telemetry"
	pb "github.com/irfndi/neuratrade/pkg/pb/telegram"

	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/irfndi/neuratrade/internal/database"
	userModels "github.com/irfndi/neuratrade/internal/models"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
)

// NotificationService handles sending notifications to users.
type NotificationService struct {
	db                 DBPool
	redis              *database.RedisClient
	telegramServiceURL string
	telegramGrpcAddr   string
	grpcClient         pb.TelegramServiceClient
	grpcConn           *grpc.ClientConn
	adminAPIKey        string
	logger             *slog.Logger
	deadLetterService  *DeadLetterService
}

// ArbitrageOpportunity represents an arbitrage opportunity for notification.
// Note: This struct might be duplicative of models.ArbitrageOpportunity, but used here for JSON marshaling.
type ArbitrageOpportunity struct {
	// Symbol is the trading pair.
	Symbol string `json:"symbol"`
	// BuyExchange is the buying exchange.
	BuyExchange string `json:"buy_exchange"`
	// SellExchange is the selling exchange.
	SellExchange string `json:"sell_exchange"`
	// BuyPrice is the buy price.
	BuyPrice float64 `json:"buy_price"`
	// SellPrice is the sell price.
	SellPrice float64 `json:"sell_price"`
	// ProfitPercent is the profit percentage.
	ProfitPercent float64 `json:"profit_percent"`
	// ProfitAmount is the profit amount.
	ProfitAmount float64 `json:"profit_amount"`
	// Volume is the volume.
	Volume float64 `json:"volume"`
	// Timestamp is the detection time.
	Timestamp time.Time `json:"timestamp"`
	// OpportunityType is the type (arbitrage, technical, etc).
	OpportunityType string `json:"opportunity_type"` // "arbitrage", "technical", "ai_generated"
}

// TechnicalSignalNotification represents a technical analysis signal for notifications.
type TechnicalSignalNotification struct {
	// Symbol is the trading pair.
	Symbol string `json:"symbol"`
	// SignalType is the type of signal.
	SignalType string `json:"signal_type"`
	// Action is the recommended action (buy/sell).
	Action string `json:"action"`
	// SignalText is the description.
	SignalText string `json:"signal_text"`
	// CurrentPrice is the asset price.
	CurrentPrice float64 `json:"current_price"`
	// EntryRange is the recommended entry price range.
	EntryRange string `json:"entry_range"`
	// Targets are the profit targets.
	Targets []Target `json:"targets"`
	// StopLoss is the stop loss level.
	StopLoss StopLoss `json:"stop_loss"`
	// RiskReward is the R:R ratio.
	RiskReward string `json:"risk_reward"`
	// Exchanges is the list of applicable exchanges.
	Exchanges []string `json:"exchanges"`
	// Timeframe is the analysis timeframe.
	Timeframe string `json:"timeframe"`
	// Confidence is the signal confidence level.
	Confidence float64 `json:"confidence"`
	// Timestamp is the signal generation time.
	Timestamp time.Time `json:"timestamp"`
}

// Target represents a profit target price.
type Target struct {
	// Price is the target price.
	Price float64 `json:"price"`
	// Profit is the projected profit percentage.
	Profit float64 `json:"profit"`
}

// StopLoss represents a stop loss level.
type StopLoss struct {
	// Price is the stop loss price.
	Price float64 `json:"price"`
	// Risk is the projected loss percentage.
	Risk float64 `json:"risk"`
}

// NewNotificationService creates a new notification service.
//
// Parameters:
//
//	db: Database connection.
//	redis: Redis client.
//	telegramServiceURL: URL of the Telegram service.
//	telegramGrpcAddress: gRPC address of the Telegram service.
//	adminAPIKey: Admin API key for authentication.
//
// Returns:
//
//	*NotificationService: Initialized service.
func NewNotificationService(db DBPool, redis *database.RedisClient, telegramServiceURL, telegramGrpcAddress, adminAPIKey string) *NotificationService {
	var deadLetterService *DeadLetterService
	if postgresDB, ok := db.(*database.PostgresDB); ok {
		deadLetterService = NewDeadLetterService(postgresDB)
	}

	ns := &NotificationService{
		db:                 db,
		redis:              redis,
		telegramServiceURL: telegramServiceURL,
		telegramGrpcAddr:   telegramGrpcAddress,
		adminAPIKey:        adminAPIKey,
		logger:             telemetry.Logger(),
		deadLetterService:  deadLetterService,
	}

	if telegramGrpcAddress != "" {
		// Use insecure credentials for internal communication
		conn, err := grpc.NewClient(telegramGrpcAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			ns.logger.Error("Failed to connect to Telegram gRPC service", "address", telegramGrpcAddress, "error", err)
		} else {
			ns.grpcClient = pb.NewTelegramServiceClient(conn)
			ns.grpcConn = conn
			ns.logger.Info("Connected to Telegram gRPC service", "address", telegramGrpcAddress)
		}
	}

	return ns
}

// TelegramErrorCode represents structured error codes from Telegram service
type TelegramErrorCode string

const (
	TelegramErrorUserBlocked    TelegramErrorCode = "USER_BLOCKED"
	TelegramErrorChatNotFound   TelegramErrorCode = "CHAT_NOT_FOUND"
	TelegramErrorRateLimited    TelegramErrorCode = "RATE_LIMITED"
	TelegramErrorInvalidRequest TelegramErrorCode = "INVALID_REQUEST"
	TelegramErrorNetworkError   TelegramErrorCode = "NETWORK_ERROR"
	TelegramErrorTimeout        TelegramErrorCode = "TIMEOUT"
	TelegramErrorInternal       TelegramErrorCode = "INTERNAL_ERROR"
	TelegramErrorUnknown        TelegramErrorCode = "UNKNOWN"
)

// TelegramSendResult represents the result of sending a Telegram message
type TelegramSendResult struct {
	OK         bool
	MessageID  string
	Error      string
	ErrorCode  TelegramErrorCode
	RetryAfter int32
}

// isRetryableError checks if an error code indicates a retryable error
func isRetryableError(code TelegramErrorCode) bool {
	switch code {
	case TelegramErrorRateLimited, TelegramErrorNetworkError, TelegramErrorTimeout, TelegramErrorInternal:
		return true
	default:
		return false
	}
}

// sendTelegramMessage sends a message via the Telegram Service.
func (ns *NotificationService) sendTelegramMessage(ctx context.Context, chatID int64, text string) error {
	result := ns.sendTelegramMessageWithResult(ctx, chatID, text)
	if result.OK {
		return nil
	}
	return fmt.Errorf("%s: %s", result.ErrorCode, result.Error)
}

// sendTelegramMessageWithResult sends a message and returns structured result
func (ns *NotificationService) sendTelegramMessageWithResult(ctx context.Context, chatID int64, text string) TelegramSendResult {
	spanCtx, span := observability.StartSpanWithTags(ctx, observability.SpanOpNotification, "NotificationService.sendTelegramMessage", map[string]string{
		"chat_id": fmt.Sprintf("%d", chatID),
	})
	defer observability.FinishSpan(span, nil)

	// Try gRPC first
	if ns.grpcClient != nil {
		grpcCtx, grpcSpan := observability.StartSpan(spanCtx, observability.SpanOpGRPC, "telegram.SendMessage")
		resp, err := ns.grpcClient.SendMessage(grpcCtx, &pb.SendMessageRequest{
			ChatId: fmt.Sprintf("%d", chatID),
			Text:   text,
		})
		observability.FinishSpan(grpcSpan, err)

		if err == nil && resp != nil {
			if resp.Ok {
				observability.AddBreadcrumb(spanCtx, "notification", "Telegram message sent via gRPC", sentry.LevelInfo)
				return TelegramSendResult{
					OK:        true,
					MessageID: resp.MessageId,
				}
			}
			// Message failed with structured error
			errorCode := TelegramErrorCode(resp.GetErrorCode())
			if errorCode == "" {
				errorCode = TelegramErrorUnknown
			}
			return TelegramSendResult{
				OK:         false,
				Error:      resp.Error,
				ErrorCode:  errorCode,
				RetryAfter: resp.GetRetryAfter(),
			}
		}

		if err != nil {
			ns.logger.Warn("Failed to send Telegram message via gRPC, falling back to HTTP", "error", err)
			observability.AddBreadcrumb(spanCtx, "notification", "gRPC failed, falling back to HTTP", sentry.LevelWarning)
		}
	}

	if ns.telegramServiceURL == "" {
		ns.logger.Warn("Telegram service URL not configured, skipping message")
		return TelegramSendResult{
			OK:        false,
			Error:     "Telegram service URL not configured",
			ErrorCode: TelegramErrorInvalidRequest,
		}
	}

	payload := map[string]interface{}{
		"chatId":    fmt.Sprintf("%d", chatID),
		"text":      text,
		"parseMode": "Markdown",
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		observability.CaptureException(spanCtx, err)
		return TelegramSendResult{
			OK:        false,
			Error:     err.Error(),
			ErrorCode: TelegramErrorInvalidRequest,
		}
	}

	httpCtx, httpSpan := observability.StartSpan(spanCtx, observability.SpanOpHTTPClient, "POST /send-message")
	req, err := http.NewRequestWithContext(httpCtx, "POST", ns.telegramServiceURL+"/send-message", bytes.NewBuffer(jsonData))
	if err != nil {
		observability.FinishSpan(httpSpan, err)
		return TelegramSendResult{
			OK:        false,
			Error:     err.Error(),
			ErrorCode: TelegramErrorNetworkError,
		}
	}

	req.Header.Set("Content-Type", "application/json")
	if ns.adminAPIKey != "" {
		req.Header.Set("X-API-Key", ns.adminAPIKey)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	// #nosec G704 -- URL is an internal service endpoint configured by trusted env
	resp, err := client.Do(req)
	if err != nil {
		observability.FinishSpan(httpSpan, err)
		observability.CaptureExceptionWithContext(spanCtx, err, "telegram_http_send", map[string]interface{}{
			"chat_id": chatID,
		})
		return TelegramSendResult{
			OK:        false,
			Error:     err.Error(),
			ErrorCode: TelegramErrorNetworkError,
		}
	}
	defer func() { _ = resp.Body.Close() }()

	httpSpan.SetData("http.status_code", resp.StatusCode)

	// Parse response body for error details
	var respBody struct {
		OK        bool   `json:"ok"`
		Error     string `json:"error"`
		ErrorCode string `json:"errorCode"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		// Couldn't parse response, but check status code
		if resp.StatusCode != http.StatusOK {
			err := fmt.Errorf("telegram service returned status: %d", resp.StatusCode)
			observability.FinishSpan(httpSpan, err)
			return TelegramSendResult{
				OK:        false,
				Error:     err.Error(),
				ErrorCode: TelegramErrorUnknown,
			}
		}
	}

	if resp.StatusCode != http.StatusOK || !respBody.OK {
		errorCode := TelegramErrorCode(respBody.ErrorCode)
		if errorCode == "" {
			errorCode = TelegramErrorUnknown
		}
		observability.FinishSpan(httpSpan, fmt.Errorf("%s", respBody.Error))
		return TelegramSendResult{
			OK:        false,
			Error:     respBody.Error,
			ErrorCode: errorCode,
		}
	}

	observability.FinishSpan(httpSpan, nil)
	observability.AddBreadcrumb(spanCtx, "notification", "Telegram message sent via HTTP", sentry.LevelInfo)
	return TelegramSendResult{OK: true}
}

// sendTelegramMessageWithRetry sends a message with retry logic for transient errors
func (ns *NotificationService) sendTelegramMessageWithRetry(ctx context.Context, chatID int64, text string, userID string) error {
	const maxRetries = 3
	baseDelay := time.Second

	var lastResult TelegramSendResult

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			// Calculate delay with exponential backoff and jitter
			delay := baseDelay * time.Duration(1<<(attempt-1))
			if lastResult.RetryAfter > 0 {
				delay = time.Duration(lastResult.RetryAfter) * time.Second
			}
			// Add jitter (random factor 0.5-1.5)
			jitter := 0.5 + (float64(time.Now().UnixNano()%1000) / 1000.0)
			delay = time.Duration(float64(delay) * jitter)

			ns.logger.Info("Retrying Telegram message", "attempt", attempt+1, "delay_ms", delay.Milliseconds(), "chat_id", chatID)
			time.Sleep(delay)
		}

		lastResult = ns.sendTelegramMessageWithResult(ctx, chatID, text)

		if lastResult.OK {
			if attempt > 0 {
				ns.logger.Info("Telegram message sent successfully after retry", "attempts", attempt+1, "chat_id", chatID)
			}
			return nil
		}

		// Handle non-retryable errors immediately
		if !isRetryableError(lastResult.ErrorCode) {
			ns.logger.Warn("Non-retryable Telegram error",
				"error_code", lastResult.ErrorCode,
				"error", lastResult.Error,
				"chat_id", chatID,
			)

			// Handle blocked users - mark them in database
			if lastResult.ErrorCode == TelegramErrorUserBlocked || lastResult.ErrorCode == TelegramErrorChatNotFound {
				if userID != "" {
					if err := ns.handleBlockedUser(ctx, userID, string(lastResult.ErrorCode)); err != nil {
						ns.logger.Error("Failed to mark user as blocked", "user_id", userID, "error", err)
					}
				}
			}

			return fmt.Errorf("%s: %s", lastResult.ErrorCode, lastResult.Error)
		}

		ns.logger.Warn("Retryable Telegram error",
			"attempt", attempt+1,
			"max_retries", maxRetries,
			"error_code", lastResult.ErrorCode,
			"error", lastResult.Error,
			"chat_id", chatID,
		)
	}

	// Add to dead letter queue for later processing
	if ns.deadLetterService != nil && userID != "" {
		chatIDStr := fmt.Sprintf("%d", chatID)
		_, dlqErr := ns.deadLetterService.AddToDeadLetter(
			ctx,
			userID,
			chatIDStr,
			"telegram_notification",
			text,
			string(lastResult.ErrorCode),
			lastResult.Error,
		)
		if dlqErr != nil {
			ns.logger.Error("Failed to add message to dead letter queue",
				"user_id", userID,
				"chat_id", chatID,
				"error", dlqErr,
			)
		}
	}

	return fmt.Errorf("failed after %d retries: %s: %s", maxRetries, lastResult.ErrorCode, lastResult.Error)
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

// logNotification records the notification in the database
func (ns *NotificationService) logNotification(ctx context.Context, userID, notificationType, message string) error {
	// Note: alert_id is nullable for notifications not tied to a specific alert
	query := `
		INSERT INTO alert_notifications (user_id, notification_type, message, sent_at)
		VALUES ($1, $2, $3, $4)
	`

	now := time.Now()
	_, err := ns.db.Exec(ctx, query, userID, notificationType, message, now)
	if err != nil {
		return fmt.Errorf("failed to log notification: %w", err)
	}

	return nil
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
	message.WriteString("🚀 *Aggregated Arbitrage Opportunities*\n\n")

	// Limit to top 5 opportunities to keep message manageable
	maxSignals := len(signals)
	if maxSignals > 5 {
		maxSignals = 5
	}

	for i, signal := range signals[:maxSignals] {
		fmt.Fprintf(&message, "*%d. %s*\n", i+1, signal.Symbol)
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
	message.WriteString("\n\n⚠️ *Trade at your own risk*")

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
	message.WriteString("📊 *Aggregated Technical Analysis*\n\n")

	// Limit to top 5 signals to keep message manageable
	maxSignals := len(signals)
	if maxSignals > 5 {
		maxSignals = 5
	}

	for i, signal := range signals[:maxSignals] {
		fmt.Fprintf(&message, "*%d. %s*\n", i+1, signal.Symbol)
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
	message.WriteString("\n\n⚠️ *Trade at your own risk*")

	return message.String()
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

// ProcessDeadLetterQueue processes pending dead letter entries and retries sending them
//
// Parameters:
//
//	ctx: Context.
//	batchSize: Maximum number of entries to process in this batch.
//
// Returns:
//
//	int: Number of successfully processed entries.
//	int: Number of failed entries.
//	error: Error if the operation fails.
func (ns *NotificationService) ProcessDeadLetterQueue(ctx context.Context, batchSize int) (int, int, error) {
	if ns.deadLetterService == nil {
		return 0, 0, fmt.Errorf("dead letter service not initialized")
	}

	// Get pending messages
	entries, err := ns.deadLetterService.GetPendingMessages(ctx, batchSize)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get pending messages: %w", err)
	}

	if len(entries) == 0 {
		return 0, 0, nil
	}

	ns.logger.Info("Processing dead letter queue", "batch_size", len(entries))

	successCount := 0
	failCount := 0

	for _, entry := range entries {
		// Mark as retrying
		if err := ns.deadLetterService.MarkAsRetrying(ctx, entry.ID); err != nil {
			ns.logger.Error("Failed to mark entry as retrying", "id", entry.ID, "error", err)
			continue
		}

		// Parse chat ID
		chatID, parseErr := strconv.ParseInt(entry.ChatID, 10, 64)
		if parseErr != nil {
			ns.logger.Error("Invalid chat ID in dead letter entry", "id", entry.ID, "chat_id", entry.ChatID)
			_ = ns.deadLetterService.UpdateDeadLetter(ctx, entry.ID, false, "INVALID_CHAT_ID", "Invalid chat ID format")
			failCount++
			continue
		}

		// Attempt to send the message
		result := ns.sendTelegramMessageWithResult(ctx, chatID, entry.MessageContent)

		if result.OK {
			// Success - update the dead letter entry
			if err := ns.deadLetterService.UpdateDeadLetter(ctx, entry.ID, true, "", ""); err != nil {
				ns.logger.Error("Failed to mark dead letter as success", "id", entry.ID, "error", err)
			}
			successCount++
			ns.logger.Info("Successfully resent dead letter message", "id", entry.ID, "chat_id", entry.ChatID)
		} else {
			// Failed - update with new error
			if err := ns.deadLetterService.UpdateDeadLetter(ctx, entry.ID, false, string(result.ErrorCode), result.Error); err != nil {
				ns.logger.Error("Failed to update dead letter error", "id", entry.ID, "error", err)
			}

			// Handle blocked users
			if result.ErrorCode == TelegramErrorUserBlocked || result.ErrorCode == TelegramErrorChatNotFound {
				if err := ns.handleBlockedUser(ctx, entry.UserID, string(result.ErrorCode)); err != nil {
					ns.logger.Error("Failed to mark user as blocked", "user_id", entry.UserID, "error", err)
				}
			}

			failCount++
			ns.logger.Warn("Failed to resend dead letter message",
				"id", entry.ID,
				"error_code", result.ErrorCode,
				"error", result.Error,
			)
		}
	}

	ns.logger.Info("Dead letter queue processing completed",
		"processed", len(entries),
		"success", successCount,
		"failed", failCount,
	)

	return successCount, failCount, nil
}

// GetDeadLetterStats returns statistics about the dead letter queue
//
// Parameters:
//
//	ctx: Context.
//
// Returns:
//
//	*DeadLetterStats: Statistics about the queue.
//	error: Error if the operation fails.
func (ns *NotificationService) GetDeadLetterStats(ctx context.Context) (*DeadLetterStats, error) {
	if ns.deadLetterService == nil {
		return nil, fmt.Errorf("dead letter service not initialized")
	}
	return ns.deadLetterService.GetDeadLetterStats(ctx)
}

// CleanupDeadLetters removes old successful or failed dead letter entries
//
// Parameters:
//
//	ctx: Context.
//	olderThan: Remove entries older than this duration.
//
// Returns:
//
//	int: Number of entries removed.
//	error: Error if the operation fails.
func (ns *NotificationService) CleanupDeadLetters(ctx context.Context, olderThan time.Duration) (int, error) {
	if ns.deadLetterService == nil {
		return 0, fmt.Errorf("dead letter service not initialized")
	}
	return ns.deadLetterService.CleanupOldEntries(ctx, olderThan)
}

type QuestProgressNotification struct {
	QuestID       string
	QuestName     string
	Current       int
	Target        int
	Percent       int
	Status        string
	TimeRemaining string
}

func (ns *NotificationService) NotifyQuestProgress(ctx context.Context, chatID int64, progress QuestProgressNotification) error {
	spanCtx, span := observability.StartSpanWithTags(ctx, observability.SpanOpNotification, "NotificationService.NotifyQuestProgress", map[string]string{
		"chat_id":  fmt.Sprintf("%d", chatID),
		"quest_id": progress.QuestID,
	})
	defer observability.FinishSpan(span, nil)

	message := ns.formatQuestProgressMessage(progress)

	if err := ns.sendTelegramMessage(spanCtx, chatID, message); err != nil {
		ns.logger.Error("Failed to send quest progress notification",
			"chat_id", chatID,
			"quest_id", progress.QuestID,
			"error", err,
		)
		return err
	}

	ns.logger.Info("Sent quest progress notification",
		"chat_id", chatID,
		"quest_id", progress.QuestID,
		"percent", progress.Percent,
	)

	return nil
}

func (ns *NotificationService) formatQuestProgressMessage(progress QuestProgressNotification) string {
	var statusEmoji string
	switch progress.Status {
	case "completed":
		statusEmoji = "✅"
	case "failed":
		statusEmoji = "❌"
	case "expired":
		statusEmoji = "⏰"
	default:
		statusEmoji = "🎯"
	}

	lines := []string{
		fmt.Sprintf("%s **Quest Progress Update**", statusEmoji),
		"",
		fmt.Sprintf("**%s**", progress.QuestName),
		fmt.Sprintf("Progress: %d/%d (%d%%)", progress.Current, progress.Target, progress.Percent),
	}

	if progress.Status == "completed" {
		lines = append(lines, "", "🎉 Quest completed!")
	} else if progress.TimeRemaining != "" {
		lines = append(lines, fmt.Sprintf("Time remaining: %s", progress.TimeRemaining))
	}

	progressBar := ns.generateProgressBar(progress.Percent, 10)
	lines = append(lines, "", progressBar)

	return fmt.Sprintf("```\n%s\n```", joinNotificationLines(lines))
}

type RiskEventNotification struct {
	EventType string
	Severity  string
	Message   string
	Details   map[string]string
}

func (ns *NotificationService) NotifyRiskEvent(ctx context.Context, chatID int64, event RiskEventNotification) error {
	spanCtx, span := observability.StartSpanWithTags(ctx, observability.SpanOpNotification, "NotificationService.NotifyRiskEvent", map[string]string{
		"chat_id":    fmt.Sprintf("%d", chatID),
		"event_type": event.EventType,
		"severity":   event.Severity,
	})
	defer observability.FinishSpan(span, nil)

	message := ns.formatRiskEventMessage(event)

	if err := ns.sendTelegramMessage(spanCtx, chatID, message); err != nil {
		ns.logger.Error("Failed to send risk event notification",
			"chat_id", chatID,
			"event_type", event.EventType,
			"error", err,
		)
		return err
	}

	ns.logger.Info("Sent risk event notification",
		"chat_id", chatID,
		"event_type", event.EventType,
		"severity", event.Severity,
	)

	return nil
}

func (ns *NotificationService) formatRiskEventMessage(event RiskEventNotification) string {
	var severityEmoji string
	switch event.Severity {
	case "critical":
		severityEmoji = "🚨"
	case "high":
		severityEmoji = "⚠️"
	case "medium":
		severityEmoji = "⚡"
	case "low":
		severityEmoji = "ℹ️"
	default:
		severityEmoji = "⚠️"
	}

	lines := []string{
		fmt.Sprintf("%s **Risk Event Alert**", severityEmoji),
		"",
		fmt.Sprintf("**Type:** %s", event.EventType),
		fmt.Sprintf("**Severity:** %s", event.Severity),
		"",
		event.Message,
	}

	if len(event.Details) > 0 {
		lines = append(lines, "", "**Details:**")
		for key, value := range event.Details {
			lines = append(lines, fmt.Sprintf("• %s: %s", key, value))
		}
	}

	lines = append(lines, "", fmt.Sprintf("_Time: %s_", time.Now().UTC().Format(time.RFC3339)))

	return fmt.Sprintf("```\n%s\n```", joinNotificationLines(lines))
}

type FundMilestoneNotification struct {
	MilestoneType  string
	CurrentValue   string
	TargetValue    string
	PercentReached int
	Achievement    string
}

func (ns *NotificationService) NotifyFundMilestone(ctx context.Context, chatID int64, milestone FundMilestoneNotification) error {
	spanCtx, span := observability.StartSpanWithTags(ctx, observability.SpanOpNotification, "NotificationService.NotifyFundMilestone", map[string]string{
		"chat_id":        fmt.Sprintf("%d", chatID),
		"milestone_type": milestone.MilestoneType,
	})
	defer observability.FinishSpan(span, nil)

	message := ns.formatFundMilestoneMessage(milestone)

	if err := ns.sendTelegramMessage(spanCtx, chatID, message); err != nil {
		ns.logger.Error("Failed to send fund milestone notification",
			"chat_id", chatID,
			"milestone_type", milestone.MilestoneType,
			"error", err,
		)
		return err
	}

	ns.logger.Info("Sent fund milestone notification",
		"chat_id", chatID,
		"milestone_type", milestone.MilestoneType,
		"percent", milestone.PercentReached,
	)

	return nil
}

func (ns *NotificationService) formatFundMilestoneMessage(milestone FundMilestoneNotification) string {
	lines := []string{
		"💰 **Fund Milestone Reached!**",
		"",
		fmt.Sprintf("**%s**", milestone.Achievement),
		"",
		fmt.Sprintf("Current: %s", milestone.CurrentValue),
		fmt.Sprintf("Target: %s", milestone.TargetValue),
		fmt.Sprintf("Progress: %d%%", milestone.PercentReached),
	}

	progressBar := ns.generateProgressBar(milestone.PercentReached, 20)
	lines = append(lines, "", progressBar)

	return fmt.Sprintf("```\n%s\n```", joinNotificationLines(lines))
}

type AIReasoningNotification struct {
	DecisionType string
	Summary      string
	Confidence   float64
	Reasons      []string
	Action       string
}

func (ns *NotificationService) NotifyAIReasoning(ctx context.Context, chatID int64, reasoning AIReasoningNotification) error {
	spanCtx, span := observability.StartSpanWithTags(ctx, observability.SpanOpNotification, "NotificationService.NotifyAIReasoning", map[string]string{
		"chat_id":       fmt.Sprintf("%d", chatID),
		"decision_type": reasoning.DecisionType,
	})
	defer observability.FinishSpan(span, nil)

	message := ns.formatAIReasoningMessage(reasoning)

	if err := ns.sendTelegramMessage(spanCtx, chatID, message); err != nil {
		ns.logger.Error("Failed to send AI reasoning notification",
			"chat_id", chatID,
			"decision_type", reasoning.DecisionType,
			"error", err,
		)
		return err
	}

	ns.logger.Info("Sent AI reasoning notification",
		"chat_id", chatID,
		"decision_type", reasoning.DecisionType,
		"confidence", reasoning.Confidence,
	)

	return nil
}

func (ns *NotificationService) formatAIReasoningMessage(reasoning AIReasoningNotification) string {
	confidencePercent := int(reasoning.Confidence * 100)

	var confidenceEmoji string
	switch {
	case reasoning.Confidence >= 0.8:
		confidenceEmoji = "🟢"
	case reasoning.Confidence >= 0.6:
		confidenceEmoji = "🟡"
	default:
		confidenceEmoji = "🔴"
	}

	lines := []string{
		"🤖 **AI Trading Decision**",
		"",
		fmt.Sprintf("**Type:** %s", reasoning.DecisionType),
		fmt.Sprintf("**Confidence:** %s %d%%", confidenceEmoji, confidencePercent),
		"",
		fmt.Sprintf("**Summary:** %s", reasoning.Summary),
	}

	if len(reasoning.Reasons) > 0 {
		lines = append(lines, "", "**Key Factors:**")
		for i, reason := range reasoning.Reasons {
			if i < 5 {
				lines = append(lines, fmt.Sprintf("• %s", reason))
			}
		}
		if len(reasoning.Reasons) > 5 {
			lines = append(lines, fmt.Sprintf("• ... and %d more factors", len(reasoning.Reasons)-5))
		}
	}

	if reasoning.Action != "" {
		lines = append(lines, "", fmt.Sprintf("**Recommended Action:** %s", reasoning.Action))
	}

	return fmt.Sprintf("```\n%s\n```", joinNotificationLines(lines))
}

func (ns *NotificationService) generateProgressBar(percent, width int) string {
	if percent > 100 {
		percent = 100
	}
	if percent < 0 {
		percent = 0
	}

	filled := (percent * width) / 100
	bar := ""
	for i := 0; i < width; i++ {
		if i < filled {
			bar += "█"
		} else {
			bar += "░"
		}
	}
	return fmt.Sprintf("[%s] %d%%", bar, percent)
}

func joinNotificationLines(lines []string) string {
	result := ""
	for i, line := range lines {
		if i > 0 {
			result += "\n"
		}
		result += line
	}
	return result
}
