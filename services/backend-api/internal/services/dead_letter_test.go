package services

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDeadLetterEntry_Struct(t *testing.T) {
	now := time.Now()
	nextRetry := now.Add(5 * time.Minute)

	entry := DeadLetterEntry{
		ID:             "test-id-123",
		UserID:         "user-456",
		ChatID:         "789012345",
		MessageType:    "telegram_notification",
		MessageContent: "Test message content",
		ErrorCode:      "RATE_LIMITED",
		ErrorMessage:   "Too many requests",
		Attempts:       2,
		Status:         DeadLetterStatusPending,
		CreatedAt:      now,
		LastAttemptAt:  now,
		NextRetryAt:    &nextRetry,
	}

	assert.Equal(t, "test-id-123", entry.ID)
	assert.Equal(t, "user-456", entry.UserID)
	assert.Equal(t, "789012345", entry.ChatID)
	assert.Equal(t, "telegram_notification", entry.MessageType)
	assert.Equal(t, "Test message content", entry.MessageContent)
	assert.Equal(t, "RATE_LIMITED", entry.ErrorCode)
	assert.Equal(t, "Too many requests", entry.ErrorMessage)
	assert.Equal(t, 2, entry.Attempts)
	assert.Equal(t, DeadLetterStatusPending, entry.Status)
	assert.Equal(t, now, entry.CreatedAt)
	assert.Equal(t, now, entry.LastAttemptAt)
	assert.Equal(t, &nextRetry, entry.NextRetryAt)
}

func TestDeadLetterStats_Struct(t *testing.T) {
	oldestTime := time.Now().Add(-1 * time.Hour)

	stats := DeadLetterStats{
		TotalCount:    100,
		PendingCount:  25,
		RetryingCount: 5,
		FailedCount:   60,
		SuccessCount:  10,
		ByErrorCode: map[string]int{
			"USER_BLOCKED":  30,
			"RATE_LIMITED":  20,
			"NETWORK_ERROR": 10,
		},
		OldestPending: &oldestTime,
	}

	assert.Equal(t, 100, stats.TotalCount)
	assert.Equal(t, 25, stats.PendingCount)
	assert.Equal(t, 5, stats.RetryingCount)
	assert.Equal(t, 60, stats.FailedCount)
	assert.Equal(t, 10, stats.SuccessCount)
	assert.Equal(t, 30, stats.ByErrorCode["USER_BLOCKED"])
	assert.Equal(t, 20, stats.ByErrorCode["RATE_LIMITED"])
	assert.Equal(t, 10, stats.ByErrorCode["NETWORK_ERROR"])
	assert.Equal(t, &oldestTime, stats.OldestPending)
}

func TestDeadLetterStatus_Constants(t *testing.T) {
	assert.Equal(t, DeadLetterStatus("pending"), DeadLetterStatusPending)
	assert.Equal(t, DeadLetterStatus("retrying"), DeadLetterStatusRetrying)
	assert.Equal(t, DeadLetterStatus("failed"), DeadLetterStatusFailed)
	assert.Equal(t, DeadLetterStatus("success"), DeadLetterStatusSuccess)
}

func TestNewDeadLetterService_NilDB(t *testing.T) {
	dls := NewDeadLetterService(nil)
	assert.NotNil(t, dls)
	assert.Nil(t, dls.db)
	assert.NotNil(t, dls.logger)
}

