package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/irfandi/celebrum-ai-go/internal/database"
	"github.com/irfandi/celebrum-ai-go/internal/telemetry"
)

// DeadLetterStatus represents the status of a dead letter entry
type DeadLetterStatus string

const (
	DeadLetterStatusPending  DeadLetterStatus = "pending"
	DeadLetterStatusRetrying DeadLetterStatus = "retrying"
	DeadLetterStatusFailed   DeadLetterStatus = "failed"
	DeadLetterStatusSuccess  DeadLetterStatus = "success"
)

// DeadLetterEntry represents a failed notification message
type DeadLetterEntry struct {
	ID             string           `json:"id"`
	UserID         string           `json:"user_id"`
	ChatID         string           `json:"chat_id"`
	MessageType    string           `json:"message_type"`
	MessageContent string           `json:"message_content"`
	ErrorCode      string           `json:"error_code"`
	ErrorMessage   string           `json:"error_message"`
	Attempts       int              `json:"attempts"`
	Status         DeadLetterStatus `json:"status"`
	CreatedAt      time.Time        `json:"created_at"`
	LastAttemptAt  time.Time        `json:"last_attempt_at"`
	NextRetryAt    *time.Time       `json:"next_retry_at"`
}

// DeadLetterStats provides statistics about the dead letter queue
type DeadLetterStats struct {
	TotalCount    int            `json:"total_count"`
	PendingCount  int            `json:"pending_count"`
	RetryingCount int            `json:"retrying_count"`
	FailedCount   int            `json:"failed_count"`
	SuccessCount  int            `json:"success_count"`
	ByErrorCode   map[string]int `json:"by_error_code"`
	OldestPending *time.Time     `json:"oldest_pending"`
}

// DeadLetterService handles failed notification messages
type DeadLetterService struct {
	db     *database.PostgresDB
	logger *slog.Logger
}

// NewDeadLetterService creates a new dead letter service
func NewDeadLetterService(db *database.PostgresDB) *DeadLetterService {
	return &DeadLetterService{
		db:     db,
		logger: telemetry.Logger(),
	}
}

// AddToDeadLetter stores a failed message in the dead letter queue
//
// Parameters:
//
//	ctx: Context.
//	userID: The user ID.
//	chatID: The Telegram chat ID.
//	messageType: Type of message (e.g., "arbitrage_alert", "technical_signal").
//	messageContent: The message content that failed to send.
//	errorCode: Error code from the sending attempt.
//	errorMessage: Error message from the sending attempt.
//
// Returns:
//
//	string: The ID of the created dead letter entry.
//	error: Error if the operation fails.
func (dls *DeadLetterService) AddToDeadLetter(
	ctx context.Context,
	userID, chatID, messageType, messageContent, errorCode, errorMessage string,
) (string, error) {
	id := uuid.New().String()

	// Calculate next retry time based on error type
	var nextRetryAt *time.Time
	if isRetryableErrorCode(errorCode) {
		// Retry in 5 minutes for retryable errors
		t := time.Now().Add(5 * time.Minute)
		nextRetryAt = &t
	}

	query := `
		INSERT INTO notification_dead_letters
		(id, user_id, chat_id, message_type, message_content, error_code, error_message, attempts, status, next_retry_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 1, 'pending', $8)
	`

	_, err := dls.db.Pool.Exec(ctx, query,
		id, userID, chatID, messageType, messageContent, errorCode, errorMessage, nextRetryAt,
	)
	if err != nil {
		return "", fmt.Errorf("failed to insert dead letter entry: %w", err)
	}

	dls.logger.Info("Added message to dead letter queue",
		"id", id,
		"user_id", userID,
		"error_code", errorCode,
		"next_retry_at", nextRetryAt,
	)

	return id, nil
}

