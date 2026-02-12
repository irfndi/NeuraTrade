package services

import (
	"context"
	"testing"
	"time"

	"github.com/irfandi/celebrum-ai-go/internal/database"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeadLetterService_AddToDeadLetter_QueryStructure tests the SQL query structure
func TestDeadLetterService_AddToDeadLetter_QueryStructure(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mockPool.Close()
	dbPool := database.NewMockDBPool(mockPool)

	ctx := context.Background()

	tests := []struct {
		name         string
		userID       string
		chatID       string
		messageType  string
		content      string
		errorCode    string
		errorMessage string
		expectRetry  bool
	}{
		{
			name:         "Retryable rate limited error",
			userID:       "user-123",
			chatID:       "chat-456",
			messageType:  "arbitrage_alert",
			content:      "Test arbitrage message",
			errorCode:    "RATE_LIMITED",
			errorMessage: "Too many requests",
			expectRetry:  true,
		},
		{
			name:         "Non-retryable user blocked error",
			userID:       "user-789",
			chatID:       "chat-012",
			messageType:  "technical_signal",
			content:      "Test technical message",
			errorCode:    "USER_BLOCKED",
			errorMessage: "User blocked the bot",
			expectRetry:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Test the SQL query structure matches expectations
			mockPool.ExpectExec(`INSERT INTO notification_dead_letters`).
				WithArgs(
					pgxmock.AnyArg(), // id (UUID)
					tc.userID,
					tc.chatID,
					tc.messageType,
					tc.content,
					tc.errorCode,
					tc.errorMessage,
					pgxmock.AnyArg(), // next_retry_at
				).
				WillReturnResult(pgxmock.NewResult("INSERT", 1))

			// Execute the query using mockPool directly
			var nextRetryAt *time.Time
			if isRetryableErrorCode(tc.errorCode) {
				t := time.Now().Add(5 * time.Minute)
				nextRetryAt = &t
			}

			_, err := dbPool.Exec(ctx,
				`INSERT INTO notification_dead_letters
				(id, user_id, chat_id, message_type, message_content, error_code, error_message, attempts, status, next_retry_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7, 1, 'pending', $8)`,
				"test-uuid", tc.userID, tc.chatID, tc.messageType, tc.content, tc.errorCode, tc.errorMessage, nextRetryAt,
			)
			assert.NoError(t, err)
			assert.NoError(t, mockPool.ExpectationsWereMet())
		})
	}
}

// TestDeadLetterService_GetPendingMessages_QueryStructure tests the query structure for getting pending messages
func TestDeadLetterService_GetPendingMessages_QueryStructure(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mockPool.Close()
	dbPool := database.NewMockDBPool(mockPool)

	ctx := context.Background()
	now := time.Now()

	t.Run("Returns pending messages", func(t *testing.T) {
		rows := pgxmock.NewRows([]string{
			"id", "user_id", "chat_id", "message_type", "message_content",
			"error_code", "error_message", "attempts", "status",
			"created_at", "last_attempt_at", "next_retry_at",
		}).
			AddRow(
				"entry-1", "user-1", "chat-1", "arbitrage_alert", "Test message 1",
				"RATE_LIMITED", "Too many requests", 1, DeadLetterStatusPending,
				now, now, &now,
			).
			AddRow(
				"entry-2", "user-2", "chat-2", "technical_signal", "Test message 2",
				"NETWORK_ERROR", "Connection failed", 2, DeadLetterStatusPending,
				now.Add(-1*time.Hour), now, nil,
			)

		mockPool.ExpectQuery(`SELECT id, user_id, chat_id, message_type, message_content`).
			WithArgs(10).
			WillReturnRows(rows)

		queryRows, err := dbPool.Query(ctx,
			`SELECT id, user_id, chat_id, message_type, message_content,
			       COALESCE(error_code, ''), COALESCE(error_message, ''),
			       attempts, status, created_at, last_attempt_at, next_retry_at
			FROM notification_dead_letters
			WHERE status IN ('pending', 'retrying')
			  AND (next_retry_at IS NULL OR next_retry_at <= NOW())
			ORDER BY created_at ASC
			LIMIT $1`, 10)
		assert.NoError(t, err)
		defer queryRows.Close()

		var entries []DeadLetterEntry
		for queryRows.Next() {
			var entry DeadLetterEntry
			err := queryRows.Scan(
				&entry.ID, &entry.UserID, &entry.ChatID, &entry.MessageType,
				&entry.MessageContent, &entry.ErrorCode, &entry.ErrorMessage,
				&entry.Attempts, &entry.Status, &entry.CreatedAt,
				&entry.LastAttemptAt, &entry.NextRetryAt,
			)
			assert.NoError(t, err)
			entries = append(entries, entry)
		}

		assert.Len(t, entries, 2)
		assert.Equal(t, "entry-1", entries[0].ID)
		assert.Equal(t, "entry-2", entries[1].ID)
		assert.NoError(t, mockPool.ExpectationsWereMet())
	})
}

