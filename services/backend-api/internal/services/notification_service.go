package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/getsentry/sentry-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/irfndi/neuratrade/internal/config"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/irfndi/neuratrade/internal/grpcutil"
	"github.com/irfndi/neuratrade/internal/observability"
	"github.com/irfndi/neuratrade/internal/telemetry"
	pb "github.com/irfndi/neuratrade/pkg/pb/telegram"
)

// NotificationService handles sending notifications to users.
type NotificationService struct {
	db                      DBPool
	redis                   *database.RedisClient
	telegramServiceURL      string
	telegramGrpcAddr        string
	grpcClient              pb.TelegramServiceClient
	grpcConn                *grpc.ClientConn
	httpClient              *http.Client
	adminAPIKey             string
	logger                  *slog.Logger
	deadLetterService       *DeadLetterService
	aiReasoningThrottle     *AlertThrottler
	telegramMaxMessageUnits int
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
		db:                      db,
		redis:                   redis,
		telegramServiceURL:      telegramServiceURL,
		telegramGrpcAddr:        telegramGrpcAddress,
		httpClient:              &http.Client{Timeout: 10 * time.Second},
		adminAPIKey:             adminAPIKey,
		logger:                  telemetry.Logger(),
		deadLetterService:       deadLetterService,
		aiReasoningThrottle:     NewAlertThrottler(90 * time.Second),
		telegramMaxMessageUnits: loadTelegramMaxMessageUnits(),
	}

	if telegramGrpcAddress != "" {
		dialOpts, dialErr := grpcutil.DialOptions(config.GRPCClientConfig{
			TLSCACertFile: os.Getenv("NEURATRADE_GRPC_TLS_CA_FILE"),
			ServerName:    grpcutil.HostOnly(telegramGrpcAddress),
		})
		if dialErr != nil {
			ns.logger.Error("Failed to resolve Telegram gRPC dial options", "address", telegramGrpcAddress, "error", dialErr)
		} else {
			conn, err := grpc.NewClient(telegramGrpcAddress, dialOpts...)
			if err != nil {
				ns.logger.Error("Failed to connect to Telegram gRPC service", "address", telegramGrpcAddress, "error", err)
			} else {
				ns.grpcClient = pb.NewTelegramServiceClient(conn)
				ns.grpcConn = conn
				ns.logger.Info("Connected to Telegram gRPC service", "address", telegramGrpcAddress)
			}
		}
	}

	return ns
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

	// Try gRPC first when service-to-service auth is configured.
	if ns.grpcClient != nil && ns.adminAPIKey != "" {
		grpcCtx, grpcSpan := observability.StartSpan(spanCtx, observability.SpanOpGRPC, "telegram.SendMessage")
		grpcCtx = metadata.AppendToOutgoingContext(grpcCtx, "x-api-key", ns.adminAPIKey)
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
	} else if ns.grpcClient != nil {
		ns.logger.Warn("Telegram gRPC admin API key not configured, falling back to HTTP")
	}

	if ns.telegramServiceURL == "" {
		ns.logger.Warn("Telegram service URL not configured, skipping message")
		return TelegramSendResult{
			OK:        false,
			Error:     "Telegram service URL not configured",
			ErrorCode: TelegramErrorInvalidRequest,
		}
	}

	// Send as plain text - messages are formatted without Markdown to avoid parse errors
	// from special characters in interpolated values (underscores, asterisks, etc.)
	payload := map[string]interface{}{
		"chatId": fmt.Sprintf("%d", chatID),
		"text":   text,
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

	// #nosec G704 -- URL is an internal service endpoint configured by trusted env
	resp, err := ns.httpClient.Do(req)
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
