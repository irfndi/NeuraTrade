-- Migration: 060_telegram_blocked_and_dlq.sql
-- Description: Add telegram_blocked fields to users table and create notification_dead_letters table
-- Date: 2025-12-27

-- Add telegram_blocked fields to users table
ALTER TABLE users ADD COLUMN IF NOT EXISTS telegram_blocked BOOLEAN DEFAULT false;
ALTER TABLE users ADD COLUMN IF NOT EXISTS telegram_blocked_at TIMESTAMP WITH TIME ZONE;

-- Create index for efficient blocked user lookups
CREATE INDEX IF NOT EXISTS idx_users_telegram_blocked
ON users(telegram_blocked)
WHERE telegram_blocked = true;

-- Create notification_dead_letters table for failed message handling
CREATE TABLE IF NOT EXISTS notification_dead_letters (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL,
    chat_id VARCHAR(255) NOT NULL,
    message_type VARCHAR(50) NOT NULL,
    message_content TEXT NOT NULL,
    error_code VARCHAR(50),
    error_message TEXT,
    attempts INT DEFAULT 1,
    status VARCHAR(20) DEFAULT 'pending', -- pending, retrying, failed, success
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    last_attempt_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    next_retry_at TIMESTAMP WITH TIME ZONE,

    -- Foreign key to users table (optional - some messages may be from deleted users)
    CONSTRAINT fk_dlq_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

-- Create indexes for efficient DLQ queries
CREATE INDEX IF NOT EXISTS idx_dlq_status ON notification_dead_letters(status);
CREATE INDEX IF NOT EXISTS idx_dlq_next_retry ON notification_dead_letters(next_retry_at) WHERE status IN ('pending', 'retrying');
CREATE INDEX IF NOT EXISTS idx_dlq_user_id ON notification_dead_letters(user_id);
CREATE INDEX IF NOT EXISTS idx_dlq_created_at ON notification_dead_letters(created_at);

-- Add comment describing the table
COMMENT ON TABLE notification_dead_letters IS 'Stores failed notification messages for retry and analysis';
COMMENT ON COLUMN notification_dead_letters.status IS 'Message status: pending, retrying, failed, success';
COMMENT ON COLUMN notification_dead_letters.error_code IS 'Error code from Telegram API: USER_BLOCKED, RATE_LIMITED, etc.';
COMMENT ON COLUMN notification_dead_letters.next_retry_at IS 'When to next attempt delivery (null = no automatic retry)';

-- Add comment for new user columns
COMMENT ON COLUMN users.telegram_blocked IS 'True if user has blocked the bot or account is deactivated';
COMMENT ON COLUMN users.telegram_blocked_at IS 'When the user was marked as blocked';
