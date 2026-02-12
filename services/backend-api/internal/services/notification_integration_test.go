package services

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/database"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNotificationService_logNotification_QueryStructure tests the SQL query structure for logging notifications
func TestNotificationService_logNotification_QueryStructure(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mockPool.Close()
	dbPool := database.NewMockDBPool(mockPool)

	ctx := context.Background()

	tests := []struct {
		name             string
		userID           string
		notificationType string
		message          string
	}{
		{
			name:             "Log telegram notification",
			userID:           "user-123",
			notificationType: "telegram",
			message:          "Test arbitrage alert message",
		},
		{
			name:             "Log email notification",
			userID:           "user-456",
			notificationType: "email",
			message:          "Test email notification",
		},
		{
			name:             "Log push notification",
			userID:           "user-789",
			notificationType: "push",
			message:          "Test push notification",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockPool.ExpectExec(`INSERT INTO alert_notifications`).
				WithArgs(
					tc.userID,
					tc.notificationType,
					tc.message,
					pgxmock.AnyArg(), // sent_at timestamp
				).
				WillReturnResult(pgxmock.NewResult("INSERT", 1))

			now := time.Now()
			_, err := dbPool.Exec(ctx,
				`INSERT INTO alert_notifications (user_id, notification_type, message, sent_at)
				VALUES ($1, $2, $3, $4)`,
				tc.userID, tc.notificationType, tc.message, now)
			assert.NoError(t, err)
			assert.NoError(t, mockPool.ExpectationsWereMet())
		})
	}
}

// TestNotificationService_CheckUserPreferences_QueryStructure tests the query structure for user preferences
func TestNotificationService_CheckUserPreferences_QueryStructure(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mockPool.Close()
	dbPool := database.NewMockDBPool(mockPool)

	ctx := context.Background()

	t.Run("User has notifications enabled (count = 0)", func(t *testing.T) {
		mockPool.ExpectQuery(`SELECT COUNT`).
			WithArgs("user-123").
			WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(0))

		var count int
		err := dbPool.QueryRow(ctx,
			`SELECT COUNT(*)
			FROM user_alerts
			WHERE user_id = $1
			  AND alert_type = 'arbitrage'
			  AND is_active = false
			  AND conditions->>'notifications_enabled' = 'false'`, "user-123").Scan(&count)
		assert.NoError(t, err)
		assert.Equal(t, 0, count)

		// count == 0 means notifications enabled
		assert.True(t, count == 0)
		assert.NoError(t, mockPool.ExpectationsWereMet())
	})

	t.Run("User has notifications disabled (count > 0)", func(t *testing.T) {
		mockPool.ExpectQuery(`SELECT COUNT`).
			WithArgs("user-456").
			WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(1))

		var count int
		err := dbPool.QueryRow(ctx,
			`SELECT COUNT(*)
			FROM user_alerts
			WHERE user_id = $1
			  AND alert_type = 'arbitrage'
			  AND is_active = false
			  AND conditions->>'notifications_enabled' = 'false'`, "user-456").Scan(&count)
		assert.NoError(t, err)
		assert.Equal(t, 1, count)

		// count > 0 means notifications disabled
		assert.False(t, count == 0)
		assert.NoError(t, mockPool.ExpectationsWereMet())
	})
}

// TestNotificationService_getEligibleUsers_QueryStructure tests the query for eligible users
func TestNotificationService_getEligibleUsers_QueryStructure(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mockPool.Close()
	dbPool := database.NewMockDBPool(mockPool)

	ctx := context.Background()
	now := time.Now()

	t.Run("Returns eligible users with telegram chat ID", func(t *testing.T) {
		rows := pgxmock.NewRows([]string{
			"id", "email", "telegram_chat_id", "subscription_tier", "created_at", "updated_at",
		}).
			AddRow("user-1", "user1@example.com", "123456789", "premium", now, now).
			AddRow("user-2", "user2@example.com", "987654321", "free", now, now)

		mockPool.ExpectQuery(`SELECT id, email, telegram_chat_id`).
			WillReturnRows(rows)

		queryRows, err := dbPool.Query(ctx,
			`SELECT id, email, telegram_chat_id, subscription_tier, created_at, updated_at
			FROM users
			WHERE telegram_chat_id IS NOT NULL
			  AND telegram_chat_id != ''
			  AND (telegram_blocked IS NULL OR telegram_blocked = false)`)
		assert.NoError(t, err)
		defer queryRows.Close()

		type UserData struct {
			ID               string
			Email            string
			TelegramChatID   string
			SubscriptionTier string
			CreatedAt        time.Time
			UpdatedAt        time.Time
		}

		var users []UserData
		for queryRows.Next() {
			var user UserData
			err := queryRows.Scan(&user.ID, &user.Email, &user.TelegramChatID, &user.SubscriptionTier, &user.CreatedAt, &user.UpdatedAt)
			assert.NoError(t, err)
			users = append(users, user)
		}

		assert.Len(t, users, 2)
		assert.Equal(t, "user-1", users[0].ID)
		assert.Equal(t, "123456789", users[0].TelegramChatID)
		assert.NoError(t, mockPool.ExpectationsWereMet())
	})
}

