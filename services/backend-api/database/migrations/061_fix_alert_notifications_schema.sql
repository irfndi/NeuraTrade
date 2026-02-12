-- Migration: 061_fix_alert_notifications_schema.sql
-- Description: Make alert_id nullable in alert_notifications table to support
--              notifications not tied to a specific user_alerts record (e.g., Telegram broadcasts)
-- Date: 2025-12-28

-- Drop the foreign key constraint first (required to make column nullable)
ALTER TABLE alert_notifications DROP CONSTRAINT IF EXISTS alert_notifications_alert_id_fkey;

-- Make alert_id nullable - some notifications (like telegram broadcasts) don't have a specific alert
ALTER TABLE alert_notifications ALTER COLUMN alert_id DROP NOT NULL;

-- Re-add the foreign key constraint with ON DELETE SET NULL
ALTER TABLE alert_notifications ADD CONSTRAINT alert_notifications_alert_id_fkey
    FOREIGN KEY (alert_id) REFERENCES user_alerts(id) ON DELETE SET NULL;

-- Add index for efficient lookups of notifications without alert_id
CREATE INDEX IF NOT EXISTS idx_alert_notifications_user_type
ON alert_notifications(user_id, notification_type, sent_at DESC);

-- Add comment explaining the nullable alert_id
COMMENT ON COLUMN alert_notifications.alert_id IS 'Optional reference to user_alerts; NULL for notifications not tied to a specific alert';