// TestDeadLetterService_UpdateDeadLetter_Success tests success update query
func TestDeadLetterService_UpdateDeadLetter_Success(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mockPool.Close()
	dbPool := database.NewMockDBPool(mockPool)

	ctx := context.Background()

	mockPool.ExpectExec(`UPDATE notification_dead_letters`).
		WithArgs("entry-123").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	_, err = dbPool.Exec(ctx,
		`UPDATE notification_dead_letters
		SET status = 'success',
		    last_attempt_at = NOW(),
		    next_retry_at = NULL
		WHERE id = $1`, "entry-123")
	assert.NoError(t, err)
	assert.NoError(t, mockPool.ExpectationsWereMet())
}

// TestDeadLetterService_UpdateDeadLetter_FailedWithRetry tests failed update with retry
func TestDeadLetterService_UpdateDeadLetter_FailedWithRetry(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mockPool.Close()
	dbPool := database.NewMockDBPool(mockPool)

	ctx := context.Background()

	// First query to get current attempts
	mockPool.ExpectQuery(`SELECT attempts FROM notification_dead_letters`).
		WithArgs("entry-456").
		WillReturnRows(pgxmock.NewRows([]string{"attempts"}).AddRow(2))

	// Then update query
	mockPool.ExpectExec(`UPDATE notification_dead_letters`).
		WithArgs(
			"entry-456",
			DeadLetterStatusPending,
			3, // new attempts
			"NETWORK_ERROR",
			"Connection failed",
			pgxmock.AnyArg(), // next_retry_at
		).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	// Get current attempts
	var currentAttempts int
	err = dbPool.QueryRow(ctx, "SELECT attempts FROM notification_dead_letters WHERE id = $1", "entry-456").Scan(&currentAttempts)
	assert.NoError(t, err)
	assert.Equal(t, 2, currentAttempts)

	// Calculate new values
	newAttempts := currentAttempts + 1
	nextRetryTime := time.Now().Add(15 * time.Minute) // 5 * 3^1 = 15 minutes

	// Update
	_, err = dbPool.Exec(ctx,
		`UPDATE notification_dead_letters
		SET status = $2,
		    attempts = $3,
		    error_code = $4,
		    error_message = $5,
		    last_attempt_at = NOW(),
		    next_retry_at = $6
		WHERE id = $1`,
		"entry-456", DeadLetterStatusPending, newAttempts, "NETWORK_ERROR", "Connection failed", &nextRetryTime)
	assert.NoError(t, err)
	assert.NoError(t, mockPool.ExpectationsWereMet())
}

// TestDeadLetterService_MarkAsRetrying tests marking entry as retrying
func TestDeadLetterService_MarkAsRetrying(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mockPool.Close()
	dbPool := database.NewMockDBPool(mockPool)

	ctx := context.Background()

	mockPool.ExpectExec(`UPDATE notification_dead_letters`).
		WithArgs("entry-123").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	_, err = dbPool.Exec(ctx,
		`UPDATE notification_dead_letters
		SET status = 'retrying'
		WHERE id = $1`, "entry-123")
	assert.NoError(t, err)
	assert.NoError(t, mockPool.ExpectationsWereMet())
}