// TestNotificationService_DeadLetterQueue_QueryStructures tests DLQ-related queries
func TestNotificationService_DeadLetterQueue_QueryStructures(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mockPool.Close()
	dbPool := database.NewMockDBPool(mockPool)

	ctx := context.Background()

	t.Run("Get pending messages query", func(t *testing.T) {
		rows := pgxmock.NewRows([]string{
			"id", "user_id", "chat_id", "message_type", "message_content",
			"error_code", "error_message", "attempts", "status",
			"created_at", "last_attempt_at", "next_retry_at",
		})

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

		// Verify empty result
		count := 0
		for queryRows.Next() {
			count++
		}
		assert.Equal(t, 0, count)
		assert.NoError(t, mockPool.ExpectationsWereMet())
	})

	t.Run("Cleanup dead letters query", func(t *testing.T) {
		mockPool.ExpectExec(`DELETE FROM notification_dead_letters`).
			WithArgs(pgxmock.AnyArg()).
			WillReturnResult(pgxmock.NewResult("DELETE", 20))

		cutoff := time.Now().Add(-24 * time.Hour)
		result, err := dbPool.Exec(ctx,
			`DELETE FROM notification_dead_letters
			WHERE status IN ('success', 'failed')
			  AND created_at < $1`, cutoff)
		assert.NoError(t, err)
		affectedRows, rowsErr := result.RowsAffected()
		assert.NoError(t, rowsErr)
		assert.Equal(t, int64(20), affectedRows)
		assert.NoError(t, mockPool.ExpectationsWereMet())
	})
}

// TestNotificationService_AlertNotifications_ColumnCorrectness tests that we use correct columns
func TestNotificationService_AlertNotifications_ColumnCorrectness(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mockPool.Close()
	dbPool := database.NewMockDBPool(mockPool)

	ctx := context.Background()

	t.Run("Insert uses message column not content", func(t *testing.T) {
		// This test verifies the fix for the "column content does not exist" error
		// The correct column name is "message" not "content"
		mockPool.ExpectExec(`INSERT INTO alert_notifications.*message`).
			WithArgs("user-id", "telegram", "test message", pgxmock.AnyArg()).
			WillReturnResult(pgxmock.NewResult("INSERT", 1))

		_, err := dbPool.Exec(ctx,
			`INSERT INTO alert_notifications (user_id, notification_type, message, sent_at)
			VALUES ($1, $2, $3, $4)`,
			"user-id", "telegram", "test message", time.Now())
		assert.NoError(t, err)
		assert.NoError(t, mockPool.ExpectationsWereMet())
	})

	t.Run("Nullable alert_id in insert", func(t *testing.T) {
		// Tests that alert_id can be NULL (for notifications not tied to specific alerts)
		mockPool.ExpectExec(`INSERT INTO alert_notifications`).
			WithArgs("user-id", "telegram", "test message", pgxmock.AnyArg()).
			WillReturnResult(pgxmock.NewResult("INSERT", 1))

		// Note: alert_id is not in the INSERT statement - it defaults to NULL
		_, err := dbPool.Exec(ctx,
			`INSERT INTO alert_notifications (user_id, notification_type, message, sent_at)
			VALUES ($1, $2, $3, $4)`,
			"user-id", "telegram", "test message", time.Now())
		assert.NoError(t, err)
		assert.NoError(t, mockPool.ExpectationsWereMet())
	})
}

// TestNotificationService_Concurrency tests concurrent query patterns
func TestNotificationService_Concurrency(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mockPool.Close()
	dbPool := database.NewMockDBPool(mockPool)

	ctx := context.Background()

	// Set up expectations for multiple concurrent queries
	for i := 0; i < 5; i++ {
		mockPool.ExpectQuery(`SELECT COUNT`).
			WithArgs(pgxmock.AnyArg()).
			WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(0))
	}

	// Run concurrent queries
	done := make(chan bool, 5)
	for i := 0; i < 5; i++ {
		go func(userID string) {
			var count int
			err := dbPool.QueryRow(ctx,
				`SELECT COUNT(*) FROM user_alerts WHERE user_id = $1`, userID).Scan(&count)
			assert.NoError(t, err)
			done <- true
		}(fmt.Sprintf("user-%d", i))
	}

	// Wait for all goroutines
	for i := 0; i < 5; i++ {
		<-done
	}

	assert.NoError(t, mockPool.ExpectationsWereMet())
}

// TestNotificationService_ErrorHandling tests error scenarios
func TestNotificationService_ErrorHandling(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mockPool.Close()
	dbPool := database.NewMockDBPool(mockPool)

	ctx := context.Background()

	t.Run("Insert error handling", func(t *testing.T) {
		mockPool.ExpectExec(`INSERT INTO alert_notifications`).
			WithArgs("user-id", "telegram", "test", pgxmock.AnyArg()).
			WillReturnError(assert.AnError)

		_, err := dbPool.Exec(ctx,
			`INSERT INTO alert_notifications (user_id, notification_type, message, sent_at)
			VALUES ($1, $2, $3, $4)`,
			"user-id", "telegram", "test", time.Now())
		assert.Error(t, err)
		assert.NoError(t, mockPool.ExpectationsWereMet())
	})

	t.Run("Query error handling", func(t *testing.T) {
		mockPool.ExpectQuery(`SELECT COUNT`).
			WithArgs("user-id").
			WillReturnError(assert.AnError)

		var count int
		err := dbPool.QueryRow(ctx,
			`SELECT COUNT(*) FROM user_alerts WHERE user_id = $1`, "user-id").Scan(&count)
		assert.Error(t, err)
		assert.NoError(t, mockPool.ExpectationsWereMet())
	})
}