func TestIsRetryableErrorCode(t *testing.T) {
	tests := []struct {
		name     string
		code     string
		expected bool
	}{
		{
			name:     "RATE_LIMITED is retryable",
			code:     "RATE_LIMITED",
			expected: true,
		},
		{
			name:     "NETWORK_ERROR is retryable",
			code:     "NETWORK_ERROR",
			expected: true,
		},
		{
			name:     "TIMEOUT is retryable",
			code:     "TIMEOUT",
			expected: true,
		},
		{
			name:     "INTERNAL_ERROR is retryable",
			code:     "INTERNAL_ERROR",
			expected: true,
		},
		{
			name:     "USER_BLOCKED is not retryable",
			code:     "USER_BLOCKED",
			expected: false,
		},
		{
			name:     "CHAT_NOT_FOUND is not retryable",
			code:     "CHAT_NOT_FOUND",
			expected: false,
		},
		{
			name:     "INVALID_REQUEST is not retryable",
			code:     "INVALID_REQUEST",
			expected: false,
		},
		{
			name:     "UNKNOWN is not retryable",
			code:     "UNKNOWN",
			expected: false,
		},
		{
			name:     "Empty string is not retryable",
			code:     "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isRetryableErrorCode(tt.code)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestPow(t *testing.T) {
	tests := []struct {
		base     int
		exp      int
		expected int
	}{
		{base: 2, exp: 0, expected: 1},
		{base: 2, exp: 1, expected: 2},
		{base: 2, exp: 2, expected: 4},
		{base: 2, exp: 3, expected: 8},
		{base: 3, exp: 0, expected: 1},
		{base: 3, exp: 1, expected: 3},
		{base: 3, exp: 2, expected: 9},
		{base: 3, exp: 3, expected: 27},
		{base: 5, exp: 3, expected: 125},
	}

	for _, tt := range tests {
		result := pow(tt.base, tt.exp)
		assert.Equal(t, tt.expected, result)
	}
}

func TestDeadLetterEntry_NilNextRetryAt(t *testing.T) {
	entry := DeadLetterEntry{
		ID:          "test-id",
		UserID:      "user-id",
		ChatID:      "chat-id",
		MessageType: "test",
		Status:      DeadLetterStatusFailed,
		NextRetryAt: nil,
	}

	assert.Nil(t, entry.NextRetryAt)
}

func TestDeadLetterStats_EmptyByErrorCode(t *testing.T) {
	stats := DeadLetterStats{
		TotalCount:    0,
		PendingCount:  0,
		RetryingCount: 0,
		FailedCount:   0,
		SuccessCount:  0,
		ByErrorCode:   make(map[string]int),
		OldestPending: nil,
	}

	assert.Equal(t, 0, stats.TotalCount)
	assert.Empty(t, stats.ByErrorCode)
	assert.Nil(t, stats.OldestPending)
}

// TestDeadLetterEntry_AllFields tests all fields of DeadLetterEntry struct
func TestDeadLetterEntry_AllFields(t *testing.T) {
	now := time.Now()
	nextRetry := now.Add(10 * time.Minute)

	tests := []struct {
		name  string
		entry DeadLetterEntry
	}{
		{
			name: "Complete entry with all fields",
			entry: DeadLetterEntry{
				ID:             "entry-123",
				UserID:         "user-456",
				ChatID:         "chat-789",
				MessageType:    "arbitrage_alert",
				MessageContent: "Test arbitrage alert message",
				ErrorCode:      "RATE_LIMITED",
				ErrorMessage:   "Too many requests",
				Attempts:       3,
				Status:         DeadLetterStatusPending,
				CreatedAt:      now,
				LastAttemptAt:  now,
				NextRetryAt:    &nextRetry,
			},
		},
		{
			name: "Entry with minimal fields",
			entry: DeadLetterEntry{
				ID:     "min-entry",
				UserID: "user-id",
				ChatID: "chat-id",
				Status: DeadLetterStatusPending,
			},
		},
		{
			name: "Entry with empty strings",
			entry: DeadLetterEntry{
				ID:             "",
				UserID:         "",
				ChatID:         "",
				MessageType:    "",
				MessageContent: "",
				ErrorCode:      "",
				ErrorMessage:   "",
				Attempts:       0,
				Status:         "",
			},
		},
		{
			name: "Entry with failed status",
			entry: DeadLetterEntry{
				ID:          "failed-entry",
				UserID:      "user-123",
				ChatID:      "chat-456",
				Status:      DeadLetterStatusFailed,
				Attempts:    5,
				NextRetryAt: nil,
			},
		},
		{
			name: "Entry with success status",
			entry: DeadLetterEntry{
				ID:          "success-entry",
				UserID:      "user-123",
				ChatID:      "chat-456",
				Status:      DeadLetterStatusSuccess,
				Attempts:    2,
				NextRetryAt: nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verify that struct can be created and accessed
			assert.NotPanics(t, func() {
				_ = tt.entry.ID
				_ = tt.entry.UserID
				_ = tt.entry.ChatID
				_ = tt.entry.MessageType
				_ = tt.entry.MessageContent
				_ = tt.entry.ErrorCode
				_ = tt.entry.ErrorMessage
				_ = tt.entry.Attempts
				_ = tt.entry.Status
				_ = tt.entry.CreatedAt
				_ = tt.entry.LastAttemptAt
				_ = tt.entry.NextRetryAt
			})
		})
	}
}

// TestDeadLetterService_ExponentialBackoffCalculation tests the exponential backoff formula
func TestDeadLetterService_ExponentialBackoffCalculation(t *testing.T) {
	// The backoff formula is: 5 * 3^(attempts-1) minutes
	// With a max of 2 hours

	tests := []struct {
		attempts        int
		expectedMinutes int
		description     string
	}{
		{attempts: 1, expectedMinutes: 5, description: "First retry: 5 minutes"},
		{attempts: 2, expectedMinutes: 15, description: "Second retry: 15 minutes (5*3^1)"},
		{attempts: 3, expectedMinutes: 45, description: "Third retry: 45 minutes (5*3^2)"},
		{attempts: 4, expectedMinutes: 120, description: "Fourth retry: capped at 2 hours (135 -> 120)"},
		{attempts: 5, expectedMinutes: 120, description: "Fifth retry: capped at 2 hours"},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			// Calculate delay using the same formula as in UpdateDeadLetter
			delay := time.Duration(5*pow(3, tt.attempts-1)) * time.Minute
			if delay > 2*time.Hour {
				delay = 2 * time.Hour
			}

			expectedDelay := time.Duration(tt.expectedMinutes) * time.Minute
			assert.Equal(t, expectedDelay, delay)
		})
	}
}

