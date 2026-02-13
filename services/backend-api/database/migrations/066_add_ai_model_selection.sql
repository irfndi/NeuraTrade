-- Migration 066: AI model selection support
-- Created: 2026-02-13

BEGIN;

-- Add selected_ai_model column for storing user's chosen AI model
ALTER TABLE users ADD COLUMN IF NOT EXISTS selected_ai_model VARCHAR(100);

-- Migration completion marker
INSERT INTO system_config (config_key, config_value, description) VALUES
    ('migration_066_completed', 'true', 'Migration 066: Add selected_ai_model to users')
ON CONFLICT (config_key) DO UPDATE SET
    config_value = EXCLUDED.config_value,
    updated_at = CURRENT_TIMESTAMP;

COMMIT;
