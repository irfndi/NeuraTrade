PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS shadow_decisions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    variant_id TEXT NOT NULL,
    live_decision_id INTEGER,
    symbol TEXT NOT NULL,
    action TEXT NOT NULL CHECK (action IN ('buy', 'sell', 'hold')),
    confidence REAL,
    size_pct REAL,
    entry_price NUMERIC,
    stop_loss NUMERIC,
    take_profit NUMERIC,
    gate_allowed BOOLEAN NOT NULL DEFAULT 0,
    gate_reason TEXT,
    gate_code TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_shadow_decisions_variant_created_at
    ON shadow_decisions (variant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_shadow_decisions_live_decision_id
    ON shadow_decisions (live_decision_id);
CREATE INDEX IF NOT EXISTS idx_shadow_decisions_symbol_created_at
    ON shadow_decisions (symbol, created_at DESC);

CREATE TABLE IF NOT EXISTS shadow_outcomes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    shadow_decision_id INTEGER NOT NULL REFERENCES shadow_decisions(id) ON DELETE CASCADE,
    exit_price NUMERIC,
    realized_pnl NUMERIC,
    max_favor NUMERIC,
    max_adverse NUMERIC,
    closed_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_shadow_outcomes_shadow_decision_id_unique
    ON shadow_outcomes (shadow_decision_id);
CREATE INDEX IF NOT EXISTS idx_shadow_outcomes_closed_at
    ON shadow_outcomes (closed_at DESC);

CREATE TABLE IF NOT EXISTS live_shadow_comparisons (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    variant_id TEXT NOT NULL,
    comparison_window_start DATETIME,
    comparison_window_end DATETIME,
    live_pnl NUMERIC NOT NULL DEFAULT 0,
    shadow_pnl NUMERIC NOT NULL DEFAULT 0,
    pnl_divergence NUMERIC NOT NULL DEFAULT 0,
    live_win_rate REAL NOT NULL DEFAULT 0,
    shadow_win_rate REAL NOT NULL DEFAULT 0,
    live_trade_count INTEGER NOT NULL DEFAULT 0,
    shadow_trade_count INTEGER NOT NULL DEFAULT 0,
    live_rejection_count INTEGER NOT NULL DEFAULT 0,
    shadow_rejection_count INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_live_shadow_comparisons_variant_created_at
    ON live_shadow_comparisons (variant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_live_shadow_comparisons_window
    ON live_shadow_comparisons (comparison_window_start, comparison_window_end);
