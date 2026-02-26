package services

import (
	"context"
	"fmt"
	"strconv"
	"time"
)

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
