-- Migration 083: persistent kill switch state across server restarts.
-- Without this, restarting the server resets kill_switch.engaged = false,
-- which can re-enable live trading when it was explicitly halted.
CREATE TABLE IF NOT EXISTS risk_kill_switch_state (
    singleton          BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    engaged            BOOLEAN NOT NULL DEFAULT FALSE,
    engaged_at         BIGINT,
    engaged_by         TEXT,
    reason             TEXT,
    cancel_orders      BOOLEAN NOT NULL DEFAULT TRUE,
    last_updated_at    BIGINT NOT NULL,
    CONSTRAINT kill_switch_singleton CHECK (singleton = TRUE)
);
