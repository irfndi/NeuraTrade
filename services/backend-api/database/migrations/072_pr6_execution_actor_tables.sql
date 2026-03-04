-- Migration: PR6 - ExecutionActor Idempotency and Audit Trail Tables
-- Creates tables for order intent tracking and audit logging

-- Order intents table for idempotency tracking
CREATE TABLE IF NOT EXISTS order_intents (
    intent_id TEXT PRIMARY KEY,
    client_order_id TEXT UNIQUE NOT NULL,
    exchange_order_id TEXT,
    status TEXT NOT NULL CHECK (status IN ('pending', 'open', 'filled', 'partial', 'cancelled', 'rejected')),
    exchange TEXT NOT NULL,
    symbol TEXT NOT NULL,
    side TEXT NOT NULL CHECK (side IN ('buy', 'sell')),
    order_type TEXT NOT NULL CHECK (order_type IN ('market', 'limit')),
    amount TEXT NOT NULL,
    price TEXT,
    stop_price TEXT,
    take_profit TEXT,
    reduce_only BOOLEAN DEFAULT FALSE,
    post_only BOOLEAN DEFAULT FALSE,
    filled_amount TEXT DEFAULT '0',
    fill_price TEXT DEFAULT '0',
    reject_reason TEXT,
    attempt_count INTEGER DEFAULT 1,
    submitted_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    strategy_id TEXT,
    signal_id TEXT,
    metadata TEXT
);

-- Indexes for efficient lookups
CREATE INDEX IF NOT EXISTS idx_order_intents_client_id ON order_intents(client_order_id);
CREATE INDEX IF NOT EXISTS idx_order_intents_exchange_id ON order_intents(exchange_order_id);
CREATE INDEX IF NOT EXISTS idx_order_intents_status ON order_intents(status);
CREATE INDEX IF NOT EXISTS idx_order_intents_submitted ON order_intents(submitted_at);
CREATE INDEX IF NOT EXISTS idx_order_intents_exchange ON order_intents(exchange);
CREATE INDEX IF NOT EXISTS idx_order_intents_symbol ON order_intents(symbol);
CREATE INDEX IF NOT EXISTS idx_order_intents_strategy ON order_intents(strategy_id);

-- Order audit log table for tracking order lifecycle events
CREATE TABLE IF NOT EXISTS order_audit_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id TEXT UNIQUE NOT NULL,
    intent_id TEXT NOT NULL,
    client_order_id TEXT NOT NULL,
    exchange_order_id TEXT,
    event_type TEXT NOT NULL CHECK (event_type IN ('submitted', 'placed', 'filled', 'rejected', 'cancelled', 'cancel_failed', 'validation_failed')),
    exchange TEXT NOT NULL,
    symbol TEXT NOT NULL,
    side TEXT,
    amount TEXT,
    price TEXT,
    filled_amount TEXT,
    fill_price TEXT,
    reason TEXT,
    metadata TEXT,
    timestamp TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    hash_chain TEXT
);

-- Indexes for audit log queries
CREATE INDEX IF NOT EXISTS idx_audit_intent ON order_audit_log(intent_id);
CREATE INDEX IF NOT EXISTS idx_audit_client_id ON order_audit_log(client_order_id);
CREATE INDEX IF NOT EXISTS idx_audit_exchange_id ON order_audit_log(exchange_order_id);
CREATE INDEX IF NOT EXISTS idx_audit_event_id ON order_audit_log(event_id);
CREATE INDEX IF NOT EXISTS idx_audit_timestamp ON order_audit_log(timestamp);
CREATE INDEX IF NOT EXISTS idx_audit_type ON order_audit_log(event_type);
CREATE INDEX IF NOT EXISTS idx_audit_exchange ON order_audit_log(exchange);

-- View for active orders (non-terminal states)
CREATE VIEW IF NOT EXISTS v_active_orders AS
SELECT 
    intent_id,
    client_order_id,
    exchange_order_id,
    status,
    exchange,
    symbol,
    side,
    order_type,
    amount,
    filled_amount,
    submitted_at,
    updated_at,
    attempt_count
FROM order_intents
WHERE status NOT IN ('filled', 'cancelled', 'rejected');

-- View for order lifecycle summary
CREATE VIEW IF NOT EXISTS v_order_lifecycle AS
SELECT 
    oi.intent_id,
    oi.client_order_id,
    oi.exchange_order_id,
    oi.exchange,
    oi.symbol,
    oi.side,
    oi.status,
    oi.amount,
    oi.filled_amount,
    oi.submitted_at,
    oi.updated_at,
    (SELECT COUNT(*) FROM order_audit_log WHERE intent_id = oi.intent_id) as event_count,
    (SELECT timestamp FROM order_audit_log WHERE intent_id = oi.intent_id AND event_type = 'filled' LIMIT 1) as filled_at,
    (SELECT timestamp FROM order_audit_log WHERE intent_id = oi.intent_id AND event_type = 'rejected' LIMIT 1) as rejected_at,
    (SELECT timestamp FROM order_audit_log WHERE intent_id = oi.intent_id AND event_type = 'cancelled' LIMIT 1) as cancelled_at
FROM order_intents oi;
