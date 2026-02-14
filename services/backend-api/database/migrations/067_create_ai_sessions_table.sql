-- Create ai_sessions table for serializing AI agent session state
CREATE TABLE IF NOT EXISTS ai_sessions (
    id VARCHAR(100) PRIMARY KEY,
    quest_id VARCHAR(100),
    symbol VARCHAR(50) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    state_data JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ai_sessions_quest_id ON ai_sessions(quest_id);
CREATE INDEX IF NOT EXISTS idx_ai_sessions_symbol ON ai_sessions(symbol);
CREATE INDEX IF NOT EXISTS idx_ai_sessions_status ON ai_sessions(status);
CREATE INDEX IF NOT EXISTS idx_ai_sessions_updated_at ON ai_sessions(updated_at DESC);

GRANT SELECT, INSERT, UPDATE, DELETE ON ai_sessions TO authenticated;

INSERT INTO system_config (config_key, config_value, description)
VALUES ('migration_067_completed', 'true', 'Migration 067: Create ai_sessions table')
ON CONFLICT (config_key) DO NOTHING;