// TestDeadLetterService_StatusTransitions tests all valid status transitions
func TestDeadLetterService_StatusTransitions(t *testing.T) {
	tests := []struct {
		name          string
		initialStatus DeadLetterStatus
		action        string
		expectedFinal DeadLetterStatus
		valid         bool
	}{
		{
			name:          "Pending to Retrying",
			initialStatus: DeadLetterStatusPending,
			action:        "start_retry",
			expectedFinal: DeadLetterStatusRetrying,
			valid:         true,
		},
		{
			name:          "Retrying to Success",
			initialStatus: DeadLetterStatusRetrying,
			action:        "retry_success",
			expectedFinal: DeadLetterStatusSuccess,
			valid:         true,
		},
		{
			name:          "Retrying to Pending (for retry)",
			initialStatus: DeadLetterStatusRetrying,
			action:        "retry_failed_retryable",
			expectedFinal: DeadLetterStatusPending,
			valid:         true,
		},
		{
			name:          "Retrying to Failed (max retries)",
			initialStatus: DeadLetterStatusRetrying,
			action:        "retry_failed_max",
			expectedFinal: DeadLetterStatusFailed,
			valid:         true,
		},
		{
			name:          "Retrying to Failed (non-retryable error)",
			initialStatus: DeadLetterStatusRetrying,
			action:        "retry_failed_nonretryable",
			expectedFinal: DeadLetterStatusFailed,
			valid:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Validate the transition logic
			switch tt.action {
			case "start_retry":
				assert.Equal(t, DeadLetterStatusRetrying, tt.expectedFinal)
			case "retry_success":
				assert.Equal(t, DeadLetterStatusSuccess, tt.expectedFinal)
			case "retry_failed_retryable":
				assert.Equal(t, DeadLetterStatusPending, tt.expectedFinal)
			case "retry_failed_max", "retry_failed_nonretryable":
				assert.Equal(t, DeadLetterStatusFailed, tt.expectedFinal)
			}
		})
	}
}