// UpdateDeadLetter updates a dead letter entry after a retry attempt
//
// Parameters:
//
//	ctx: Context.
//	id: The dead letter entry ID.
//	success: Whether the retry was successful.
//	errorCode: New error code if failed.
//	errorMessage: New error message if failed.
//
// Returns:
//
//	error: Error if the operation fails.
func (dls *DeadLetterService) UpdateDeadLetter(
	ctx context.Context,
	id string,
	success bool,
	errorCode, errorMessage string,
) error {
	var query string
	var args []interface{}

	if success {
		query = `
			UPDATE notification_dead_letters
			SET status = 'success',
			    last_attempt_at = NOW(),
			    next_retry_at = NULL
			WHERE id = $1
		`
		args = []interface{}{id}
	} else {
		// Check current attempts and increment
		var currentAttempts int
		err := dls.db.Pool.QueryRow(ctx, "SELECT attempts FROM notification_dead_letters WHERE id = $1", id).Scan(&currentAttempts)
		if err != nil {
			return fmt.Errorf("failed to get current attempts: %w", err)
		}

		newAttempts := currentAttempts + 1
		maxAttempts := 5

		var status DeadLetterStatus
		var nextRetryAt *time.Time

		if newAttempts >= maxAttempts || !isRetryableErrorCode(errorCode) {
			status = DeadLetterStatusFailed
		} else {
			status = DeadLetterStatusPending
			// Exponential backoff: 5min, 15min, 45min, 2h
			delay := time.Duration(5*pow(3, newAttempts-1)) * time.Minute
			if delay > 2*time.Hour {
				delay = 2 * time.Hour
			}
			t := time.Now().Add(delay)
			nextRetryAt = &t
		}

		query = `
			UPDATE notification_dead_letters
			SET status = $2,
			    attempts = $3,
			    error_code = $4,
			    error_message = $5,
			    last_attempt_at = NOW(),
			    next_retry_at = $6
			WHERE id = $1
		`
		args = []interface{}{id, status, newAttempts, errorCode, errorMessage, nextRetryAt}
	}

	_, err := dls.db.Pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to update dead letter entry: %w", err)
	}

	dls.logger.Info("Updated dead letter entry",
		"id", id,
		"success", success,
	)

	return nil
}

// GetPendingMessages retrieves messages ready for retry
//
// Parameters:
//
//	ctx: Context.
//	limit: Maximum number of messages to retrieve.
//
// Returns:
//
//	[]DeadLetterEntry: List of pending messages.
//	error: Error if the operation fails.
func (dls *DeadLetterService) GetPendingMessages(ctx context.Context, limit int) ([]DeadLetterEntry, error) {
	query := `
		SELECT id, user_id, chat_id, message_type, message_content,
		       COALESCE(error_code, ''), COALESCE(error_message, ''),
		       attempts, status, created_at, last_attempt_at, next_retry_at
		FROM notification_dead_letters
		WHERE status IN ('pending', 'retrying')
		  AND (next_retry_at IS NULL OR next_retry_at <= NOW())
		ORDER BY created_at ASC
		LIMIT $1
	`

	rows, err := dls.db.Pool.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query pending messages: %w", err)
	}
	defer rows.Close()

	var entries []DeadLetterEntry
	for rows.Next() {
		var entry DeadLetterEntry
		err := rows.Scan(
			&entry.ID, &entry.UserID, &entry.ChatID, &entry.MessageType,
			&entry.MessageContent, &entry.ErrorCode, &entry.ErrorMessage,
			&entry.Attempts, &entry.Status, &entry.CreatedAt,
			&entry.LastAttemptAt, &entry.NextRetryAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan dead letter entry: %w", err)
		}
		entries = append(entries, entry)
	}

	return entries, nil
}

// MarkAsRetrying marks a dead letter entry as currently being retried
//
// Parameters:
//
//	ctx: Context.
//	id: The dead letter entry ID.
//
// Returns:
//
//	error: Error if the operation fails.
func (dls *DeadLetterService) MarkAsRetrying(ctx context.Context, id string) error {
	query := `
		UPDATE notification_dead_letters
		SET status = 'retrying'
		WHERE id = $1
	`

	_, err := dls.db.Pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to mark as retrying: %w", err)
	}

	return nil
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
func (dls *DeadLetterService) GetDeadLetterStats(ctx context.Context) (*DeadLetterStats, error) {
	stats := &DeadLetterStats{
		ByErrorCode: make(map[string]int),
	}

	// Get counts by status
	statusQuery := `
		SELECT status, COUNT(*)
		FROM notification_dead_letters
		GROUP BY status
	`
	rows, err := dls.db.Pool.Query(ctx, statusQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to query status counts: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, fmt.Errorf("failed to scan status count: %w", err)
		}
		stats.TotalCount += count
		switch DeadLetterStatus(status) {
		case DeadLetterStatusPending:
			stats.PendingCount = count
		case DeadLetterStatusRetrying:
			stats.RetryingCount = count
		case DeadLetterStatusFailed:
			stats.FailedCount = count
		case DeadLetterStatusSuccess:
			stats.SuccessCount = count
		}
	}

	// Get counts by error code
	errorQuery := `
		SELECT COALESCE(error_code, 'UNKNOWN'), COUNT(*)
		FROM notification_dead_letters
		WHERE status != 'success'
		GROUP BY error_code
	`
	rows, err = dls.db.Pool.Query(ctx, errorQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to query error code counts: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var errorCode string
		var count int
		if err := rows.Scan(&errorCode, &count); err != nil {
			return nil, fmt.Errorf("failed to scan error code count: %w", err)
		}
		stats.ByErrorCode[errorCode] = count
	}

	// Get oldest pending message
	oldestQuery := `
		SELECT created_at
		FROM notification_dead_letters
		WHERE status IN ('pending', 'retrying')
		ORDER BY created_at ASC
		LIMIT 1
	`
	var oldestTime time.Time
	err = dls.db.Pool.QueryRow(ctx, oldestQuery).Scan(&oldestTime)
	if err == nil {
		stats.OldestPending = &oldestTime
	}

	return stats, nil
}

