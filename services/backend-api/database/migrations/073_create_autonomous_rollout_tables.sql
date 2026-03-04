-- Persistent rollout state and rollback events for PR10 autonomous scalping.

CREATE TABLE IF NOT EXISTS autonomous_rollout_states (
    strategy_id TEXT PRIMARY KEY,
    chat_id TEXT NOT NULL DEFAULT '',
    current_stage TEXT NOT NULL,
    status TEXT NOT NULL,
    entered_at TIMESTAMP NOT NULL,
    payload TEXT NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_autonomy_rollout_chat_id
    ON autonomous_rollout_states(chat_id);

CREATE INDEX IF NOT EXISTS idx_autonomy_rollout_stage_status
    ON autonomous_rollout_states(current_stage, status);

CREATE TABLE IF NOT EXISTS autonomous_rollback_events (
    id TEXT PRIMARY KEY,
    strategy_id TEXT NOT NULL,
    chat_id TEXT NOT NULL DEFAULT '',
    trigger TEXT NOT NULL,
    from_stage TEXT,
    to_stage TEXT,
    reason TEXT,
    payload TEXT NOT NULL,
    occurred_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_autonomy_rollback_strategy_time
    ON autonomous_rollback_events(strategy_id, occurred_at DESC);

CREATE INDEX IF NOT EXISTS idx_autonomy_rollback_chat_time
    ON autonomous_rollback_events(chat_id, occurred_at DESC);
