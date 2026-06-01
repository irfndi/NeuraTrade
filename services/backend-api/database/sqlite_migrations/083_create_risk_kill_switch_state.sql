-- Migration 083: persistent kill switch state across server restarts.
CREATE TABLE IF NOT EXISTS risk_kill_switch_state (
    singleton          INTEGER PRIMARY KEY DEFAULT 1 CHECK (singleton = 1),
    engaged            INTEGER NOT NULL DEFAULT 0,
    engaged_at         INTEGER,
    engaged_by         TEXT,
    reason             TEXT,
    cancel_orders      INTEGER NOT NULL DEFAULT 1,
    last_updated_at    INTEGER NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_risk_kill_switch_singleton ON risk_kill_switch_state(singleton);
