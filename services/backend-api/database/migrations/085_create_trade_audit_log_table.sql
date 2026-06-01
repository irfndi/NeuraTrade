-- Migration 085: Create trade_audit_log table for append-only order placement audit trail.
CREATE TABLE IF NOT EXISTS trade_audit_log (
    audit_id TEXT PRIMARY KEY,
    chat_id TEXT,
    intent_id TEXT,
    symbol TEXT NOT NULL,
    exchange TEXT NOT NULL,
    side TEXT NOT NULL,
    order_type TEXT NOT NULL,
    amount TEXT NOT NULL,
    price TEXT,
    stop_loss TEXT,
    take_profit TEXT,
    client_order_id TEXT,
    ai_reasoning TEXT,
    ai_confidence TEXT,
    pre_trade_safety_status TEXT,
    order_id TEXT,
    order_status TEXT,
    error_message TEXT,
    created_at TIMESTAMP NOT NULL,
    indexed_at TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_trade_audit_log_chat_id ON trade_audit_log(chat_id);
CREATE INDEX IF NOT EXISTS idx_trade_audit_log_intent_id ON trade_audit_log(intent_id);
CREATE INDEX IF NOT EXISTS idx_trade_audit_log_symbol ON trade_audit_log(symbol);
CREATE INDEX IF NOT EXISTS idx_trade_audit_log_created_at ON trade_audit_log(created_at DESC);