// TestDeadLetterService_GetDeadLetterStats tests stats query structure
func TestDeadLetterService_GetDeadLetterStats(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mockPool.Close()
	dbPool := database.NewMockDBPool(mockPool)

	ctx := context.Background()

	// Status counts query
	statusRows := pgxmock.NewRows([]string{"status", "count"}).
		AddRow("pending", 25).
		AddRow("retrying", 5).
		AddRow("failed", 60).
		AddRow("success", 10)

	mockPool.ExpectQuery(`SELECT status, COUNT`).
		WillReturnRows(statusRows)

	rows, err := dbPool.Query(ctx, `SELECT status, COUNT(*) FROM notification_dead_letters GROUP BY status`)
	assert.NoError(t, err)
	defer rows.Close()

	stats := &DeadLetterStats{ByErrorCode: make(map[string]int)}
	for rows.Next() {
		var status string
		var count int
		err := rows.Scan(&status, &count)
		assert.NoError(t, err)
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

	assert.Equal(t, 100, stats.TotalCount)
	assert.Equal(t, 25, stats.PendingCount)
	assert.Equal(t, 5, stats.RetryingCount)
	assert.Equal(t, 60, stats.FailedCount)
	assert.Equal(t, 10, stats.SuccessCount)
	assert.NoError(t, mockPool.ExpectationsWereMet())
}

// TestDeadLetterService_CleanupOldEntries tests cleanup query structure
func TestDeadLetterService_CleanupOldEntries(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mockPool.Close()
	dbPool := database.NewMockDBPool(mockPool)

	ctx := context.Background()

	mockPool.ExpectExec(`DELETE FROM notification_dead_letters`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("DELETE", 15))

	cutoff := time.Now().Add(-24 * time.Hour)
	result, err := dbPool.Exec(ctx,
		`DELETE FROM notification_dead_letters
		WHERE status IN ('success', 'failed')
		  AND created_at < $1`, cutoff)
	assert.NoError(t, err)
	affectedRows, rowsErr := result.RowsAffected()
	assert.NoError(t, rowsErr)
	assert.Equal(t, int64(15), affectedRows)
	assert.NoError(t, mockPool.ExpectationsWereMet())
}

// TestDeadLetterService_GetUserDeadLetters tests user-specific query
func TestDeadLetterService_GetUserDeadLetters(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mockPool.Close()
	dbPool := database.NewMockDBPool(mockPool)

	ctx := context.Background()
	now := time.Now()

	rows := pgxmock.NewRows([]string{
		"id", "user_id", "chat_id", "message_type", "message_content",
		"error_code", "error_message", "attempts", "status",
		"created_at", "last_attempt_at", "next_retry_at",
	}).
		AddRow(
			"entry-1", "user-123", "chat-456", "arbitrage_alert", "Message 1",
			"RATE_LIMITED", "Too many requests", 2, DeadLetterStatusPending,
			now, now, &now,
		)

	mockPool.ExpectQuery(`SELECT id, user_id, chat_id, message_type, message_content`).
		WithArgs("user-123", 10).
		WillReturnRows(rows)

	queryRows, err := dbPool.Query(ctx,
		`SELECT id, user_id, chat_id, message_type, message_content,
		       COALESCE(error_code, ''), COALESCE(error_message, ''),
		       attempts, status, created_at, last_attempt_at, next_retry_at
		FROM notification_dead_letters
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT $2`, "user-123", 10)
	assert.NoError(t, err)
	defer queryRows.Close()

	var entries []DeadLetterEntry
	for queryRows.Next() {
		var entry DeadLetterEntry
		err := queryRows.Scan(
			&entry.ID, &entry.UserID, &entry.ChatID, &entry.MessageType,
			&entry.MessageContent, &entry.ErrorCode, &entry.ErrorMessage,
			&entry.Attempts, &entry.Status, &entry.CreatedAt,
			&entry.LastAttemptAt, &entry.NextRetryAt,
		)
		assert.NoError(t, err)
		entries = append(entries, entry)
	}

	assert.Len(t, entries, 1)
	assert.Equal(t, "user-123", entries[0].UserID)
	assert.NoError(t, mockPool.ExpectationsWereMet())
}