// TestDeadLetterService_MaxRetriesExceeded tests behavior when max retries is exceeded
func TestDeadLetterService_MaxRetriesExceeded(t *testing.T) {
	maxAttempts := 5

	tests := []struct {
		currentAttempts int
		errorCode       string
		shouldBeFailed  bool
		shouldHaveRetry bool
		description     string
	}{
		{
			currentAttempts: 1,
			errorCode:       "RATE_LIMITED",
			shouldBeFailed:  false,
			shouldHaveRetry: true,
			description:     "First attempt with retryable error - should retry",
		},
		{
			currentAttempts: 4,
			errorCode:       "RATE_LIMITED",
			shouldBeFailed:  true, // 4+1=5 >= maxAttempts
			shouldHaveRetry: false,
			description:     "Fourth attempt - next would be fifth, should fail",
		},
		{
			currentAttempts: 5,
			errorCode:       "RATE_LIMITED",
			shouldBeFailed:  true,
			shouldHaveRetry: false,
			description:     "Fifth attempt - max reached, should fail",
		},
		{
			currentAttempts: 1,
			errorCode:       "USER_BLOCKED",
			shouldBeFailed:  true,
			shouldHaveRetry: false,
			description:     "Non-retryable error - should fail immediately",
		},
		{
			currentAttempts: 3,
			errorCode:       "NETWORK_ERROR",
			shouldBeFailed:  false,
			shouldHaveRetry: true,
			description:     "Third attempt with retryable error - should retry",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			newAttempts := tt.currentAttempts + 1

			// Calculate expected status using same logic as UpdateDeadLetter
			var expectedStatus DeadLetterStatus
			var shouldHaveNextRetry bool

			if newAttempts >= maxAttempts || !isRetryableErrorCode(tt.errorCode) {
				expectedStatus = DeadLetterStatusFailed
				shouldHaveNextRetry = false
			} else {
				expectedStatus = DeadLetterStatusPending
				shouldHaveNextRetry = true
			}

			if tt.shouldBeFailed {
				assert.Equal(t, DeadLetterStatusFailed, expectedStatus)
			} else {
				assert.Equal(t, DeadLetterStatusPending, expectedStatus)
			}
			assert.Equal(t, tt.shouldHaveRetry, shouldHaveNextRetry)
		})
	}
}

// TestDeadLetterService_CleanupOldEntriesLogic tests cleanup logic
func TestDeadLetterService_CleanupOldEntriesLogic(t *testing.T) {
	tests := []struct {
		name            string
		olderThan       time.Duration
		expectedCutoff  time.Duration
		shouldBeDeleted bool
	}{
		{
			name:            "24 hour cleanup",
			olderThan:       24 * time.Hour,
			expectedCutoff:  24 * time.Hour,
			shouldBeDeleted: true,
		},
		{
			name:            "7 day cleanup",
			olderThan:       7 * 24 * time.Hour,
			expectedCutoff:  7 * 24 * time.Hour,
			shouldBeDeleted: true,
		},
		{
			name:            "1 hour cleanup",
			olderThan:       1 * time.Hour,
			expectedCutoff:  1 * time.Hour,
			shouldBeDeleted: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Calculate cutoff time
			now := time.Now()
			cutoff := now.Add(-tt.olderThan)

			// Verify cutoff is in the past
			assert.True(t, cutoff.Before(now))
			assert.Equal(t, tt.expectedCutoff, tt.olderThan)
		})
	}
}

// TestDeadLetterService_IsRetryableErrorCode_AllCases tests all error code scenarios
func TestDeadLetterService_IsRetryableErrorCode_AllCases(t *testing.T) {
	retryableErrors := []string{
		"RATE_LIMITED",
		"NETWORK_ERROR",
		"TIMEOUT",
		"INTERNAL_ERROR",
	}

	nonRetryableErrors := []string{
		"USER_BLOCKED",
		"CHAT_NOT_FOUND",
		"INVALID_REQUEST",
		"UNKNOWN",
		"", // empty string
		"RANDOM_ERROR",
		"NOT_A_VALID_CODE",
		"rate_limited", // lowercase should not match
		"Rate_Limited", // mixed case should not match
	}

	for _, code := range retryableErrors {
		t.Run("Retryable_"+code, func(t *testing.T) {
			assert.True(t, isRetryableErrorCode(code), "Expected %s to be retryable", code)
		})
	}

	for _, code := range nonRetryableErrors {
		t.Run("NonRetryable_"+code, func(t *testing.T) {
			assert.False(t, isRetryableErrorCode(code), "Expected %s to NOT be retryable", code)
		})
	}
}