// CleanupOldEntries removes old successful or failed entries
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
func (dls *DeadLetterService) CleanupOldEntries(ctx context.Context, olderThan time.Duration) (int, error) {
	cutoff := time.Now().Add(-olderThan)

	query := `
		DELETE FROM notification_dead_letters
		WHERE status IN ('success', 'failed')
		  AND created_at < $1
	`

	result, err := dls.db.Pool.Exec(ctx, query, cutoff)
	if err != nil {
		return 0, fmt.Errorf("failed to cleanup old entries: %w", err)
	}

	count := int(result.RowsAffected())

	if count > 0 {
		dls.logger.Info("Cleaned up old dead letter entries",
			"count", count,
			"older_than", olderThan.String(),
		)
	}

	return count, nil
}

// GetUserDeadLetters retrieves dead letter entries for a specific user
//
// Parameters:
//
//	ctx: Context.
//	userID: The user ID.
//	limit: Maximum number of entries to retrieve.
//
// Returns:
//
//	[]DeadLetterEntry: List of dead letter entries for the user.
//	error: Error if the operation fails.
func (dls *DeadLetterService) GetUserDeadLetters(ctx context.Context, userID string, limit int) ([]DeadLetterEntry, error) {
	query := `
		SELECT id, user_id, chat_id, message_type, message_content,
		       COALESCE(error_code, ''), COALESCE(error_message, ''),
		       attempts, status, created_at, last_attempt_at, next_retry_at
		FROM notification_dead_letters
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`

	rows, err := dls.db.Pool.Query(ctx, query, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query user dead letters: %w", err)
	}
	defer rows.Close()

	var entries []DeadLetterEntry
	for rows.Next() {
		var entry DeadLetterEntry
		err := rows.Scan(
			&entry.ID, &entry.UserID, &entry.ChatID, &entry.MessageType,
			&entry.MessageContent, &entry.ErrorCode, &entry.ErrorMessage,
			&entry.Attempts, &entry.Status, &entry.CreatedAt,
			&entry.LastAttemptAt, &entry.NextRetryAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan dead letter entry: %w", err)
		}
		entries = append(entries, entry)
	}

	return entries, nil
}

// ExportForAnalysis exports dead letter entries as JSON for analysis
//
// Parameters:
//
//	ctx: Context.
//	limit: Maximum number of entries to export.
//
// Returns:
//
//	[]byte: JSON-encoded dead letter entries.
//	error: Error if the operation fails.
func (dls *DeadLetterService) ExportForAnalysis(ctx context.Context, limit int) ([]byte, error) {
	query := `
		SELECT id, user_id, chat_id, message_type, message_content,
		       COALESCE(error_code, ''), COALESCE(error_message, ''),
		       attempts, status, created_at, last_attempt_at, next_retry_at
		FROM notification_dead_letters
		ORDER BY created_at DESC
		LIMIT $1
	`

	rows, err := dls.db.Pool.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query for export: %w", err)
	}
	defer rows.Close()

	var entries []DeadLetterEntry
	for rows.Next() {
		var entry DeadLetterEntry
		err := rows.Scan(
			&entry.ID, &entry.UserID, &entry.ChatID, &entry.MessageType,
			&entry.MessageContent, &entry.ErrorCode, &entry.ErrorMessage,
			&entry.Attempts, &entry.Status, &entry.CreatedAt,
			&entry.LastAttemptAt, &entry.NextRetryAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan dead letter entry: %w", err)
		}
		entries = append(entries, entry)
	}

	return json.Marshal(entries)
}

// Helper function to check if an error code is retryable
func isRetryableErrorCode(code string) bool {
	switch code {
	case "RATE_LIMITED", "NETWORK_ERROR", "TIMEOUT", "INTERNAL_ERROR":
		return true
	default:
		return false
	}
}

// Helper function for integer power
func pow(base, exp int) int {
	result := 1
	for i := 0; i < exp; i++ {
		result *= base
	}
	return result
}
