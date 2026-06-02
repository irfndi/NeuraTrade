-- Migration 088: Create trade_audit_log table for append-only trade audit trail.
-- This provides a persistent, queryable record of all order placement attempts,
-- their pre-trade risk context, AI reasoning, exchange responses, and outcomes.
--
-- Note on append-only enforcement:
-- SQLite does not support CREATE RULE or column-level REVOKE, so we use
-- BEFORE UPDATE/DELETE triggers to reject modifications. Application-level
-- enforcement is also maintained: all writes go through TradeAuditLogger.LogTrade
-- which only issues INSERT statements.
--
-- Retention policy:
-- Hot (main table): 90 days. Cold (archive): 1 year.
-- TODO: Implement archive job that moves rows older than 90 days to
--       trade_audit_log_archive table, then purges from main table.
--       See services/backend-api/internal/services/trade_audit_logger.go

PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS trade_audit_log (
    audit_id          TEXT    PRIMARY KEY,
    chat_id           TEXT,
    user_id           TEXT,
    symbol            TEXT    NOT NULL,
    side              TEXT    NOT NULL,
    order_type        TEXT    NOT NULL,
    size              TEXT    NOT NULL,  -- decimal string (shopspring/decimal)
    requested_price   TEXT,              -- decimal string, NULL for market orders
    signal_id         TEXT,              -- cross-reference to signal provenance
    ai_reasoning_snapshot  TEXT,         -- JSON: AI decision chain
    pre_trade_risk_snapshot TEXT,        -- JSON: pre-trade risk check result
    order_request     TEXT,              -- JSON: order payload sent to exchange
    order_response    TEXT,              -- JSON: raw exchange response
    position_state    TEXT,              -- JSON: post-order position snapshot
    outcome           TEXT,              -- 'pending', 'placed', 'rejected', 'error', 'filled', 'cancelled'
    realized_pnl      TEXT,              -- decimal string, NULL until closed
    holding_seconds   INTEGER,           -- NULL until closed
    exchange          TEXT,
    intent_id         TEXT,
    amount            TEXT,
    price             TEXT,
    stop_loss         TEXT,
    take_profit       TEXT,
    client_order_id   TEXT,
    ai_reasoning      TEXT,
    ai_confidence     TEXT,
    pre_trade_safety_status TEXT,
    order_id          TEXT,
    order_status      TEXT,
    error_message     TEXT,
    created_at        TIMESTAMP NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    indexed_at        TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_trade_audit_log_created_at ON trade_audit_log(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_trade_audit_log_chat_id ON trade_audit_log(chat_id);
CREATE INDEX IF NOT EXISTS idx_trade_audit_log_symbol ON trade_audit_log(symbol);
CREATE INDEX IF NOT EXISTS idx_trade_audit_log_audit_id ON trade_audit_log(audit_id);

-- Append-only enforcement: reject UPDATE and DELETE on trade_audit_log
CREATE TRIGGER IF NOT EXISTS trg_trade_audit_log_prevent_update
BEFORE UPDATE ON trade_audit_log
BEGIN
    SELECT RAISE(ABORT, 'UPDATE on trade_audit_log is forbidden: audit log is append-only');
END;

CREATE TRIGGER IF NOT EXISTS trg_trade_audit_log_prevent_delete
BEFORE DELETE ON trade_audit_log
BEGIN
    SELECT RAISE(ABORT, 'DELETE on trade_audit_log is forbidden: audit log is append-only');
END;
