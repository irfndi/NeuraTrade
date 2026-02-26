-- Runtime persistence tables for autonomous quest engine.
-- Kept separate from legacy quests/autonomous_state tables to avoid schema collisions.

CREATE TABLE IF NOT EXISTS autonomous_quests (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT,
    type TEXT NOT NULL,
    cadence TEXT NOT NULL,
    cron_expr TEXT,
    status TEXT NOT NULL,
    prompt TEXT,
    target_count INTEGER DEFAULT 0,
    current_count INTEGER DEFAULT 0,
    checkpoint TEXT,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    last_executed_at TIMESTAMP,
    completed_at TIMESTAMP,
    last_error TEXT,
    metadata TEXT
);

CREATE TABLE IF NOT EXISTS autonomous_state_runtime (
    chat_id TEXT PRIMARY KEY,
    is_active BOOLEAN NOT NULL,
    started_at TIMESTAMP,
    paused_at TIMESTAMP,
    active_quests TEXT,
    updated_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_autonomous_quests_status
    ON autonomous_quests(status);

CREATE INDEX IF NOT EXISTS idx_autonomous_quests_type
    ON autonomous_quests(type);

CREATE INDEX IF NOT EXISTS idx_autonomous_state_runtime_updated_at
    ON autonomous_state_runtime(updated_at);
