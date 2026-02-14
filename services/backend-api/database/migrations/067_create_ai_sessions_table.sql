-- Create ai_sessions table for serializing AI agent session state
-- This enables session persistence for resumption after interruptions

CREATE TABLE IF NOT EXISTS ai_sessions (
    id VARCHAR(100) PRIMARY KEY,
    quest_id VARCHAR(100),
    symbol VARCHAR(50) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    state_data JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    CONSTRAINT fk_ai_sessions_quest FOREIGN KEY (quest_id) REFERENCES quests(id) ON DELETE SET NULL
);

-- Indexes for common query patterns
CREATE INDEX IF NOT EXISTS idx_ai_sessions_quest_id ON ai_sessions(quest_id);
CREATE INDEX IF NOT EXISTS idx_ai_sessions_symbol ON ai_sessions(symbol);
CREATE INDEX IF NOT EXISTS idx_ai_sessions_status ON ai_sessions(status);
CREATE INDEX IF NOT EXISTS idx_ai_sessions_created_at ON ai_sessions(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai_sessions_updated_at ON ai_sessions(updated_at DESC);

-- Composite index for active sessions lookup
CREATE INDEX IF NOT EXISTS idx_ai_sessions_active ON ai_sessions(status, updated_at DESC) WHERE status = 'active';

-- Grants
GRANT SELECT, INSERT, UPDATE, DELETE ON ai_sessions TO authenticated;
GRANT SELECT ON ai_sessions TO anon;

-- Comments for documentation
COMMENT ON TABLE ai_sessions IS 'Stores serialized AI agent session state for persistence and resumption';
COMMENT ON COLUMN ai_sessions.id IS 'Unique session identifier';
COMMENT ON COLUMN ai_sessions.quest_id IS 'Optional link to quest that triggered this session';
COMMENT ON COLUMN ai_sessions.symbol IS 'Trading pair being analyzed in this session';
COMMENT ON COLUMN ai_sessions.status IS 'Session status: active, paused, completed, failed';
COMMENT ON COLUMN ai_sessions.state_data IS 'Full serialized session state as JSONB';
COMMENT ON COLUMN ai_sessions.created_at IS 'When the session was created';
COMMENT ON COLUMN ai_sessions.updated_at IS 'When the session was last modified';

-- Insert migration record
INSERT INTO system_settings (key, value, description)
VALUES ('migration_067_completed', 'true', 'Migration 067: Create ai_sessions table')
ON CONFLICT (key) DO NOTHING;

-- Insert migration log
INSERT INTO migration_log (id, filename, description, executed_at)
VALUES (67, '067_create_ai_sessions_table.sql', 'Create AI sessions table for state serialization', NOW())
ON CONFLICT (id) DO NOTHING;
