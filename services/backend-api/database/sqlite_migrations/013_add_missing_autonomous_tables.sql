-- Add critical missing tables from Postgres migrations
-- These tables are needed for autonomous runtime and exchange reliability

-- Trading mode states (from PG 070)
CREATE TABLE IF NOT EXISTS trading_mode_states (
    chat_id TEXT PRIMARY KEY,
    mode TEXT NOT NULL DEFAULT 'paper' CHECK (mode IN ('paper', 'live')),
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    source TEXT DEFAULT 'telegram',
    dry_run TEXT DEFAULT '0',
    confirmation_count INTEGER DEFAULT 0
);

-- Autonomous quests (from PG 071)
CREATE TABLE IF NOT EXISTS autonomous_quests (
    id TEXT PRIMARY KEY,
    chat_id TEXT NOT NULL,
    quest_type TEXT NOT NULL,
    definition TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    execution_mode TEXT DEFAULT 'paper',
    dry_run INTEGER DEFAULT 1,
    paper_trading INTEGER DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Autonomous state runtime (from PG 071)
CREATE TABLE IF NOT EXISTS autonomous_state_runtime (
    chat_id TEXT PRIMARY KEY,
    enabled INTEGER NOT NULL DEFAULT 0,
    last_tick DATETIME,
    active_quest_ids TEXT DEFAULT '[]',
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Exchange reliability metrics (from PG 056)
CREATE TABLE IF NOT EXISTS exchange_reliability_metrics (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    exchange TEXT NOT NULL,
    uptime_percent_24h DECIMAL(5,2) DEFAULT 100.0,
    uptime_percent_7d DECIMAL(5,2) DEFAULT 100.0,
    avg_latency_ms INTEGER DEFAULT 0,
    failure_count_24h INTEGER DEFAULT 0,
    failure_count_7d INTEGER DEFAULT 0,
    last_failure DATETIME,
    risk_score DECIMAL(5,2) DEFAULT 0.0,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(exchange)
);

-- Autonomous rollback events (from PG 073)
CREATE TABLE IF NOT EXISTS autonomous_rollback_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    chat_id TEXT NOT NULL,
    quest_id TEXT,
    event_type TEXT NOT NULL,
    details TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