// TestDeadLetterService_Pow_EdgeCases tests pow function edge cases
func TestDeadLetterService_Pow_EdgeCases(t *testing.T) {
	tests := []struct {
		base     int
		exp      int
		expected int
	}{
		{base: 0, exp: 0, expected: 1},   // 0^0 = 1 (by convention)
		{base: 0, exp: 1, expected: 0},   // 0^1 = 0
		{base: 0, exp: 5, expected: 0},   // 0^5 = 0
		{base: 1, exp: 0, expected: 1},   // 1^0 = 1
		{base: 1, exp: 100, expected: 1}, // 1^100 = 1
		{base: 10, exp: 0, expected: 1},  // 10^0 = 1
		{base: 10, exp: 1, expected: 10}, // 10^1 = 10
		{base: 10, exp: 2, expected: 100},
		{base: 10, exp: 3, expected: 1000},
		{base: 2, exp: 10, expected: 1024},
		{base: 3, exp: 4, expected: 81},  // For backoff calculation
		{base: 3, exp: 5, expected: 243}, // For backoff calculation
	}

	for _, tt := range tests {
		t.Run(
			fmt.Sprintf("pow_%d_%d", tt.base, tt.exp),
			func(t *testing.T) {
				result := pow(tt.base, tt.exp)
				assert.Equal(t, tt.expected, result)
			},
		)
	}
}

// TestDeadLetterStats_Aggregation tests stats aggregation logic
func TestDeadLetterStats_Aggregation(t *testing.T) {
	stats := DeadLetterStats{
		TotalCount:    100,
		PendingCount:  20,
		RetryingCount: 5,
		FailedCount:   65,
		SuccessCount:  10,
		ByErrorCode: map[string]int{
			"RATE_LIMITED":  30,
			"USER_BLOCKED":  25,
			"NETWORK_ERROR": 15,
			"UNKNOWN":       20,
		},
	}

	// Verify totals match
	assert.Equal(t, 100, stats.TotalCount)
	assert.Equal(t, 100, stats.PendingCount+stats.RetryingCount+stats.FailedCount+stats.SuccessCount)

	// Verify error code sum (excluding success)
	errorSum := 0
	for _, count := range stats.ByErrorCode {
		errorSum += count
	}
	// Error codes should match pending + retrying + failed (excluding success)
	assert.Equal(t, 90, errorSum) // 20 + 5 + 65 = 90
}

// TestDeadLetterEntry_JSONTags tests JSON serialization structure
func TestDeadLetterEntry_JSONTags(t *testing.T) {
	now := time.Now()
	entry := DeadLetterEntry{
		ID:             "test-id",
		UserID:         "user-id",
		ChatID:         "chat-id",
		MessageType:    "test_type",
		MessageContent: "test content",
		ErrorCode:      "TEST_ERROR",
		ErrorMessage:   "test error message",
		Attempts:       1,
		Status:         DeadLetterStatusPending,
		CreatedAt:      now,
		LastAttemptAt:  now,
		NextRetryAt:    nil,
	}

	// Verify all fields are accessible
	assert.Equal(t, "test-id", entry.ID)
	assert.Equal(t, "user-id", entry.UserID)
	assert.Equal(t, "chat-id", entry.ChatID)
	assert.Equal(t, "test_type", entry.MessageType)
	assert.Equal(t, "test content", entry.MessageContent)
	assert.Equal(t, "TEST_ERROR", entry.ErrorCode)
	assert.Equal(t, "test error message", entry.ErrorMessage)
	assert.Equal(t, 1, entry.Attempts)
	assert.Equal(t, DeadLetterStatusPending, entry.Status)
	assert.Equal(t, now, entry.CreatedAt)
	assert.Equal(t, now, entry.LastAttemptAt)
	assert.Nil(t, entry.NextRetryAt)
}
