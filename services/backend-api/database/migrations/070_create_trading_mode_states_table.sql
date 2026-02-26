-- Migration 070: Create trading_mode_states table for dry/live mode management

-- Create trading_mode_states table
CREATE TABLE IF NOT EXISTS trading_mode_states (
    id BIGSERIAL PRIMARY KEY,
    chat_id VARCHAR(255) NOT NULL UNIQUE,
    mode VARCHAR(20) NOT NULL DEFAULT 'dry',
    changed_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    changed_by VARCHAR(255),
    previous_mode VARCHAR(20),
    confirmations INTEGER DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

-- Create index on chat_id for fast lookups
CREATE INDEX IF NOT EXISTS idx_trading_mode_states_chat_id ON trading_mode_states(chat_id);

-- Create index on mode for filtering
CREATE INDEX IF NOT EXISTS idx_trading_mode_states_mode ON trading_mode_states(mode);

-- Add check constraint for valid modes
ALTER TABLE trading_mode_states DROP CONSTRAINT IF EXISTS chk_trading_mode_valid;
ALTER TABLE trading_mode_states ADD CONSTRAINT chk_trading_mode_valid 
    CHECK (mode IN ('dry', 'live'));

-- Create trigger to update updated_at timestamp
CREATE OR REPLACE FUNCTION update_trading_mode_states_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_trading_mode_states_updated_at ON trading_mode_states;
CREATE TRIGGER trg_trading_mode_states_updated_at
    BEFORE UPDATE ON trading_mode_states
    FOR EACH ROW
    EXECUTE FUNCTION update_trading_mode_states_updated_at();

-- Insert default dry mode for any existing chats
INSERT INTO trading_mode_states (chat_id, mode, changed_at, changed_by)
SELECT DISTINCT chat_id, 'dry', NOW(), 'system'
FROM telegram_operators
WHERE chat_id IS NOT NULL
ON CONFLICT (chat_id) DO NOTHING;
